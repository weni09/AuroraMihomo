package netcheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TProxy 规则的开机持久化：把面板下发的规则集与策略路由落盘成宿主
// 开机链路可自动加载的文件，宿主重启后无需手动重新启用。
//
// 支持的 init 系统（见 DetectInitSystem）：
//   - systemd（Debian/Ubuntu）：写 /etc/aurora-tproxy.nft +
//     /etc/aurora-tproxy.sh + /etc/systemd/system/aurora-tproxy.service，
//     注册 systemctl enable；
//   - openrc（Alpine）：写 /etc/aurora-tproxy.nft + /etc/aurora-tproxy.sh +
//     /etc/init.d/aurora-tproxy，注册 rc-update add ... default。
//
// 两后端共用同一份 shell 脚本（/etc/aurora-tproxy.sh start|stop）承载命令序列
// （策略路由 + nft -f + 自定义 iptables 规则 + 拆除），init/unit 只是薄封装：
// 命令序列只存一份，避免 systemd 的 ExecStart 引号解析与 OpenRC 函数体
// 双份实现漂移。
//
// 恢复的是「上次确认过的参数快照」：Write 只在规则已确认生效、且配置未变
// 时由面板调用（见 TransparentService.syncBootPersist），因此开机恢复的
// 规则与用户确认过的那套完全一致，不涉及新参数，也就不与「规则变更须经
// 90 秒确认窗口」的设计原则冲突。
//
// 只支持 Linux + root（写入 /etc 需要）；非 Linux 或不具备写权限时调用方
// 不构造本组件。写文件/注册服务失败只记录日志、不影响运行时规则下发——
// 持久化是增强能力，规则是否生效仍以 nft 实时下发为准。

const (
	// bootPersistNFTRel 持久化 nft 规则文件的相对路径（相对 Root）
	bootPersistNFTRel = "etc/aurora-tproxy.nft"
	// bootPersistShellRel 承载命令序列的通用 shell 脚本路径
	bootPersistShellRel = "etc/aurora-tproxy.sh"
	// bootPersistInitRel OpenRC init 脚本的相对路径
	bootPersistInitRel = "etc/init.d/aurora-tproxy"
	// bootPersistUnitRel systemd unit 文件的相对路径
	bootPersistUnitRel = "etc/systemd/system/aurora-tproxy.service"
	// bootPersistServiceName 服务名（init 脚本/unit 文件名与注册名）
	bootPersistServiceName = "aurora-tproxy"
)

// BootPersist 负责把 TProxy 规则集写入宿主开机链路。
type BootPersist struct {
	// Root 宿主根目录；生产为 "/"，测试注入临时目录
	Root string
	// Init 目标 init 系统（systemd / openrc / none），构造时注入
	Init string
	// Runner 执行 systemctl / rc-update 等命令，测试可替换
	Runner CommandRunner
	// Logf 记录写文件/注册服务的结果；nil 时静默
	Logf func(format string, args ...interface{})
}

func (b *BootPersist) logf(format string, args ...interface{}) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

// nftPath / shellPath / initPath / unitPath 返回落盘文件的绝对路径。
func (b *BootPersist) nftPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistNFTRel))
}

func (b *BootPersist) shellPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistShellRel))
}

func (b *BootPersist) initPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistInitRel))
}

func (b *BootPersist) unitPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistUnitRel))
}

// lookPathFn 可注入便于单测模拟系统环境（Windows 开发机上无 systemctl）。
var lookPathFn = exec.LookPath

// DetectInitSystem 返回宿主当前的 init 系统（systemd / openrc / none）。
//
// 判据与 backend/internal/adguard/svc_controller.go 的 detectServiceManager、
// scripts/install.sh 的 init_system 保持一致：systemctl 存在 → systemd；
// rc-update + rc-service 都存在 → openrc；否则 none（含 Windows/macOS）。
// 刻意不查 /run/systemd/system：chroot 与容器里该目录可能缺失，且
// 三处判据不一致会让「Go 判定 none、shell 判定 systemd」的部署对不上。
func DetectInitSystem() string {
	if _, err := lookPathFn("systemctl"); err == nil {
		return "systemd"
	}
	if _, err := lookPathFn("rc-update"); err == nil {
		if _, err := lookPathFn("rc-service"); err == nil {
			return "openrc"
		}
	}
	return "none"
}

// StripNFTPrefix 去掉 BuildNFTRules 开头的幂等头（add/delete table 两行）。
//
// 运行时 `nft -f -` 每批都先删表再建，保证重复下发幂等；而持久化文件在
// 开机时由 init/unit 加载，若带 delete 会在已加载了该表（如面板在重启前已
// 下发过、表随内核存在到开机服务执行）时先删后建——行为虽然无碍，但更
// 干净的持久化文件应只保留表定义本身。头两行内容固定，其前是注释行，
// 因此先跳过注释再剥离：
//
//	# AuroraMihomo ...
//	add table inet aurora_tproxy
//	delete table inet aurora_tproxy
func StripNFTPrefix(rules string) string {
	lines := strings.Split(rules, "\n")
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "#") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "add table ") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "delete table ") {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// Write 把当前规则集、命令序列与开机服务落盘并注册开机自启。
//
// 幂等：文件整体重写（写文件而非追加，避免重复执行堆积），init/unit 存在
// 则覆盖，systemctl enable / rc-update add 重复执行是空操作。任一步失败
// 返回错误，由调用方决定降级（记日志不阻断运行时规则）。
func (b *BootPersist) Write(ctx context.Context, p TProxyParams, customRules []string) error {
	rules, err := BuildNFTRules(p)
	if err != nil {
		return err
	}

	// 1. 规则表文件（两后端共用）
	nftPath := b.nftPath()
	if err := os.MkdirAll(filepath.Dir(nftPath), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	var bld strings.Builder
	bld.WriteString("# 由 AuroraMihomo 自动生成：透明代理（TProxy）规则，供开机加载。\n")
	bld.WriteString("# 面板会在规则变更/关闭时自动重写或删除本文件；手工修改会在下次\n")
	bld.WriteString("# 同步时被覆盖，请改「系统设置 · 透明代理」或自定义规则。\n")
	bld.WriteString("\n")
	bld.WriteString(strings.TrimRight(StripNFTPrefix(rules), "\n"))
	bld.WriteString("\n")
	if err := os.WriteFile(nftPath, []byte(bld.String()), 0o644); err != nil {
		return fmt.Errorf("写入 nft 规则文件失败: %w", err)
	}

	// 2. 命令序列脚本（systemd 与 OpenRC 共用，命令只存一份）
	shellPath := b.shellPath()
	if err := os.MkdirAll(filepath.Dir(shellPath), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(shellPath, []byte(renderBootRules(p, customRules)), 0o755); err != nil {
		return fmt.Errorf("写入命令序列脚本失败: %w", err)
	}

	// 3. 按 init 系统写服务描述并注册自启
	switch b.Init {
	case "systemd":
		unitPath := b.unitPath()
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			return fmt.Errorf("创建 systemd 目录失败: %w", err)
		}
		if err := os.WriteFile(unitPath, []byte(renderBootUnit()), 0o644); err != nil {
			return fmt.Errorf("写入 systemd unit 失败: %w", err)
		}
		if err := b.registerBootService(ctx); err != nil {
			// 注册失败不删文件：unit 已落盘，仅差 enable，留给下次同步再试
			b.logf("注册 %s 开机自启失败: %v", bootPersistServiceName, err)
		}
	case "openrc":
		initPath := b.initPath()
		if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
			return fmt.Errorf("创建 init.d 目录失败: %w", err)
		}
		if err := os.WriteFile(initPath, []byte(renderBootInit()), 0o755); err != nil {
			return fmt.Errorf("写入开机服务脚本失败: %w", err)
		}
		if err := b.registerBootService(ctx); err != nil {
			b.logf("注册 %s 开机自启失败: %v", bootPersistServiceName, err)
		}
	}
	b.logf("透明代理规则已持久化到开机链路（init=%s）: %s", b.Init, nftPath)
	return nil
}

// registerBootService 按 init 系统注册开机自启。
func (b *BootPersist) registerBootService(ctx context.Context) error {
	if b.Runner == nil {
		return nil
	}
	switch b.Init {
	case "systemd":
		if _, err := b.Runner.Run(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		if _, err := b.Runner.Run(ctx, "systemctl", "enable", bootPersistServiceName); err != nil {
			return fmt.Errorf("systemctl enable %s: %w", bootPersistServiceName, err)
		}
	case "openrc":
		if _, err := b.Runner.Run(ctx, "rc-update", "add", bootPersistServiceName, "default"); err != nil {
			return fmt.Errorf("rc-update add %s default: %w", bootPersistServiceName, err)
		}
	}
	return nil
}

// Remove 移除开机持久化并注销服务。
//
// 幂等：文件不存在、服务未注册时静默成功。被 disable() / 回落关闭 / 切 TUN
// 时调用，避免「面板已关闭但重启后规则复活、流量被引向已无人监听的端口」。
func (b *BootPersist) Remove(ctx context.Context) error {
	if b.Runner != nil {
		switch b.Init {
		case "systemd":
			_, _ = b.Runner.Run(ctx, "systemctl", "disable", bootPersistServiceName)
		case "openrc":
			_, _ = b.Runner.Run(ctx, "rc-update", "delete", bootPersistServiceName)
		}
	}
	removed := false
	removeIfExists := func(path string, what string) error {
		err := os.Remove(path)
		if err == nil {
			removed = true
			return nil
		}
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("删除%s失败: %w", what, err)
	}
	// 按 init 系统删服务描述文件；.sh 与 .nft 两后端共用
	desc := b.initPath()
	if b.Init == "systemd" {
		desc = b.unitPath()
	}
	if err := removeIfExists(desc, "开机服务文件"); err != nil {
		return err
	}
	if err := removeIfExists(b.shellPath(), "命令序列脚本"); err != nil {
		return err
	}
	if err := removeIfExists(b.nftPath(), "nft 规则文件"); err != nil {
		return err
	}
	if b.Runner != nil && b.Init == "systemd" {
		// unit 文件删除后需重载，让 systemd 忘掉它（enable 时已建 symlink）
		_, _ = b.Runner.Run(ctx, "systemctl", "daemon-reload")
	}
	if removed {
		b.logf("已移除透明代理开机持久化（%s）", bootPersistServiceName)
	}
	return nil
}

// Present 报告服务描述文件（unit 或 init 脚本）是否已落盘。
//
// 供 ReconcileState 在「规则仍生效但持久化文件缺失」（旧版本升级上来、
// 或首次在已启用状态下启动新版面板）时补写，无需用户重新启用。
func (b *BootPersist) Present() bool {
	path := b.initPath()
	if b.Init == "systemd" {
		path = b.unitPath()
	}
	_, err := os.Stat(path)
	return err == nil
}

// renderBootRules 渲染承载命令序列的通用 shell 脚本。
//
// start() 先幂等清理再建立：避免 ip rule 重复叠加、nft 表已存在时
// `nft -f` 静默失败。顺序仍是「路由 → 表 → 自定义规则」。
// stop() 循环删除全部同 mark/table 的 ip rule（del 一次只删一条）。
// systemd unit 与 OpenRC init 都调它，命令只存这一份。
func renderBootRules(p TProxyParams, customRules []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# 由 AuroraMihomo 自动生成：开机恢复/拆除透明代理（TProxy）规则。\n")
	b.WriteString("# 被 systemd unit 与 OpenRC init 脚本共同调用；请勿手工编辑。\n")
	b.WriteString("AURORA_NFT=\"/etc/aurora-tproxy.nft\"\n")
	fmt.Fprintf(&b, "AURORA_FWMARK=%d\n", FirewallMark)
	fmt.Fprintf(&b, "AURORA_TABLE=%d\n", RouteTable)
	b.WriteString("\n")
	// purge：Linux 允许多条相同 selector 的 ip rule（priority 不同）；
	// `ip rule del` 一次只删一条，必须循环直到没有匹配。
	b.WriteString("purge() {\n")
	b.WriteString("\twhile ip rule del fwmark \"$AURORA_FWMARK\" table \"$AURORA_TABLE\" 2>/dev/null; do :; done\n")
	b.WriteString("\twhile ip -6 rule del fwmark \"$AURORA_FWMARK\" table \"$AURORA_TABLE\" 2>/dev/null; do :; done\n")
	b.WriteString("\tip route flush table \"$AURORA_TABLE\" 2>/dev/null || true\n")
	b.WriteString("\tip -6 route flush table \"$AURORA_TABLE\" 2>/dev/null || true\n")
	fmt.Fprintf(&b, "\tnft delete table inet %s 2>/dev/null || true\n", NFTTableName)
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("start() {\n")
	b.WriteString("\t# 先清干净再装：防重复 start / 面板 Apply 叠加出多条 rule\n")
	b.WriteString("\tpurge\n")
	b.WriteString("\t# 策略路由必须在规则之前（同 Apply 的顺序），否则打了标记的包无路可走\n")
	for _, c := range PolicyRouteCommands(p.EnableIPv6) {
		fmt.Fprintf(&b, "\t%s || true\n", strings.Join(c, " "))
	}
	b.WriteString("\tnft -f \"$AURORA_NFT\" || true\n")
	// 自定义规则与内置 nft 是两条通道，开机同样要恢复
	for _, r := range customRules {
		fmt.Fprintf(&b, "\tsh -c %q || true\n", r)
	}
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("stop() {\n")
	b.WriteString("\tpurge\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("case \"$1\" in\n")
	b.WriteString("\tstart) start ;;\n")
	b.WriteString("\tstop) stop ;;\n")
	b.WriteString("\t*) echo \"用法: $0 start|stop\" >&2; exit 1 ;;\n")
	b.WriteString("esac\n")
	return b.String()
}

// renderBootInit 渲染 OpenRC init 脚本（薄封装，命令在 aurora-tproxy.sh）。
//
// before auroramihomo：必须先于面板启动。否则面板 ReconcileState 在规则
// 恢复前探测不到 aurora_tproxy 表，会把已确认的 TProxy 回落为关闭。
func renderBootInit() string {
	return `#!/sbin/openrc-run
# 由 AuroraMihomo 自动生成：开机恢复透明代理（TProxy）规则。
# 面板会在规则变更/关闭时自动重写或删除本脚本；请勿手工编辑。
name="aurora-tproxy"
description="AuroraMihomo TProxy 防火墙规则恢复"
AURORA_SCRIPT="/etc/aurora-tproxy.sh"

depend() {
	need net
	before auroramihomo
}

start() {
	ebegin "Restoring AuroraMihomo TProxy rules"
	/bin/sh "$AURORA_SCRIPT" start
	eend $?
}

stop() {
	ebegin "Removing AuroraMihomo TProxy rules"
	/bin/sh "$AURORA_SCRIPT" stop
	eend $?
}
`
}

// renderBootUnit 渲染 systemd oneshot unit（命令在 aurora-tproxy.sh）。
//
// Type=oneshot + RemainAfterExit：start 跑完仍保持 active，关机/stop 才会
// 触发 ExecStop。After=network-online.target：策略路由需要网卡就绪。
// Before=auroramihomo.service：先建规则再起面板，避免 ReconcileState
// 在规则恢复前把 TProxy 误判为失效并回落关闭。
func renderBootUnit() string {
	return `[Unit]
Description=AuroraMihomo TProxy firewall rules
After=network-online.target
Wants=network-online.target
Before=auroramihomo.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh /etc/aurora-tproxy.sh start
ExecStop=/bin/sh /etc/aurora-tproxy.sh stop

[Install]
WantedBy=multi-user.target
`
}

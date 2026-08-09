package netcheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TProxy 规则的开机持久化：把面板下发的规则集与策略路由落盘成宿主
// 开机链路可自动加载的文件，宿主重启后无需手动重新启用。
//
// 载体（OpenRC / Alpine 为目标平台）：
//   - <Root>/etc/aurora-tproxy.nft —— 当前 TProxy 规则的纯表定义
//     （去掉 BuildNFTRules 开头的 add/delete table 幂等头），由 init 脚本
//     在开机时 `nft -f` 加载；
//   - <Root>/etc/init.d/aurora-tproxy —— OpenRC 服务：start() 加载 nft 规则、
//     重建策略路由、追加自定义 iptables 规则；stop() 拆除全部。
//     注册进 default 运行级，开机自启。
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
	// bootPersistInitRel OpenRC init 脚本的相对路径
	bootPersistInitRel = "etc/init.d/aurora-tproxy"
	// bootPersistServiceName OpenRC 服务名（init 脚本文件名）
	bootPersistServiceName = "aurora-tproxy"
)

// BootPersist 负责把 TProxy 规则集写入宿主开机链路。
type BootPersist struct {
	// Root 宿主根目录；生产为 "/"，测试注入临时目录
	Root string
	// Runner 执行 rc-update / rc-service 等命令，测试可替换
	Runner CommandRunner
	// Logf 记录写文件/注册服务的结果；nil 时静默
	Logf func(format string, args ...interface{})
}

func (b *BootPersist) logf(format string, args ...interface{}) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

// nftPath / initPath 返回落盘文件的绝对路径。
func (b *BootPersist) nftPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistNFTRel))
}

func (b *BootPersist) initPath() string {
	return filepath.Join(b.Root, filepath.FromSlash(bootPersistInitRel))
}

// StripNFTPrefix 去掉 BuildNFTRules 开头的幂等头（add/delete table 两行）。
//
// 运行时 `nft -f -` 每批都先删表再建，保证重复下发幂等；而持久化文件在
// 开机时由 nftables 加载，若带 delete 会在已加载了该表（如面板在重启前已
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

// Write 把当前规则集与策略路由落盘并注册开机服务。
//
// 幂等：文件整体重写（写文件而非追加，避免重复执行堆积），init 脚本存在
// 则覆盖，rc-update add 重复执行是空操作。任一步失败返回错误，由调用方
// 决定降级（记日志不阻断运行时规则）。
func (b *BootPersist) Write(ctx context.Context, p TProxyParams, customRules []string) error {
	rules, err := BuildNFTRules(p)
	if err != nil {
		return err
	}
	persist := StripNFTPrefix(rules)

	nftPath := b.nftPath()
	if err := os.MkdirAll(filepath.Dir(nftPath), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	var bld strings.Builder
	bld.WriteString("# 由 AuroraMihomo 自动生成：透明代理（TProxy）规则，供开机加载。\n")
	bld.WriteString("# 面板会在规则变更/关闭时自动重写或删除本文件；手工修改会在下次\n")
	bld.WriteString("# 同步时被覆盖，请改「系统设置 · 透明代理」或自定义规则。\n")
	bld.WriteString("\n")
	bld.WriteString(strings.TrimRight(persist, "\n"))
	bld.WriteString("\n")
	if err := os.WriteFile(nftPath, []byte(bld.String()), 0o644); err != nil {
		return fmt.Errorf("写入 nft 规则文件失败: %w", err)
	}

	initContent := renderBootInit(p, customRules)
	initPath := b.initPath()
	if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
		return fmt.Errorf("创建 init.d 目录失败: %w", err)
	}
	if err := os.WriteFile(initPath, []byte(initContent), 0o755); err != nil {
		return fmt.Errorf("写入开机服务脚本失败: %w", err)
	}

	if b.Runner != nil {
		if _, err := b.Runner.Run(ctx, "rc-update", "add", bootPersistServiceName, "default"); err != nil {
			// rc-update 在脚本已注册时返回"already added"类信息，视为成功；
			// 失败不删文件——规则文件已落盘，仅差自启注册，留给下次同步再试。
			b.logf("注册 %s 开机自启失败: %v", bootPersistServiceName, err)
		}
	}
	b.logf("透明代理规则已持久化到开机链路: %s", nftPath)
	return nil
}

// Remove 移除开机持久化并注销服务。
//
// 幂等：文件不存在、服务未注册时静默成功。被 disable() / 回落关闭 / 切 TUN
// 时调用，避免「面板已关闭但重启后规则复活、流量被引向已无人监听的端口」。
func (b *BootPersist) Remove(ctx context.Context) error {
	if b.Runner != nil {
		_, _ = b.Runner.Run(ctx, "rc-update", "delete", bootPersistServiceName)
	}
	removed := false
	if err := os.Remove(b.initPath()); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("删除开机服务脚本失败: %w", err)
	}
	if err := os.Remove(b.nftPath()); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("删除 nft 规则文件失败: %w", err)
	}
	if removed {
		b.logf("已移除透明代理开机持久化（%s）", bootPersistServiceName)
	}
	return nil
}

// Present 报告开机服务脚本是否已落盘。
//
// 供 ReconcileState 在「规则仍生效但持久化文件缺失」（旧版本升级上来、
// 或首次在已启用状态下启动新版面板）时补写，无需用户重新启用。
func (b *BootPersist) Present() bool {
	_, err := os.Stat(b.initPath())
	return err == nil
}

// renderBootInit 渲染 OpenRC init 脚本内容。
//
// start() 顺序与 netcheck.Applier.Apply 保持一致：先策略路由再 nft 规则
// （反序会导致打标的包被丢弃），最后追加自定义 iptables 规则；每条命令
// 容忍失败（|| true），与 Apply 对「file exists」的幂等容忍对齐。
// stop() 按 Teardown 顺序拆除。nft 文件路径用 $AURORA_NFT 变量，脚本内
// 只引用一次，便于日后换路径。
func renderBootInit(p TProxyParams, customRules []string) string {
	var b strings.Builder
	b.WriteString("#!/sbin/openrc-run\n")
	b.WriteString("# 由 AuroraMihomo 自动生成：开机恢复透明代理（TProxy）规则。\n")
	b.WriteString("# 面板会在规则变更/关闭时自动重写或删除本脚本；请勿手工编辑。\n")
	b.WriteString("\n")
	b.WriteString("name=\"aurora-tproxy\"\n")
	b.WriteString("description=\"AuroraMihomo TProxy 防火墙规则恢复\"\n")
	b.WriteString("AURORA_NFT=\"/etc/aurora-tproxy.nft\"\n")
	b.WriteString("\n")
	b.WriteString("depend() {\n")
	b.WriteString("\tneed net\n")
	b.WriteString("\t# 与面板服务同序：面板重启时先恢复规则再等面板接入\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("start() {\n")
	b.WriteString("\tebegin \"Restoring AuroraMihomo TProxy rules\"\n")
	b.WriteString("\t# 策略路由必须在规则之前（同 Apply 的顺序），否则打了标记的包无路可走\n")
	for _, c := range PolicyRouteCommands(p.EnableIPv6) {
		fmt.Fprintf(&b, "\t%s || true\n", strings.Join(c, " "))
	}
	b.WriteString("\tnft -f \"$AURORA_NFT\" || true\n")
	// 自定义规则与内置 nft 是两条通道，开机同样要恢复，否则重启后自定义放行/分流失效
	for _, r := range customRules {
		fmt.Fprintf(&b, "\tsh -c %q || true\n", r)
	}
	b.WriteString("\teend $?\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("stop() {\n")
	b.WriteString("\tebegin \"Removing AuroraMihomo TProxy rules\"\n")
	for _, c := range PolicyRouteTeardownCommands() {
		fmt.Fprintf(&b, "\t%s || true\n", strings.Join(c, " "))
	}
	fmt.Fprintf(&b, "\tnft delete table inet %s || true\n", NFTTableName)
	b.WriteString("\teend $?\n")
	b.WriteString("}\n")
	return b.String()
}

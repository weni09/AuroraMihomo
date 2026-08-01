package netcheck

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Applier 把生成的规则真正落到系统上，并负责快照与拆除。
//
// 本文件不带 //go:build linux：它只是编排 nft / ip 命令，没有任何 Linux
// 专有系统调用，因此逻辑（顺序、幂等、失败回滚）可以在任意平台上用假
// Runner 测试。"是否允许下发"由调用方按 Detect() 的结论决定，
// 不靠编译标签来兜。
//
// 所有写操作都集中在这里，便于审计"面板到底改了宿主什么"。
// 三条硬规则：
//   - 只碰自己的 nft 表与自己建的策略路由表，绝不整体 flush
//   - 应用前先快照，拆除失败时用户至少能手工恢复
//   - 拆除必须幂等：可能本来就没应用成功，也可能被别处清掉了
type Applier struct {
	// SnapshotDir 快照落盘目录，通常是 <ConfigDir>/netbackup
	SnapshotDir string
	// Runner 执行外部命令，测试可替换
	Runner CommandRunner
	// Logf 记录执行过程，可为 nil
	Logf func(format string, args ...interface{})
	// UDPPortInUse 探测本机某 UDP 端口是否已有监听者，nil 时用默认实现。
	//
	// 存在的意义：DNS 规则会把 53 端口的查询重定向到 mihomo 的 DNS 端口，
	// 而那个端口是否真的有人监听，只有探测才知道。没人监听时下发规则
	// 等于把所有 DNS 查询导进黑洞（实测为 connection refused、
	// 整机域名解析不可用），比不劫持糟得多。
	//
	// 抽成字段是为了可测：真实实现要绑端口，测试里不能真去占用端口。
	UDPPortInUse func(port int) bool
}

// CommandRunner 抽象命令执行，便于测试与审计。
type CommandRunner interface {
	// Run 执行命令并返回合并输出
	Run(ctx context.Context, name string, args ...string) (string, error)
	// RunWithStdin 执行命令并写入 stdin（nft -f - 需要）
	RunWithStdin(ctx context.Context, stdin string, name string, args ...string) (string, error)
	// RunEnv 执行命令并追加环境变量。
	// Provisioner 用它设 DEBIAN_FRONTEND=noninteractive——不设的话 apt 在
	// 某些镜像里会尝试打开交互式配置界面，而这里没有 tty，进程会挂住。
	RunEnv(ctx context.Context, env []string, name string, args ...string) (string, error)
}

// execRunner 走真实 exec。
type execRunner struct{}

// NewExecRunner 返回执行真实系统命令的 Runner。
func NewExecRunner() CommandRunner { return execRunner{} }

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

func (execRunner) RunWithStdin(ctx context.Context, stdin, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (execRunner) RunEnv(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		// 追加而非替换：包管理器需要继承 PATH、HTTP(S)_PROXY 等
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *Applier) logf(format string, args ...interface{}) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// udpPortInUse 报告本机是否已有进程在监听该 UDP 端口。
//
// 用"尝试绑定"而不是解析 ss/netstat 的输出：不依赖外部命令是否存在、
// 不受输出格式差异影响。绑得上说明没人监听，随即释放。
//
// 同时探 0.0.0.0 与 127.0.0.1：mihomo 通常绑在 0.0.0.0（通配），
// 只探回环在某些内核/参数组合下可能绑得上通配端口而误判为空闲。
// 任一处绑不上就认为端口已被占用——宁可保守，误判为"有监听"只会放过一次
// 下发，而误判为"无监听"会拒绝一次合法的启用。
func defaultUDPPortInUse(port int) bool {
	for _, host := range []string{"0.0.0.0", "127.0.0.1"} {
		pc, err := net.ListenPacket("udp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return true
		}
		_ = pc.Close()
	}
	return false
}

func (a *Applier) udpPortInUse(port int) bool {
	if a.UDPPortInUse != nil {
		return a.UDPPortInUse(port)
	}
	return defaultUDPPortInUse(port)
}

// Snapshot 保存当前防火墙与路由状态。
//
// 采集失败不阻断流程（某些命令可能不存在），但会记录下来：
// 快照本身是"出问题后能手工恢复"的兜底，不是应用的前置条件。
// 返回快照目录路径。
func (a *Applier) Snapshot(ctx context.Context) (string, error) {
	if a.SnapshotDir == "" {
		return "", fmt.Errorf("未配置快照目录")
	}
	dir := filepath.Join(a.SnapshotDir, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("创建快照目录失败: %w", err)
	}

	// 文件名 -> 采集命令
	items := []struct {
		file string
		cmd  []string
	}{
		{"iptables.rules", []string{"iptables-save"}},
		{"ip6tables.rules", []string{"ip6tables-save"}},
		{"nft.ruleset", []string{"nft", "list", "ruleset"}},
		{"ip-rule.txt", []string{"ip", "rule", "show"}},
		{"ip-route-all.txt", []string{"ip", "route", "show", "table", "all"}},
		{"ip6-rule.txt", []string{"ip", "-6", "rule", "show"}},
	}
	for _, it := range items {
		out, err := a.Runner.Run(ctx, it.cmd[0], it.cmd[1:]...)
		if err != nil {
			// 命令不存在是常见情况（如未装 nftables），记录后继续
			a.logf("快照 %s 失败（继续）: %v", it.file, err)
			continue
		}
		path := filepath.Join(dir, it.file)
		if err := os.WriteFile(path, []byte(out), 0o640); err != nil {
			a.logf("写快照 %s 失败（继续）: %v", it.file, err)
		}
	}
	a.logf("防火墙快照已保存: %s", dir)
	return dir, nil
}

// Apply 下发 TProxy 规则与策略路由。
//
// 顺序是刻意的：先干跑校验规则语法，再建策略路由，最后才下发规则。
// 反过来的话，规则一生效而路由还没建好，打了标记的包会被丢弃——
// 那正是"应用瞬间断网"的典型成因。
//
// 任一步失败都会尝试拆除已完成的部分，避免留下半应用状态。
func (a *Applier) Apply(ctx context.Context, p TProxyParams) error {
	rules, err := BuildNFTRules(p)
	if err != nil {
		return err
	}

	// 0. DNS 端口必须真的有人监听，否则规则会把所有 DNS 查询导进黑洞。
	//
	// 放在最前面（连快照都还没做）：这是纯只读检查，失败时宿主一点没被碰过。
	//
	// 这道校验主要防的是"把 dns.listen 设成 53"：nft 语法上 `redirect to :53`
	// 完全合法，`nft --check` 也过得去，但 redirect 是把包**重定向到本机**，
	// 53 上无人监听就直接 connection refused——真机实测整机域名解析不可用，
	// 而清掉规则立刻恢复。mihomo 绑不上 53 的原因很常见：dns.enable 为 false、
	// 非 root 且缺 CAP_NET_BIND_SERVICE、或宿主已有 systemd-resolved/dnsmasq
	// 占着 53。这些情况 mihomo 只在自己日志里报一行，面板无从得知。
	//
	// 用 UDP 探测：DNS 以 UDP 为主，mihomo 的 dns.listen 同时监听 TCP 与 UDP，
	// 探一个即可。
	if p.DNSPort > 0 && !a.udpPortInUse(p.DNSPort) {
		return fmt.Errorf("未检测到有程序在 UDP 端口 %d 上监听，"+
			"下发规则会把所有 DNS 查询重定向到该端口而导致域名解析全部失败（未做任何改动）。"+
			"请检查 mihomo 的 dns.enable 是否开启、dns.listen 是否为该端口；"+
			"若填的是 53，注意它是特权端口且常被 systemd-resolved / dnsmasq 占用，"+
			"建议改用 1053 等高位端口", p.DNSPort)
	}

	// 1. 干跑校验。nft --check 只解析不应用，能挡住语法错误与
	//    内核不支持的表达式（如缺 nft_tproxy 模块）。
	if out, err := a.Runner.RunWithStdin(ctx, rules, "nft", "--check", "-f", "-"); err != nil {
		return fmt.Errorf("规则校验失败（未做任何改动）: %w: %s", err, strings.TrimSpace(out))
	}

	// 2. 策略路由。必须在规则之前，理由见函数头注释。
	for _, cmd := range PolicyRouteCommands(p.EnableIPv6) {
		if out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
			// 已存在（File exists）视为成功，使重复应用幂等
			if strings.Contains(strings.ToLower(out), "file exists") {
				continue
			}
			_ = a.Teardown(ctx)
			return fmt.Errorf("配置策略路由失败 %v: %w: %s", cmd, err, strings.TrimSpace(out))
		}
	}

	// 3. 下发规则
	if out, err := a.Runner.RunWithStdin(ctx, rules, "nft", "-f", "-"); err != nil {
		_ = a.Teardown(ctx)
		return fmt.Errorf("下发规则失败: %w: %s", err, strings.TrimSpace(out))
	}

	a.logf("透明代理规则已生效（tproxy=%d, 放行端口=%v）", p.TProxyPort, p.KeepPorts)
	return nil
}

// Teardown 拆除本项目下发的一切改动。
//
// 幂等且尽力而为：每一步失败都只记录不中断，因为拆除常发生在
// "已经出问题"的场景（自动回滚、异常恢复），此时任何一步卡住都会
// 让系统停在更糟的中间态。
//
// 不接受"当初有没有启用 v6"这类参数：v4 与 v6 一律清理，
// 理由见 PolicyRouteTeardownCommands 的注释。
func (a *Applier) Teardown(ctx context.Context) error {
	var firstErr error

	// 先删规则再撤路由：反序会短暂出现"规则在、路由没了"的黑洞
	if out, err := a.Runner.Run(ctx, NFTTeardownCommand()[0], NFTTeardownCommand()[1:]...); err != nil {
		// 表不存在说明本来就没应用，不算错误
		low := strings.ToLower(out)
		if !strings.Contains(low, "no such file") && !strings.Contains(low, "does not exist") {
			a.logf("删除 nft 表失败: %v: %s", err, strings.TrimSpace(out))
			firstErr = err
		}
	}

	for _, cmd := range PolicyRouteTeardownCommands() {
		if out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
			low := strings.ToLower(out)
			// 规则不存在同样属于"已经是目标状态"
			if !strings.Contains(low, "no such") && !strings.Contains(low, "cannot find") {
				a.logf("拆除策略路由失败 %v: %v: %s", cmd, err, strings.TrimSpace(out))
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	if firstErr == nil {
		a.logf("透明代理规则已拆除")
	}
	return firstErr
}

// RulesActive 探测本项目的 nft 表是否还存在于宿主上。
//
// 真机测试发现的问题：TProxy 规则与策略路由不持久化到宿主重启（见
// AuroraMihomo-Transparent-Proxy-Test-Report.md 第 6.3 节）——重启后
// nftables 状态被内核清空，但数据库里"已确认启用"的记录不会跟着变。
// 若启动时只看数据库，界面会一直显示"已开启"，而宿主上实际什么都没有，
// 用户没有任何信号能察觉这个不一致，只能自己碰一下开关才会发现。
//
// 这个方法只做只读探测，不做任何修改；由调用方（TransparentService）
// 决定探测结果与数据库记录不一致时如何处理。
func (a *Applier) RulesActive(ctx context.Context) (bool, error) {
	cmd := NFTRulesCheckCommand()
	out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...)
	if err == nil {
		return true, nil
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "no such file") || strings.Contains(low, "does not exist") {
		// 表不存在，这是"规则已失效"的正常信号，不算探测出错
		return false, nil
	}
	// 其它错误（如 nft 命令本身不可执行）无法判断真实状态，交给调用方
	// 决定——保守起见不应就此断言"规则不存在"，那可能是误诊
	return false, fmt.Errorf("探测防火墙规则状态失败: %w: %s", err, strings.TrimSpace(out))
}

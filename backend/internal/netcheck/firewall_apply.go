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
	// 短超时：探测只是绑一下端口，卡住说明内核/网络栈异常，不应拖死调用方。
	lc := net.ListenConfig{}
	for _, host := range []string{"0.0.0.0", "127.0.0.1"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pc, err := lc.ListenPacket(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(port)))
		cancel()
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

	// 失败回滚时必须同时覆盖「上一批已应用」与「本批目标」：
	// 只拆本批会漏掉改 A→B 时的旧 A；只拆上一批会漏掉本批已追加的几条。
	teardownRules := MergeCustomRuleLists(p.PreviousCustomRules, p.CustomRules)

	// 2. 策略路由。必须在规则之前，理由见函数头注释。
	// 先清掉同 mark/table 的旧 rule（可能因重复 Apply / 开机脚本叠加了多条），
	// 再 add，保证最终只有一套。用 shell while：`ip rule del` 一次只删一条。
	a.purgePolicyRules(ctx)
	for _, cmd := range PolicyRouteCommands(p.EnableIPv6) {
		if out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
			// 已存在（File exists）视为成功，使重复应用幂等
			if strings.Contains(strings.ToLower(out), "file exists") {
				continue
			}
			// 自定义规则一并拆：可能是上次应用残留（重复应用场景）
			_ = a.Teardown(ctx, teardownRules)
			return fmt.Errorf("配置策略路由失败 %v: %w: %s", cmd, err, strings.TrimSpace(out))
		}
	}

	// 3. 下发规则
	if out, err := a.Runner.RunWithStdin(ctx, rules, "nft", "-f", "-"); err != nil {
		_ = a.Teardown(ctx, teardownRules)
		return fmt.Errorf("下发规则失败: %w: %s", err, strings.TrimSpace(out))
	}

	// 4. 先拆上一批自定义规则，再追加本批。
	//
	// iptables -A 不幂等：重复 Apply 会叠规则；改 A→B 时若不拆 A，
	// A 会永久留在链里，而 Teardown 只按当前 DB 列表拆，孤儿永远清不掉。
	// 内置 nft 靠 delete table 重建天然幂等，自定义通道没有这层保护。
	// Previous 为空（首次启用）时 removeCustomRules 是空转。
	a.removeCustomRules(ctx, p.PreviousCustomRules)

	// 5. 用户自定义规则（iptables 语法），在内置规则生效后逐条追加。
	//
	// 用 sh -c 而不是拆词 exec：iptables 参数可能含引号（如
	// -m comment --comment "xxx"），拆词会把引号语义吃掉。
	// 规则由管理员自己书写，shell 执行不存在注入面。
	//
	// 任一条失败：整体回滚（拆内置 + 逆序拆旧批与本批），
	// 避免"内置生效但自定义没生效"的半应用状态让用户误判。
	if err := a.applyCustomRules(ctx, p.CustomRules); err != nil {
		_ = a.Teardown(ctx, teardownRules)
		return err
	}

	a.logf("透明代理规则已生效（tproxy=%d, 放行端口=%v）", p.TProxyPort, p.KeepPorts)
	return nil
}

// applyCustomRules 逐条执行自定义防火墙规则。
//
// 失败时返回带行号与命令输出的错误，由调用方决定是否整体回滚
// （Apply 里会拆内置并逆序拆已执行的自定义规则）。
func (a *Applier) applyCustomRules(ctx context.Context, rules []string) error {
	for i, rule := range rules {
		if out, err := a.Runner.Run(ctx, "sh", "-c", rule); err != nil {
			return fmt.Errorf("自定义防火墙规则第 %d 行执行失败: %w: %s", i+1, err, strings.TrimSpace(out))
		}
	}
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
// customRules 是用户自定义规则（可选，变参避免修改既有调用点）：
// 逆序 -D 拆除，规则已不在宿主上（如主机重启）时容忍为成功。
func (a *Applier) Teardown(ctx context.Context, customRules ...[]string) error {
	var rules []string
	if len(customRules) > 0 {
		rules = customRules[0]
	}

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

	// 策略路由：rule 可能叠多条，purgePolicyRules 循环删干净；再 flush table。
	a.purgePolicyRules(ctx)
	for _, cmd := range [][]string{
		{"ip", "route", "flush", "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "route", "flush", "table", fmt.Sprint(RouteTable)},
	} {
		if out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
			low := strings.ToLower(out)
			if !strings.Contains(low, "no such") && !strings.Contains(low, "cannot find") {
				a.logf("拆除策略路由失败 %v: %v: %s", cmd, err, strings.TrimSpace(out))
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	// 自定义规则最后拆（与 nft 表/路由独立，顺序无硬性要求）。
	// 失败只记录：拆除时系统可能已处于异常状态，不能卡在这里。
	a.removeCustomRules(ctx, rules)

	if firstErr == nil {
		a.logf("透明代理规则已拆除")
	}
	return firstErr
}

// purgePolicyRules 循环删除同 mark/table 的全部 ip rule（v4+v6）。
//
// Linux 允许相同 selector 有多条不同 priority 的 rule；单次 `ip rule del`
// 只删一条。用 shell while 一次调完，避免在 Go 里空转几十次。
func (a *Applier) purgePolicyRules(ctx context.Context) {
	script := fmt.Sprintf(
		`while ip rule del fwmark %d table %d 2>/dev/null; do :; done; `+
			`while ip -6 rule del fwmark %d table %d 2>/dev/null; do :; done`,
		FirewallMark, RouteTable, FirewallMark, RouteTable,
	)
	if out, err := a.Runner.Run(ctx, "sh", "-c", script); err != nil {
		// 最终没有匹配时 while 以非 0 结束，属于目标状态，不记错
		low := strings.ToLower(out)
		if strings.Contains(low, "no such") || strings.Contains(low, "cannot find") ||
			strings.TrimSpace(out) == "" {
			return
		}
		a.logf("清理策略路由 rule 时出现异常: %v: %s", err, strings.TrimSpace(out))
	}
}

// CleanupMihomoAutoRedirect 尽力拆除 mihomo tun.auto-redirect 留下的宿主痕迹。
//
// 背景：旁路由 TUN 开启 auto-redirect 后，mihomo 会在宿主写入
// iptables nat 链 mihomo-prerouting / mihomo-output（DISABLE_NFTABLES=1 时）
// 以及 900x 段策略路由 / table 2022。热重载把 tun.enable 改成 false 时，
// 这些痕迹**不保证**被清干净；随后再开 TProxy 会叠两套劫持，手机断网。
//
// 本函数只清 mihomo 命名空间内的残留，不动 aurora_tproxy 表
// （那张表仍由 Teardown 负责）。所有步骤幂等、失败只记日志。
func (a *Applier) CleanupMihomoAutoRedirect(ctx context.Context) {
	// iptables-nft / iptables-legacy 都试：Alpine 上两者可能并存，
	// mihomo 实际写入哪套取决于 DISABLE_NFTABLES 与 xtables 后端。
	for _, ipt := range []string{"iptables", "ip6tables"} {
		a.cleanupMihomoIptablesChain(ctx, ipt, "PREROUTING", "mihomo-prerouting")
		a.cleanupMihomoIptablesChain(ctx, ipt, "OUTPUT", "mihomo-output")
	}
	// nft 后端表名是 mihomo（见 sing_tun AutoRedirect TableName）
	for _, family := range []string{"inet", "ip", "ip6"} {
		if out, err := a.Runner.Run(ctx, "nft", "delete", "table", family, "mihomo"); err != nil {
			low := strings.ToLower(out)
			if !strings.Contains(low, "no such") && !strings.Contains(low, "does not exist") {
				a.logf("删除 mihomo nft 表 %s 失败: %v: %s", family, err, strings.TrimSpace(out))
			}
		}
	}
	// auto-route 留下的 900x 规则与 table 2022（与面板 TProxy 的 8999/table 100 不同）
	for _, prio := range []string{"9000", "9001", "9002", "9010"} {
		// 同一优先级可能有多条（v4/v6、多条 from），循环删到报错为止
		for i := 0; i < 8; i++ {
			if out, err := a.Runner.Run(ctx, "ip", "rule", "del", "priority", prio); err != nil {
				low := strings.ToLower(out)
				if strings.Contains(low, "no such") || strings.Contains(low, "cannot find") || strings.TrimSpace(out) == "" {
					break
				}
				a.logf("删除 mihomo ip rule priority %s 失败: %v: %s", prio, err, strings.TrimSpace(out))
				break
			}
		}
		for i := 0; i < 8; i++ {
			if out, err := a.Runner.Run(ctx, "ip", "-6", "rule", "del", "priority", prio); err != nil {
				low := strings.ToLower(out)
				if strings.Contains(low, "no such") || strings.Contains(low, "cannot find") || strings.TrimSpace(out) == "" {
					break
				}
				break
			}
		}
	}
	for _, table := range []string{"2022"} {
		if out, err := a.Runner.Run(ctx, "ip", "route", "flush", "table", table); err != nil {
			low := strings.ToLower(out)
			if !strings.Contains(low, "no such") && strings.TrimSpace(out) != "" {
				a.logf("flush mihomo route table %s 失败: %v: %s", table, err, strings.TrimSpace(out))
			}
		}
		_, _ = a.Runner.Run(ctx, "ip", "-6", "route", "flush", "table", table)
	}
	// Meta 网卡：热重载关掉 TUN 后偶发残留，TProxy 不需要它
	if out, err := a.Runner.Run(ctx, "ip", "link", "del", "Meta"); err != nil {
		low := strings.ToLower(out)
		if !strings.Contains(low, "cannot find") && !strings.Contains(low, "no such") && strings.TrimSpace(out) != "" {
			a.logf("删除 Meta 网卡失败: %v: %s", err, strings.TrimSpace(out))
		}
	}
	a.logf("已尽力清理 mihomo auto-redirect / auto-route 残留")
}

// cleanupMihomoIptablesChain 从 hook 上摘掉 -j chain 引用并删除 user chain。
func (a *Applier) cleanupMihomoIptablesChain(ctx context.Context, ipt, hook, chain string) {
	// 同一 jump 可能被重复 -A，多删几次直到没有
	for i := 0; i < 4; i++ {
		if out, err := a.Runner.Run(ctx, ipt, "-t", "nat", "-D", hook, "-j", chain); err != nil {
			low := strings.ToLower(out)
			if strings.Contains(low, "bad rule") || strings.Contains(low, "no chain") ||
				strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				break
			}
			// 其他错误也停，避免死循环
			break
		}
	}
	if out, err := a.Runner.Run(ctx, ipt, "-t", "nat", "-F", chain); err != nil {
		low := strings.ToLower(out)
		if !strings.Contains(low, "no chain") && !strings.Contains(low, "doesn't exist") &&
			!strings.Contains(low, "does not exist") {
			a.logf("%s -F %s 失败: %v: %s", ipt, chain, err, strings.TrimSpace(out))
		}
	}
	if out, err := a.Runner.Run(ctx, ipt, "-t", "nat", "-X", chain); err != nil {
		low := strings.ToLower(out)
		if !strings.Contains(low, "no chain") && !strings.Contains(low, "doesn't exist") &&
			!strings.Contains(low, "does not exist") {
			a.logf("%s -X %s 失败: %v: %s", ipt, chain, err, strings.TrimSpace(out))
		}
	}
}

// removeCustomRules 逆序拆除自定义规则（-A/-I → -D）。
//
// "Bad rule" 是 iptables 对 -D 找不到匹配规则的报错——宿主重启后
// iptables 状态同样被清空，此时拆除等于重复删除，视为成功。
// 无法自动逆反的规则（-N/-F/-X 等）跳过并记日志，提示用户手工清理。
func (a *Applier) removeCustomRules(ctx context.Context, rules []string) {
	for i := len(rules) - 1; i >= 0; i-- {
		del := toDeleteCommand(rules[i])
		if del == "" {
			a.logf("自定义规则无法自动拆除（仅 -A/-I 支持），请手工清理: %s", rules[i])
			continue
		}
		if out, err := a.Runner.Run(ctx, "sh", "-c", del); err != nil {
			low := strings.ToLower(out)
			if strings.Contains(low, "no such rule") || strings.Contains(low, "bad rule") {
				continue
			}
			a.logf("拆除自定义规则失败 [%d] %s: %v: %s", i+1, del, err, strings.TrimSpace(out))
		}
	}
}

// DumpRules 输出本面板 nft 表的当前规则集，供界面展示实际生效的内置规则。
//
// 表不存在（TProxy 未开启或已拆除）时返回空字符串而非报错。
func (a *Applier) DumpRules(ctx context.Context) (string, error) {
	cmd := NFTRulesCheckCommand()
	out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...)
	if err != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "no such file") || strings.Contains(low, "does not exist") {
			return "", nil
		}
		return "", fmt.Errorf("读取防火墙规则失败: %w: %s", err, strings.TrimSpace(out))
	}
	return out, nil
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

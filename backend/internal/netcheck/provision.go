package netcheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provisioner 补齐透明代理所需的系统条件：装缺失的包、写 sysctl 配置。
//
// 与 detect_*.go 的关系：探测始终只读，本文件是唯一会改动宿主"系统层"的
// 地方（改防火墙那部分在 Applier）。二者刻意分开，便于回答"面板到底动了
// 什么"这个问题。
//
// 三条硬规则（对应 Applier 的同名约定）：
//   - 只装下面 requiredPackages 里硬编码的包，绝不接受调用方传入包名或
//     命令。参数化命令等于开一个远程命令执行入口，收益不抵风险。
//   - sysctl 只写自己的 drop-in（sysctlDropInPath），绝不改 /etc/sysctl.conf
//     或别人的 drop-in；文件整体重写而非追加，重复执行不会堆积。
//   - 幂等：包已装时包管理器自身即为空操作；sysctl 本来就合规时跳过不写。
//
// 本文件不带 //go:build linux：它只编排外部命令与文件读写，没有 Linux 专有
// 系统调用，因此逻辑可以在任意平台用假 Runner/假文件系统测试。
// "是否允许执行"由调用方按 Detect() 的结论决定，不靠编译标签兜。
type Provisioner struct {
	// Runner 执行外部命令，测试可替换。复用 Applier 那套接口。
	Runner CommandRunner
	// Root 是 sysctl drop-in 的写入根目录，默认 "/"。测试指向临时目录。
	Root string
	// Logf 记录执行过程，可为 nil
	Logf func(format string, args ...interface{})
}

// sysctlDropInRelPath 是本程序专用的 sysctl 配置文件（相对于 Root）。
//
// 99- 前缀确保它在字典序上排在发行版自带配置之后，从而覆盖它们；
// 独立文件使得"面板改了哪些内核参数"一目了然，卸载时删这一个文件即可。
const sysctlDropInRelPath = "etc/sysctl.d/99-auroramihomo.conf"

// requiredPackages 按包管理器列出 TProxy 所需的包。
//
// Debian 系的 iptables 包自带 ip6tables，Alpine 把它拆成了独立包，
// 所以两边的列表不同——这与 installHintFor 给出的手动命令保持一致。
var requiredPackages = map[string][]string{
	"apt-get": {"iptables", "nftables", "iproute2"},
	"apk":     {"iptables", "ip6tables", "nftables", "iproute2"},
}

// installHintFor 给出补齐依赖的手动命令。
//
// 与 Provisioner 实际执行的命令共用 requiredPackages，避免"界面上让你装 A、
// 按钮实际装 B"这种会白白浪费排查时间的漂移。
//
// 放在这个文件而非 detect_linux.go：它是纯字符串拼接，没有 Linux 专有调用，
// 而 Provisioner（不带 build tag）也要用它。
func installHintFor(pm string) string {
	pkgs, ok := requiredPackages[pm]
	if !ok {
		return "请用发行版包管理器安装 iptables（或 nftables）与 iproute2"
	}
	joined := strings.Join(pkgs, " ")
	switch pm {
	case "apk":
		// Alpine 的 iproute2 拆得很细，iproute2-minimal 不够用，要装完整包；
		// ip6tables 也是独立包（Debian 系由 iptables 一并提供）
		return "apk add --no-cache " + joined
	case "apt-get":
		return "apt-get update && apt-get install -y --no-install-recommends " + joined
	default:
		return "请用发行版包管理器安装 " + joined
	}
}

// ProvisionOptions 控制这次要做哪些动作。
// 两个开关都为 false 时直接返回，不做任何事。
type ProvisionOptions struct {
	InstallPackages bool
	ApplySysctl     bool
}

// ProvisionStep 是单个步骤的结果。
type ProvisionStep struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	// Detail 成功时是简要说明，失败时放命令原始输出——
	// 把 apt/apk 的报错原文交给用户，比"安装失败"有用得多
	Detail string `json:"detail"`
	// Skipped 表示无需执行（已满足条件）
	Skipped bool `json:"skipped"`
}

// ProvisionResult 汇总一次准备的结果。
type ProvisionResult struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Steps   []ProvisionStep `json:"steps"`
	// NotPersistent 表示改动会随重启/容器重建丢失
	NotPersistent bool `json:"notPersistent"`
	// ManualCommands 等价手动命令，成功与否都给
	ManualCommands []string `json:"manualCommands"`
}

func (p *Provisioner) logf(format string, args ...interface{}) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}

func (p *Provisioner) root() string {
	if p.Root == "" {
		return string(os.PathSeparator)
	}
	return p.Root
}

// Provision 按 opts 执行准备动作。
//
// report 必须是调用方刚探测到的结果：这里依赖它判断包管理器、是否 root、
// 是否在容器里、以及哪些 sysctl 真的需要改。不自己重新探测是为了让调用方
// 能在同一份报告上做"是否允许执行"的判断，避免两次探测之间状态漂移。
//
// 返回的 error 只表示"整件事没能开始"（如环境根本不支持）；单步失败体现在
// Steps 里并让 Success 为 false，因为部分成功也需要如实告诉用户。
func (p *Provisioner) Provision(ctx context.Context, report *Report,
	opts ProvisionOptions) (*ProvisionResult, error) {
	if report == nil {
		return nil, fmt.Errorf("缺少环境探测结果")
	}
	if report.OS != "linux" {
		// macOS 只支持 TUN 且没有这套包管理器；Windows 压根不支持透明代理
		return nil, fmt.Errorf("仅 Linux 支持自动准备环境，当前系统为 %s", report.OS)
	}
	if !opts.InstallPackages && !opts.ApplySysctl {
		return nil, fmt.Errorf("未指定要执行的操作")
	}

	res := &ProvisionResult{
		// 容器内装的包重启即丢，必须让用户知道，否则他会以为一次搞定了
		NotPersistent:  report.InContainer,
		ManualCommands: p.manualCommands(report, opts),
	}

	// 装包与写 sysctl 都需要 root。提前拒绝而不是跑一半失败：
	// 后者会留下"装了一半"的状态，且报错藏在命令输出里不易理解。
	if !report.Root {
		return nil, fmt.Errorf("需要以 root 运行才能安装依赖或修改 sysctl。" +
			"当前为非 root，请用下面的手动命令在宿主上执行，" +
			"或以 root 运行本程序（容器部署可设 user: \"0:0\"）")
	}

	if opts.InstallPackages {
		res.Steps = append(res.Steps, p.installPackages(ctx, report)...)
	}
	if opts.ApplySysctl {
		res.Steps = append(res.Steps, p.applySysctl(ctx, report)...)
	}

	res.Success = true
	var failed int
	for _, s := range res.Steps {
		if !s.Success && !s.Skipped {
			res.Success = false
			failed++
		}
	}
	res.Message = p.summarize(res.Steps, failed)
	return res, nil
}

// summarize 生成一句面向用户的总结。
func (p *Provisioner) summarize(steps []ProvisionStep, failed int) string {
	var done, skipped int
	for _, s := range steps {
		switch {
		case s.Skipped:
			skipped++
		case s.Success:
			done++
		}
	}
	switch {
	case failed > 0:
		return fmt.Sprintf("完成 %d 项，跳过 %d 项，失败 %d 项。"+
			"失败项的命令输出见下方，可改用手动命令排查", done, skipped, failed)
	case done == 0 && skipped > 0:
		return "环境已满足条件，无需改动"
	default:
		return fmt.Sprintf("完成 %d 项，跳过 %d 项（已满足）", done, skipped)
	}
}

// installPackages 安装缺失的包。
//
// 不逐个判断"哪个包没装"再精确安装：包管理器对已安装的包本就是空操作，
// 一次装齐比自己维护一份"已装/未装"的判断更可靠（同一个命令可能由不同包
// 提供，逐个映射容易出错）。
func (p *Provisioner) installPackages(ctx context.Context, report *Report) []ProvisionStep {
	pkgs, ok := requiredPackages[report.PackageManager]
	if !ok {
		return []ProvisionStep{{
			Name: "安装软件包",
			Detail: "未能识别包管理器（既没有 apt-get 也没有 apk），" +
				"无法自动安装。请用发行版自带的包管理器手动安装 " +
				strings.Join([]string{"iptables", "nftables", "iproute2"}, "、"),
		}}
	}

	// 已经齐了就不折腾包管理器：省掉一次联网的 apt-get update
	if p.packagesSatisfied(report) {
		return []ProvisionStep{{
			Name:    "安装软件包",
			Success: true,
			Skipped: true,
			Detail:  "nft/iptables 与 iproute2 均已就绪",
		}}
	}

	var steps []ProvisionStep
	if report.PackageManager == "apt-get" {
		// 不先 update 的话，全新系统上 apt-get install 会因为没有索引而失败
		steps = append(steps, p.run(ctx, "刷新软件源", "apt-get", "update"))
		if !steps[0].Success {
			// 源不可达时继续装必然失败，早停并把原始输出留给用户
			return steps
		}
		args := append([]string{"install", "-y", "--no-install-recommends"}, pkgs...)
		steps = append(steps, p.runEnv(ctx,
			[]string{"DEBIAN_FRONTEND=noninteractive"},
			"安装软件包", "apt-get", args...))
		return steps
	}

	args := append([]string{"add", "--no-cache"}, pkgs...)
	steps = append(steps, p.run(ctx, "安装软件包", "apk", args...))
	return steps
}

// packagesSatisfied 判断 TProxy 需要的工具是否已齐。
// 判据与 checkTProxy 保持一致，否则会出现"装完仍显示缺"或反之。
func (p *Provisioner) packagesSatisfied(report *Report) bool {
	st := report.ModeStatusOf(ModeTProxy)
	return len(st.Missing) == 0
}

// sysctlSetting 是一条要写入 drop-in 的内核参数。
type sysctlSetting struct {
	key   string
	value string
	// why 写进配置文件的注释里，便于日后有人看到这个文件时知道为什么
	why string
}

// applySysctl 写 drop-in 并让其立即生效。
//
// 两件事必须分开报告：文件写成功 ≠ 当前已生效（drop-in 要 sysctl --system
// 才加载），混为一谈会让用户以为搞定了却仍然丢包。
func (p *Provisioner) applySysctl(ctx context.Context, report *Report) []ProvisionStep {
	// 容器内 sysctl 要么被内核拒绝（非特权），要么直接改到宿主上（host 网络），
	// 两种都不该由面板悄悄替用户决定
	if report.InContainer {
		return []ProvisionStep{{
			Name: "写入 sysctl 配置",
			Detail: "容器内不修改 sysctl：非特权容器会被内核拒绝，" +
				"host 网络下则会直接改动宿主。请在宿主机上执行下方手动命令",
		}}
	}

	settings := p.neededSysctl(report)
	if len(settings) == 0 {
		return []ProvisionStep{{
			Name:    "写入 sysctl 配置",
			Success: true,
			Skipped: true,
			Detail:  "ip_forward / ipv6.forwarding 与 rp_filter 均已合规，无需改动",
		}}
	}

	path := filepath.Join(p.root(), filepath.FromSlash(sysctlDropInRelPath))
	step := ProvisionStep{Name: "写入 sysctl 配置", Command: "write " + path}
	if err := p.writeDropIn(path, settings); err != nil {
		step.Detail = err.Error()
		return []ProvisionStep{step}
	}
	step.Success = true
	step.Detail = fmt.Sprintf("已写入 %s（%d 项）", path, len(settings))
	p.logf("已写入 sysctl 配置 %s（%d 项）", path, len(settings))

	return append([]ProvisionStep{step}, p.reloadSysctl(ctx, path)...)
}

// reloadSysctl 让 drop-in 生效，必要时回退到逐文件加载。
//
// 先试 `sysctl --system`（procps-ng 的标准做法，会重新加载所有 drop-in），
// 失败则退回 `sysctl -p <本文件>`。
//
// 为什么需要回退：Alpine 等以 BusyBox 为基础的发行版里 sysctl 是 BusyBox
// applet，只支持 `-p FILE`，不认 `--system` 这个 GNU 扩展。真机上的表现是
// 配置文件写成功、这一步报 "unrecognized option: system"，于是 rp_filter
// 保持严格模式、TProxy 静默收不到包 —— 用户只看到"部分成功"，很难联想到
// 是一个命令行选项的兼容性问题。
//
// 只加载自己这一个文件是可接受的降级：本函数刚写完它，而其余 drop-in
// 本就由系统在启动时加载过，不需要本程序代劳。
//
// 同一类根因也曾出现在 isRealIproute2()（假设 `ip --version` 可用），
// 处理策略保持一致：试长选项，失败退短选项，不预先探测发行版。
func (p *Provisioner) reloadSysctl(ctx context.Context, dropIn string) []ProvisionStep {
	step := p.run(ctx, "使 sysctl 生效", "sysctl", "--system")
	if step.Success {
		return []ProvisionStep{step}
	}

	// 只在"选项不被识别"时回退。刻意不对所有失败都重试：
	// 像 "cannot stat /proc/sys/..." 这类错误说明某个键在当前内核上不存在，
	// 换成 -p 会得到同样的结果，把它也转成成功等于掩盖真实问题。
	if !isUnsupportedOptionError(step.Detail) {
		return []ProvisionStep{step}
	}

	fallback := p.run(ctx, "使 sysctl 生效（逐文件加载）", "sysctl", "-p", dropIn)
	if !fallback.Success {
		// 两种都失败：把两步都报出来。只留后者会丢掉"--system 为何失败"
		// 这条线索，而那正是判断环境类型的依据。
		return []ProvisionStep{step, fallback}
	}
	// 回退成功时不把 --system 的失败计入失败数：目标是"内核值已生效"，
	// 用哪个选项达成不影响结果。但仍保留该步骤的记录，便于识别 BusyBox 环境。
	step.Skipped = true
	step.Success = true
	step.Detail = "当前系统的 sysctl 不支持 --system（常见于 BusyBox），" +
		"已改用逐文件加载。原始输出：" + step.Detail
	return []ProvisionStep{step, fallback}
}

// isUnsupportedOptionError 判断错误输出是否为"命令行选项不被识别"。
//
// BusyBox 的 applet 与 GNU 工具对未知选项的措辞不同，且不同版本还有差异，
// 因此按子串匹配几种常见形式而非精确匹配。宁可漏判（不回退、如实报错）
// 也不要误判（把真实故障当成兼容性问题掩盖过去）。
func isUnsupportedOptionError(out string) bool {
	low := strings.ToLower(out)
	for _, pat := range []string{
		"unrecognized option", // BusyBox
		"invalid option",      // 部分 busybox 构建与 ash 内建
		"unknown option",      // 少数实现
		"illegal option",      // BSD 风格
	} {
		if strings.Contains(low, pat) {
			return true
		}
	}
	return false
}

// neededSysctl 只返回确实需要改的项。
//
// 刻意不无条件全写：转发与 rp_filter 已经合规时没必要动。
// 改得越少越好。键名与 scripts/sysctl-auroramihomo.conf、install.sh
// 的 apply_sysctl 保持一致，避免"面板准备"与"在线安装"写出两套内容。
func (p *Provisioner) neededSysctl(report *Report) []sysctlSetting {
	var out []sysctlSetting
	if report.SysctlIPForward == "0" {
		out = append(out, sysctlSetting{
			key:   "net.ipv4.ip_forward",
			value: "1",
			why:   "作为局域网网关转发其它设备的 IPv4 流量",
		})
	}
	// IPv6 转发与 v4 成对写入：只开 v4 时双栈局域网的 v6 流量仍无法经本机转发。
	// 读不到（空）表示当前内核没有该开关，不写入，以免 sysctl -p 报错。
	if report.SysctlIPv6Forward == "0" {
		out = append(out, sysctlSetting{
			key:   "net.ipv6.conf.all.forwarding",
			value: "1",
			why:   "作为局域网网关/旁路由转发其它设备的 IPv6 流量",
		})
	}
	// rp_filter=1 是严格反向路径校验，会丢掉 TPROXY 打标后回环的包。
	// 设为 2（宽松）而非 0：保留基本的源地址校验，且与 Debian/Ubuntu
	// 的默认值一致，比直接关掉更保守。
	if report.SysctlRPFilter == "1" {
		out = append(out, sysctlSetting{
			key:   "net.ipv4.conf.all.rp_filter",
			value: "2",
			why:   "严格反向路径校验会丢弃 TProxy 打标后回环的包",
		})
		// 内核对某网卡取 max(all, <该网卡>)，所以只改 all 往往不生效：
		// 已存在的网卡仍保留自己的 1。default 管新出现的网卡，
		// 逐网卡覆盖已存在的。漏掉这些会导致"改了却依然丢包"。
		out = append(out, sysctlSetting{
			key:   "net.ipv4.conf.default.rp_filter",
			value: "2",
			why:   "新建网卡的默认值，否则后出现的网卡仍是严格模式",
		})
		for _, ifc := range report.RPFilterStrictIfaces {
			out = append(out, sysctlSetting{
				key:   "net.ipv4.conf." + ifc + ".rp_filter",
				value: "2",
				why:   "内核取 all 与本网卡的最大值，不改这里则 all 的设置不生效",
			})
		}
	}
	return out
}

// writeDropIn 整体重写配置文件。
//
// 重写而非追加：追加会让重复执行不断堆积同名键，文件越来越长且难以判断
// 哪一行真正生效（后出现的覆盖前面的）。整体重写天然幂等。
func (p *Provisioner) writeDropIn(path string, settings []sysctlSetting) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	var b strings.Builder
	b.WriteString("# 由 AuroraMihomo 自动生成，用于透明代理。\n")
	b.WriteString("# 本文件只包含面板需要的内核参数；不再需要时可直接删除此文件后重启，\n")
	b.WriteString("# 或执行 sysctl --system（BusyBox 环境用 sysctl -p <其它 drop-in>）\n")
	b.WriteString("# 恢复发行版默认值。\n")
	for _, s := range settings {
		fmt.Fprintf(&b, "\n# %s\n%s = %s\n", s.why, s.key, s.value)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("写入失败: %w", err)
	}
	return nil
}

// manualCommands 返回等价的手动命令。
//
// 无论自动执行成功与否都提供：失败时它是兜底，成功时用户也常需要把它记进
// 自己的部署脚本或 Ansible 里。
func (p *Provisioner) manualCommands(report *Report, opts ProvisionOptions) []string {
	var out []string
	if opts.InstallPackages {
		if hint := installHintFor(report.PackageManager); hint != "" {
			out = append(out, hint)
		}
	}
	if opts.ApplySysctl {
		if settings := p.neededSysctl(report); len(settings) > 0 {
			path := "/" + sysctlDropInRelPath
			var lines []string
			for _, s := range settings {
				lines = append(lines, fmt.Sprintf("%s = %s", s.key, s.value))
			}
			// 用 tee 而非 > 重定向：前者配合 sudo 才能写入特权目录，
			// 用户直接复制到普通用户的 shell 里也能用
			out = append(out, fmt.Sprintf("printf '%%s\\n' %s | sudo tee %s",
				shellQuote(strings.Join(lines, "\n")), path))
			// 用 -p <文件> 而非 --system：BusyBox 的 sysctl 不认后者，
			// 而手动命令是给用户直接粘贴执行的，没有回退可依赖。
			// -p 在 procps-ng 与 BusyBox 上都支持，是两边通用的写法。
			out = append(out, "sudo sysctl -p "+path)
		}
	}
	return out
}

// shellQuote 用单引号包裹字符串，内部单引号按 shell 惯例转义。
// 手动命令是给用户复制到终端里执行的，必须能原样粘贴。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (p *Provisioner) run(ctx context.Context, name, cmd string, args ...string) ProvisionStep {
	return p.runEnv(ctx, nil, name, cmd, args...)
}

// runEnv 执行命令并把结果整理成 ProvisionStep。
//
// env 目前只用于 DEBIAN_FRONTEND=noninteractive：不设它，apt 在某些镜像里
// 会尝试打开交互式配置界面并挂住，而这里没有 tty 可用。
func (p *Provisioner) runEnv(ctx context.Context, env []string,
	name, cmd string, args ...string) ProvisionStep {
	display := strings.TrimSpace(cmd + " " + strings.Join(args, " "))
	if len(env) > 0 {
		display = strings.Join(env, " ") + " " + display
	}
	step := ProvisionStep{Name: name, Command: display}

	p.logf("环境准备执行: %s", display)
	out, err := p.Runner.RunEnv(ctx, env, cmd, args...)
	trimmed := strings.TrimSpace(out)
	if err != nil {
		// 原始输出直接交给用户：apt/apk 的报错（源不可达、磁盘满、锁被占）
		// 各不相同，概括成一句话会丢掉唯一有用的信息
		step.Detail = trimmed
		if step.Detail == "" {
			step.Detail = err.Error()
		}
		p.logf("环境准备失败: %s: %v", display, err)
		return step
	}
	step.Success = true
	step.Detail = tailLines(trimmed, 8)
	return step
}

// tailLines 只保留末尾若干行。
// apt-get install 的输出可能上百行，界面上展示末尾几行足够判断结果，
// 全量塞进响应会让 JSON 变得很大。
func tailLines(s string, n int) string {
	if s == "" {
		return "完成"
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return "…\n" + strings.Join(lines[len(lines)-n:], "\n")
}

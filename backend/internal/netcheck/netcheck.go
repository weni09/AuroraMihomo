// Package netcheck 探测运行环境是否具备透明代理条件，并在用户显式要求时
// 补齐这些条件。
//
// 设计约束（这些直接决定了本包的形态）：
//
//   - 探测只读。detect_*.go 里的探测路径不安装依赖、不改 sysctl、不动
//     防火墙，只把"缺什么"和"怎么装"如实报出来。
//   - 修复需显式触发、可审计、默认不动。写操作集中在两个组件里：
//     Applier（防火墙规则与策略路由）与 Provisioner（软件包与 sysctl）。
//     二者都只在用户主动操作后运行，且只碰自己那一份东西——
//     Applier 只管自己的 nft 表，Provisioner 只装硬编码的包、只写自己的
//     sysctl drop-in。
//   - 结论要能解释。每种模式给出 available 与 reason，前端要能直接把
//     reason 显示给用户，因此文案是面向使用者而非开发者的。
//   - 平台差异用 build tag 拆文件（detect_linux.go / detect_darwin.go /
//     detect_other.go），沿用项目里 signal_unix.go / signal_windows.go 的
//     既有范式，避免在函数内部堆 runtime.GOOS 分支。
//
// 关于"代为安装依赖"这条约束的变更（原先是完全不做）：
// 原理由是"容器里装包重启即丢，代执行包管理器把失败形态放大（无网络、
// 源不可达、只读文件系统），收益不抵风险"。真机测试（见
// docs/AuroraMihomo-Transparent-Proxy-Test-Report.md）表明这些顾虑成立但
// 不足以否掉整个功能：手动步骤本身是可靠的、也是用户频繁需要的，
// 而失败形态可以靠"如实回报每一步的原始输出 + 始终提供等价手动命令"来消化。
// 于是改为提供 Provisioner，但保留原理由中仍然成立的部分：
// 容器内的改动会明确标注不持久，容器内不碰 sysctl（非特权会被拒绝、
// host 网络会直接改到宿主，都不该由面板替用户决定）。
package netcheck

import (
	"os"
	"strings"
)

// Mode 是透明代理的实现方式。
type Mode string

const (
	// ModeTUN 走虚拟网卡。mihomo 自己用 netlink 改路由、自己写并清理
	// nftables/iptables 规则，面板不碰防火墙，风险最低。
	// 支持 TCP/UDP/ICMP 与 IPv6，且是 macOS 上唯一可行的方式。
	ModeTUN Mode = "tun"

	// ModeTProxy 走 TPROXY 目标转发。需要面板自己写 mangle 规则与策略
	// 路由，是"锁死自己"风险的主要来源，仅 Linux 可用。
	// 用于没有 TUN 设备但有 TPROXY 内核支持的环境。
	ModeTProxy Mode = "tproxy"

	// ModeOff 关闭透明代理。
	ModeOff Mode = "off"
)

// ValidMode 判断字符串是否为受支持的模式。
func ValidMode(s string) bool {
	switch Mode(s) {
	case ModeTUN, ModeTProxy, ModeOff:
		return true
	default:
		return false
	}
}

// ModeStatus 是单一模式的可用性结论。
type ModeStatus struct {
	Mode Mode `json:"mode"`
	// Available 为 true 表示当前环境可以启用该模式
	Available bool `json:"available"`
	// Reason 说明不可用的原因，或可用时的注意事项。面向最终用户。
	Reason string `json:"reason"`
	// Missing 列出缺失的依赖（命令名或内核特性），供前端展示
	Missing []string `json:"missing,omitempty"`
	// InstallHint 是补齐依赖的具体命令，用户可直接复制执行
	InstallHint string `json:"installHint,omitempty"`
}

// Report 是一次完整探测的结果。
type Report struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// Kernel 是内核版本串（Linux 上取 /proc/sys/kernel/osrelease）
	Kernel string `json:"kernel,omitempty"`
	// Distro 是发行版标识（取 /etc/os-release 的 ID），如 alpine/debian/ubuntu
	Distro string `json:"distro,omitempty"`
	// PackageManager 是探测到的包管理器：apk / apt-get / 空
	PackageManager string `json:"packageManager,omitempty"`

	// Root 表示是否以 root（euid 0）运行
	Root bool `json:"root"`
	// CapNetAdmin / CapNetRaw 是有效集里是否持有对应 capability。
	// 注意与 CapBounding 的区别：容器里 cap_add 只填充 bounding set，
	// 非 root 进程的 effective 集仍是空的——这正是 docker-compose.yml
	// 注释里记录的陷阱。
	CapNetAdmin bool `json:"capNetAdmin"`
	CapNetRaw   bool `json:"capNetRaw"`
	// CapNetAdminBounding 为 true 但 CapNetAdmin 为 false，说明
	// cap_add 给了但当前进程拿不到，提示信息要能区分这两种情况
	CapNetAdminBounding bool `json:"capNetAdminBounding"`

	// InContainer 表示运行在容器里
	InContainer bool `json:"inContainer"`
	// HostNetwork 表示与 PID 1 共享网络命名空间（即 network_mode: host）。
	// 桥接网络里改路由只影响容器自己，服务不了局域网其它设备。
	HostNetwork bool `json:"hostNetwork"`

	// TunDevice 实际找到的 TUN 设备节点路径（/dev/net/tun 或 /dev/tun），
	// 都不存在时为空。单独报出来是为了让"设备缺失"与"有设备但权限不足"
	// 在界面上可区分——两者的修复动作完全不同（映射设备 vs 调整权限）。
	TunDevice string `json:"tunDevice,omitempty"`

	// IPTablesBackend 是 iptables 命令的后端类型：nf_tables / legacy / 空。
	// 自定义防火墙规则按 iptables 语法执行，用户需要知道规则落在哪套后端：
	// legacy 与 nftables 规则互不可见，写错地方等于没写（见 Warnings 里
	// 的同名告警）。detect 阶段一次探测，随 env 返回前端展示。
	IPTablesBackend string `json:"iptablesBackend,omitempty"`

	// Modes 按 tun、tproxy 顺序给出各模式结论
	Modes []ModeStatus `json:"modes"`

	// Warnings 是不阻塞启用但需要用户知道的问题
	Warnings []string `json:"warnings,omitempty"`

	// 以下 sysctl 原始值不出现在 JSON 里：界面上要看的是 Warnings 里那句
	// 人话，而 Provisioner 需要原始值才能判断"到底哪几项要改"。
	// 之前这些值只在 collectWarnings 内部读完就丢，导致准备逻辑只能重新
	// 读一遍文件——两次读之间状态可能已经变了。
	//
	// SysctlIPForward 是 net.ipv4.ip_forward 的当前值，读不到时为空
	SysctlIPForward string `json:"-"`
	// SysctlIPv6Forward 是 net.ipv6.conf.all.forwarding 的当前值，读不到时为空。
	// 网关/旁路由要转发局域网 IPv6 时必须为 1；与 ip_forward 成对处理。
	SysctlIPv6Forward string `json:"-"`
	// SysctlRPFilter 是 net.ipv4.conf.all.rp_filter 的当前值，读不到时为空
	SysctlRPFilter string `json:"-"`
	// RPFilterStrictIfaces 是当前 rp_filter 仍为 1（严格）的具体网卡名。
	// 内核对某网卡取 max(all, <该网卡>)，只改 all 不会让已存在的网卡生效，
	// 所以必须知道有哪些网卡需要一起改。
	RPFilterStrictIfaces []string `json:"-"`

	// HasIPv6Egress 表示宿主确实具备 IPv6 出网能力（有全局 v6 地址且有
	// v6 默认路由）。TProxy 据此决定是否下发 v6 规则与 v6 策略路由：
	// 两者必须同时有或同时没有，只下发规则而没有路由会让 v6 包被打标后
	// 无路可走，从"不分流"恶化成"不通"。
	HasIPv6Egress bool `json:"-"`

	// DNSLoopbackStub 是 /etc/resolv.conf 里指向回环的 nameserver 地址
	// （systemd-resolved 的 127.0.0.53 最常见），没有则为空。
	// 本机 DNS 劫持刻意排除回环目标（否则与 mihomo 自己的 DNS 自环），
	// 所以这类机器上本机的域名分流不生效——必须如实告警而不是假装劫持了。
	DNSLoopbackStub string `json:"-"`
}

// ModeStatusOf 取出指定模式的结论；不存在时返回不可用。
func (r *Report) ModeStatusOf(m Mode) ModeStatus {
	for _, s := range r.Modes {
		if s.Mode == m {
			return s
		}
	}
	return ModeStatus{Mode: m, Available: false, Reason: "当前平台不支持该模式"}
}

// AnyAvailable 表示至少有一种模式可用。用于决定前端开关是否可操作。
func (r *Report) AnyAvailable() bool {
	for _, s := range r.Modes {
		if s.Available {
			return true
		}
	}
	return false
}

// Detect 探测当前环境。实现按平台分文件，见 detect_*.go。
func Detect() *Report { return detect() }

// hasCommand 与 fileExists 定义在 detect_linux.go：
// 只有 Linux 的探测路径用得到它们，放在本文件（无构建标签、全平台编译）
// 会在 darwin/windows 下成为未被引用的死代码，被 unused 检查拦下。

// readFileTrim 读文件并去掉首尾空白；读不到返回空串。
// 探测场景下"读不到"与"内容为空"处理方式相同，不需要区分。
func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

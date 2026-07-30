// Package netcheck 探测运行环境是否具备透明代理条件。
//
// 设计约束（这些直接决定了本包的形态）：
//
//   - 只读探测。本包不安装依赖、不改 sysctl、不动防火墙。缺什么就把
//     "缺什么"和"怎么装"如实报出来，由用户自己决定是否执行。容器里装包
//     重启即丢，代面板执行包管理器更是把失败形态放大（无网络、源不可达、
//     只读文件系统），收益不抵风险。
//   - 结论要能解释。每种模式给出 available 与 reason，前端要能直接把
//     reason 显示给用户，因此文案是面向使用者而非开发者的。
//   - 平台差异用 build tag 拆文件（detect_linux.go / detect_darwin.go /
//     detect_other.go），沿用项目里 signal_unix.go / signal_windows.go 的
//     既有范式，避免在函数内部堆 runtime.GOOS 分支。
package netcheck

import (
	"os"
	"os/exec"
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

	// Modes 按 tun、tproxy 顺序给出各模式结论
	Modes []ModeStatus `json:"modes"`

	// Warnings 是不阻塞启用但需要用户知道的问题
	Warnings []string `json:"warnings,omitempty"`
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

// hasCommand 判断外部命令是否在 PATH 上。
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// fileExists 判断路径存在（不区分文件类型）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readFileTrim 读文件并去掉首尾空白；读不到返回空串。
// 探测场景下"读不到"与"内容为空"处理方式相同，不需要区分。
func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

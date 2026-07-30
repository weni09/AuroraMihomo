//go:build linux

package netcheck

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// probePaths 把所有被探测的路径收成一处，测试可替换成临时目录里的假数据。
// 生产代码只用 defaultProbePaths。
type probePaths struct {
	procStatus          string // /proc/self/status，读 CapEff/CapBnd
	procModules         string // /proc/modules，查内核模块
	osRelease           string // /etc/os-release，判发行版
	kernelRelease       string // /proc/sys/kernel/osrelease
	devNetTun           string // /dev/net/tun
	devTun              string // /dev/tun，sing-tun 的备选路径
	sysClassMiscTun     string // /sys/class/misc/tun，模块内建时也存在
	dockerEnv           string // /.dockerenv
	procOneCgroup       string // /proc/1/cgroup
	selfNetNS           string // /proc/self/ns/net
	oneNetNS            string // /proc/1/ns/net
	sysctlIPForward     string
	sysctlRPFilter      string
	sysctlRouteLocalnet string
}

func defaultProbePaths() probePaths {
	return probePaths{
		procStatus:          "/proc/self/status",
		procModules:         "/proc/modules",
		osRelease:           "/etc/os-release",
		kernelRelease:       "/proc/sys/kernel/osrelease",
		devNetTun:           "/dev/net/tun",
		devTun:              "/dev/tun",
		sysClassMiscTun:     "/sys/class/misc/tun",
		dockerEnv:           "/.dockerenv",
		procOneCgroup:       "/proc/1/cgroup",
		selfNetNS:           "/proc/self/ns/net",
		oneNetNS:            "/proc/1/ns/net",
		sysctlIPForward:     "/proc/sys/net/ipv4/ip_forward",
		sysctlRPFilter:      "/proc/sys/net/ipv4/conf/all/rp_filter",
		sysctlRouteLocalnet: "/proc/sys/net/ipv4/conf/all/route_localnet",
	}
}

// commandProbe 抽出对外部命令的依赖，测试可注入假实现。
type commandProbe struct {
	// lookPath 判断命令是否存在
	lookPath func(string) bool
	// version 取命令的版本输出（用于区分 iptables legacy/nft、busybox ip）
	version func(name string, args ...string) string
}

func defaultCommandProbe() commandProbe {
	return commandProbe{
		lookPath: hasCommand,
		version: func(name string, args ...string) string {
			// 只读探测，超时交由调用方的 context 控制；这里用 CombinedOutput
			// 是因为部分工具把版本写到 stderr
			out, err := exec.Command(name, args...).CombinedOutput()
			if err != nil && len(out) == 0 {
				return ""
			}
			return string(out)
		},
	}
}

func detect() *Report {
	return detectWith(defaultProbePaths(), defaultCommandProbe(), os.Geteuid())
}

// detectWith 是可测试的探测主体。
func detectWith(p probePaths, c commandProbe, euid int) *Report {
	r := &Report{
		OS:     "linux",
		Arch:   runtime.GOARCH,
		Kernel: readFileTrim(p.kernelRelease),
		Root:   euid == 0,
	}

	eff, bnd := readCapabilities(p.procStatus)
	r.CapNetAdmin = r.Root || hasCap(eff, capNetAdmin)
	r.CapNetRaw = r.Root || hasCap(eff, capNetRaw)
	r.CapNetAdminBounding = hasCap(bnd, capNetAdmin)

	r.Distro, _ = parseOSRelease(readFileTrim(p.osRelease))
	r.PackageManager = detectPackageManager(c)

	r.InContainer = fileExists(p.dockerEnv) || cgroupLooksContainerized(readFileTrim(p.procOneCgroup))
	r.HostNetwork = sameNetNS(p.selfNetNS, p.oneNetNS)

	r.Modes = []ModeStatus{
		checkTUN(r, p, c),
		checkTProxy(r, p, c),
	}
	r.Warnings = collectWarnings(r, p, c)
	return r
}

func detectPackageManager(c commandProbe) string {
	// 顺序有意义：Alpine 镜像里不会有 apt-get，而某些 Debian 派生版
	// 可能同时装了别的工具，apt-get 是最稳的判据
	for _, pm := range []string{"apk", "apt-get"} {
		if c.lookPath(pm) {
			return pm
		}
	}
	return ""
}

// sameNetNS 比较两个 netns 符号链接是否指向同一命名空间。
// 相同即与 PID 1 共享网络（host network 或非容器环境）。
func sameNetNS(selfPath, onePath string) bool {
	self, err1 := os.Readlink(selfPath)
	one, err2 := os.Readlink(onePath)
	if err1 != nil || err2 != nil {
		// 读不到时保守认为共享：非容器的普通主机上这两个链接本来就相同，
		// 而权限不足读不到 /proc/1 的情况下也不该误报"网络被隔离"
		return true
	}
	return self == one
}

// checkTUN 判断 TUN 模式可用性。
func checkTUN(r *Report, p probePaths, c commandProbe) ModeStatus {
	s := ModeStatus{Mode: ModeTUN}

	// sing-tun 优先用 /dev/tun（Android），否则 /dev/net/tun。
	// 记下实际路径，便于界面区分"设备缺失"与"有设备但权限不足"。
	switch {
	case isCharDevice(p.devTun):
		r.TunDevice = p.devTun
	case isCharDevice(p.devNetTun):
		r.TunDevice = p.devNetTun
	}
	hasDevice := r.TunDevice != ""
	moduleLoaded := modulePresent(readFileTrim(p.procModules), "tun") || fileExists(p.sysClassMiscTun)

	switch {
	case !hasDevice && r.InContainer:
		s.Missing = append(s.Missing, "/dev/net/tun")
		s.Reason = "容器内没有 TUN 设备。需要在 compose 里加 devices: [\"/dev/net/tun:/dev/net/tun\"]"
		return s
	case !hasDevice && !moduleLoaded:
		s.Missing = append(s.Missing, "tun 内核模块")
		s.Reason = "内核 tun 模块未加载，且 /dev/net/tun 不存在。先执行 modprobe tun"
		s.InstallHint = "modprobe tun"
		return s
	case !hasDevice:
		s.Missing = append(s.Missing, "/dev/net/tun")
		s.Reason = "tun 模块已加载但 /dev/net/tun 缺失，可能需要手工创建设备节点"
		return s
	}

	if !r.CapNetAdmin {
		s.Missing = append(s.Missing, "CAP_NET_ADMIN")
		if r.CapNetAdminBounding {
			// 这是容器里最容易踩的情况：cap_add 给了，但非 root 拿不到
			s.Reason = "已授予 NET_ADMIN 但当前进程未实际持有（cap_add 只填充 bounding 集）。" +
				"以 root 运行容器（user: \"0:0\"）或给二进制设置 file capability，" +
				"同时需去掉 no-new-privileges"
		} else {
			s.Reason = "缺少 CAP_NET_ADMIN，无法创建 TUN 设备与修改路由。需以 root 运行或授予该 capability"
		}
		return s
	}

	if r.InContainer && !r.HostNetwork {
		// 桥接网络里 TUN 能起来，但只影响容器自身，服务不了局域网设备
		s.Reason = "可用，但容器未使用 host 网络，接管范围仅限容器内部。" +
			"要服务局域网设备需 network_mode: host"
		s.Available = true
		return s
	}

	s.Available = true
	s.Reason = "可用。mihomo 会自行管理路由与防火墙规则，并在退出时清理"
	return s
}

// checkTProxy 判断 TProxy 模式可用性。
//
// 比 TUN 严格得多：需要 nft 或 iptables 能写 mangle 表、需要 TPROXY 与
// socket 匹配的内核支持、需要真正的 iproute2（busybox 的 ip 不支持
// `ip rule add fwmark`）。
func checkTProxy(r *Report, p probePaths, c commandProbe) ModeStatus {
	s := ModeStatus{Mode: ModeTProxy}

	hasNft := c.lookPath("nft")
	hasIptables := c.lookPath("iptables")
	hasIP := c.lookPath("ip")

	if !hasNft && !hasIptables {
		s.Missing = append(s.Missing, "nft 或 iptables")
	}
	if !hasIP {
		s.Missing = append(s.Missing, "iproute2")
	} else if !isRealIproute2(c) {
		// busybox 的 ip applet 不是 iproute2 的替代品，
		// `ip rule add fwmark ... table ...` 与 `ip route add local` 都不支持
		s.Missing = append(s.Missing, "iproute2（当前是 busybox 内置的 ip）")
	}

	mods := readFileTrim(p.procModules)
	// 模块可能被编进内核（此时 /proc/modules 里没有），所以查不到只作为
	// 参考而非硬性判据；真正的判据是上面的命令是否存在
	hasTProxyModule := modulePresent(mods, "nft_tproxy") || modulePresent(mods, "xt_TPROXY")

	if len(s.Missing) > 0 {
		s.Reason = "缺少必要工具：" + strings.Join(s.Missing, "、")
		s.InstallHint = installHintFor(r.PackageManager)
		return s
	}

	if !r.CapNetAdmin {
		s.Missing = append(s.Missing, "CAP_NET_ADMIN")
		s.Reason = "缺少 CAP_NET_ADMIN，无法写防火墙规则与策略路由"
		return s
	}

	if r.InContainer && !r.HostNetwork {
		s.Reason = "容器未使用 host 网络，TProxy 规则只作用于容器自身命名空间，无法服务局域网设备"
		return s
	}

	s.Available = true
	if !hasTProxyModule {
		s.Reason = "可用（未在 /proc/modules 见到 TPROXY 模块，若已编进内核可忽略）。" +
			"注意：面板会修改宿主防火墙与路由，首次启用请确保有控制台或物理访问手段"
	} else {
		s.Reason = "可用。注意：面板会修改宿主防火墙与路由，首次启用请确保有控制台或物理访问手段"
	}
	return s
}

// installHintFor 给出补齐依赖的命令。
func installHintFor(pm string) string {
	switch pm {
	case "apk":
		// Alpine 的 iproute2 拆得很细，iproute2-minimal 不够用，要装完整包
		return "apk add --no-cache iptables ip6tables nftables iproute2"
	case "apt-get":
		return "apt-get update && apt-get install -y --no-install-recommends iptables nftables iproute2"
	default:
		return "请用发行版包管理器安装 iptables（或 nftables）与 iproute2"
	}
}

// isRealIproute2 区分 iproute2 与 busybox 的 ip applet。
// iproute2 的 `ip --version` 输出形如 "ip utility, iproute2-6.x"。
func isRealIproute2(c commandProbe) bool {
	out := strings.ToLower(c.version("ip", "--version"))
	return strings.Contains(out, "iproute2")
}

func isCharDevice(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// collectWarnings 汇总不阻塞启用但需要用户知道的问题。
func collectWarnings(r *Report, p probePaths, c commandProbe) []string {
	var w []string

	// 网关模式必须开转发，否则局域网设备的流量到不了内核
	if readFileTrim(p.sysctlIPForward) == "0" {
		w = append(w, "net.ipv4.ip_forward 为 0，作为局域网网关时需要开启："+
			"sysctl -w net.ipv4.ip_forward=1（持久化写 /etc/sysctl.d/）")
	}
	// rp_filter=1（严格）会丢掉 TPROXY 打标后回环的包；Debian/Ubuntu 默认 2（宽松）可用
	if readFileTrim(p.sysctlRPFilter) == "1" {
		w = append(w, "net.ipv4.conf.all.rp_filter 为 1（严格反向路径校验），"+
			"会导致 TProxy 丢包，建议设为 0 或 2")
	}
	if b := iptablesBackend(c); b == "legacy" && c.lookPath("nft") {
		w = append(w, "iptables 是 legacy 后端但系统同时装有 nft，"+
			"两套规则互不可见，容易出现规则存在却不生效")
	}
	if r.InContainer && r.HostNetwork {
		// host 网络下容器内 sysctl 改的是宿主，docker 也会拒绝 --sysctl
		w = append(w, "容器使用 host 网络，sysctl 需在宿主机上设置（容器内修改会被拒绝或影响宿主）")
	}
	// DNS 劫持常把 mihomo 的 dns.listen 放在 53 端口，非 root 需要这个 capability
	if !r.Root {
		eff, _ := readCapabilities(p.procStatus)
		if !hasCap(eff, capNetBindService) {
			w = append(w, "未持有 CAP_NET_BIND_SERVICE，若要让 mihomo 直接监听 53 端口做 DNS 劫持会失败，"+
				"可改用 1053 等高位端口并用防火墙重定向")
		}
	}
	return w
}

// iptablesBackend 返回 iptables 的后端类型：nf_tables / legacy / 空（探测不到）。
// 同一台机器上混用两种后端是常见故障：规则看着存在却永不匹配。
func iptablesBackend(c commandProbe) string {
	out := strings.ToLower(c.version("iptables", "--version"))
	switch {
	case strings.Contains(out, "nf_tables"):
		return "nf_tables"
	case strings.Contains(out, "legacy"):
		return "legacy"
	default:
		return ""
	}
}

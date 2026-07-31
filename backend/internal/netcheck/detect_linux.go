//go:build linux

package netcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// probePaths 把所有被探测的路径收成一处，测试可替换成临时目录里的假数据。
// 生产代码只用 defaultProbePaths。
type probePaths struct {
	procStatus      string // /proc/self/status，读 CapEff/CapBnd
	procModules     string // /proc/modules，查内核模块
	osRelease       string // /etc/os-release，判发行版
	kernelRelease   string // /proc/sys/kernel/osrelease
	devNetTun       string // /dev/net/tun
	devTun          string // /dev/tun，sing-tun 的备选路径
	sysClassMiscTun string // /sys/class/misc/tun，模块内建时也存在
	dockerEnv       string // /.dockerenv
	procOneCgroup   string // /proc/1/cgroup
	selfNetNS       string // /proc/self/ns/net
	oneNetNS        string // /proc/1/ns/net
	sysctlIPForward string
	sysctlRPFilter  string
	// sysctlConfDir 是 /proc/sys/net/ipv4/conf，用于枚举每个网卡的 rp_filter。
	// 内核对某网卡取 max(all, <该网卡>)，只看 all 会漏掉"all 已宽松但网卡仍严格"
	// 的情况——那种机器上 TProxy 依然丢包。
	sysctlConfDir string
	// resolvConf 是 /etc/resolv.conf，用于发现回环 DNS stub（systemd-resolved）
	resolvConf string
	// procNetIPv6Route 是 /proc/net/ipv6_route，用于判断有没有 v6 默认路由。
	// 用 /proc 而不是执行 `ip -6 route`：探测路径要保持只读且不依赖外部命令，
	// 而这台机器可能压根没装 iproute2（那正是 TProxy 不可用的原因之一）。
	procNetIPv6Route string
	// procNetIfInet6 是 /proc/net/if_inet6，用于判断有没有全局 v6 地址
	procNetIfInet6 string
}

func defaultProbePaths() probePaths {
	return probePaths{
		procStatus:       "/proc/self/status",
		procModules:      "/proc/modules",
		osRelease:        "/etc/os-release",
		kernelRelease:    "/proc/sys/kernel/osrelease",
		devNetTun:        "/dev/net/tun",
		devTun:           "/dev/tun",
		sysClassMiscTun:  "/sys/class/misc/tun",
		dockerEnv:        "/.dockerenv",
		procOneCgroup:    "/proc/1/cgroup",
		selfNetNS:        "/proc/self/ns/net",
		oneNetNS:         "/proc/1/ns/net",
		sysctlIPForward:  "/proc/sys/net/ipv4/ip_forward",
		sysctlRPFilter:   "/proc/sys/net/ipv4/conf/all/rp_filter",
		sysctlConfDir:    "/proc/sys/net/ipv4/conf",
		resolvConf:       "/etc/resolv.conf",
		procNetIPv6Route: "/proc/net/ipv6_route",
		procNetIfInet6:   "/proc/net/if_inet6",
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

	// sysctl 原始值在这里一次读出并留在 Report 上：Warnings 需要它们生成
	// 人话提示，Provisioner 需要它们判断哪几项真的要改。此前只在
	// collectWarnings 里读完即丢，准备逻辑只能重新读一遍。
	r.SysctlIPForward = readFileTrim(p.sysctlIPForward)
	r.SysctlRPFilter = readFileTrim(p.sysctlRPFilter)
	r.RPFilterStrictIfaces = strictRPFilterIfaces(p.sysctlConfDir)

	// 这两项服务于"本机流量被接管"这条路径：前者决定要不要下发 v6 规则，
	// 后者决定本机的域名分流能不能生效。都在探测阶段一次读出，
	// 避免下发规则时再读一遍（两次读之间状态可能已经变了）。
	r.HasIPv6Egress = hasIPv6Egress(p)
	r.DNSLoopbackStub = loopbackDNSStub(readFileTrim(p.resolvConf))

	r.Modes = []ModeStatus{
		checkTUN(r, p, c),
		checkTProxy(r, p, c),
	}
	r.Warnings = collectWarnings(r, p, c)
	return r
}

// strictRPFilterIfaces 列出 rp_filter 仍为 1（严格）的网卡。
//
// 为什么要逐网卡看：内核判定用的是 max(conf.all.rp_filter,
// conf.<iface>.rp_filter)，所以把 all 改成 2 之后，任何仍是 1 的网卡上
// 依然按严格模式丢包。只改 all 就以为搞定，是这块最容易踩空的地方。
//
// 跳过 all 与 default：前者是聚合项，后者只影响将来新建的网卡，
// 两者都由调用方单独处理。lo 也跳过，回环不参与反向路径校验。
func strictRPFilterIfaces(confDir string) []string {
	if confDir == "" {
		return nil
	}
	entries, err := os.ReadDir(confDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == "all" || name == "default" || name == "lo" {
			continue
		}
		if readFileTrim(filepath.Join(confDir, name, "rp_filter")) == "1" {
			out = append(out, name)
		}
	}
	return out
}

// hasIPv6Egress 判断宿主是否真的能往外走 IPv6。
//
// 两个条件都要满足：有全局单播地址、且有非 lo 的默认路由。少任何一个都
// 意味着 v6 流量出不去，此时不该下发 v6 的 TProxy 规则与策略路由。
func hasIPv6Egress(p probePaths) bool {
	return hasGlobalIPv6Addr(readFileTrim(p.procNetIfInet6)) &&
		hasIPv6DefaultRoute(readFileTrim(p.procNetIPv6Route))
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

// isRealIproute2 区分 iproute2 与 busybox 的 ip applet。
//
// 必须用短选项 `-V`：iproute2 的 `ip` 不认 `--version`，会把它归一化成
// `-version` 再当未知选项拒绝——以退出码 255 失败并往 stderr 打
// `Option "-version" is unknown, try "ip -help".`。两个相隔很远的版本上
// 都实测到同样行为（Ubuntu 24.04 的 6.1.0、Alpine 3.21 的 6.11.0），
// 所以这不是某个版本的个例。而 `-V` 稳定输出 "ip utility, iproute2-<版本>"。
//
// 原实现只试 `--version`，探测函数用 CombinedOutput 把 stderr 也收进来，
// 拿到的是那句报错文字，不含 "iproute2"，于是把真正装着的 iproute2
// 误判成 busybox 的 ip applet，进而把 TProxy 模式整体判定为不可用。
// 这是真机测试发现的，见 AuroraMihomo-Transparent-Proxy-Test-Report.md 第 4 节；
// 此前的单元测试用不区分参数的 mock 返回版本串，测不出"参数传错"这类问题。
//
// 仍保留对 `--version` 的回落尝试：busybox 的 ip 两种写法都给不出
// "iproute2" 字样（已在容器里实测），多试一次不会造成误判，
// 而万一将来某个打包反过来只认长选项，这一层能兜住。
func isRealIproute2(c commandProbe) bool {
	if strings.Contains(strings.ToLower(c.version("ip", "-V")), "iproute2") {
		return true
	}
	return strings.Contains(strings.ToLower(c.version("ip", "--version")), "iproute2")
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
	if r.SysctlIPForward == "0" {
		w = append(w, "net.ipv4.ip_forward 为 0，作为局域网网关时需要开启："+
			"sysctl -w net.ipv4.ip_forward=1（持久化写 /etc/sysctl.d/）")
	}
	// rp_filter=1（严格）会丢掉 TPROXY 打标后回环的包；Debian/Ubuntu 默认 2（宽松）可用
	if r.SysctlRPFilter == "1" {
		msg := "net.ipv4.conf.all.rp_filter 为 1（严格反向路径校验），" +
			"会导致 TProxy 丢包，建议设为 0 或 2"
		// 内核取 max(all, 网卡)，只改 all 在这些网卡上不生效——
		// 不点明的话用户改完 all 仍然丢包，且很难想到是这个原因
		if len(r.RPFilterStrictIfaces) > 0 {
			msg += "；注意内核取 all 与各网卡的最大值，以下网卡也需一并调整：" +
				strings.Join(r.RPFilterStrictIfaces, "、")
		}
		w = append(w, msg)
	}
	if b := iptablesBackend(c); b == "legacy" && c.lookPath("nft") {
		w = append(w, "iptables 是 legacy 后端但系统同时装有 nft，"+
			"两套规则互不可见，容易出现规则存在却不生效")
	}
	// 本机流量在两种模式下都会被接管（TProxy 靠 output 链 + 策略路由，
	// TUN 靠 auto-route），所以这两条与"本机能不能正确分流"直接相关的
	// 情况必须让用户知道——否则表现是"开着但本机行为不如预期"，很难自查。
	if r.DNSLoopbackStub != "" {
		w = append(w, "本机 DNS 指向回环地址 "+r.DNSLoopbackStub+
			"（常见于 systemd-resolved），本机自身的 DNS 查询不会被劫持，"+
			"域名类分流规则对本机流量不生效（局域网设备不受影响）。"+
			"需要本机也按域名分流时，可把 /etc/resolv.conf 的 nameserver 改为"+
			"非回环地址，或关闭 systemd-resolved 的 DNSStubListener")
	}
	if !r.HasIPv6Egress {
		w = append(w, "未探测到 IPv6 出网能力（缺全局 IPv6 地址或 IPv6 默认路由），"+
			"透明代理只会接管 IPv4 流量。这是刻意的：下发 IPv6 规则却没有对应的"+
			"IPv6 策略路由会让 IPv6 流量被标记后无路可走，比不接管更糟")
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

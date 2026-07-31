//go:build linux

package netcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv 在临时目录里搭出一套假的 /proc 与 /etc 文件，
// 让探测逻辑可以在任何机器上被确定性地测试（包括 CI 容器里）。
type fakeEnv struct {
	dir   string
	paths probePaths
	cmd   commandProbe
	// present 记录哪些命令视为存在
	present map[string]bool
	// versions 记录命令的版本输出，对所有 flag 一视同仁
	versions map[string]string
	// versionArgs 按"命令 参数"精确匹配版本输出，用于区分同一命令在不同
	// flag 下的不同表现（例如 ip -V 与 ip --version 行为不一致的场景）。
	// 优先于 versions；未命中时才回落到 versions。
	versionArgs map[string]string
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	dir := t.TempDir()
	f := &fakeEnv{
		dir:         dir,
		present:     map[string]bool{},
		versions:    map[string]string{},
		versionArgs: map[string]string{},
	}
	// 所有路径都指向临时目录下的同名文件；不写的文件即"不存在"
	f.paths = probePaths{
		procStatus:       filepath.Join(dir, "status"),
		procModules:      filepath.Join(dir, "modules"),
		osRelease:        filepath.Join(dir, "os-release"),
		kernelRelease:    filepath.Join(dir, "osrelease"),
		devNetTun:        filepath.Join(dir, "dev-net-tun"),
		devTun:           filepath.Join(dir, "dev-tun"),
		sysClassMiscTun:  filepath.Join(dir, "sys-class-misc-tun"),
		dockerEnv:        filepath.Join(dir, "dockerenv"),
		procOneCgroup:    filepath.Join(dir, "one-cgroup"),
		selfNetNS:        filepath.Join(dir, "self-net"),
		oneNetNS:         filepath.Join(dir, "one-net"),
		sysctlIPForward:  filepath.Join(dir, "ip_forward"),
		sysctlRPFilter:   filepath.Join(dir, "rp_filter"),
		resolvConf:       filepath.Join(dir, "resolv.conf"),
		procNetIPv6Route: filepath.Join(dir, "ipv6_route"),
		procNetIfInet6:   filepath.Join(dir, "if_inet6"),
	}
	f.cmd = commandProbe{
		lookPath: func(name string) bool { return f.present[name] },
		version: func(name string, args ...string) string {
			key := name + " " + strings.Join(args, " ")
			if v, ok := f.versionArgs[key]; ok {
				return v
			}
			return f.versions[name]
		},
	}
	return f
}

func (f *fakeEnv) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写假文件失败: %v", err)
	}
}

// haveTools 把 TProxy 需要的工具都标记为可用
func (f *fakeEnv) haveTools() {
	f.present["nft"] = true
	f.present["iptables"] = true
	f.present["ip"] = true
	f.versions["ip"] = "ip utility, iproute2-6.1.0"
	f.versions["iptables"] = "iptables v1.8.9 (nf_tables)"
}

// haveTunDevice 让探测认为 TUN 设备存在。
//
// 必须指向一个真实的字符设备：checkTUN 用 isCharDevice 判定，普通文件
// 过不了这一关。此前这里写一个普通文件到 sysClassMiscTun 就以为模拟出了
// 设备，结果探测一路走到"设备缺失"分支，那些本该校验 capability 提示与
// 桥接网络范围提示的用例全都在断言错误的分支上——测试是红的，且红得
// 没有意义（失败信息指向设备缺失，与用例意图无关）。
//
// /dev/null 在任何 Linux 上都是字符设备（CI 容器里也是），拿它当替身
// 既真实又不需要特权。
func (f *fakeEnv) haveTunDevice() {
	f.paths.devNetTun = "/dev/null"
}

// 容器里最容易踩的坑：docker 的 cap_add 只填充 bounding 集，
// 非 root 进程的 effective 集仍是空的，于是"给了却拿不到"。
// 提示必须能区分这两种情况，否则用户会以为 compose 配错了。
func TestTUNDistinguishesBoundingOnlyCapability(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapBnd:\t0000000000001000\nCapEff:\t0000000000000000\n")
	f.write(t, f.paths.dockerEnv, "")
	f.write(t, f.paths.procModules, "tun 57344 2 - Live 0x0000000000000000\n")
	// 设备必须是真的字符设备，否则探测会走"设备缺失"分支，
	// 下面对 capability 提示的断言就落在了错误的分支上
	f.haveTunDevice()

	r := detectWith(f.paths, f.cmd, 1000) // 非 root
	tun := r.ModeStatusOf(ModeTUN)

	if tun.Available {
		t.Error("非 root 且 effective 集无 NET_ADMIN 时 TUN 不应可用")
	}
	if !r.CapNetAdminBounding {
		t.Error("应识别出 bounding 集里有 NET_ADMIN")
	}
	// 关键：提示要点明是"给了但没生效"，并给出 user: "0:0" 与
	// no-new-privileges 这两个具体动作
	for _, kw := range []string{"bounding", "no-new-privileges"} {
		if !strings.Contains(tun.Reason, kw) {
			t.Errorf("提示应包含 %q，实际: %s", kw, tun.Reason)
		}
	}
}

// root 运行时应直接判定持有 capability，不必解析 CapEff
func TestRootImpliesCapabilities(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000000000\n")
	f.write(t, f.paths.sysClassMiscTun, "")
	f.write(t, f.paths.procModules, "tun 57344 2 - Live 0x0\n")

	r := detectWith(f.paths, f.cmd, 0)
	if !r.CapNetAdmin || !r.CapNetRaw {
		t.Error("root 应视为持有 NET_ADMIN 与 NET_RAW")
	}
}

// 容器内缺 /dev/net/tun 时，提示要给出 devices 映射的具体写法
func TestTUNMissingDeviceInContainer(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.write(t, f.paths.dockerEnv, "")

	r := detectWith(f.paths, f.cmd, 0)
	tun := r.ModeStatusOf(ModeTUN)
	if tun.Available {
		t.Error("无 TUN 设备时不应可用")
	}
	if !strings.Contains(tun.Reason, "/dev/net/tun") {
		t.Errorf("提示应说明缺少设备，实际: %s", tun.Reason)
	}
}

// busybox 的 ip applet 不支持 `ip rule add fwmark`，必须识别出来
func TestTProxyRejectsBusyboxIP(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.present["nft"] = true
	f.present["iptables"] = true
	f.present["ip"] = true
	f.versions["ip"] = "BusyBox v1.36.1 (2023-11-07 18:53:09 UTC) multi-call binary."

	r := detectWith(f.paths, f.cmd, 0)
	tp := r.ModeStatusOf(ModeTProxy)
	if tp.Available {
		t.Error("busybox 的 ip 不能用于策略路由，TProxy 不应可用")
	}
	if !strings.Contains(strings.Join(tp.Missing, " "), "iproute2") {
		t.Errorf("应指出缺少 iproute2，实际 Missing=%v", tp.Missing)
	}
}

// 回归测试：真机测试（Ubuntu 24.04，iproute2 6.1.0）发现 `ip --version`
// 在这个版本上不识别长选项，会报 "Option "-version" is unknown"，
// 若探测代码只试这一种写法，会把真实装着的 iproute2 误判成 busybox，
// TProxy 因此被错误拒绝。见 detect_linux.go 的 isRealIproute2。
func TestTProxyAcceptsIproute2WhenLongFlagUnsupported(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.present["nft"] = true
	f.present["iptables"] = true
	f.present["ip"] = true
	// `-V` 给出正常输出，`--version` 给出 iproute2 6.1.0 在真机上实测到的
	// 错误提示（不含 "iproute2" 字样）——必须优先信任 -V 的结果
	f.versionArgs["ip -V"] = "ip utility, iproute2-6.1.0, libbpf 1.3.0"
	f.versionArgs["ip --version"] = `Option "-version" is unknown, try "ip -help".`

	r := detectWith(f.paths, f.cmd, 0)
	tp := r.ModeStatusOf(ModeTProxy)
	if !tp.Available {
		t.Errorf("真实 iproute2 应判定 TProxy 可用，实际 Reason=%s Missing=%v", tp.Reason, tp.Missing)
	}
}

// 缺工具时要按发行版给出可直接复制的安装命令
func TestTProxyInstallHintPerDistro(t *testing.T) {
	cases := []struct {
		distro string
		pm     string
		want   string
	}{
		{"alpine", "apk", "apk add"},
		{"debian", "apt-get", "apt-get install"},
		{"ubuntu", "apt-get", "apt-get install"},
	}
	for _, c := range cases {
		t.Run(c.distro, func(t *testing.T) {
			f := newFakeEnv(t)
			f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
			f.write(t, f.paths.osRelease, "ID="+c.distro+"\nVERSION_ID=\"1\"\n")
			f.present[c.pm] = true // 只有包管理器，缺 nft/iptables/ip

			r := detectWith(f.paths, f.cmd, 0)
			if r.Distro != c.distro {
				t.Errorf("发行版识别错误: %q", r.Distro)
			}
			tp := r.ModeStatusOf(ModeTProxy)
			if !strings.Contains(tp.InstallHint, c.want) {
				t.Errorf("安装提示应含 %q，实际: %s", c.want, tp.InstallHint)
			}
		})
	}
}

// 桥接网络的容器里改路由只影响容器自身，服务不了局域网设备
func TestBridgeNetworkContainerWarnsOnScope(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.write(t, f.paths.dockerEnv, "")
	f.haveTunDevice()
	f.write(t, f.paths.procModules, "tun 1 1 - Live 0x0\n")
	f.haveTools()
	// 两个 netns 符号链接指向不同目标 → 非 host 网络
	if err := os.Symlink("net:[4026531840]", f.paths.selfNetNS); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if err := os.Symlink("net:[4026532000]", f.paths.oneNetNS); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	r := detectWith(f.paths, f.cmd, 0)
	if r.HostNetwork {
		t.Error("不同 netns 应判为非 host 网络")
	}
	// TUN 仍可用但要说明范围受限
	tun := r.ModeStatusOf(ModeTUN)
	if !tun.Available {
		t.Error("桥接网络下 TUN 仍可用（只是范围受限）")
	}
	if !strings.Contains(tun.Reason, "host") {
		t.Errorf("应提示需要 host 网络，实际: %s", tun.Reason)
	}
	// TProxy 在桥接网络下没有意义
	if r.ModeStatusOf(ModeTProxy).Available {
		t.Error("桥接网络下 TProxy 不应可用")
	}
}

func TestWarnsOnIPForwardAndRPFilter(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.write(t, f.paths.sysctlIPForward, "0")
	f.write(t, f.paths.sysctlRPFilter, "1")
	f.haveTools()

	r := detectWith(f.paths, f.cmd, 0)
	joined := strings.Join(r.Warnings, "\n")
	if !strings.Contains(joined, "ip_forward") {
		t.Error("应警告 ip_forward 未开启")
	}
	if !strings.Contains(joined, "rp_filter") {
		t.Error("应警告 rp_filter 严格模式会导致 TProxy 丢包")
	}
}

// legacy 与 nft 后端混用时规则看着存在却永不匹配，属高价值警告
func TestWarnsOnMixedIptablesBackend(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.present["iptables"] = true
	f.present["nft"] = true
	f.present["ip"] = true
	f.versions["ip"] = "ip utility, iproute2-6.1.0"
	f.versions["iptables"] = "iptables v1.8.7 (legacy)"

	r := detectWith(f.paths, f.cmd, 0)
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "legacy") {
		t.Errorf("应警告后端混用，实际 warnings=%v", r.Warnings)
	}
}

// 本机流量在两种模式下都会被接管，所以"本机 DNS 没被劫持"必须告警：
// 否则表现是本机只按 IP 分流、域名规则静默失效，用户很难自查到原因。
func TestWarnsOnLoopbackDNSStub(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.write(t, f.paths.resolvConf, "nameserver 127.0.0.53\noptions edns0\n")
	f.haveTools()

	r := detectWith(f.paths, f.cmd, 0)
	if r.DNSLoopbackStub != "127.0.0.53" {
		t.Errorf("应探测到回环 DNS stub，实际 %q", r.DNSLoopbackStub)
	}
	joined := strings.Join(r.Warnings, "\n")
	if !strings.Contains(joined, "127.0.0.53") {
		t.Errorf("告警里应带上具体地址便于用户对照，实际 warnings=%v", r.Warnings)
	}
	// 必须说清影响范围：局域网设备不受这个限制，混为一谈会让用户以为
	// 整个透明代理的域名分流都坏了
	if !strings.Contains(joined, "局域网设备不受影响") {
		t.Errorf("告警应说明局域网设备不受影响，实际 warnings=%v", r.Warnings)
	}
}

func TestNoLoopbackDNSWarningWhenResolverIsExternal(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
	f.write(t, f.paths.resolvConf, "nameserver 192.168.1.1\n")
	f.haveTools()

	r := detectWith(f.paths, f.cmd, 0)
	if r.DNSLoopbackStub != "" {
		t.Errorf("非回环 DNS 不该被判为 stub，实际 %q", r.DNSLoopbackStub)
	}
	if strings.Contains(strings.Join(r.Warnings, "\n"), "DNS 指向回环") {
		t.Errorf("不该产生回环 DNS 告警，实际 warnings=%v", r.Warnings)
	}
}

// v6 出网能力决定要不要下发 v6 规则。这里同时校验探测结论与告警：
// 没有 v6 能力时要告警（说明只接管 v4），有能力时不该多此一举。
func TestIPv6EgressDetection(t *testing.T) {
	globalAddr := "24010db8000000000000000000000001 02 40 00 00 ens18\n"
	defaultRoute := strings.Repeat("0", 32) + " 00 " + strings.Repeat("0", 32) +
		" 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000003 ens18\n"

	cases := []struct {
		name       string
		addr       string
		route      string
		wantEgress bool
	}{
		{"全局地址与默认路由都有", globalAddr, defaultRoute, true},
		{"有地址但没默认路由", globalAddr, "", false},
		{"有默认路由但没全局地址", "", defaultRoute, false},
		{"两者都没有", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEnv(t)
			f.write(t, f.paths.procStatus, "CapEff:\t0000000000001000\n")
			f.haveTools()
			if c.addr != "" {
				f.write(t, f.paths.procNetIfInet6, c.addr)
			}
			if c.route != "" {
				f.write(t, f.paths.procNetIPv6Route, c.route)
			}

			r := detectWith(f.paths, f.cmd, 0)
			if r.HasIPv6Egress != c.wantEgress {
				t.Errorf("HasIPv6Egress = %v，期望 %v", r.HasIPv6Egress, c.wantEgress)
			}
			hasWarn := strings.Contains(strings.Join(r.Warnings, "\n"), "IPv6 出网能力")
			if c.wantEgress && hasWarn {
				t.Errorf("有 v6 出网能力时不该告警，实际 warnings=%v", r.Warnings)
			}
			if !c.wantEgress && !hasWarn {
				t.Errorf("无 v6 出网能力时应告警只接管 IPv4，实际 warnings=%v", r.Warnings)
			}
		})
	}
}

func TestValidMode(t *testing.T) {
	for _, ok := range []string{"tun", "tproxy", "off"} {
		if !ValidMode(ok) {
			t.Errorf("%q 应是合法模式", ok)
		}
	}
	for _, bad := range []string{"", "redir", "TUN", "on"} {
		if ValidMode(bad) {
			t.Errorf("%q 不应是合法模式", bad)
		}
	}
}

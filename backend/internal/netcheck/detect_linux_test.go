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
	// versions 记录命令的版本输出
	versions map[string]string
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	dir := t.TempDir()
	f := &fakeEnv{
		dir:      dir,
		present:  map[string]bool{},
		versions: map[string]string{},
	}
	// 所有路径都指向临时目录下的同名文件；不写的文件即"不存在"
	f.paths = probePaths{
		procStatus:          filepath.Join(dir, "status"),
		procModules:         filepath.Join(dir, "modules"),
		osRelease:           filepath.Join(dir, "os-release"),
		kernelRelease:       filepath.Join(dir, "osrelease"),
		devNetTun:           filepath.Join(dir, "dev-net-tun"),
		devTun:              filepath.Join(dir, "dev-tun"),
		sysClassMiscTun:     filepath.Join(dir, "sys-class-misc-tun"),
		dockerEnv:           filepath.Join(dir, "dockerenv"),
		procOneCgroup:       filepath.Join(dir, "one-cgroup"),
		selfNetNS:           filepath.Join(dir, "self-net"),
		oneNetNS:            filepath.Join(dir, "one-net"),
		sysctlIPForward:     filepath.Join(dir, "ip_forward"),
		sysctlRPFilter:      filepath.Join(dir, "rp_filter"),
		sysctlRouteLocalnet: filepath.Join(dir, "route_localnet"),
	}
	f.cmd = commandProbe{
		lookPath: func(name string) bool { return f.present[name] },
		version:  func(name string, _ ...string) string { return f.versions[name] },
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

// 容器里最容易踩的坑：docker 的 cap_add 只填充 bounding 集，
// 非 root 进程的 effective 集仍是空的，于是"给了却拿不到"。
// 提示必须能区分这两种情况，否则用户会以为 compose 配错了。
func TestTUNDistinguishesBoundingOnlyCapability(t *testing.T) {
	f := newFakeEnv(t)
	f.write(t, f.paths.procStatus, "CapBnd:\t0000000000001000\nCapEff:\t0000000000000000\n")
	f.write(t, f.paths.dockerEnv, "")
	f.write(t, f.paths.procModules, "tun 57344 2 - Live 0x0000000000000000\n")
	// 设备存在（用普通文件模拟不了字符设备，所以走 sysClassMiscTun 分支）
	f.write(t, f.paths.sysClassMiscTun, "")

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
	f.write(t, f.paths.sysClassMiscTun, "")
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

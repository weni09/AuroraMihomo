package netcheck

import (
	"os"
	"strings"
	"testing"
)

// 这些用例不带平台限制：解析逻辑本身与运行平台无关，
// 放在这里才能在开发机与 CI（未必是 Linux）上真正跑到。

// capability 位的解析是容器场景的关键判据，位号写错会让"能不能开透明代理"
// 的结论完全相反。
func TestReadCapabilitiesParsesEffAndBnd(t *testing.T) {
	// 取自真实 /proc/self/status 的片段格式
	dir := t.TempDir()
	path := dir + "/status"
	content := "Name:\tapi\n" +
		"Uid:\t0\t0\t0\t0\n" +
		"CapInh:\t0000000000000000\n" +
		"CapPrm:\t0000000000003000\n" +
		"CapEff:\t0000000000001000\n" + // 仅 NET_ADMIN(bit12)
		"CapBnd:\t0000000000003000\n" + // NET_ADMIN + NET_RAW(bit13)
		"CapAmb:\t0000000000000000\n"
	writeFile(t, path, content)

	eff, bnd := readCapabilities(path)

	if !hasCap(eff, capNetAdmin) {
		t.Errorf("CapEff 应含 NET_ADMIN，实际 %#x", eff)
	}
	if hasCap(eff, capNetRaw) {
		t.Errorf("CapEff 不应含 NET_RAW，实际 %#x", eff)
	}
	// bounding 里有但 effective 里没有 —— 正是 docker cap_add + 非 root 的形态
	if !hasCap(bnd, capNetRaw) {
		t.Errorf("CapBnd 应含 NET_RAW，实际 %#x", bnd)
	}
}

func TestReadCapabilitiesMissingFile(t *testing.T) {
	eff, bnd := readCapabilities(t.TempDir() + "/nope")
	if eff != 0 || bnd != 0 {
		t.Errorf("读不到时应返回 0/0，实际 %#x/%#x", eff, bnd)
	}
}

// capability 位号必须与内核定义一致，写错会导致判断完全错位
func TestCapabilityBitNumbers(t *testing.T) {
	cases := []struct {
		name string
		bit  uint
		want uint64
	}{
		{"CAP_NET_BIND_SERVICE", capNetBindService, 1 << 10},
		{"CAP_NET_ADMIN", capNetAdmin, 1 << 12},
		{"CAP_NET_RAW", capNetRaw, 1 << 13},
	}
	for _, c := range cases {
		if got := uint64(1) << c.bit; got != c.want {
			t.Errorf("%s 位号 %d 得到掩码 %#x，期望 %#x", c.name, c.bit, got, c.want)
		}
		if !hasCap(c.want, c.bit) {
			t.Errorf("%s: hasCap 未能识别自身掩码", c.name)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name, content, wantID, wantLike string
	}{
		{
			name:     "alpine",
			content:  "NAME=\"Alpine Linux\"\nID=alpine\nVERSION_ID=3.21.0\n",
			wantID:   "alpine",
			wantLike: "",
		},
		{
			name:     "ubuntu 带 ID_LIKE",
			content:  "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n",
			wantID:   "ubuntu",
			wantLike: "debian",
		},
		{
			name:     "单引号与空行",
			content:  "\nID='debian'\n\nVERSION_ID=\"12\"\n",
			wantID:   "debian",
			wantLike: "",
		},
		{
			name:    "内容为空",
			content: "",
			wantID:  "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, like := parseOSRelease(c.content)
			if id != c.wantID {
				t.Errorf("ID = %q，期望 %q", id, c.wantID)
			}
			if like != c.wantLike {
				t.Errorf("ID_LIKE = %q，期望 %q", like, c.wantLike)
			}
		})
	}
}

func TestCgroupLooksContainerized(t *testing.T) {
	cases := map[string]bool{
		"0::/init.scope":                 false,
		"0::/system.slice/ssh.service":   false,
		"12:pids:/docker/3f6a...":        true,
		"0::/kubepods/besteffort/pod123": true,
		"1:name=systemd:/containerd/abc": true,
		"11:devices:/lxc/mycontainer":    true,
	}
	for content, want := range cases {
		if got := cgroupLooksContainerized(content); got != want {
			t.Errorf("cgroupLooksContainerized(%q) = %v，期望 %v", content, got, want)
		}
	}
}

// 首列精确匹配，避免把 nft_tproxy 误判成 tun 之类的子串命中
func TestModulePresentMatchesFirstColumnExactly(t *testing.T) {
	// /proc/modules 的真实格式：模块名 体积 引用数 依赖 状态 地址
	content := "nft_tproxy 12288 1 - Live 0x0000000000000000\n" +
		"tun 57344 3 - Live 0x0000000000000000\n" +
		"xt_socket 16384 1 - Live 0x0000000000000000\n"

	for _, name := range []string{"tun", "nft_tproxy", "xt_socket"} {
		if !modulePresent(content, name) {
			t.Errorf("应识别到模块 %q", name)
		}
	}
	// 子串不算：tproxy 是 nft_tproxy 的子串，但不是独立模块
	for _, name := range []string{"tproxy", "socket", "nf_tables", ""} {
		if modulePresent(content, name) {
			t.Errorf("不应把 %q 判为已加载模块", name)
		}
	}
	if modulePresent("", "tun") {
		t.Error("内容为空时不应报告模块存在")
	}
}

// writeFile 写测试用的假 /proc 文件。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s 失败: %v", path, err)
	}
}

// 回环 DNS stub 必须被识别出来：本机 DNS 劫持规则刻意排除回环目标，
// 所以这类机器上本机的域名分流不生效，必须如实告警而不是假装劫持了。
func TestLoopbackDNSStubDetection(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			// systemd-resolved 的典型形态，也是最常见的一种
			name:    "systemd-resolved 的 stub 地址",
			content: "nameserver 127.0.0.53\noptions edns0 trust-ad\n",
			want:    "127.0.0.53",
		},
		{
			name:    "dnsmasq 之类监听在 127.0.0.1",
			content: "nameserver 127.0.0.1\n",
			want:    "127.0.0.1",
		},
		{
			name:    "普通的局域网 DNS 不算 stub",
			content: "nameserver 192.168.1.1\nnameserver 8.8.8.8\n",
			want:    "",
		},
		{
			// 混合情形：只要有一个回环项就会命中，因为解析器按顺序用第一个
			name:    "回环与外部混列时报告回环那个",
			content: "nameserver 127.0.0.53\nnameserver 8.8.8.8\n",
			want:    "127.0.0.53",
		},
		{
			// 被注释掉的配置不生效，算进去会产生假告警
			name:    "注释行不应被采纳",
			content: "# nameserver 127.0.0.53\n; nameserver 127.0.0.1\nnameserver 1.1.1.1\n",
			want:    "",
		},
		{
			name:    "search/domain 等其它指令不参与判断",
			content: "search example.com\ndomain example.com\nnameserver 10.0.0.1\n",
			want:    "",
		},
		{name: "空文件", content: "", want: ""},
		{
			name:    "IPv6 回环同样算 stub",
			content: "nameserver ::1\n",
			want:    "::1",
		},
		{
			name:    "格式残缺的行不应导致误判",
			content: "nameserver\nnameserver not-an-ip\nnameserver 8.8.4.4\n",
			want:    "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := loopbackDNSStub(c.content); got != c.want {
				t.Errorf("loopbackDNSStub() = %q，期望 %q", got, c.want)
			}
		})
	}
}

// v6 默认路由的判定决定了要不要下发 v6 规则。判错的代价是不对称的：
// 误判"有"会导致 v6 流量被打标后无路可走（网络不通），
// 误判"没有"只是 v6 不分流，所以宁可漏判不可误判。
func TestHasIPv6DefaultRoute(t *testing.T) {
	zeros := strings.Repeat("0", 32)
	// /proc/net/ipv6_route 的真实列序：目的地址 前缀长度 源地址 源前缀长度
	// 下一跳 度量值 引用数 使用数 标志 设备名
	defaultViaEth := zeros + " 00 " + zeros + " 00 " +
		"fe800000000000000000000000000001 00000400 00000000 00000000 00000003 ens18\n"
	loopbackOnly := zeros + " 00 " + zeros + " 00 " + zeros +
		" ffffffff 00000001 00000000 80200001 lo\n"
	ulaPrefix := "fd000000000000000000000000000000 40 " + zeros + " 00 " + zeros +
		" 00000100 00000000 00000000 00000001 ens18\n"

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"有网卡上的默认路由", defaultViaEth, true},
		{
			// 内核会给回环装一条 ::/0 的 unreachable 路由，算进来会把
			// 没有 v6 出网的机器误判成有
			name:    "只有 lo 上的默认路由不算",
			content: loopbackOnly,
			want:    false,
		},
		{"只有 ULA 前缀路由不算默认路由", ulaPrefix, false},
		{"lo 与真实默认路由并存时算有", loopbackOnly + defaultViaEth, true},
		{"空内容", "", false},
		{"列数不足的残缺行", zeros + " 00\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasIPv6DefaultRoute(c.content); got != c.want {
				t.Errorf("hasIPv6DefaultRoute() = %v，期望 %v", got, c.want)
			}
		})
	}
}

// 只有全局单播地址才意味着能往外走 v6。只有链路本地地址的机器上
// 下发 v6 规则等于制造黑洞。
func TestHasGlobalIPv6Addr(t *testing.T) {
	// /proc/net/if_inet6 列序：地址 接口序号 前缀长度 scope 标志 设备名。
	// scope 00=全局、10=回环、20=链路本地
	global := "24010db8000000000000000000000001 02 40 00 00 ens18\n"
	linkLocal := "fe800000000000000a00270fffe9d2f1 02 40 20 80 ens18\n"
	loopback := "00000000000000000000000000000001 01 80 10 80 lo\n"

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"有全局地址", global, true},
		{"只有链路本地地址", linkLocal, false},
		{"只有回环地址", loopback, false},
		{"链路本地与回环并存但没有全局地址", linkLocal + loopback, false},
		{"全局地址与其它并存", linkLocal + loopback + global, true},
		{"空内容", "", false},
		{"列数不足的残缺行", "2401 02 40\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasGlobalIPv6Addr(c.content); got != c.want {
				t.Errorf("hasGlobalIPv6Addr() = %v，期望 %v", got, c.want)
			}
		})
	}
}

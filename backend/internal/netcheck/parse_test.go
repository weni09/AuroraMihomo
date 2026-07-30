package netcheck

import (
	"os"
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

package netcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestBootPersist 构造一个写向临时目录的 BootPersist（不碰真实 /etc）。
func newTestBootPersist(t *testing.T, initSystem string) (*BootPersist, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	runner := newFakeRunner()
	return &BootPersist{Root: root, Init: initSystem, Runner: runner}, runner, root
}

// sampleBootParams 返回一套确定性参数，覆盖 v6 与 keep 端口。
func sampleBootParams() TProxyParams {
	return TProxyParams{
		TProxyPort: 7893,
		DNSPort:    1053,
		KeepPorts:  []int{22, 8899, 9090},
		EnableIPv6: true,
	}
}

// sampleCustomRules 一条带引号的自定义 iptables 规则，验证 .sh 恢复路径。
var sampleCustomRules = []string{
	`iptables -A INPUT -p tcp --dport 8080 -j ACCEPT`,
	`iptables -A FORWARD -m comment --comment "aurora tproxy test" -j ACCEPT`,
}

func TestStripNFTPrefix(t *testing.T) {
	rules := "add table inet aurora_tproxy\n" +
		"delete table inet aurora_tproxy\n" +
		"table inet aurora_tproxy {\n  chain prerouting {\n  }\n}\n"
	got := StripNFTPrefix(rules)
	if strings.Contains(got, "add table") || strings.Contains(got, "delete table") {
		t.Fatalf("幂等头未被剥离:\n%s", got)
	}
	if !strings.Contains(got, "table inet aurora_tproxy {") {
		t.Fatalf("表定义应保留:\n%s", got)
	}
}

// OpenRC 分支：Write 生成 .nft + .sh + init.d，注册 rc-update
func TestBootPersistWriteOpenRC(t *testing.T) {
	bp, runner, root := newTestBootPersist(t, "openrc")
	ctx := context.Background()

	if err := bp.Write(ctx, sampleBootParams(), sampleCustomRules); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	nftPath := filepath.Join(root, filepath.FromSlash(bootPersistNFTRel))
	shellPath := filepath.Join(root, filepath.FromSlash(bootPersistShellRel))
	initPath := filepath.Join(root, filepath.FromSlash(bootPersistInitRel))
	for _, p := range []string{nftPath, shellPath, initPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("缺少文件 %s: %v", p, err)
		}
	}

	// nft 文件不应带幂等头
	data, _ := os.ReadFile(nftPath)
	if strings.Contains(string(data), "delete table") {
		t.Fatalf("持久化 nft 文件不应含 delete table 幂等头")
	}
	// 命令序列脚本应含策略路由、nft 加载与自定义规则（含引号那条）
	shellData, _ := os.ReadFile(shellPath)
	for _, want := range []string{
		"ip rule add fwmark 1 table 100",
		"ip -6 rule add fwmark 1 table 100",
		`nft -f "$AURORA_NFT"`,
		`--comment \"aurora tproxy test\"`,
		"ip rule del fwmark 1 table 100",
		"nft delete table inet aurora_tproxy",
		"case \"$1\" in",
	} {
		if !strings.Contains(string(shellData), want) {
			t.Fatalf("命令序列脚本缺少 %q:\n%s", want, shellData)
		}
	}
	// OpenRC init 脚本是薄封装：调 .sh，不含内联命令
	initData, _ := os.ReadFile(initPath)
	for _, want := range []string{
		"#!/sbin/openrc-run",
		`/bin/sh "$AURORA_SCRIPT" start`,
		`/bin/sh "$AURORA_SCRIPT" stop`,
	} {
		if !strings.Contains(string(initData), want) {
			t.Fatalf("init 脚本缺少 %q:\n%s", want, initData)
		}
	}
	if strings.Contains(string(initData), "nft -f") {
		t.Fatalf("init 脚本不应内联 nft 命令（应统一在 .sh）:\n%s", initData)
	}
	// 已注册 rc-update add
	if runner.indexOf("rc-update add aurora-tproxy default") < 0 {
		t.Fatalf("未注册 rc-update:\n%s", runner.joined())
	}
}

// systemd 分支：Write 生成 .service + .sh，注册 daemon-reload + enable
func TestBootPersistWriteSystemd(t *testing.T) {
	bp, runner, root := newTestBootPersist(t, "systemd")
	ctx := context.Background()

	if err := bp.Write(ctx, sampleBootParams(), sampleCustomRules); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	unitPath := filepath.Join(root, filepath.FromSlash(bootPersistUnitRel))
	shellPath := filepath.Join(root, filepath.FromSlash(bootPersistShellRel))
	nftPath := filepath.Join(root, filepath.FromSlash(bootPersistNFTRel))
	for _, p := range []string{unitPath, shellPath, nftPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("缺少文件 %s: %v", p, err)
		}
	}
	// systemd 分支不应生成 OpenRC init 脚本
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(bootPersistInitRel))); !os.IsNotExist(err) {
		t.Fatalf("systemd 分支不应生成 init.d 脚本")
	}

	unitData, _ := os.ReadFile(unitPath)
	for _, want := range []string{
		"Type=oneshot",
		"RemainAfterYes=yes",
		"After=network-online.target",
		"Before=auroramihomo.service",
		"ExecStart=/bin/sh /etc/aurora-tproxy.sh start",
		"ExecStop=/bin/sh /etc/aurora-tproxy.sh stop",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(string(unitData), want) {
			t.Fatalf("unit 缺少 %q:\n%s", want, unitData)
		}
	}

	// 命令序列在 .sh，unit 不内联
	shellData, _ := os.ReadFile(shellPath)
	if !strings.Contains(string(shellData), "nft -f") {
		t.Fatalf(".sh 应含 nft 加载命令:\n%s", shellData)
	}
	// 已注册 daemon-reload + enable
	joined := runner.joined()
	if runner.indexOf("systemctl daemon-reload") < 0 {
		t.Fatalf("未执行 daemon-reload:\n%s", joined)
	}
	if runner.indexOf("systemctl enable aurora-tproxy") < 0 {
		t.Fatalf("未执行 systemctl enable:\n%s", joined)
	}
}

// Write 重复执行应幂等（文件整体重写，不堆积）
func TestBootPersistWriteIdempotent(t *testing.T) {
	bp, _, root := newTestBootPersist(t, "openrc")
	ctx := context.Background()
	if err := bp.Write(ctx, sampleBootParams(), nil); err != nil {
		t.Fatalf("首次 Write 失败: %v", err)
	}
	if err := bp.Write(ctx, sampleBootParams(), nil); err != nil {
		t.Fatalf("重复 Write 失败: %v", err)
	}
	nftPath := filepath.Join(root, filepath.FromSlash(bootPersistNFTRel))
	data, _ := os.ReadFile(nftPath)
	if n := strings.Count(string(data), "table inet aurora_tproxy {"); n != 1 {
		t.Fatalf("重复写应只有一份表定义，实际 %d 份", n)
	}
}

// OpenRC：Write 后 Present 为真；Remove 后文件与服务都清掉
func TestBootPersistRemoveOpenRC(t *testing.T) {
	bp, runner, root := newTestBootPersist(t, "openrc")
	ctx := context.Background()

	if bp.Present() {
		t.Fatal("初始不应 Present")
	}
	if err := bp.Write(ctx, sampleBootParams(), nil); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if !bp.Present() {
		t.Fatal("Write 后应 Present")
	}

	if err := bp.Remove(ctx); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	if bp.Present() {
		t.Fatal("Remove 后不应 Present")
	}
	for _, rel := range []string{bootPersistNFTRel, bootPersistShellRel, bootPersistInitRel} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s 应已删除", rel)
		}
	}
	if runner.indexOf("rc-update delete aurora-tproxy") < 0 {
		t.Fatalf("未执行 rc-update delete:\n%s", runner.joined())
	}

	if err := bp.Remove(ctx); err != nil {
		t.Fatalf("重复 Remove 失败: %v", err)
	}
}

// systemd：Remove 走 disable + 删 unit + daemon-reload
func TestBootPersistRemoveSystemd(t *testing.T) {
	bp, runner, root := newTestBootPersist(t, "systemd")
	ctx := context.Background()

	if err := bp.Write(ctx, sampleBootParams(), nil); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if !bp.Present() {
		t.Fatal("Write 后应 Present")
	}

	if err := bp.Remove(ctx); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	if bp.Present() {
		t.Fatal("Remove 后不应 Present")
	}
	unitPath := filepath.Join(root, filepath.FromSlash(bootPersistUnitRel))
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("unit 应已删除")
	}
	joined := runner.joined()
	if runner.indexOf("systemctl disable aurora-tproxy") < 0 {
		t.Fatalf("未执行 systemctl disable:\n%s", joined)
	}
	// daemon-reload 至少出现两次（enable 后 + 删除后）
	if n := strings.Count(joined, "systemctl daemon-reload"); n < 2 {
		t.Fatalf("daemon-reload 应至少 2 次（enable 后与删除后），实际 %d:\n%s", n, joined)
	}
}

// v6 关闭时策略路由只生成 v4 两条
func TestBootPersistIPv4Only(t *testing.T) {
	bp, _, root := newTestBootPersist(t, "openrc")
	p := sampleBootParams()
	p.EnableIPv6 = false
	if err := bp.Write(context.Background(), p, nil); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	shellPath := filepath.Join(root, filepath.FromSlash(bootPersistShellRel))
	data, _ := os.ReadFile(shellPath)
	if strings.Contains(string(data), "ip -6 rule add") {
		t.Fatalf("v6 关闭时不应生成 v6 策略路由")
	}
	if !strings.Contains(string(data), "ip rule add fwmark 1 table 100") {
		t.Fatalf("应含 v4 策略路由")
	}
}

// detectInitSystem 判定：systemctl → systemd；rc-update+rc-service → openrc
func TestDetectInitSystem(t *testing.T) {
	orig := lookPathFn
	t.Cleanup(func() { lookPathFn = orig })

	cases := []struct {
		name     string
		have     []string
		expected string
	}{
		{"仅 systemctl", []string{"systemctl"}, "systemd"},
		{"systemd + openrc 共存时优先 systemd", []string{"systemctl", "rc-update", "rc-service"}, "systemd"},
		{"仅 rc-update 不算 openrc", []string{"rc-update"}, "none"},
		{"rc-update + rc-service", []string{"rc-update", "rc-service"}, "openrc"},
		{"都没有", nil, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			have := map[string]bool{}
			for _, h := range tc.have {
				have[h] = true
			}
			lookPathFn = func(name string) (string, error) {
				if have[name] {
					return "/usr/bin/" + name, nil
				}
				return "", os.ErrNotExist
			}
			if got := DetectInitSystem(); got != tc.expected {
				t.Fatalf("DetectInitSystem() = %q, want %q", got, tc.expected)
			}
		})
	}
}

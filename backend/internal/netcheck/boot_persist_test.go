package netcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestBootPersist 构造一个写向临时目录的 BootPersist（不碰真实 /etc）。
func newTestBootPersist(t *testing.T) (*BootPersist, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	runner := newFakeRunner()
	return &BootPersist{Root: root, Runner: runner}, runner, root
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

// Write 后应生成两个文件（.nft + init.d 脚本）并注册 rc-update
func TestBootPersistWriteCreatesFiles(t *testing.T) {
	bp, runner, root := newTestBootPersist(t)
	ctx := context.Background()

	if err := bp.Write(ctx, sampleBootParams(), []string{"iptables -A INPUT -p tcp --dport 8080 -j ACCEPT"}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	nftPath := filepath.Join(root, filepath.FromSlash(bootPersistNFTRel))
	initPath := filepath.Join(root, filepath.FromSlash(bootPersistInitRel))
	for _, p := range []string{nftPath, initPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("缺少文件 %s: %v", p, err)
		}
	}

	// nft 文件不应带幂等头
	data, _ := os.ReadFile(nftPath)
	if strings.Contains(string(data), "delete table") {
		t.Fatalf("持久化 nft 文件不应含 delete table 幂等头")
	}
	// init 脚本应含策略路由、nft 加载与自定义规则
	initData, _ := os.ReadFile(initPath)
	for _, want := range []string{
		"ip rule add fwmark 1 table 100",
		"ip -6 rule add fwmark 1 table 100",
		`nft -f "$AURORA_NFT"`,
		"iptables -A INPUT -p tcp --dport 8080 -j ACCEPT",
		"ip rule del fwmark 1 table 100",
		"nft delete table inet aurora_tproxy",
	} {
		if !strings.Contains(string(initData), want) {
			t.Fatalf("init 脚本缺少 %q:\n%s", want, initData)
		}
	}

	// 已注册 rc-update add
	if runner.indexOf("rc-update add aurora-tproxy default") < 0 {
		t.Fatalf("未注册 rc-update:\n%s", runner.joined())
	}
}

// Write 重复执行应幂等（文件整体重写，不堆积）
func TestBootPersistWriteIdempotent(t *testing.T) {
	bp, _, root := newTestBootPersist(t)
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

// Write 后 Present 为真；Remove 后文件与服务都清掉，Present 为假
func TestBootPersistRemove(t *testing.T) {
	bp, runner, root := newTestBootPersist(t)
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
	// 文件都应删除
	nftPath := filepath.Join(root, filepath.FromSlash(bootPersistNFTRel))
	if _, err := os.Stat(nftPath); !os.IsNotExist(err) {
		t.Fatalf("nft 文件应已删除: %v", err)
	}
	// rc-update delete 已执行
	if runner.indexOf("rc-update delete aurora-tproxy") < 0 {
		t.Fatalf("未执行 rc-update delete:\n%s", runner.joined())
	}

	// Remove 重复执行幂等
	if err := bp.Remove(ctx); err != nil {
		t.Fatalf("重复 Remove 失败: %v", err)
	}
}

// v6 关闭时策略路由只生成 v4 两条
func TestBootPersistIPv4Only(t *testing.T) {
	bp, _, root := newTestBootPersist(t)
	p := sampleBootParams()
	p.EnableIPv6 = false
	if err := bp.Write(context.Background(), p, nil); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	initPath := filepath.Join(root, filepath.FromSlash(bootPersistInitRel))
	data, _ := os.ReadFile(initPath)
	// stop() 里用 PolicyRouteTeardownCommands 无条件含 v6 拆除命令，属预期；
	// 这里只断言 start() 的 v6 建路由不应出现。
	if strings.Contains(string(data), "ip -6 rule add") {
		t.Fatalf("v6 关闭时不应生成 v6 策略路由")
	}
	if !strings.Contains(string(data), "ip rule add fwmark 1 table 100") {
		t.Fatalf("应含 v4 策略路由")
	}
}

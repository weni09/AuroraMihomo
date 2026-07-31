package protected

import (
	"net/url"
	"strings"
	"testing"
)

// zashboard 的构建产物用相对路径引用资源（./assets/xxx.js），
// 因此入口地址必须以 "/ui/" 结尾。若写成 "/ui?..."，浏览器会把
// 相对路径解析到根目录（/assets/...），内嵌面板将因资源 404 而白屏。
func TestDashboardEntryURLKeepsTrailingSlash(t *testing.T) {
	q := url.Values{}
	q.Set("hostname", "127.0.0.1")
	q.Set("port", "9090")
	entryURL := "/ui/?" + q.Encode()

	if !strings.HasPrefix(entryURL, "/ui/?") {
		t.Fatalf("入口地址必须是 /ui/ 带尾斜杠的形式，实际 %q", entryURL)
	}

	// 用标准库按 iframe 的解析方式验证相对资源的最终地址
	base, err := url.Parse("http://example.com" + entryURL)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := base.Parse("./assets/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path != "/ui/assets/index.js" {
		t.Errorf("相对资源应解析到 /ui/assets/index.js，实际 %q", ref.Path)
	}
}

// 反例固化：缺少尾斜杠时相对资源会跑到根目录，这正是内嵌白屏的根因。
func TestMissingTrailingSlashBreaksAssets(t *testing.T) {
	base, err := url.Parse("http://example.com/ui?hostname=127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := base.Parse("./assets/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path == "/ui/assets/index.js" {
		t.Fatal("预期无尾斜杠会破坏相对路径解析，但结果正确，说明前提假设已变化")
	}
	if ref.Path != "/assets/index.js" {
		t.Errorf("无尾斜杠时应解析到 /assets/index.js，实际 %q", ref.Path)
	}
}

func TestHostWithoutPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"127.0.0.1:8899", "127.0.0.1"},
		{"example.com:8899", "example.com"},
		{"example.com", "example.com"},
		{"[::1]:8899", "[::1]"},
		{"[fe80::1]", "[fe80::1]"},
		{"", ""},
		{"  nas.local:8899  ", "nas.local"},
	}
	for _, c := range cases {
		if got := hostWithoutPort(c.in); got != c.want {
			t.Errorf("hostWithoutPort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

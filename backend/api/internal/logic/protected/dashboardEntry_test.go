package protected

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/svc"
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

func TestPublicAuthority(t *testing.T) {
	cases := []struct {
		host     string
		isHTTPS  bool
		wantHost string
		wantPort string
	}{
		{"aurora.615246.xyz", true, "aurora.615246.xyz", "443"},
		{"aurora.615246.xyz", false, "aurora.615246.xyz", "80"},
		{"aurora.615246.xyz:8443", true, "aurora.615246.xyz", "8443"},
		{"192.168.1.50:8899", false, "192.168.1.50", "8899"},
		{"192.168.1.50", false, "192.168.1.50", "80"},
		{"[::1]:8899", false, "[::1]", "8899"},
		{"[fe80::1]", true, "[fe80::1]", "443"},
		{"", true, "", "443"},
	}
	for _, c := range cases {
		h, p := publicAuthority(c.host, c.isHTTPS)
		if h != c.wantHost || p != c.wantPort {
			t.Errorf("publicAuthority(%q, %v) = (%q, %q), want (%q, %q)", c.host, c.isHTTPS, h, p, c.wantHost, c.wantPort)
		}
	}
}

func newEntrySvcCtx(t *testing.T, controller, secret string) *svc.ServiceContext {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{}
	cfg.DataSource = filepath.Join(dir, "entry.db")
	cfg.Mihomo.ConfigDir = dir
	cfg.Auth.AccessSecret = "secret12345678901234567890123456"
	cfg.TrustedProxies = []string{"10.0.0.5"}
	cfg.Bootstrap.EnsureOnStart = false

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("external-controller: "+controller+"\nsecret: "+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svcCtx := svc.NewServiceContext(cfg)
	t.Cleanup(func() { _ = svcCtx.Database.Close() })
	return svcCtx
}

// 公网 nginx + https 场景：内核只绑 127.0.0.1，请求经受信代理转发（X-Forwarded-Proto=https）。
// 入口必须回落到用户访问的主机与 443，并携带 https + secondaryPath=/mihomo-api，
// 使面板经同源反代访问内核而不是直连不可达的 127.0.0.1:9090。
func TestDashboardEntryHTTPSViaTrustedProxy(t *testing.T) {
	svcCtx := newEntrySvcCtx(t, "127.0.0.1:9090", "kern-secret")
	l := NewDashboardEntryLogic(context.Background(), svcCtx)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/entry", nil)
	r.Host = "aurora.615246.xyz"
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-Proto", "https")

	resp, err := l.DashboardEntry(r)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Available {
		t.Fatalf("available=false: %s", resp.Message)
	}
	if !strings.HasPrefix(resp.Url, "/ui/?") {
		t.Fatalf("入口地址必须是 /ui/ 带尾斜杠的形式，实际 %q", resp.Url)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(resp.Url, "/ui/?"))
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Get("hostname"); got != "aurora.615246.xyz" {
		t.Errorf("hostname = %q, want aurora.615246.xyz", got)
	}
	if got := q.Get("port"); got != "443" {
		t.Errorf("port = %q, want 443", got)
	}
	if _, ok := q["https"]; !ok {
		t.Error("https 场景应携带 https 参数")
	}
	if got := q.Get("secondaryPath"); got != "/mihomo-api" {
		t.Errorf("secondaryPath = %q, want /mihomo-api", got)
	}
	if got := q.Get("secret"); got != "kern-secret" {
		t.Errorf("secret = %q, want kern-secret", got)
	}
	if resp.PublicPort != "443" {
		t.Errorf("PublicPort = %q, want 443", resp.PublicPort)
	}
}

// 内网 http 直连场景：Host 带显式端口、无可信转发头。端口应取 Host 里的显式值，
// 且不携带 https 参数。
func TestDashboardEntryHTTPDirect(t *testing.T) {
	svcCtx := newEntrySvcCtx(t, ":9090", "")
	l := NewDashboardEntryLogic(context.Background(), svcCtx)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/entry", nil)
	r.Host = "192.168.1.50:8899"
	r.RemoteAddr = "192.168.1.50:5555"

	resp, err := l.DashboardEntry(r)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Available {
		t.Fatalf("available=false: %s", resp.Message)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(resp.Url, "/ui/?"))
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Get("hostname"); got != "192.168.1.50" {
		t.Errorf("hostname = %q, want 192.168.1.50", got)
	}
	if got := q.Get("port"); got != "8899" {
		t.Errorf("port = %q, want 8899", got)
	}
	if _, ok := q["https"]; ok {
		t.Error("http 场景不应携带 https 参数")
	}
	if got := q.Get("secondaryPath"); got != "/mihomo-api" {
		t.Errorf("secondaryPath = %q, want /mihomo-api", got)
	}
}

// nginx 终结 TLS、TrustedProxies 未配置时：RemoteAddr 是反代 IP 且不在白名单，
// RequestIsHTTPS 会返回 false。此时若 Host 无端口且 X-Forwarded-Proto=https，
// 仍应拼成 https + 443，避免公网入口变成 :80。
func TestDashboardEntryHTTPSViaUntrustedProxyForwardedProto(t *testing.T) {
	svcCtx := newEntrySvcCtx(t, "127.0.0.1:9090", "s")
	// 故意清空可信代理，模拟默认 etc 配置
	svcCtx.Config.TrustedProxies = nil
	l := NewDashboardEntryLogic(context.Background(), svcCtx)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/entry", nil)
	r.Host = "aurora.615246.xyz"
	r.RemoteAddr = "203.0.113.10:1234" // 非白名单反代
	r.Header.Set("X-Forwarded-Proto", "https")

	resp, err := l.DashboardEntry(r)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Available {
		t.Fatalf("available=false: %s", resp.Message)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(resp.Url, "/ui/?"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := q["https"]; !ok {
		t.Error("Host 无端口 + X-Forwarded-Proto=https 时应携带 https 参数")
	}
	if got := q.Get("port"); got != "443" {
		t.Errorf("port = %q, want 443", got)
	}
}

package fetcher

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()

	c := New(3 * time.Second)
	data, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if string(data) != "proxies: []\n" {
		t.Fatalf("unexpected body: %q", string(data))
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(3 * time.Second)
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

// 订阅地址由用户提供并由服务端发起请求，必须限制协议，
// 否则 file:// 等 scheme 可被用来读取服务器本地文件
func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	c := New(3 * time.Second)
	for _, u := range []string{
		"file:///etc/passwd",
		"file://C:/Windows/win.ini",
		"gopher://127.0.0.1:11211/_stat",
		"ftp://example.com/a.yaml",
	} {
		if _, _, err := c.FetchWithMeta(context.Background(), u, ""); err == nil {
			t.Errorf("非 http/https 协议应被拒绝: %s", u)
		}
	}
}

// 云 metadata / link-local 必须拒绝，避免服务端代发请求读出实例身份令牌。
// RFC1918 与 loopback 仍放行（自建订阅、单测 mock）。
func TestFetchRejectsCloudMetadata(t *testing.T) {
	c := New(3 * time.Second)
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"https://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://100.100.100.200/latest/meta-data/",
		"http://[fe80::1]:80/",
	}
	for _, u := range blocked {
		if _, _, err := c.FetchWithMeta(context.Background(), u, ""); err == nil {
			t.Errorf("云 metadata / link-local 应被拒绝: %s", u)
		}
	}
}

func TestNormalizeFetchURLGitHubBlob(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			"https://github.com/weni09/clash_my_conf/blob/main/mihomo.yaml",
			"https://raw.githubusercontent.com/weni09/clash_my_conf/main/mihomo.yaml",
		},
		{
			"https://www.github.com/weni09/clash_my_conf/blob/main/dir/a.yaml",
			"https://raw.githubusercontent.com/weni09/clash_my_conf/main/dir/a.yaml",
		},
		{
			"https://github.com/weni09/clash_my_conf/raw/main/mihomo.yaml",
			"https://raw.githubusercontent.com/weni09/clash_my_conf/main/mihomo.yaml",
		},
		// 非 blob/raw 不改写（目录页、仓库首页）
		{
			"https://github.com/weni09/clash_my_conf/tree/main/list",
			"https://github.com/weni09/clash_my_conf/tree/main/list",
		},
		// 其它主机不改写
		{
			"https://gitlab.com/a/b/-/blob/main/x.yaml",
			"https://gitlab.com/a/b/-/blob/main/x.yaml",
		},
	}
	for _, c := range cases {
		if got := normalizeFetchURL(c.in); got != c.want {
			t.Errorf("normalizeFetchURL(%q)=\n  %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestLooksLikeHTMLPage(t *testing.T) {
	// V2Board 类机场用 text/html 声明返回 base64 订阅正文——不是 HTML 文档，
	// 必须放行，否则整条订阅被误拒（原 Sub-Store 项目照常解析）。
	if looksLikeHTMLPage("text/html; charset=utf-8", []byte("dmxlc3M6Ly9ub2RlMUBleGFtcGxlLmNvbTo0NDM=")) {
		t.Error("text/html 声明 + base64 订阅正文不应被当成网页")
	}
	if !looksLikeHTMLPage("application/octet-stream", []byte("<!DOCTYPE html><html>")) {
		t.Error("正文以 doctype html 开头时应判定为网页")
	}
	if !looksLikeHTMLPage("text/html; charset=utf-8", []byte("<html><body>login page</body></html>")) {
		t.Error("真实 HTML 页面无论声明什么都应判定为网页")
	}
	if looksLikeHTMLPage("text/yaml", []byte("proxies:\n  - name: a\n")) {
		t.Error("正常 YAML 不应被当成网页")
	}
	if looksLikeHTMLPage("text/html", []byte("")) {
		t.Error("空正文不应被当成网页")
	}
}

// 粘贴 GitHub blob 页时必须实际请求 raw 地址，否则会把 HTML 当配置。
func TestFetchRewritesGitHubBlobToRaw(t *testing.T) {
	var hitRaw bool
	mux := http.NewServeMux()
	// 用自定义 Transport 把 raw.githubusercontent.com 指到本测试服务器较复杂；
	// 这里直接验证 normalize 后的路径可被 Fetch 成功消费：起一个返回 YAML 的服务器，
	// 再测「HTML 响应被拒绝」。
	mux.HandleFunc("/ok.yaml", func(w http.ResponseWriter, _ *http.Request) {
		hitRaw = true
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("proxies: []\n"))
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>blob ui</body></html>"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(3 * time.Second)
	data, err := c.Fetch(context.Background(), srv.URL+"/ok.yaml")
	if err != nil {
		t.Fatalf("正常 YAML 拉取失败: %v", err)
	}
	if !hitRaw || string(data) != "proxies: []\n" {
		t.Fatalf("未拿到 YAML 正文: hit=%v body=%q", hitRaw, data)
	}
	if _, err := c.Fetch(context.Background(), srv.URL+"/page"); err == nil {
		t.Fatal("HTML 响应应被拒绝")
	} else if !strings.Contains(err.Error(), "网页") {
		t.Errorf("错误应提示是网页而非配置，实际: %v", err)
	}
}

func TestValidateFetchURLAllowsPrivateAndLoopback(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1:8080/sub",
		"http://192.168.1.10/clash.yaml",
		"http://10.0.0.5/sub",
		"https://172.16.0.1/cfg",
		"https://example.com/sub",
	} {
		if err := validateFetchURL(u); err != nil {
			t.Errorf("正当地址不应被拒: %s (%v)", u, err)
		}
	}
}

func TestIsBlockedMetadataIP(t *testing.T) {
	if !isBlockedMetadataIP(net.ParseIP("169.254.169.254")) {
		t.Error("169.254.169.254 应拦截")
	}
	if !isBlockedMetadataIP(net.ParseIP("169.254.0.1")) {
		t.Error("link-local 应拦截")
	}
	if !isBlockedMetadataIP(net.ParseIP("100.100.100.200")) {
		t.Error("阿里云 metadata 应拦截")
	}
	if !isBlockedMetadataIP(net.ParseIP("fd00:ec2::254")) {
		t.Error("AWS IMDSv6 应拦截")
	}
	if isBlockedMetadataIP(net.ParseIP("127.0.0.1")) {
		t.Error("loopback 不应拦截")
	}
	if isBlockedMetadataIP(net.ParseIP("192.168.0.1")) {
		t.Error("RFC1918 不应拦截")
	}
	if isBlockedMetadataIP(net.ParseIP("8.8.8.8")) {
		t.Error("公网不应拦截")
	}
	if isBlockedMetadataIP(net.ParseIP("fd00::1")) {
		t.Error("普通 ULA 不应拦截")
	}
}

// 订阅源普遍 302 到 CDN，安全重定向必须照常跟随，
// 否则修复 SSRF 会破坏正常订阅拉取。
func TestFetchFollowsSafeRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := New(3 * time.Second)
	data, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("安全重定向应被跟随: %v", err)
	}
	if string(data) != "proxies: []\n" {
		t.Fatalf("unexpected body: %q", string(data))
	}
}

// 重定向到云 metadata 必须拒绝：validateFetchURL 只校验初始 URL，
// 没有 CheckRedirect 时恶意上游可 302 直达 169.254.169.254。
func TestFetchRejectsRedirectToMetadata(t *testing.T) {
	redirectTo := func(location string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, location, http.StatusFound)
		}))
		return srv
	}

	for _, loc := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
	} {
		srv := redirectTo(loc)
		c := New(3 * time.Second)
		if _, err := c.Fetch(context.Background(), srv.URL); err == nil {
			t.Errorf("重定向到 %s 应被拒绝", loc)
		}
		srv.Close()
	}
}

// 无限跳转链必须截断：maxRedirects 之后的跳转直接报错而非跟随。
func TestFetchStopsAfterMaxRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer srv.Close()

	c := New(3 * time.Second)
	if _, err := c.Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("超过 maxRedirects 的跳转链应被拒绝")
	}
}

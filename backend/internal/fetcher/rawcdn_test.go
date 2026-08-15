package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// raw 官方链接且配置了下载源时，应依次尝试各源，成功即停。
// 用本地服务器 + 完整前缀源构造，验证轮换、成功回调与错误聚合。
func TestFetchWithRawCDNRotatesSources(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failSrv.Close()
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer okSrv.Close()

	var remembered []string
	c := New(3 * time.Second)
	c.SetRawSuccessCallback(func(p string) { remembered = append(remembered, p) })
	// 注入的列表已优先化（模拟 updater.PrioritizedCDNProviders）；
	// 第一个源失败（500），第二个源成功——应跳过失败源、取成功源并记 last。
	c.SetRawCDNProviderFunc(func() []string {
		return []string{failSrv.URL + "/", okSrv.URL + "/", "github"}
	})

	data, _, err := c.fetchWithRawCDN(context.Background(),
		"https://raw.githubusercontent.com/owner/repo/main/f.yaml", "")
	if err != nil {
		t.Fatalf("raw 轮换拉取失败: %v", err)
	}
	if string(data) != "proxies: []\n" {
		t.Fatalf("unexpected body: %q", string(data))
	}
	if len(remembered) != 1 || remembered[0] != okSrv.URL+"/" {
		t.Fatalf("应记录成功源 %q, got %#v", okSrv.URL+"/", remembered)
	}
}

// 官方源成功不应记 last（与 Release 下载「代理成功不写入」同构）。
func TestShouldRememberRaw(t *testing.T) {
	cases := []struct {
		provider string
		want     bool
	}{
		{"github", false},
		{"official", false},
		{"raw.githubusercontent.com", false},
		{"Github", false},
		{"ghproxy.com", true},
		{"mirror.ghproxy.com", true},
		{"https://cdn.example.com", true},
	}
	for _, c := range cases {
		if got := shouldRememberRaw(c.provider); got != c.want {
			t.Errorf("shouldRememberRaw(%q) = %v, want %v", c.provider, got, c.want)
		}
	}
}

// 全部源失败时聚合错误信息返回，不静默吞掉。
func TestFetchWithRawCDNAllFailed(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	c := New(3 * time.Second)
	c.SetRawCDNProviderFunc(func() []string { return []string{failSrv.URL + "/"} })

	_, _, err := c.fetchWithRawCDN(context.Background(),
		"https://raw.githubusercontent.com/owner/repo/main/f.yaml", "")
	if err == nil {
		t.Fatal("全源失败应报错")
	}
	if !strings.Contains(err.Error(), "all raw sources failed") {
		t.Errorf("错误应说明全源失败, got: %v", err)
	}
}

// 代理优先：配置了可用代理时先经代理直取官方地址，成功即返回，不落镜像。
// 用 http scheme 的目标让 Go 经代理发「绝对 URL 的 GET」，httptest 可断言。
func TestFetchWithRawCDNProxyFirst(t *testing.T) {
	var proxiedHost string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHost = r.URL.Host
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("via-proxy\n"))
	}))
	defer proxySrv.Close()

	var remembered []string
	c := New(3 * time.Second)
	c.SetRawSuccessCallback(func(p string) { remembered = append(remembered, p) })
	c.SetProxyURLFunc(func() string { return proxySrv.URL })
	c.SetRawCDNProviderFunc(func() []string { return []string{"github"} })

	// 目标 scheme 用 http：Go 对 http 目标经代理发绝对 URL 的 GET，
	// 代理服务器（httptest）可直接接收并回包。https 目标会走 CONNECT 隧道，
	// 单测难以模拟 TLS 层，此处验证「代理优先 + 不记 last」的规则本身。
	data, _, err := c.fetchWithRawCDN(context.Background(),
		"http://raw.githubusercontent.com/owner/repo/main/f.yaml", "")
	if err != nil {
		t.Fatalf("代理优先拉取失败: %v", err)
	}
	if string(data) != "via-proxy\n" {
		t.Fatalf("应经代理取到官方正文, got %q", string(data))
	}
	if proxiedHost != "raw.githubusercontent.com" {
		t.Fatalf("代理请求目标应为 raw 官方, got %q", proxiedHost)
	}
	// 代理成功不记 last（与 Release 下载「代理成功不写入」同构）
	if len(remembered) != 0 {
		t.Fatalf("代理成功不应记 last, got %#v", remembered)
	}
}

// 代理不可用时自动回落镜像列表；镜像成功记 last（与 Release 下载共用优先序）。
func TestFetchWithRawCDNProxyUnavailableFallsBack(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("mirror-ok\n"))
	}))
	defer okSrv.Close()

	var remembered []string
	c := New(3 * time.Second)
	c.SetRawSuccessCallback(func(p string) { remembered = append(remembered, p) })
	// 未注入 proxyURLFn → 直接回落镜像
	c.SetRawCDNProviderFunc(func() []string { return []string{okSrv.URL + "/", "github"} })

	data, _, err := c.fetchWithRawCDN(context.Background(),
		"https://raw.githubusercontent.com/owner/repo/main/f.yaml", "")
	if err != nil {
		t.Fatalf("回落镜像拉取失败: %v", err)
	}
	if string(data) != "mirror-ok\n" {
		t.Fatalf("应经镜像取到正文, got %q", string(data))
	}
	if len(remembered) != 1 || remembered[0] != okSrv.URL+"/" {
		t.Fatalf("镜像成功应记 last, got %#v", remembered)
	}
}

// 全源失败时聚合错误只暴露源标识，不暴露完整 URL（可能带 token）。
func TestFetchWithRawCDNErrorRedactsURL(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	c := New(3 * time.Second)
	c.SetRawCDNProviderFunc(func() []string { return []string{failSrv.URL + "/"} })

	_, _, err := c.fetchWithRawCDN(context.Background(),
		"https://raw.githubusercontent.com/owner/repo/main/f.yaml?token=SECRET", "")
	if err == nil {
		t.Fatal("全源失败应报错")
	}
	if strings.Contains(err.Error(), "token=SECRET") {
		t.Fatalf("错误不应暴露完整 URL 中的凭据, got: %v", err)
	}
}

func TestIsRawOfficial(t *testing.T) {
	if !isRawOfficial("https://raw.githubusercontent.com/a/b/main/c.yaml") {
		t.Error("raw.githubusercontent.com 应识别为 raw 官方")
	}
	if isRawOfficial("https://github.com/a/b/blob/main/c.yaml") {
		t.Error("github.com 不是 raw 官方")
	}
	if isRawOfficial("https://example.com/c.yaml") {
		t.Error("其它主机不是 raw 官方")
	}
}

// 未注入下载源查询函数时，raw 官方链接走单源直拉（不加速）。
func TestFetchWithRawCDNNoProviderFuncFallsBackToDirect(t *testing.T) {
	c := New(3 * time.Second)
	if got := c.rawProviders(); got != nil {
		t.Fatalf("未注入时应返回 nil, got %#v", got)
	}
}

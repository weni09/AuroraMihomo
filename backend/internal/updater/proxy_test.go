package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// jsdelivr 只镜像仓库内文件，代理不了 Release 资产。
// 填进下载源列表应被跳过，而不是拼出一个必然 404 的地址。
func TestBuildCDNURLsSkipsJsdelivr(t *testing.T) {
	release := "https://github.com/MetaCubeX/mihomo/releases/download/v1.0.0/a.zip"
	urls := buildCDNURLs(release, []string{"https://cdn.jsdelivr.net", "ghproxy.com", "github"})
	for _, u := range urls {
		if strings.Contains(u, "jsdelivr") {
			t.Fatalf("Release 下载不应出现 jsdelivr 候选: %s", u)
		}
	}
	if len(urls) != 2 {
		t.Fatalf("expected ghproxy + official, got %#v", urls)
	}
}

func TestBuildCDNURLsCustomPrefixAndTemplate(t *testing.T) {
	release := "https://github.com/x/y/releases/download/v1/a.zip"
	urls := buildCDNURLs(release, []string{
		"https://mirror.example.com",
		"https://tpl.example.com/fetch?u=%s",
		"github",
	})
	if urls[0] != "https://mirror.example.com/"+release {
		t.Fatalf("自定义前缀拼接错误: %s", urls[0])
	}
	if urls[1] != "https://tpl.example.com/fetch?u="+release {
		t.Fatalf("%%s 模板替换错误: %s", urls[1])
	}
}

// 裸域名无法判断该怎么拼，忽略而非猜测
func TestBuildCDNURLsIgnoresUnknownBareHost(t *testing.T) {
	release := "https://github.com/x/y/releases/download/v1/a.zip"
	urls := buildCDNURLs(release, []string{"mirror.example.com", "github"})
	if len(urls) != 1 || urls[0] != release {
		t.Fatalf("未知裸域名应被忽略，got %#v", urls)
	}
}

func TestIsJsdelivr(t *testing.T) {
	cases := map[string]bool{
		"https://cdn.jsdelivr.net":           true,
		"https://fastly.jsdelivr.net/":       true,
		"jsdelivr.net":                       true,
		"https://example.com/jsdelivr.net/x": false, // 仅域名部分参与判断
		"https://ghproxy.com":                false,
		"https://notjsdelivr.example.com":    false,
	}
	for in, want := range cases {
		if got := isJsdelivr(in); got != want {
			t.Errorf("isJsdelivr(%q) = %v, want %v", in, got, want)
		}
	}
}

// 镜像全挂时仍要能直连，官方源必须留作最后兜底
func TestNormalizeCDNListKeepsGithubFallback(t *testing.T) {
	out := normalizeCDNList([]string{"https://mirror.example.com"})
	found := false
	for _, v := range out {
		if v == "github" {
			found = true
		}
	}
	if !found {
		t.Fatalf("github fallback missing in %#v", out)
	}
}

func TestNormalizeCDNListEmptyUsesDefaults(t *testing.T) {
	if out := normalizeCDNList(nil); len(out) != len(DefaultCDNProviders) {
		t.Fatalf("expected defaults, got %#v", out)
	}
}

// ---- mihomo 代理 ----

func newProxyTestManager(t *testing.T, proxyURL string, useProxy bool) *Manager {
	t.Helper()
	m := New(Config{DataDir: t.TempDir(), UseMihomoProxy: useProxy})
	m.SetProxyURLFunc(func() string { return proxyURL })
	return m
}

// 开启代理且地址可用时，请求应经代理发出
func TestHTTPClientUsesProxyWhenEnabled(t *testing.T) {
	m := newProxyTestManager(t, "http://127.0.0.1:7890", true)
	_, proxy := m.httpClient()
	if proxy != "http://127.0.0.1:7890" {
		t.Fatalf("应使用 mihomo 代理，实际 %q", proxy)
	}
}

// 关掉开关时即便回调有值也必须直连
func TestHTTPClientDirectWhenDisabled(t *testing.T) {
	m := newProxyTestManager(t, "http://127.0.0.1:7890", false)
	client, proxy := m.httpClient()
	if proxy != "" {
		t.Fatalf("关闭代理后不应走代理，实际 %q", proxy)
	}
	if client != m.client {
		t.Fatal("关闭代理后应复用直连客户端")
	}
}

// 内核未运行时回调返回空串，应自动回落直连而不是报错
func TestHTTPClientDirectWhenProxyUnavailable(t *testing.T) {
	m := newProxyTestManager(t, "", true)
	if _, proxy := m.httpClient(); proxy != "" {
		t.Fatalf("代理不可用时应直连，实际 %q", proxy)
	}
}

// 代理地址非法时不能让整次下载失败，应记录并直连
func TestHTTPClientDirectWhenProxyMalformed(t *testing.T) {
	m := newProxyTestManager(t, "::not a url::", true)
	client, proxy := m.httpClient()
	if proxy != "" {
		t.Fatalf("非法代理地址应回落直连，实际 %q", proxy)
	}
	if client != m.client {
		t.Fatal("应复用直连客户端")
	}
}

// 版本查询只打官方地址，不套任何镜像前缀：
// 没有镜像代理 REST API，套上去只是一串必然 404 的请求。
func TestLatestReleaseHitsOfficialAPIOnly(t *testing.T) {
	var paths []string
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"mihomo-windows-amd64-v1.2.3.zip","browser_download_url":"https://example.com/a.zip","size":2048}]}`))
	}))
	defer srv.Close()

	m := New(Config{
		DataDir:   t.TempDir(),
		GitHubAPI: srv.URL,
		// 即便配了一堆镜像，API 查询也不该用它们
		CDNProviders: []string{"ghproxy.com", "https://cdn.jsdelivr.net", "github"},
	})

	rel, err := m.latestRelease(context.Background(), "MetaCubeX/mihomo")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("tag 解析错误: %q", rel.TagName)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("应只请求一次官方 API，实际 %d 次", got)
	}
	if len(paths) != 1 || paths[0] != "/repos/MetaCubeX/mihomo/releases/latest" {
		t.Errorf("请求路径不应带镜像前缀: %#v", paths)
	}
}

// 代理不通时版本查询要回落直连，否则内核没跑起来就永远查不到更新
func TestLatestReleaseFallsBackToDirectWhenProxyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","assets":[{"name":"mihomo-windows-amd64-v9.9.9.zip","browser_download_url":"https://example.com/a.zip","size":2048}]}`))
	}))
	defer srv.Close()

	m := New(Config{DataDir: t.TempDir(), GitHubAPI: srv.URL, UseMihomoProxy: true})
	// 指向一个没人监听的端口，模拟内核已退出但配置仍在
	m.SetProxyURLFunc(func() string { return "http://127.0.0.1:1" })

	rel, err := m.latestRelease(context.Background(), "x/y")
	if err != nil {
		t.Fatalf("代理失败后应回落直连: %v", err)
	}
	if rel.TagName != "v9.9.9" {
		t.Errorf("tag 解析错误: %q", rel.TagName)
	}
}

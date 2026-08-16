package diagnostics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

func TestHTTPProbeSuccess(t *testing.T) {
	// 本地 HTTP 服务返回 200，验证状态/延迟/Detail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := &HTTPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	if res.Error != "" {
		t.Fatalf("成功结果不应有 Error, got %q", res.Error)
	}
	// 本机回环响应常在 1ms 内，LatencyMs 可能为 0——只断言非负（毫秒级延迟合法）
	if res.LatencyMs < 0 {
		t.Fatalf("延迟不应为负, got %d", res.LatencyMs)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	if sc, _ := detail["statusCode"].(int); sc != 200 {
		t.Fatalf("statusCode 应为 200, got %v", detail["statusCode"])
	}
}

func TestHTTPProbeRedirect(t *testing.T) {
	// 302→/final：默认 client 跟随重定向，最终 URL 应落在 /final
	mux := http.NewServeMux()
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	probe := &HTTPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("跟随重定向后应成功, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	finalURL, _ := detail["finalURL"].(string)
	if !strings.Contains(finalURL, "/final") {
		t.Fatalf("finalURL 应含 /final, got %q", finalURL)
	}
	// 重定向链：302 跳转到 /final，应记录该跳转目标（最终 URL 已由 finalURL 体现）
	redirects, ok := detail["redirects"].([]string)
	if !ok || len(redirects) != 1 || !strings.Contains(redirects[0], "/final") {
		t.Fatalf("redirects 应记录一次跳转指向 /final, got %v", detail["redirects"])
	}
}

func TestHTTPProbeRedirectChain(t *testing.T) {
	// 302 链 / → /b → /final：redirects 应记录每一跳目标（含最终 URL）
	mux := http.NewServeMux()
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	probe := &HTTPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("跟随重定向链后应成功, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	redirects, ok := detail["redirects"].([]string)
	if !ok || len(redirects) != 2 {
		t.Fatalf("redirects 应记录 2 跳, got %v", detail["redirects"])
	}
	if !strings.Contains(redirects[0], "/b") {
		t.Fatalf("第 1 跳应指向 /b, got %q", redirects[0])
	}
	if !strings.Contains(redirects[1], "/final") {
		t.Fatalf("第 2 跳应指向 /final, got %q", redirects[1])
	}
}

func TestHTTPProbe404(t *testing.T) {
	// 404 应标记 fail 且错误信息含状态码
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	probe := &HTTPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL + "/missing"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("404 应标记 fail, got %+v", res)
	}
	if !strings.Contains(res.Error, "HTTP 404") {
		t.Fatalf("Error 应含 HTTP 404, got %q", res.Error)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	if sc, _ := detail["statusCode"].(int); sc != 404 {
		t.Fatalf("statusCode 应为 404, got %v", detail["statusCode"])
	}
}

func TestHTTPProbeTimeout(t *testing.T) {
	// handler 挂起不返回，ctx 先于 client 超时到期，应标记 timeout
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	probe := &HTTPProbe{Timeout: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := probe.Run(ctx, DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusTimeout {
		t.Fatalf("超时应标记 timeout, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("timeout 结果应包含错误信息")
	}
}

func TestHTTPProbeDefaultClientBlocksMetadata(t *testing.T) {
	// 默认 client 必须带 SSRF 防线：目标为云 metadata IP 字面量时，
	// guardedDialContext 在拨号前即拦截，结果应为 fail 且错误含 metadata，
	// 而不是真实发起请求。
	probe := &HTTPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: "http://169.254.169.254/latest/meta-data/"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("metadata 地址应被拦截为 fail, got %+v", res)
	}
	if !strings.Contains(res.Error, "metadata") {
		t.Fatalf("错误应说明被拦截的 metadata 地址, got %q", res.Error)
	}
}

func TestHTTPProbeProxyPathFallbackToDirect(t *testing.T) {
	// 代理地址不可用（空串）：选择器回落直连 client，proxy 路径应标注回落，
	// 而不是把直连成功冒充代理路径（both 模式对比才真实）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := &HTTPProbe{Selector: defaultClientSelector(func() string { return "" })}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathProxy, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("回落直连应成功, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("回落结果 Detail 应为 map, got %T", res.Detail)
	}
	if fb, _ := detail["proxyFallback"].(bool); !fb {
		t.Fatalf("回落结果应标注 proxyFallback, got %+v", detail)
	}
	if note, _ := detail["note"].(string); note != "代理不可用，已直连" {
		t.Fatalf("回落标注 note 应为「代理不可用，已直连」, got %q", note)
	}
}

func TestHTTPProbeConcurrentSafe(t *testing.T) {
	// 同一实例并发执行：Run 内不写回结构体字段，配合 -race 验证并发安全
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := &HTTPProbe{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
			if res.Status != StatusSuccess {
				t.Errorf("并发探测应成功, got %+v", res)
			}
		}()
	}
	wg.Wait()
}

func TestCloneHTTPClientReadOnlyCopy(t *testing.T) {
	// cloneHTTPClient 是只读浅拷贝：返回新实例、复制标量字段、共享 Transport，
	// 修改 clone 不影响共享 client（探测内临时 client 用完即弃）。
	shared := &http.Client{Timeout: 3 * time.Second, CheckRedirect: fetcher.CheckRedirect}
	rec := &redirectRecorder{}
	cloned := cloneHTTPClient(shared, rec.wrap(shared.CheckRedirect))
	if cloned == shared {
		t.Fatal("clone 不应返回共享实例")
	}
	if cloned.Timeout != shared.Timeout {
		t.Fatalf("clone 应复制 Timeout, got %v", cloned.Timeout)
	}
	if cloned.Transport != shared.Transport {
		t.Fatal("clone 应共享 Transport")
	}
	// 包装版 CheckRedirect：记录跳转后继续委托原 SSRF 校验（example.com 合法）
	if err := cloned.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}, nil); err != nil {
		t.Fatalf("wrap 应委托 base 校验, got %v", err)
	}
	if len(rec.urls) != 1 || rec.urls[0] != "https://example.com" {
		t.Fatalf("wrap 应记录跳转 URL, got %v", rec.urls)
	}
	// 修改 clone 不影响共享 client 的字段
	cloned.Timeout = 99 * time.Second
	if shared.Timeout != 3*time.Second {
		t.Fatal("修改 clone 不应影响共享 client")
	}
}

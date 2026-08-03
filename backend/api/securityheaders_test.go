package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticFallback 是唯一的最外层 Handler，安全响应头必须对 API 与静态资源
// 两条路径都生效，否则新增 handler 时极易遗漏。
func TestSecurityHeadersAppliedOnBothPaths(t *testing.T) {
	apiHit, staticHit := false, false
	h := staticFallback(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { apiHit = true }),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { staticHit = true }),
		nil,
	)

	// 三个基础头对所有路径都必须存在
	// X-Frame-Options 用 SAMEORIGIN 而非 DENY：管理端需要内嵌同源的 /ui/ 面板
	common := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "no-referrer",
	}

	t.Run("API 路径", func(t *testing.T) {
		for _, path := range []string{"/api/v1/subscriptions", "/ws", "/healthz"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			for k, want := range common {
				if got := rec.Header().Get(k); got != want {
					t.Errorf("%s 的 %s = %q, want %q", path, k, got, want)
				}
			}
			// API 响应含订阅内容与分享 token，不得被缓存
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s 的 Cache-Control = %q, want no-store", path, got)
			}
		}
		if !apiHit {
			t.Error("API 请求未被转发给 apiHandler")
		}
	})

	t.Run("静态资源路径", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config", nil))

		for k, want := range common {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("静态路径的 %s = %q, want %q", k, got, want)
			}
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatal("静态资源响应应带 CSP")
		}
		// 内嵌 zashboard 需要同源 iframe：既不能是 'none'，也要显式允许 frame-src
		if strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("CSP 不应禁止同源 iframe，否则内嵌面板白屏: %s", csp)
		}
		if !strings.Contains(csp, "frame-ancestors 'self'") {
			t.Errorf("CSP 应允许同源内嵌: %s", csp)
		}
		if !strings.Contains(csp, "frame-src 'self'") {
			t.Errorf("CSP 应允许加载同源 iframe 文档: %s", csp)
		}
		// 仍需挡住第三方站点内嵌本管理端
		if !strings.Contains(csp, "base-uri 'self'") {
			t.Errorf("CSP 应限制 base-uri: %s", csp)
		}
		if !staticHit {
			t.Error("静态请求未被转发给 static handler")
		}
	})
}

// 无 TLS 时下发 HSTS 会把用户锁死在无法访问的 https:// 上，
// 该头应留给反向代理层决定。
func TestNoHSTSByDefault(t *testing.T) {
	h := staticFallback(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		nil,
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("默认不应下发 HSTS，实际 %q", got)
	}
}

// /healthz 必须走 API 分支，否则会被 SPA 回退成 index.html，
// 使容器健康检查永远"通过"。
func TestHealthzRoutedToAPI(t *testing.T) {
	routedToAPI := false
	h := staticFallback(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { routedToAPI = true }),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("/healthz 不应交给静态服务")
		}),
		nil,
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !routedToAPI {
		t.Error("/healthz 应被路由到 API")
	}
}

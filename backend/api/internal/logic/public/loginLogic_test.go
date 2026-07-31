package public

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 核心回归：不可信来源伪造的 XFF/X-Real-IP 必须被忽略。
// 否则攻击者每次请求换一个伪造头部，失败计数就永远落在不同 key 上，
// 登录限流完全失效。
func TestClientIPIgnoresSpoofedHeadersFromUntrustedSource(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Real-IP", "5.6.7.8")

	// 未配置任何可信代理
	if got := clientIP(r, nil); got != "203.0.113.9" {
		t.Fatalf("未配置可信代理时应使用 RemoteAddr，实际 %q", got)
	}

	// 配置了可信代理，但请求并非来自它
	trusted := []string{"10.0.0.1", "192.168.1.0/24"}
	if got := clientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("来源不在白名单时应使用 RemoteAddr，实际 %q", got)
	}
}

// 来自可信代理的转发头部应被采信，否则反代部署下所有请求会被
// collapse 到同一个代理 IP，一个用户的失败会锁死全部用户。
func TestClientIPTrustsHeadersFromTrustedProxy(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		trusted []string
	}{
		{"精确 IP 匹配", "10.0.0.1:9999", []string{"10.0.0.1"}},
		{"CIDR 匹配", "192.168.1.77:9999", []string{"192.168.1.0/24"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			r.RemoteAddr = c.remote
			r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")

			if got := clientIP(r, c.trusted); got != "1.2.3.4" {
				t.Fatalf("应采信可信代理转发的最左侧客户端 IP，实际 %q", got)
			}
		})
	}
}

// X-Real-IP 优先于 XFF，且来自可信代理时生效
func TestClientIPPrefersXRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Real-IP", "9.9.9.9")
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := clientIP(r, []string{"10.0.0.1"}); got != "9.9.9.9" {
		t.Fatalf("X-Real-IP 应优先于 XFF，实际 %q", got)
	}
}

// 可信代理未附带任何转发头部时，退化为直连地址而非空串，
// 否则限流会把所有此类请求归到同一个空 key 上。
func TestClientIPFallsBackWhenTrustedProxySendsNoHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:9999"

	if got := clientIP(r, []string{"10.0.0.1"}); got != "10.0.0.1" {
		t.Fatalf("无转发头部时应退化为直连地址，实际 %q", got)
	}
}

func TestClientIPNilRequest(t *testing.T) {
	if got := clientIP(nil, nil); got != "unknown" {
		t.Fatalf("nil 请求应返回 unknown，实际 %q", got)
	}
}

func TestIsTrustedProxy(t *testing.T) {
	cases := []struct {
		ip      string
		trusted []string
		want    bool
	}{
		{"10.0.0.1", nil, false},
		{"10.0.0.1", []string{}, false},
		{"10.0.0.1", []string{"10.0.0.1"}, true},
		{"10.0.0.2", []string{"10.0.0.1"}, false},
		{"192.168.1.5", []string{"192.168.1.0/24"}, true},
		{"192.168.2.5", []string{"192.168.1.0/24"}, false},
		{"10.0.0.1", []string{"", "  ", "10.0.0.1"}, true},
		// 非法条目不应导致 panic，只是不匹配
		{"10.0.0.1", []string{"not-an-ip", "10.0.0.0/99"}, false},
	}
	for _, c := range cases {
		if got := isTrustedProxy(c.ip, c.trusted); got != c.want {
			t.Errorf("isTrustedProxy(%q, %v) = %v, want %v", c.ip, c.trusted, got, c.want)
		}
	}
}

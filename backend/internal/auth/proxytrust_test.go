package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsTrustedProxy(t *testing.T) {
	if IsTrustedProxy("10.0.0.1", nil) {
		t.Fatal("空白名单不应信任任何 IP")
	}
	if !IsTrustedProxy("10.0.0.1", []string{"10.0.0.1"}) {
		t.Fatal("精确 IP 应匹配")
	}
	if !IsTrustedProxy("192.168.1.50", []string{"192.168.1.0/24"}) {
		t.Fatal("CIDR 应匹配")
	}
	if IsTrustedProxy("203.0.113.9", []string{"10.0.0.1"}) {
		t.Fatal("非白名单 IP 不应信任")
	}
}

func TestRequestIsHTTPSDirectTLS(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://example/", nil)
	r.TLS = &tls.ConnectionState{}
	if !RequestIsHTTPS(r, nil) {
		t.Fatal("直连 TLS 应为 HTTPS")
	}
}

func TestRequestIsHTTPSTrustedProxyForwarded(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-Proto", "https")
	if !RequestIsHTTPS(r, []string{"10.0.0.1"}) {
		t.Fatal("受信代理 + X-Forwarded-Proto=https 应视为 HTTPS")
	}
}

func TestRequestIsHTTPSIgnoresSpoofedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-Proto", "https")
	if RequestIsHTTPS(r, []string{"10.0.0.1"}) {
		t.Fatal("未受信来源伪造的 X-Forwarded-Proto 不得启用 Secure")
	}
	if RequestIsHTTPS(r, nil) {
		t.Fatal("未配置可信代理时伪造头不得启用 Secure")
	}
}

func TestRequestIsHTTPSForwardedSslOn(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-Ssl", "on")
	if !RequestIsHTTPS(r, []string{"10.0.0.1"}) {
		t.Fatal("X-Forwarded-Ssl=on 应视为 HTTPS")
	}
}

// IsLocalDialableHost：同源反代上游白名单（回环 / localhost / 本机接口 IP 放行，
// 域名、公网 IP、通配符拒绝）。与 adguard 反代共用，归属在 auth 包保证单一实现。
func TestIsLocalDialableHost(t *testing.T) {
	allowed := []string{"127.0.0.1", "127.0.0.2", "::1", "localhost", "LOCALHOST"}
	for _, h := range allowed {
		if !IsLocalDialableHost(h) {
			t.Errorf("%q 应放行", h)
		}
	}
	for _, h := range []string{"0.0.0.0", "8.8.8.8", "1.1.1.1", "example.com", "", "[::]"} {
		if IsLocalDialableHost(h) {
			t.Errorf("%q 应拒绝", h)
		}
	}
}

// IsAllowedKernelUpstream：mihomo 反代上游白名单。在 IsLocalDialableHost 基础上
// 额外放行私网地址（分机部署的内核常配置成其它机器 IP），公网与域名仍拒绝。
func TestIsAllowedKernelUpstream(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "::1", "localhost", "192.168.1.252", "10.0.0.5", "172.16.0.1", "169.254.1.1", "fe80::1"} {
		if !IsAllowedKernelUpstream(h) {
			t.Errorf("私网/回环 %q 应放行", h)
		}
	}
	for _, h := range []string{"8.8.8.8", "1.1.1.1", "114.114.114.114", "example.com", "", "0.0.0.0"} {
		if IsAllowedKernelUpstream(h) {
			t.Errorf("公网/域名 %q 应拒绝", h)
		}
	}
}

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

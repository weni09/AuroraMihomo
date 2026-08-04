package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWSOriginAllowed(t *testing.T) {
	mk := func(origin, host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/ws", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}

	if !wsOriginAllowed(mk("", "127.0.0.1:8899")) {
		t.Fatal("空 Origin 应放行（非浏览器）")
	}
	if !wsOriginAllowed(mk("http://127.0.0.1:8899", "127.0.0.1:8899")) {
		t.Fatal("同源应放行")
	}
	if !wsOriginAllowed(mk("https://example.com", "example.com")) {
		t.Fatal("仅 scheme 不同且 host 无端口时应放行")
	}
	if wsOriginAllowed(mk("https://evil.example", "127.0.0.1:8899")) {
		t.Fatal("跨站 Origin 应拒绝")
	}
	if wsOriginAllowed(mk("not-a-url", "127.0.0.1:8899")) {
		t.Fatal("非法 Origin 应拒绝")
	}
}

func TestSameWSHostDefaultPorts(t *testing.T) {
	if !sameWSHost("example.com", "example.com:443") {
		t.Fatal("缺省 443 应视为同 host")
	}
	if !sameWSHost("example.com:80", "example.com") {
		t.Fatal("缺省 80 应视为同 host")
	}
	if sameWSHost("example.com:8080", "example.com:8899") {
		t.Fatal("不同非默认端口不应相等")
	}
}

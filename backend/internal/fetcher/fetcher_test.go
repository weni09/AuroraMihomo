package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
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

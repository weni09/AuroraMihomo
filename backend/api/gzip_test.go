package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// gzip 静态压缩：文本类按 Accept-Encoding 压缩，其余条件不压。
// 覆盖手机弱网首载场景（1.16MB 裸 JS 是「首次打开卡」的根因）。
func TestStaticGzipHandler(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("app.js", "export const app = 'hello world';")
	mustWrite("logo.png", "\x89PNG\x0d\x0a\x1a\x0a")
	mustWrite("index.html", "<!doctype html><title>aurora</title>")

	handler := staticGzipHandler(spaFileSystemServer("", func() http.FileSystem {
		return http.Dir(dir)
	}))

	t.Run("JS 接受 gzip 则压缩且内容可解", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("期望 Content-Encoding: gzip，实际 %q", got)
		}
		if rec.Header().Get("Vary") == "" {
			t.Fatal("缺少 Vary: Accept-Encoding")
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("gzip 解压失败: %v", err)
		}
		defer zr.Close()
		body, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("读取解压内容失败: %v", err)
		}
		if string(body) != "export const app = 'hello world';" {
			t.Fatalf("内容被破坏: %q", body)
		}
	})

	t.Run("无 Accept-Encoding 不压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("不应压缩，实际 Content-Encoding=%q", got)
		}
		if body := rec.Body.String(); body != "export const app = 'hello world';" {
			t.Fatalf("内容异常: %q", body)
		}
	})

	t.Run("gzip;q=0 明确拒绝不压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		req.Header.Set("Accept-Encoding", "gzip;q=0")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("gzip;q=0 不应压缩，实际 %q", got)
		}
	})

	t.Run("图片等已内压格式不压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/logo.png", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("png 不应压缩，实际 %q", got)
		}
	})

	t.Run("SPA 回退的 index.html 也压缩", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("index.html 回退应压缩，实际 %q", got)
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("gzip 解压失败: %v", err)
		}
		defer zr.Close()
		body, _ := io.ReadAll(zr)
		if !bytes.Contains(body, []byte("aurora")) {
			t.Fatalf("回退内容异常: %q", body)
		}
	})

	t.Run("304 条件请求不压缩且保留 304 语义", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req) // 预热 Last-Modified
		lastMod := rec.Header().Get("Last-Modified")

		req2 := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		req2.Header.Set("Accept-Encoding", "gzip")
		req2.Header.Set("If-Modified-Since", lastMod)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusNotModified {
			t.Fatalf("期望 304，实际 %d", rec2.Code)
		}
		if got := rec2.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("304 不应带 Content-Encoding，实际 %q", got)
		}
	})
}

// acceptsGzip 边界：常见浏览器头。
func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"gzip, deflate, br", true},
		{"deflate, br", false},
		{"gzip;q=0", false},
		{"gzip;q=0.5, deflate", true},
		{"", false},
	}
	for _, c := range cases {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v，期望 %v", c.header, got, c.want)
		}
	}
}

// 静态资源缓存策略：/assets/ 构建产物内容寻址可长缓存（immutable），
// index.html 与 SPA 回退必须可重验。这是手机端「反复白屏刷新多次才出」
// 的关键修复——此前无 Cache-Control，zashboard 30+ 资源每次全部重验。
func TestCachedFileServer(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("assets/index-abc123.js", "console.log(1)")
	mustWrite("index.html", "<!doctype html><title>aurora</title>")

	handler := spaFileSystemServer("", func() http.FileSystem {
		return http.Dir(dir)
	})

	t.Run("assets 产物 immutable 长缓存", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc123.js", nil))
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("期望 immutable 长缓存，实际 %q", got)
		}
	})

	t.Run("index.html no-cache 可重验", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("期望 no-cache，实际 %q", got)
		}
	})

	t.Run("SPA 回退 no-cache 可重验", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/route", nil))
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("期望 no-cache，实际 %q", got)
		}
	})
}

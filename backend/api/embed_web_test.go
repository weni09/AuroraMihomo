package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gzipFileForTest 把 content 压缩成 .gz 写盘（测试用 helper）。
func gzipFileForTest(path string, content []byte) error {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(content); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// mustGzipLen 返回 content 的 gzip 后字节数。
func mustGzipLen(content string) int {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte(content))
	_ = gw.Close()
	return buf.Len()
}

// 回归：go:embed 的内嵌资源必须真实包含前端产物。
// 曾出现 backend/api/public 只留 .gitkeep、二进制内嵌为空壳的情况——
// 当时磁盘 public/ 存在会被优先使用，删掉磁盘目录降级到内嵌资源就 404。
// 现已移除磁盘 public/ 兼容链路，内嵌资源是唯一真相，更不可缺失。
// make build-frontend 会把 frontend/dist 同步进 backend/api/public；
// CI 后端 job 不构建前端，产物缺失时跳过。
func TestEmbeddedWebFSHasIndex(t *testing.T) {
	f, err := embeddedWebFS.Open("public/index.html")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("未找到内嵌的前端产物：请先运行 make build-frontend 同步 frontend/dist 到 backend/api/public")
	}
	if err != nil {
		t.Fatalf("打开内嵌 index.html 失败: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatalf("stat 内嵌 index.html 失败: %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("内嵌的 index.html 是空文件，疑似 embed 源未同步")
	}
}

func TestSpaFileSystemServer(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>head</html>"), 0644); err != nil {
		t.Fatalf("failed to write test index.html: %v", err)
	}

	testFilePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFilePath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test.txt: %v", err)
	}

	handler := spaFileSystemServer("", func() http.FileSystem { return http.Dir(tmpDir) })

	// 1. Test existing file
	req := httptest.NewRequest(http.MethodGet, "/test.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for existing file, got %d", w.Code)
	}
	if w.Body.String() != "hello world" {
		t.Errorf("expected body 'hello world', got '%s'", w.Body.String())
	}

	// 2. Test unknown route (SPA fallback to index.html)
	reqSPA := httptest.NewRequest(http.MethodGet, "/unknown/route", nil)
	wSPA := httptest.NewRecorder()
	handler.ServeHTTP(wSPA, reqSPA)

	if wSPA.Code != http.StatusOK {
		t.Errorf("expected status 200 for SPA fallback, got %d", wSPA.Code)
	}
	if wSPA.Body.String() != "<html>head</html>" {
		t.Errorf("expected body '<html>head</html>', got '%s'", wSPA.Body.String())
	}
}

// 回归：fsysProvider 必须按请求求值，而不是启动时绑定一次。
// 曾出现静态服务在启动时固定指向磁盘目录，部署后目录变化请求仍打向
// 旧路径（404）。这里模拟「目录先缺失、后补回」，同一 handler 应随
// provider 切换响应。
func TestSpaFileSystemServerSwitchesProviderPerRequest(t *testing.T) {
	emptyDir := t.TempDir()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("<html>head</html>"), 0644); err != nil {
		t.Fatalf("failed to write test index.html: %v", err)
	}

	current := emptyDir
	handler := spaFileSystemServer("", func() http.FileSystem { return http.Dir(current) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// 目录缺失：应 404，而不是 200（若绑定在启动时，这里会拿到旧目录的内容）
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 when dir missing, got %d", w.Code)
	}

	// 目录恢复：同一 handler 立即开始服务新目录
	current = tmpDir
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 after dir restored, got %d", w.Code)
	}
	if w.Body.String() != "<html>head</html>" {
		t.Errorf("expected body '<html>head</html>', got '%s'", w.Body.String())
	}
}

// 预压缩直传：assets 下有 .gz 且客户端接受 gzip 时，应直接返回预压缩字节
// （Content-Encoding: gzip），不重复压缩；浏览器解出内容与原文件一致。
func TestSpaFileSystemServerServesPrecompressedGzip(t *testing.T) {
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("console.log('zashboard');\n", 50)
	jsPath := filepath.Join(assetsDir, "index-abc123.js")
	if err := os.WriteFile(jsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// 预压缩副本
	gzPath := jsPath + ".gz"
	if err := gzipFileForTest(gzPath, []byte(content)); err != nil {
		t.Fatal(err)
	}

	handler := spaFileSystemServer("/ui", func() http.FileSystem { return http.Dir(tmpDir) })

	// 客户端接受 gzip：应直传预压缩字节
	req := httptest.NewRequest(http.MethodGet, "/ui/assets/index-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable 长缓存", got)
	}
	// 响应体就是预压缩的 gzip 字节（未经二次压缩）
	if w.Body.Len() != mustGzipLen(content) {
		t.Fatalf("响应体应等于预压缩字节, got %d bytes want %d", w.Body.Len(), mustGzipLen(content))
	}
	// 解出内容应一致
	gr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("响应不是合法 gzip: %v", err)
	}
	got, _ := io.ReadAll(gr)
	if string(got) != content {
		t.Fatal("解压内容与原文件不一致")
	}
}

// 客户端不接受 gzip：应返回原文件裸字节，不带 Content-Encoding。
func TestSpaFileSystemServerNoGzipWhenNotAccepted(t *testing.T) {
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "raw-js"
	if err := os.WriteFile(filepath.Join(assetsDir, "a.js"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	handler := spaFileSystemServer("/ui", func() http.FileSystem { return http.Dir(tmpDir) })
	req := httptest.NewRequest(http.MethodGet, "/ui/assets/a.js", nil)
	// 不带 Accept-Encoding
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Body.String() != content {
		t.Fatalf("body = %q, want 原文件", w.Body.String())
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("不应有 Content-Encoding, got %q", got)
	}
}

// 预压缩产物比原文件旧（过期）：应忽略 .gz，走运行时 gzip。
func TestSpaFileSystemServerIgnoresStaleGzip(t *testing.T) {
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("stale", 100)
	jsPath := filepath.Join(assetsDir, "b.js")
	if err := os.WriteFile(jsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// 预压缩副本 mtime 设为过去（旧于原文件）
	gzPath := jsPath + ".gz"
	if err := gzipFileForTest(gzPath, []byte(content)); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(gzPath, past, past)

	handler := spaFileSystemServer("/ui", func() http.FileSystem { return http.Dir(tmpDir) })
	req := httptest.NewRequest(http.MethodGet, "/ui/assets/b.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// 过期 .gz 被忽略 → 走运行时 gzip（staticGzipHandler 外层）或裸传。
	// 本 handler 单独测时应返回原文件（不设 Content-Encoding）。
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("过期 .gz 不应直传, Content-Encoding = %q", got)
	}
	if w.Body.String() != content {
		t.Fatal("应返回原文件内容")
	}
}

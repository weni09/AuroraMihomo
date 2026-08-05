package main

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

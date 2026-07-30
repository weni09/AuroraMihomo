package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mihomo 官方 release 不提供 sha256 校验文件，因此用 GitHub API 声明的
// asset 体积做完整性校验：体积不符的下载产物必须被拒绝，
// 绝不能落盘后被当作内核执行。
func TestDownloadRejectsSizeMismatch(t *testing.T) {
	payload := strings.Repeat("A", 4096) // 冒充被篡改的内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	m := New(Config{DataDir: t.TempDir(), CDNProviders: []string{"github"}})
	dest := filepath.Join(t.TempDir(), "asset.zip")

	// 官方声明 999999 字节，实际只有 4096 -> 必须失败
	err := m.downloadWithCDN(context.Background(), srv.URL+"/a.zip", dest, 999999)
	if err == nil {
		t.Fatal("体积与官方声明不符时必须报错")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("被拒绝的下载产物必须从磁盘删除，不能留下可被执行的文件")
	}
}

// 体积相符时应正常通过
func TestDownloadAcceptsMatchingSize(t *testing.T) {
	payload := strings.Repeat("B", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	m := New(Config{DataDir: t.TempDir(), CDNProviders: []string{"github"}})
	dest := filepath.Join(t.TempDir(), "asset.zip")

	if err := m.downloadWithCDN(context.Background(), srv.URL+"/a.zip", dest, int64(len(payload))); err != nil {
		t.Fatalf("体积相符时应下载成功: %v", err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() != int64(len(payload)) {
		t.Fatal("产物应完整保留在磁盘上")
	}
}

// expectedSize 为 0（元数据来自不可信镜像、拿不到体积）时退化为仅做基本校验
func TestDownloadUnknownSizeFallsBackToBasicCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("C", 2048)))
	}))
	defer srv.Close()

	m := New(Config{DataDir: t.TempDir(), CDNProviders: []string{"github"}})
	dest := filepath.Join(t.TempDir(), "asset.zip")
	if err := m.downloadWithCDN(context.Background(), srv.URL+"/a.zip", dest, 0); err != nil {
		t.Fatalf("体积未知时应退化为基本校验并通过: %v", err)
	}
}

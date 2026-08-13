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

// AdGuard 专用升级链接不能劫持内核/面板/主程序的 GitHub CDN 回落。
// 那些模板是完整 AdGuard 下载 URL，当前缀包 github.com 会得到无效地址。
func TestDownloadWithCDNIgnoresAdGuardProviders(t *testing.T) {
	payload := strings.Repeat("D", 2048)

	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(official.Close)

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(cdn.Close)

	adguardOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downloadWithCDN 不应请求 AdGuard 专用源: %s", r.URL.Path)
		http.Error(w, "adguard-only", http.StatusTeapot)
	}))
	t.Cleanup(adguardOnly.Close)

	m := New(Config{
		DataDir:        t.TempDir(),
		CDNProviders:   []string{cdn.URL},
		UseMihomoProxy: false,
	})
	m.SetAdGuardCDNProviders([]string{adguardOnly.URL + "/AdGuardHome_${GOOS}_${Arch}.tar.gz"})

	dest := filepath.Join(t.TempDir(), "asset.bin")
	officialURL := official.URL + "/owner/repo/releases/download/v1/a.bin"
	if err := m.downloadWithCDN(context.Background(), officialURL, dest, int64(len(payload))); err != nil {
		t.Fatalf("应走全局下载源，不应被 AdGuard 专用源劫持: %v", err)
	}
}

// 上次成功的全局下载源下次必须排到最前；代理成功不记入该标记。
func TestDownloadWithCDNPrefersLastSuccess(t *testing.T) {
	payload := strings.Repeat("E", 2048)
	var hits []string

	failThenOK := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "first")
		if failThenOK == 0 {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(first.Close)

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "second")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(second.Close)

	official := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(official.Close)

	m := New(Config{
		DataDir:        t.TempDir(),
		CDNProviders:   []string{first.URL, second.URL},
		UseMihomoProxy: false,
	})

	dest := filepath.Join(t.TempDir(), "a.bin")
	officialURL := official.URL + "/owner/repo/releases/download/v1/a.bin"
	if err := m.downloadWithCDN(context.Background(), officialURL, dest, int64(len(payload))); err != nil {
		t.Fatalf("第一次应回落到第二个源: %v", err)
	}
	if m.LastCDNProvider() != second.URL {
		t.Fatalf("应记下成功源 %q, 实际 %q", second.URL, m.LastCDNProvider())
	}
	if len(hits) < 2 || hits[0] != "first" || hits[1] != "second" {
		t.Fatalf("第一次应按列表顺序, hits=%v", hits)
	}

	hits = nil
	failThenOK = 1
	dest2 := filepath.Join(t.TempDir(), "b.bin")
	if err := m.downloadWithCDN(context.Background(), officialURL, dest2, int64(len(payload))); err != nil {
		t.Fatalf("第二次应优先上次成功源: %v", err)
	}
	if len(hits) == 0 || hits[0] != "second" {
		t.Fatalf("第二次应先打上次成功源, hits=%v", hits)
	}
}

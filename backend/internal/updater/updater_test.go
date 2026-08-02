package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager 构造一个把 GitHubAPI 指向本地 httptest 服务器的 Manager，
// 避免真实网络请求，用于验证 CheckLatest 的版本比对逻辑。
func newTestManager(t *testing.T, mihomoTag, zashboardTag, adguardTag string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tag string
		switch {
		case strings.Contains(r.URL.Path, "mihomo-repo"):
			tag = mihomoTag
		case strings.Contains(r.URL.Path, "zashboard-repo"):
			tag = zashboardTag
		case strings.Contains(r.URL.Path, "adguard-repo"):
			tag = adguardTag
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// fetchReleaseJSON 要求 assets 非空才算合法响应，
		// 空数组会导致校验失败并触发真实网络的 CDN 回退链。
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"assets": []map[string]string{
				{"name": "asset.zip", "browser_download_url": "https://example.com/asset.zip"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	m := New(Config{
		DataDir:       dir,
		MihomoRepo:    "mihomo-repo/x",
		ZashboardRepo: "zashboard-repo/x",
		AdGuardRepo:   "adguard-repo/x",
		GitHubAPI:     srv.URL,
		// CDN 回退列表若含 github 会绕过 mock 服务器直连真实网络，
		// 这里只留一个必然失败的探针以外全部清空，官方地址（即 mock server）始终作为兜底存在。
		CDNProviders: []string{},
	})
	return m, dir
}

// AdGuard 二进制默认落在 DataDir/bin/adguardFileName()
func TestAdGuardBinaryPathDefault(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{DataDir: dir})
	want := filepath.Join(dir, "bin", adguardFileName())
	if got := m.AdGuardBinaryPath(); got != want {
		t.Fatalf("AdGuardBinaryPath 默认路径不符\n期望 %q\n实际 %q", want, got)
	}
}

// 显式配置应覆盖默认路径
func TestAdGuardBinaryPathCustom(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "custom", "agh")
	m := New(Config{DataDir: dir, AdGuardBinaryPath: custom})
	if got := m.AdGuardBinaryPath(); got != custom {
		t.Fatalf("应使用自定义路径，实际 %q", got)
	}
}

// 未配置时默认仓库为官方 AdguardTeam/AdGuardHome
func TestAdGuardRepoDefault(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	if got := m.repoAdGuard(); got != "AdguardTeam/AdGuardHome" {
		t.Fatalf("默认 AdGuardRepo 应为 AdguardTeam/AdGuardHome，实际 %q", got)
	}
}

// 本地未安装 mihomo 时，Present 应为 false，且应判定为需要更新
func TestCheckLatestMihomoNotPresent(t *testing.T) {
	m, _ := newTestManager(t, "v1.2.3", "v9.9.9", "v0.1.0")
	mihomo, _, _ := m.CheckLatest(context.Background(), "", "")
	if mihomo.Present {
		t.Fatal("未安装 mihomo 时 Present 应为 false")
	}
	if !mihomo.UpdateNeeded {
		t.Fatal("未安装时应判定为需要更新")
	}
	if mihomo.LatestVersion != "v1.2.3" {
		t.Fatalf("应正确取回远端最新版本，实际 %q", mihomo.LatestVersion)
	}
}

// 本地版本与远端一致时不应判定为需要更新
func TestCheckLatestMihomoUpToDate(t *testing.T) {
	m, dir := newTestManager(t, "v1.2.3", "v9.9.9", "v0.1.0")
	binPath := filepath.Join(dir, "bin", mihomoFileName())
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	mihomo, _, _ := m.CheckLatest(context.Background(), "Mihomo Meta v1.2.3 windows amd64", "")
	if !mihomo.Present {
		t.Fatal("已写入二进制文件，Present 应为 true")
	}
	if mihomo.UpdateNeeded {
		t.Fatalf("本地版本已包含远端 tag，不应判定为需要更新: local=%q latest=%q", "Mihomo Meta v1.2.3 windows amd64", mihomo.LatestVersion)
	}
}

// 本地版本与远端不一致时应判定为需要更新
func TestCheckLatestMihomoOutdated(t *testing.T) {
	m, dir := newTestManager(t, "v2.0.0", "v9.9.9", "v0.1.0")
	binPath := filepath.Join(dir, "bin", mihomoFileName())
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	mihomo, _, _ := m.CheckLatest(context.Background(), "Mihomo Meta v1.2.3 windows amd64", "")
	if !mihomo.UpdateNeeded {
		t.Fatal("本地版本落后于远端，应判定为需要更新")
	}
	if mihomo.LatestVersion != "v2.0.0" {
		t.Fatalf("应取回远端最新版本 v2.0.0，实际 %q", mihomo.LatestVersion)
	}
}

// zashboard 是纯静态资源，只按目录是否就绪判断，不做版本号比对
func TestCheckLatestZashboardPresence(t *testing.T) {
	m, dir := newTestManager(t, "v1.0.0", "v3.4.5", "v0.1.0")

	_, zash, _ := m.CheckLatest(context.Background(), "", "")
	if zash.Present {
		t.Fatal("未下载 zashboard 时 Present 应为 false")
	}
	if !zash.UpdateNeeded {
		t.Fatal("未下载时应判定为需要更新")
	}

	zdir := filepath.Join(dir, "zashboard")
	if err := os.MkdirAll(zdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zdir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, zash, _ = m.CheckLatest(context.Background(), "", "")
	if !zash.Present {
		t.Fatal("写入 index.html 后 Present 应为 true")
	}
	if zash.UpdateNeeded {
		t.Fatal("zashboard 已就绪时不应判定为需要更新")
	}
	if zash.LatestVersion != "v3.4.5" {
		t.Fatalf("应取回远端最新版本，实际 %q", zash.LatestVersion)
	}
}

// AdGuard 未安装时 Present=false 且需要更新；安装后按版本比对
func TestCheckLatestAdGuard(t *testing.T) {
	m, dir := newTestManager(t, "v1.0.0", "v1.0.0", "v0.107.50")

	_, _, agh := m.CheckLatest(context.Background(), "", "")
	if agh.Present {
		t.Fatal("未安装 AdGuard 时 Present 应为 false")
	}
	if !agh.UpdateNeeded {
		t.Fatal("未安装时应判定为需要更新")
	}
	if agh.LatestVersion != "v0.107.50" {
		t.Fatalf("应取回远端最新版本，实际 %q", agh.LatestVersion)
	}

	binPath := filepath.Join(dir, "bin", adguardFileName())
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake agh"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, agh = m.CheckLatest(context.Background(), "", "AdGuard Home, version v0.107.50")
	if !agh.Present {
		t.Fatal("已写入二进制，Present 应为 true")
	}
	if agh.UpdateNeeded {
		t.Fatal("本地版本已匹配远端 tag，不应需要更新")
	}

	_, _, agh = m.CheckLatest(context.Background(), "", "AdGuard Home, version v0.100.0")
	if !agh.UpdateNeeded {
		t.Fatal("本地版本落后时应判定为需要更新")
	}
}

// GitHub API 不可达时应把错误信息透出，而不是 panic 或吞掉
func TestCheckLatestAPIFailure(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{
		DataDir:       dir,
		MihomoRepo:    "mihomo-repo/x",
		ZashboardRepo: "zashboard-repo/x",
		AdGuardRepo:   "adguard-repo/x",
		GitHubAPI:     "http://127.0.0.1:1/definitely-unreachable",
		CDNProviders:  []string{},
		// 该场景会触发完整的 CDN 回退链（8 个候选地址逐一失败），
		// 缩短超时避免测试套件被真实网络的连接/DNS 超时拖慢。
		HTTPTimeoutSeconds: 2,
	})

	mihomo, zash, agh := m.CheckLatest(context.Background(), "", "")
	if mihomo.Error == "" {
		t.Fatal("API 不可达时应记录错误信息")
	}
	if zash.Error == "" {
		t.Fatal("API 不可达时应记录错误信息")
	}
	if agh.Error == "" {
		t.Fatal("API 不可达时应记录错误信息")
	}
}

package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/version"
)

// 未配置 SelfRepo 时，检查接口必须返回 ErrSelfRepoNotConfigured，
// 而不是请求一个错误的默认仓库地址。
func TestCheckSelfUpdateNotConfigured(t *testing.T) {
	// 显式清空仓库 = 停用自升级，即使默认值存在也报未配置
	m := New(Config{DataDir: t.TempDir()})
	m.SetSelfRepo("")
	_, err := m.CheckSelfUpdate(context.Background())
	if !errors.Is(err, ErrSelfRepoNotConfigured) {
		t.Fatalf("清空仓库后应返回 ErrSelfRepoNotConfigured，实际 %v", err)
	}
}

// SelfRepo 默认值为 weni09/AuroraMihomo；SetSelfRepo 可覆盖或清空（清空=停用）。
func TestSelfRepoDefaultAndSetter(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	if got := m.SelfRepo(); got != DefaultSelfRepo {
		t.Fatalf("默认仓库应为 %q，实际 %q", DefaultSelfRepo, got)
	}

	// 覆盖为自建仓库
	m.SetSelfRepo("myuser/AuroraMihomo")
	if got := m.SelfRepo(); got != "myuser/AuroraMihomo" {
		t.Fatalf("SetSelfRepo 后应生效，实际 %q", got)
	}
	if !m.SelfRepoConfigured() {
		t.Fatal("有效仓库应判定为已配置")
	}

	// 幂等：重复设置同一值
	m.SetSelfRepo("  myuser/AuroraMihomo  ")
	if got := m.SelfRepo(); got != "myuser/AuroraMihomo" {
		t.Fatalf("重复设置应幂等且 trim，实际 %q", got)
	}

	// 清空 = 显式停用
	m.SetSelfRepo("")
	if m.SelfRepo() != "" {
		t.Fatalf("清空后 SelfRepo 应为空，实际 %q", m.SelfRepo())
	}
	if m.SelfRepoConfigured() {
		t.Fatal("空仓库应判定为未配置")
	}
	// 清空后 CheckSelfUpdate 报未配置（不允许从空仓库拉取）
	if _, err := m.CheckSelfUpdate(context.Background()); !errors.Is(err, ErrSelfRepoNotConfigured) {
		t.Fatalf("空仓库检查应报未配置，实际 %v", err)
	}
}

// 版本对比：当前版本已包含最新 tag 时无更新，否则有更新。
func TestCheckSelfUpdateVersionCompare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3",
			"assets":   []map[string]string{{"name": "x.zip", "browser_download_url": "https://example.com/x.zip"}},
		})
	}))
	t.Cleanup(srv.Close)

	m := New(Config{DataDir: t.TempDir(), SelfRepo: "owner/AuroraMihomo", GitHubAPI: srv.URL})

	orig := version.AppVersion
	defer func() { version.AppVersion = orig }()

	version.AppVersion = "v1.2.3"
	check, err := m.CheckSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("检查失败: %v", err)
	}
	if !check.Configured {
		t.Fatal("已配置仓库，Configured 应为 true")
	}
	if check.UpdateAvailable {
		t.Fatal("当前版本已是最新，不应提示更新")
	}
	if check.LatestVersion != "v1.2.3" || check.CurrentVersion != "v1.2.3" {
		t.Fatalf("版本字段不符: %+v", check)
	}

	version.AppVersion = "v1.0.0"
	check, err = m.CheckSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("检查失败: %v", err)
	}
	if !check.UpdateAvailable {
		t.Fatal("当前版本落后，应提示更新")
	}
}

// 主程序版本必须精确匹配，不能用 Contains：否则 v1.2.3 会被误判为已覆盖 v1.2.30。
func TestSelfVersionEquals(t *testing.T) {
	if !selfVersionEquals("v1.2.3", "v1.2.3") {
		t.Fatal("同 tag 应相等")
	}
	if !selfVersionEquals("1.2.3", "v1.2.3") {
		t.Fatal("有无 v 前缀应视为同一版本")
	}
	if selfVersionEquals("v1.2.3", "v1.2.30") {
		t.Fatal("v1.2.3 不应被判定为已覆盖 v1.2.30")
	}
	if selfVersionEquals("v1.2.3", "v1.3.0") {
		t.Fatal("不同小版本应判定为需要更新")
	}
}

// 远端查询失败时返回带 Error 字段的检查结果，而非报错或谎报"无更新"。
func TestCheckSelfUpdateAPIFailure(t *testing.T) {
	m := New(Config{
		DataDir:            t.TempDir(),
		SelfRepo:           "owner/AuroraMihomo",
		GitHubAPI:          "http://127.0.0.1:1/definitely-unreachable",
		HTTPTimeoutSeconds: 2,
	})
	check, err := m.CheckSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("API 失败应返回带 Error 的检查结果而非报错: %v", err)
	}
	if check.Error == "" {
		t.Fatal("API 失败时应记录错误信息")
	}
	if check.UpdateAvailable {
		t.Fatal("无法确认版本时不应谎报有更新")
	}
}

// tarGzOf 构造一个含单文件（可执行 shell 脚本）的 tar.gz 归档。
func tarGzOf(t *testing.T, fileName string, content []byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(zw)
	hdr := &tar.Header{
		Name: fileName,
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

// selfUpdateAsset 构造一个可被 UpdateSelf 接受的主程序 release 资产：
//
//   - Windows：zip 内放当前测试进程的可执行文件（命名为 auroramihomo.exe）。
//     verifySelfBinary 的规则是"进程能启动且输出非空即通过"：Go 测试二进制
//     收到未知 -version flag 会打印 flag 错误并退出（输出非空），恰好通过校验，
//     因此整条下载-校验-暂存-交换链路在 Windows 上也能真实跑通。
//   - 其它平台：tar.gz 内放可执行 shell 脚本（经 paddedShellScript 填充，
//     满足 downloadWithCDN 的 1024 字节最小体积校验）。
//
// 返回 (资产内容, 资产文件名)。
func selfUpdateAsset(t *testing.T) ([]byte, string) {
	t.Helper()
	name := selfArchiveName("v2.0.0")
	if runtime.GOOS != "windows" {
		return tarGzOf(t, "auroramihomo", selfTestPayload()), name
	}

	// Windows：zip 内塞测试进程自身
	exePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeBytes, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hw, err := zw.Create("auroramihomo.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hw.Write(exeBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), name
}

// UpdateSelf 下载-校验-暂存为 .new；SwapSelfBinary 在关停时替换生效。
// 完整链路：release JSON → 资产 → .sha256 → 解压 → -version 校验
// → 暂存 → 交换 → 清理。
func TestSelfUpdateStageAndSwap(t *testing.T) {
	archive, name := selfUpdateAsset(t)
	sum := sha256.Sum256(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2.0.0",
				"assets":   []map[string]string{{"name": "x.zip", "browser_download_url": "https://example.com/x.zip"}},
			})
		case strings.HasSuffix(r.URL.Path, ".tar.gz") || strings.HasSuffix(r.URL.Path, ".zip"):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	if err := os.WriteFile(target, []byte("OLD-SELF"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := New(Config{
		DataDir:          dir,
		SelfRepo:         "owner/AuroraMihomo",
		SelfBinaryPath:   target,
		SelfDownloadBase: srv.URL,
		GitHubAPI:        srv.URL,
		CDNProviders:     []string{"http://127.0.0.1:1/cdn"},
		UseMihomoProxy:   false,
	})

	if err := runSelfUpdateWait(t, m); err != nil {
		t.Fatalf("UpdateSelf 失败: %v", err)
	}

	stage := target + ".new"
	stageContent, err := os.ReadFile(stage)
	if err != nil {
		t.Fatalf(".new 未生成: %v", err)
	}
	// .new 必须真的被替换为新二进制（内容 ≠ 预置的 OLD-SELF）
	if string(stageContent) == "OLD-SELF" {
		t.Fatalf(".new 内容仍是旧二进制，替换未发生")
	}
	if !bytes.Equal(stageContent, archiveContentOf(t, archive)) {
		t.Fatalf(".new 内容与解压出的新二进制不一致")
	}
	// 暂存阶段不得触碰运行中的二进制
	if cur, _ := os.ReadFile(target); string(cur) != "OLD-SELF" {
		t.Fatalf("暂存阶段不应改动运行中的二进制")
	}

	// 交换后自身被新版替换
	if err := m.SwapSelfBinary(); err != nil {
		t.Fatalf("SwapSelfBinary 失败: %v", err)
	}
	if cur, _ := os.ReadFile(target); bytes.Equal(cur, []byte("OLD-SELF")) {
		t.Fatalf("交换后自身应被新版替换")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf(".new 应在交换后被消费")
	}

	// 再次交换无 .new 时是空操作
	if err := m.SwapSelfBinary(); err != nil {
		t.Fatalf("无 .new 时 SwapSelfBinary 应为空操作: %v", err)
	}
}

// selfTestPayload 返回非 Windows 分支的测试二进制内容（可执行 shell 脚本）。
// 供资产构造与内容断言复用，保证两处一致：脚本执行 -v 输出版本行，
// 填充部分是不影响执行的注释。
func selfTestPayload() []byte {
	return paddedShellScript("echo auroramihomo-v2.0.0")
}

// archiveContentOf 返回资产解压后应得到的单个二进制内容，用于断言 .new 内容。
// Windows 的 zip 内条目即测试进程 exe，非 Windows 的 tar.gz 内条目即 shell 脚本。
func archiveContentOf(t *testing.T, archive []byte) []byte {
	t.Helper()
	if runtime.GOOS != "windows" {
		return selfTestPayload()
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "auroramihomo.exe" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			content, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return content
		}
	}
	t.Fatal("zip 内找不到 auroramihomo.exe")
	return nil
}

// 校验和不匹配时拒绝暂存，磁盘上不留下任何半成品。
// 模拟历史 release 场景：无独立 .sha256 资产，只有 SHA256SUMS.txt（错误值）。
func TestSelfUpdateRejectsChecksumMismatch(t *testing.T) {
	archive, name := selfUpdateAsset(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2.0.0",
				"assets":   []map[string]string{{"name": "x.zip", "browser_download_url": "https://example.com/x.zip"}},
			})
		case strings.HasSuffix(r.URL.Path, ".tar.gz") || strings.HasSuffix(r.URL.Path, ".zip"):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			// 独立 .sha256 缺失（历史 release 没有）→ 应回落 SHA256SUMS.txt
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS.txt"):
			// 汇总文件里给错误校验和
			_, _ = fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	if err := os.WriteFile(target, []byte("OLD-SELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		DataDir:          dir,
		SelfRepo:         "owner/AuroraMihomo",
		SelfBinaryPath:   target,
		SelfDownloadBase: srv.URL,
		GitHubAPI:        srv.URL,
		CDNProviders:     []string{"http://127.0.0.1:1/cdn"},
		UseMihomoProxy:   false,
	})

	if err := runSelfUpdateWait(t, m); err == nil {
		t.Fatal("校验和不匹配时 UpdateSelf 应报错")
	}
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Fatalf("校验失败时不应留下 .new")
	}
}

// 独立 .sha256 缺失但 SHA256SUMS.txt 命中时，应通过校验并完成暂存。
func TestSelfUpdateFallsBackToSumsFile(t *testing.T) {
	archive, name := selfUpdateAsset(t)
	sum := sha256.Sum256(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2.0.0",
				"assets":   []map[string]string{{"name": "x.zip", "browser_download_url": "https://example.com/x.zip"}},
			})
		case strings.HasSuffix(r.URL.Path, ".tar.gz") || strings.HasSuffix(r.URL.Path, ".zip"):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "SHA256SUMS.txt"):
			// 包含本包的正确校验和（还混了一个无关包的行）
			_, _ = fmt.Fprintf(w, "%s  other-package.tar.gz\n%s  %s\n",
				strings.Repeat("1", 64), hex.EncodeToString(sum[:]), name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	if err := os.WriteFile(target, []byte("OLD-SELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		DataDir:          dir,
		SelfRepo:         "owner/AuroraMihomo",
		SelfBinaryPath:   target,
		SelfDownloadBase: srv.URL,
		GitHubAPI:        srv.URL,
		CDNProviders:     []string{"http://127.0.0.1:1/cdn"},
		UseMihomoProxy:   false,
	})

	if err := runSelfUpdateWait(t, m); err != nil {
		t.Fatalf("SHA256SUMS.txt 回落路径应通过校验: %v", err)
	}
	if _, err := os.Stat(target + ".new"); err != nil {
		t.Fatalf("应生成 .new: %v", err)
	}
}

// parseChecksumFile 支持普通与 * 前缀（sha256sum -c 二进制模式）两种写法。
func TestParseChecksumFile(t *testing.T) {
	body := []byte("aa  a.tar.gz\nbb *b.tar.gz\n\n  # 注释行\ncc   c.tar.gz")
	sum, err := parseChecksumFile(body, "b.tar.gz")
	if err != nil {
		t.Fatalf("带 * 前缀的行应被解析: %v", err)
	}
	if sum != "bb" {
		t.Fatalf("b.tar.gz 校验和应为 bb，实际 %q", sum)
	}

	if _, err := parseChecksumFile(body, "missing.tar.gz"); err == nil {
		t.Fatal("不存在的文件名应报错")
	}
}

// 自升级成功暂存后应置位 InProgress，二次调用立刻返回 ErrSelfUpdateInProgress。
func TestSelfUpdateInProgressRejectsSecondCall(t *testing.T) {
	archive, name := selfUpdateAsset(t)
	sum := sha256.Sum256(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2.0.0",
				"assets":   []map[string]string{{"name": "x.zip", "browser_download_url": "https://example.com/x.zip"}},
			})
		case strings.HasSuffix(r.URL.Path, ".tar.gz") || strings.HasSuffix(r.URL.Path, ".zip"):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	if err := os.WriteFile(target, []byte("OLD-SELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(Config{
		DataDir:          dir,
		SelfRepo:         "owner/AuroraMihomo",
		SelfBinaryPath:   target,
		SelfDownloadBase: srv.URL,
		GitHubAPI:        srv.URL,
		CDNProviders:     []string{"http://127.0.0.1:1/cdn"},
		UseMihomoProxy:   false,
	})

	if err := runSelfUpdateWait(t, m); err != nil {
		t.Fatalf("首次 UpdateSelf 失败: %v", err)
	}
	if !m.SelfUpdateInProgress() {
		t.Fatal("暂存成功后 SelfUpdateInProgress 应为 true")
	}
	if err := m.StartSelfUpdate(context.Background()); !errors.Is(err, ErrSelfUpdateInProgress) {
		t.Fatalf("二次调用应返回 ErrSelfUpdateInProgress，实际 %v", err)
	}
}

// 下载/校验失败后必须释放 InProgress，允许重试。
func TestSelfUpdateFailureClearsInProgress(t *testing.T) {
	m := New(Config{
		DataDir:            t.TempDir(),
		SelfRepo:           "owner/AuroraMihomo",
		SelfBinaryPath:     filepath.Join(t.TempDir(), "auroramihomo"),
		GitHubAPI:          "http://127.0.0.1:1/definitely-unreachable",
		HTTPTimeoutSeconds: 2,
		CDNProviders:       []string{"http://127.0.0.1:1/cdn"},
		UseMihomoProxy:     false,
	})
	if err := runSelfUpdateWait(t, m); err == nil {
		t.Fatal("API 不可达时应失败")
	}
	if m.SelfUpdateInProgress() {
		t.Fatal("失败后 SelfUpdateInProgress 应被清除，以便重试")
	}
}

// CleanupStaleSelf 删 .old 残留；.new 仅在「仍是更高版本的升级」时保留。
// 这里 .new 是不可执行内容（无法通过 -version 探测），必须被清理，
// 否则下次关停 SwapSelfBinary 会换入坏版本。
func TestCleanupStaleSelf(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	_ = os.WriteFile(target, []byte("x"), 0o755)
	_ = os.WriteFile(target+".old", []byte("old"), 0o755)
	_ = os.WriteFile(target+".new", []byte("new"), 0o755)

	m := New(Config{DataDir: dir, SelfBinaryPath: target})
	m.CleanupStaleSelf()

	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old 残留应被清理")
	}
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Fatal("无法探测版本的 .new 应被清理（避免换入坏版本）")
	}
}

// CleanupStaleSelf 对「版本高于当前的待生效升级」必须保留，等待下次关停交换：
// 崩溃恢复语义（暂存后异常退出 → 下次启动继续完成升级）仍然成立。
func TestCleanupStaleSelfKeepsValidUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("版本探测需要执行二进制，Windows 用真实 exe，测试不便构造")
	}
	orig := version.AppVersion
	defer func() { version.AppVersion = orig }()
	version.AppVersion = "v1.0.0"

	dir := t.TempDir()
	target := filepath.Join(dir, "auroramihomo")
	_ = os.WriteFile(target, []byte("x"), 0o755)
	_ = os.WriteFile(target+".new", paddedShellScript("echo auroramihomo-v2.0.0"), 0o755)

	m := New(Config{DataDir: dir, SelfBinaryPath: target})
	m.CleanupStaleSelf()

	if _, err := os.Stat(target + ".new"); err != nil {
		t.Fatal("更高版本的待生效 .new 应保留，等待下次关停交换")
	}
}

// stagedSelfStatus 在交换/启动时核验待生效版本，防止降级或换入坏版本。
func TestStagedSelfStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("版本探测需要执行二进制，Windows 用真实 exe，测试不便构造")
	}
	orig := version.AppVersion
	defer func() { version.AppVersion = orig }()
	version.AppVersion = "v1.0.0"

	dir := t.TempDir()
	m := New(Config{DataDir: dir})
	stage := filepath.Join(dir, "auroramihomo.new")

	t.Run("更高版本保留", func(t *testing.T) {
		if err := os.WriteFile(stage, paddedShellScript("echo auroramihomo-v2.0.0"), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stage)
		keep, reason := m.stagedSelfStatus(stage)
		if !keep {
			t.Fatalf("更高版本应保留，继续完成升级，got reason=%q", reason)
		}
	})
	t.Run("同版本丢弃", func(t *testing.T) {
		if err := os.WriteFile(stage, paddedShellScript("echo auroramihomo-v1.0.0"), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stage)
		keep, reason := m.stagedSelfStatus(stage)
		if keep {
			t.Fatalf("同版本应丢弃（无降级也不重复升级），got reason=%q", reason)
		}
	})
	t.Run("旧版本丢弃", func(t *testing.T) {
		if err := os.WriteFile(stage, paddedShellScript("echo auroramihomo-v0.9.0"), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stage)
		keep, reason := m.stagedSelfStatus(stage)
		if keep {
			t.Fatalf("旧版本应丢弃，避免降级，got reason=%q", reason)
		}
	})
	t.Run("损坏丢弃", func(t *testing.T) {
		if err := os.WriteFile(stage, []byte("not a binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stage)
		keep, reason := m.stagedSelfStatus(stage)
		if keep {
			t.Fatalf("损坏的 .new 应丢弃，got reason=%q", reason)
		}
	})
	t.Run("两侧均无版本号保留", func(t *testing.T) {
		// .new 输出无版本号（dev），当前也是 dev：无法判断，保留崩溃恢复语义
		if err := os.WriteFile(stage, paddedShellScript("echo dev"), 0o755); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stage)
		version.AppVersion = "dev"
		defer func() { version.AppVersion = "v1.0.0" }()
		keep, reason := m.stagedSelfStatus(stage)
		if !keep {
			t.Fatalf("两侧均无版本号时不应丢弃，保持崩溃恢复语义，got reason=%q", reason)
		}
	})
}

// selfVersionNumeric 从 -version 输出形态的字符串里提取主.次.补丁数值。
func TestSelfVersionNumeric(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.3", "1002003"},
		{"auroramihomo-v2.0.0", "2000000"},
		{"v0.9.4", "9004"},
		{"dev", "0"},
		{"", "0"},
	}
	for _, c := range cases {
		if got := selfVersionNumeric(c.in); fmt.Sprint(got) != c.want {
			t.Errorf("selfVersionNumeric(%q) = %d, want %s", c.in, got, c.want)
		}
	}
	// 数值大小关系：主版本优先于补丁
	if selfVersionNumeric("v2.0.0") <= selfVersionNumeric("v1.999.999") {
		t.Fatal("主版本号应优先于补丁版本号")
	}
}

// 官方 github.com 拉不到 .sha256 / SHA256SUMS 时，必须按「下载源」回落。
// 国内直连 GitHub 失败、包却已从 ghproxy 下到，是主程序升级最常见的卡点。
func TestFetchSelfChecksumFallsBackToCDN(t *testing.T) {
	const name = "auroramihomo_v1.0.0_linux_amd64.tar.gz"
	wantSum := strings.Repeat("ab", 32)
	shaBody := wantSum + "  " + name + "\n"

	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(official.Close)

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// buildCDNURLs 前缀源拼成 "<cdn>/<officialURL>"
		if !strings.Contains(r.URL.Path, name+".sha256") && !strings.HasSuffix(r.URL.Path, name+".sha256") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(shaBody))
	}))
	t.Cleanup(cdn.Close)

	m := New(Config{
		DataDir:          t.TempDir(),
		SelfRepo:         "owner/AuroraMihomo",
		SelfDownloadBase: official.URL,
		CDNProviders:     []string{cdn.URL},
		UseMihomoProxy:   false,
	})

	archiveURL := m.selfDownloadURL("v1.0.0", name)
	got, err := m.fetchSelfChecksum(context.Background(), "owner/AuroraMihomo", "v1.0.0", archiveURL, name)
	if err != nil {
		t.Fatalf("官方校验和 404 时应回落下载源: %v", err)
	}
	if got != wantSum {
		t.Fatalf("校验和 %q，期望 %q", got, wantSum)
	}
}

// SHA256SUMS.txt 同样必须走下载源：历史 release 只有汇总文件。
func TestFetchSelfChecksumSumsFileFallsBackToCDN(t *testing.T) {
	const name = "auroramihomo_v1.0.0_linux_amd64.tar.gz"
	wantSum := strings.Repeat("cd", 32)

	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(official.Close)

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "SHA256SUMS.txt") {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, "%s  %s\n", wantSum, name)
	}))
	t.Cleanup(cdn.Close)

	m := New(Config{
		DataDir:          t.TempDir(),
		SelfRepo:         "owner/AuroraMihomo",
		SelfDownloadBase: official.URL,
		CDNProviders:     []string{cdn.URL},
		UseMihomoProxy:   false,
	})

	archiveURL := m.selfDownloadURL("v1.0.0", name)
	got, err := m.fetchSelfChecksum(context.Background(), "owner/AuroraMihomo", "v1.0.0", archiveURL, name)
	if err != nil {
		t.Fatalf("SHA256SUMS 官方 404 时应回落下载源: %v", err)
	}
	if got != wantSum {
		t.Fatalf("校验和 %q，期望 %q", got, wantSum)
	}
}

func TestClassifySelfUpdateError(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ErrSelfRepoNotConfigured, "repo_not_configured"},
		{ErrSelfUpdateInProgress, "already_in_progress"},
		{fmt.Errorf("query failed: %w", errSelfCheckFailed), "check_failed"},
		{fmt.Errorf("download failed: %w", errSelfDownloadFailed), "download_failed"},
		{fmt.Errorf("checksum: %w", errSelfChecksumMismatch), "checksum_mismatch"},
		{fmt.Errorf("extract: %w", errSelfExtractFailed), "extract_failed"},
		{fmt.Errorf("verify: %w", errSelfVerifyFailed), "verify_failed"},
		{errors.New("boom"), "internal"},
	}
	for _, c := range cases {
		got := classifySelfUpdateError(c.err)
		if got == nil || got.Code != c.code {
			t.Fatalf("%v => code %q, want %q", c.err, gotCode(got), c.code)
		}
	}
}

// 下载失败必须把各源明细留在 Message，不能只给一句「全部失败」。
func TestClassifySelfUpdateErrorKeepsDownloadDetail(t *testing.T) {
	err := fmt.Errorf("%w: all download sources failed: ghproxy => 502 | github => timeout", errSelfDownloadFailed)
	got := classifySelfUpdateError(err)
	if got == nil || got.Code != "download_failed" {
		t.Fatalf("code=%v", got)
	}
	if !strings.Contains(got.Message, "ghproxy => 502") || !strings.Contains(got.Message, "github => timeout") {
		t.Fatalf("Message 应保留各源失败明细, got %q", got.Message)
	}
}

func gotCode(e *SelfUpdateError) string {
	if e == nil {
		return "<nil>"
	}
	return e.Code
}

func TestSelfUpdateStatusInitialAndPhase(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), SelfRepo: "owner/AuroraMihomo"})
	st := m.GetSelfUpdateStatus()
	if st.Running || st.Phase != "idle" {
		t.Fatalf("初始应为 idle 非运行, got %+v", st)
	}
	m.setSelfPhase("downloading", "下载中")
	st = m.GetSelfUpdateStatus()
	if !st.Running || st.Phase != "downloading" || st.Message != "下载中" {
		t.Fatalf("setSelfPhase 应置 Running 并推进阶段, got %+v", st)
	}
	m.setSelfPhase("idle", "")
	if st2 := m.GetSelfUpdateStatus(); st2.Running || st2.Phase != "idle" {
		t.Fatalf("idle 应清 Running, got %+v", st2)
	}
}

// 异步启动：StartSelfUpdate 应立即返回（不等下载完成），
// 状态机随后推进到 downloading 并最终 failed（无可用下载源）。
func TestStartSelfUpdateAsyncReturnsImmediately(t *testing.T) {
	// API 可达并返回 release 元数据，但下载源全失败 → 走 download_failed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v2.0.0",
				"assets":   []map[string]any{{"name": selfArchiveName("v2.0.0"), "browser_download_url": "http://127.0.0.1:1/a.zip", "size": 1}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	m := New(Config{
		DataDir:            t.TempDir(),
		SelfRepo:           "owner/AuroraMihomo",
		GitHubAPI:          srv.URL,
		UseMihomoProxy:     false,
		HTTPTimeoutSeconds: 2,
		CDNProviders:       []string{"http://127.0.0.1:1/cdn"},
	})
	err := m.StartSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("StartSelfUpdate 应立即接受: %v", err)
	}
	if !m.SelfUpdateInProgress() {
		t.Fatal("StartSelfUpdate 后 SelfUpdateInProgress 应为 true")
	}
	st := m.GetSelfUpdateStatus()
	if !st.Running || st.Phase == "idle" {
		t.Fatalf("启动后应处于某进行中阶段, got %+v", st)
	}
	// 等后台 goroutine 收敛（下载源全失败 → failed）
	deadline := time.Now().Add(15 * time.Second)
	for {
		st = m.GetSelfUpdateStatus()
		if !st.Running || st.Phase == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("升级应在无下载源时快速失败, 状态卡在 %+v", st)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Error == nil || st.Error.Code != "download_failed" {
		t.Fatalf("应映射 download_failed, got %+v", st.Error)
	}
}

// 未配置仓库时异步启动应同步返回错误，且不进入运行态。
func TestStartSelfUpdateNotConfigured(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), SelfRepo: " "})
	err := m.StartSelfUpdate(context.Background())
	if !errors.Is(err, ErrSelfRepoNotConfigured) {
		t.Fatalf("未配置仓库应同步返回 ErrSelfRepoNotConfigured, got %v", err)
	}
	if st := m.GetSelfUpdateStatus(); st.Running || st.Phase != "idle" {
		t.Fatalf("未配置不应进入运行态, got %+v", st)
	}
}

// 已在升级中时二次启动应同步拒绝。
func TestStartSelfUpdateRejectsSecond(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), SelfRepo: "owner/AuroraMihomo", UseMihomoProxy: false})
	if err := m.StartSelfUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.StartSelfUpdate(context.Background()); !errors.Is(err, ErrSelfUpdateInProgress) {
		t.Fatalf("二次启动应返回 ErrSelfUpdateInProgress, got %v", err)
	}
}

// runSelfUpdateWait 异步启动升级并等待后台完成，返回升级结果。
// 用于把原同步 UpdateSelf 的测试改到异步语义：测试只关心最终成败与 .new
// 落盘，不关心中间阶段。失败时返回分类后的错误信息。
func runSelfUpdateWait(t *testing.T, m *Manager) error {
	t.Helper()
	if err := m.StartSelfUpdate(context.Background()); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		st := m.GetSelfUpdateStatus()
		// restarting 是成功路径的最终态：selfUpdating 保持 true 直到进程退出，
		// 测试里没有进程管理器拉起新版，以 restarting 作为完成信号
		if !st.Running || st.Phase == "restarting" {
			if st.Error != nil {
				return fmt.Errorf("%s: %s", st.Error.Code, st.Error.Message)
			}
			return nil
		}
		if time.Now().After(deadline) {
			t.Fatalf("升级后台任务超时未完成, 状态 %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CheckSelfUpdate 应带回 release body 作变更日志，超长时截断。
func TestCheckSelfUpdateCarriesReleaseNotes(t *testing.T) {
	notes := strings.Repeat("x", maxReleaseNotesLen+100)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"body":     notes,
			"assets": []map[string]any{
				{"name": selfArchiveName("v2.0.0"), "browser_download_url": srv.URL + "/a", "size": 1},
			},
		})
	}))
	defer srv.Close()
	m := New(Config{DataDir: t.TempDir(), SelfRepo: "owner/AuroraMihomo", GitHubAPI: srv.URL})
	check, err := m.CheckSelfUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(check.ReleaseNotes) != maxReleaseNotesLen {
		t.Fatalf("release notes 应截断到 %d, got %d", maxReleaseNotesLen, len(check.ReleaseNotes))
	}
	if check.ReleaseNotes[:10] != notes[:10] {
		t.Fatal("截断应保留开头内容")
	}
}

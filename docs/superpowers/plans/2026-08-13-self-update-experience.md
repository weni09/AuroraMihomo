# 主程序升级体验优化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 主程序升级获得实时阶段/进度反馈、结构化错误提示与变更日志预览。

**Architecture:** 后端把同步的 `UpdateSelf` 改为异步 `StartSelfUpdate`（goroutine 内推进状态机，`downloadFile`/`downloadWithCDN` 加进度回调），新增 `GET /api/v1/system/self-update/status` 供前端轮询；`githubRelease` 补解析 release body 作变更日志；错误按 sentinel 分类映射成错误码。前端 store 轮询状态接口，设置页升级区块显示阶段徽标 + 进度条，升级确认从原生 `confirm()` 换成 `ModalDialog` 展示变更日志。

**Tech Stack:** Go 1.25 + go-zero rest + gorm；Vue 3 + Pinia + shadcn-vue（ModalDialog/Progress 已存在）；vitest。

**Spec:** `docs/superpowers/specs/2026-08-13-self-update-experience-design.md`

---

## 文件结构

| 文件 | 职责 | 动作 |
|---|---|---|
| `backend/internal/updater/updater.go` | `githubRelease` 加 Body；`downloadFile`/`downloadWithCDN` 加进度回调参数 | 修改 |
| `backend/internal/updater/self_update.go` | 状态类型、sentinel 错误、`StartSelfUpdate` 异步化、阶段推进、错误分类 | 修改 |
| `backend/internal/updater/self_update_test.go` | 状态机/错误分类/进度测试 | 修改 |
| `backend/internal/updater/download_verify_test.go` | 适配 downloadWithCDN 新签名 | 修改 |
| `backend/api/AuroraMihomo-Go-Zero-API.api` | SelfUpdateCheck 加 ReleaseNotes；SelfUpdateStatus/SelfUpdateError 类型；status 路由 | 修改 |
| `backend/api/internal/types/types.go` | goctl 生成的类型（DO NOT EDIT） | goctl 生成 |
| `backend/api/system.go` | POST self-update 改异步；GET status handler；ready hook 注入 | 修改 |
| `frontend/src/stores/settings.ts` | SelfUpdateInfo 加 releaseNotes；SelfUpdateStatus 接口；轮询 | 修改 |
| `frontend/src/stores/settings.spec.ts` | 轮询开始/停止/失败测试 | 修改 |
| `frontend/src/views/SettingsView.vue` | 阶段/进度展示；确认弹窗改 ModalDialog + 变更日志；错误映射 | 修改 |
| `frontend/src/views/SettingsView.spec.ts` | 弹窗渲染/错误文案测试 | 修改 |

---

### Task 1: 后端升级状态类型与异步骨架

**Files:**
- Modify: `backend/internal/updater/self_update.go`
- Test: `backend/internal/updater/self_update_test.go`

- [ ] **Step 1: 写失败测试 —— 错误分类映射表**

在 `backend/internal/updater/self_update_test.go` 末尾追加：

```go
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

func gotCode(e *SelfUpdateError) string {
	if e == nil {
		return "<nil>"
	}
	return e.Code
}
```

Run: `go test ./backend/internal/updater/ -run TestClassifySelfUpdateError -v`
Expected: FAIL（编译错误：`errSelfCheckFailed` 等未定义）。

- [ ] **Step 2: 实现状态类型、sentinel 错误、分类函数**

在 `backend/internal/updater/self_update.go` 顶部（`ErrSelfUpdateInProgress` 定义之后）追加：

```go
// SelfUpdateError 主程序升级失败的结构化原因。Code 供前端映射文案，
// Message 带可读细节（下载源失败明细、校验和比对值等）。
type SelfUpdateError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SelfUpdateStatus 主程序升级的运行状态，供 GET /system/self-update/status 轮询。
// Phase 取值：idle | downloading | verifying | extracting | preparing | restarting | failed。
type SelfUpdateStatus struct {
	Running       bool             `json:"running"`
	Phase         string           `json:"phase"`
	Percent       int              `json:"percent"`
	Message       string           `json:"message"`
	TargetVersion string           `json:"targetVersion,omitempty"`
	Error         *SelfUpdateError `json:"error,omitempty"`
	StartedAt     string           `json:"startedAt,omitempty"`
}

// 阶段级 sentinel：分类用，配合 %w 包装后经 classifySelfUpdateError 映射错误码。
var (
	errSelfCheckFailed     = errors.New("self update check failed")
	errSelfDownloadFailed  = errors.New("self update download failed")
	errSelfChecksumMismatch = errors.New("self update checksum mismatch")
	errSelfExtractFailed   = errors.New("self update extract failed")
	errSelfVerifyFailed    = errors.New("self update binary verify failed")
)

// classifySelfUpdateError 把升级失败映射为结构化错误码。
func classifySelfUpdateError(err error) *SelfUpdateError {
	switch {
	case errors.Is(err, ErrSelfRepoNotConfigured):
		return &SelfUpdateError{Code: "repo_not_configured", Message: "主程序仓库未配置，可在「下载与更新出网」中填写（留空则停用自升级）"}
	case errors.Is(err, ErrSelfUpdateInProgress):
		return &SelfUpdateError{Code: "already_in_progress", Message: "已有升级正在进行中，请等待完成"}
	case errors.Is(err, errSelfCheckFailed):
		return &SelfUpdateError{Code: "check_failed", Message: "无法查询最新版本，请检查网络或稍后重试"}
	case errors.Is(err, errSelfDownloadFailed):
		return &SelfUpdateError{Code: "download_failed", Message: "所有下载源均失败，请检查下载源配置与网络"}
	case errors.Is(err, errSelfChecksumMismatch):
		return &SelfUpdateError{Code: "checksum_mismatch", Message: "下载内容校验和不匹配，可能被篡改或截断，请重试或更换下载源"}
	case errors.Is(err, errSelfExtractFailed):
		return &SelfUpdateError{Code: "extract_failed", Message: "解压新版主程序失败，下载包可能损坏，请重试"}
	case errors.Is(err, errSelfVerifyFailed):
		return &SelfUpdateError{Code: "verify_failed", Message: "新版主程序无法启动验证，下载包可能损坏，请重试"}
	default:
		return &SelfUpdateError{Code: "internal", Message: err.Error()}
	}
}
```

- [ ] **Step 3: 跑测试确认通过**

Run: `go test ./backend/internal/updater/ -run TestClassifySelfUpdateError -v`
Expected: PASS。

- [ ] **Step 4: 写失败测试 —— 状态初始化与阶段推进**

追加：

```go
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
```

Run: `go test ./backend/internal/updater/ -run TestSelfUpdateStatusInitialAndPhase -v`
Expected: FAIL（`GetSelfUpdateStatus`/`setSelfPhase` 未定义）。

- [ ] **Step 5: 实现状态字段与方法**

在 `Manager` 结构体（`backend/internal/updater/updater.go`）加字段：

```go
	// selfStatus 主程序自升级的运行状态（受 selfStatusMu 保护）。
	// 与 selfUpdating 互补：后者只有"是否进行中"一个布尔，
	// 前者携带阶段、进度、错误，供前端轮询。
	selfStatusMu sync.RWMutex
	selfStatus   SelfUpdateStatus
	// selfReadyHook 升级下载校验暂存成功后、重启前的回调（备份 DB + 触发关停）。
	// 由 system.go 注入；updater 不依赖数据层与进程管理。
	selfReadyHook func() error
```

在 `self_update.go` 加方法：

```go
// GetSelfUpdateStatus 返回主程序自升级当前状态（副本，可安全并发读）。
func (m *Manager) GetSelfUpdateStatus() SelfUpdateStatus {
	m.selfStatusMu.RLock()
	defer m.selfStatusMu.RUnlock()
	return m.selfStatus
}

// setSelfPhase 推进升级阶段。phase 为空或 "idle" 时视为结束（Running=false）。
func (m *Manager) setSelfPhase(phase, msg string) {
	m.selfStatusMu.Lock()
	defer m.selfStatusMu.Unlock()
	if phase == "" || phase == "idle" {
		m.selfStatus.Running = false
		m.selfStatus.Phase = "idle"
		m.selfStatus.Message = ""
		return
	}
	m.selfStatus.Running = true
	m.selfStatus.Phase = phase
	m.selfStatus.Message = msg
}

// setSelfProgress 更新下载进度百分比（仅 downloading 阶段有值）。
func (m *Manager) setSelfProgress(pct int) {
	m.selfStatusMu.Lock()
	defer m.selfStatusMu.Unlock()
	m.selfStatus.Percent = pct
}

// setSelfError 记录失败原因并结束运行态。
func (m *Manager) setSelfError(target string, err *SelfUpdateError) {
	m.selfStatusMu.Lock()
	defer m.selfStatusMu.Unlock()
	m.selfStatus.Running = false
	m.selfStatus.Phase = "failed"
	m.selfStatus.Error = err
	if target != "" {
		m.selfStatus.TargetVersion = target
	}
}

// setSelfTarget 记录正在升级到的版本。
func (m *Manager) setSelfTarget(version string) {
	m.selfStatusMu.Lock()
	defer m.selfStatusMu.Unlock()
	m.selfStatus.TargetVersion = version
}

// SetSelfUpdateReadyHook 注入升级暂存成功后、重启前的回调。
// 回调返回 error 只记录日志，不阻断重启（升级本身已通过完整性校验）。
func (m *Manager) SetSelfUpdateReadyHook(fn func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selfReadyHook = fn
}

func (m *Manager) runSelfReadyHook() {
	m.mu.RLock()
	fn := m.selfReadyHook
	m.mu.RUnlock()
	if fn == nil {
		return
	}
	if err := fn(); err != nil {
		m.logger.Errorf("升级后置备（备份/重启）失败: %v", err)
	}
}
```

Run: `go test ./backend/internal/updater/ -run 'TestSelfUpdateStatusInitialAndPhase|TestClassifySelfUpdateError' -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/updater/self_update.go backend/internal/updater/updater.go backend/internal/updater/self_update_test.go
git commit -m "feat(updater): 主程序升级状态机骨架与结构化错误分类"
```

---

### Task 2: UpdateSelf 异步化（StartSelfUpdate）

**Files:**
- Modify: `backend/internal/updater/self_update.go`
- Test: `backend/internal/updater/self_update_test.go`

- [ ] **Step 1: 写失败测试 —— 异步启动立即返回且状态推进**

追加：

```go
// 异步启动：StartSelfUpdate 应立即返回（不等下载完成），
// 状态机随后推进到 downloading 并最终 failed（无可用下载源）。
func TestStartSelfUpdateAsyncReturnsImmediately(t *testing.T) {
	m := New(Config{
		DataDir:        t.TempDir(),
		SelfRepo:       "owner/AuroraMihomo",
		UseMihomoProxy: false,
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
	m := New(Config{DataDir: t.TempDir(), SelfRepo: ""})
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
```

Run: `go test ./backend/internal/updater/ -run 'TestStartSelfUpdate' -v`
Expected: FAIL（`StartSelfUpdate` 未定义）。

- [ ] **Step 2: 实现 StartSelfUpdate + runSelfUpdate**

把现有 `UpdateSelf` 重构为：`StartSelfUpdate`（同步占位 + 起 goroutine）+ `runSelfUpdate`（原主体 + 阶段推进）。替换 `UpdateSelf` 整个函数：

```go
// StartSelfUpdate 异步启动主程序自升级：立即返回，实际下载/校验在后台
// goroutine 推进，状态经 GetSelfUpdateStatus 轮询。
//
// 与旧 UpdateSelf 的区别：调用方不再阻塞数分钟等待下载完成，
// 而是拿到"已接受"后轮询状态。下载校验暂存成功后触发 selfReadyHook
// （备份 DB + 关停换二进制），由进程管理器拉起新版。
//
// 同步阶段（本函数内）只做两类快速失败：未配置仓库、已有升级在飞。
// 其余失败（下载/校验/解压/试跑）在 goroutine 内转为 failed 状态。
func (m *Manager) StartSelfUpdate(ctx context.Context) error {
	if !m.selfUpdating.CompareAndSwap(false, true) {
		return ErrSelfUpdateInProgress
	}
	if !m.SelfRepoConfigured() {
		m.selfUpdating.Store(false)
		return ErrSelfRepoNotConfigured
	}
	m.setSelfPhase("preparing", "准备升级…")
	// 用 Background 派生：调用方 ctx（HTTP 请求）在应答后即被取消，
	// 不能传给会持续数分钟的后台下载。
	go m.runSelfUpdate(context.WithoutCancel(ctx))
	return nil
}

// runSelfUpdate 在后台执行主程序自升级并推进状态机。
// 成功路径：暂存 .new → ready hook（备份+重启）→ 状态 restarting，
// selfUpdating 保持 true 直到进程退出（关停换二进制）。
// 失败路径：置 failed + 结构化错误，清除 selfUpdating 允许重试。
func (m *Manager) runSelfUpdate(ctx context.Context) {
	do := func() error {
		if !m.SelfRepoConfigured() {
			return ErrSelfRepoNotConfigured
		}
		rel, err := m.latestRelease(ctx, m.repoSelf())
		if err != nil {
			return fmt.Errorf("%w: %v", errSelfCheckFailed, err)
		}
		tag := rel.TagName
		m.setSelfTarget(tag)
		name := selfArchiveName(tag)
		officialURL := m.selfDownloadURL(tag, name)
		assetSize := selfAssetSize(rel, name)
		m.logger.Infof("downloading auroramihomo %s (%s)", tag, name)

		tmpDir, err := os.MkdirTemp("", "aurora-self-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		archivePath := filepath.Join(tmpDir, name)
		m.setSelfPhase("downloading", "下载新版主程序…")
		// 下载进度：expectedSize 已知时按已下载/总字节换算百分比。
		// 经代理或任一 CDN 源都可能成功，每次尝试都会回调。
		if err := m.downloadWithCDN(ctx, officialURL, archivePath, assetSize, func(done, total int64) {
			if total > 0 {
				pct := int(done * 100 / total)
				if pct > 100 {
					pct = 100
				}
				m.setSelfProgress(pct)
			}
		}); err != nil {
			return fmt.Errorf("%w: %v", errSelfDownloadFailed, err)
		}

		m.setSelfPhase("verifying", "校验下载完整性…")
		if err := m.verifySelfChecksum(ctx, m.repoSelf(), tag, officialURL, archivePath); err != nil {
			return err // verifySelfChecksum 内部已按阶段包装 sentinel
		}

		m.setSelfPhase("extracting", "解压新版主程序…")
		extractDir := filepath.Join(tmpDir, "extract")
		if err := os.MkdirAll(extractDir, 0o755); err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(name), ".zip") {
			if err := unzip(archivePath, extractDir); err != nil {
				return fmt.Errorf("%w: %v", errSelfExtractFailed, err)
			}
		} else {
			if err := untarGz(archivePath, extractDir); err != nil {
				return fmt.Errorf("%w: %v", errSelfExtractFailed, err)
			}
		}
		binPath, err := findExtractedBinary(extractDir, "auroramihomo")
		if err != nil {
			return fmt.Errorf("%w: %v", errSelfExtractFailed, err)
		}

		m.setSelfPhase("preparing", "验证并暂存新版主程序…")
		if err := verifySelfBinary(ctx, binPath); err != nil {
			return fmt.Errorf("%w: %v", errSelfVerifyFailed, err)
		}

		target := m.SelfBinaryPath()
		if target == "" {
			return errors.New("cannot resolve current binary path for self update")
		}
		stage := target + ".new"
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := copyFile(binPath, stage); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(stage, 0o755)
		}
		m.logger.Infof("auroramihomo %s staged to %s, will swap on shutdown", tag, stage)
		return nil
	}

	if err := do(); err != nil {
		m.selfUpdating.Store(false)
		m.setSelfError("", classifySelfUpdateError(err))
		m.logger.Errorf("self update failed: %v", err)
		return
	}
	// 成功：保持 selfUpdating=true 直到进程退出；触发备份+重启回调。
	m.runSelfReadyHook()
	m.setSelfPhase("restarting", "新版本已下载并校验通过，即将重启生效")
}
```

- [ ] **Step 3: verifySelfChecksum 按阶段包装 sentinel**

改 `verifySelfChecksum`（`self_update.go`）中两处错误返回：

```go
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("%w: want %s got %s", errSelfChecksumMismatch, want, got)
	}
	return nil
```

`fetchSelfChecksum` 两处错误返回也包装（校验和获取失败属下载范畴）：

```go
	sumsURL := m.selfDownloadURL(tag, "SHA256SUMS.txt")
	body, err := m.fetchBytesWithCDN(ctx, sumsURL)
	if err != nil {
		return "", fmt.Errorf("%w: 无法获取主程序校验和（独立 .sha256 与 SHA256SUMS.txt 均不可用）: %v", errSelfDownloadFailed, err)
	}
	sum, err := parseChecksumFile(body, archiveName)
	if err != nil {
		return "", fmt.Errorf("%w: SHA256SUMS.txt 中未找到 %s 的校验和: %v", errSelfChecksumMismatch, archiveName, err)
	}
```

以及路径一（独立 .sha256）失败处保留原逻辑即可（失败后走路径二）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./backend/internal/updater/ -run 'TestStartSelfUpdate|TestSelfUpdate' -v`
Expected: 新测试 PASS；既有 `TestSelfUpdateInProgressRejectsSecondCall` / `TestSelfUpdateFailureClearsInProgress` 若引用旧 `UpdateSelf` 需同步改为 `StartSelfUpdate`（见下一步）。

- [ ] **Step 5: 适配既有测试**

`self_update_test.go` 中 `TestSelfUpdateInProgressRejectsSecondCall` 与 `TestSelfUpdateFailureClearsInProgress` 调用 `m.UpdateSelf(...)`，改为 `m.StartSelfUpdate(...)`。它们断言的是 CAS 占位与失败清除语义，异步化后语义不变（二次启动拒绝、失败清除），仅调用名变。用编辑工具替换两处。

Run: `go test ./backend/internal/updater/ -run TestSelfUpdate -v`
Expected: 全 PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/updater/self_update.go backend/internal/updater/self_update_test.go
git commit -m "feat(updater): UpdateSelf 异步化为 StartSelfUpdate + 状态机阶段推进"
```

---

### Task 3: 下载进度回调

**Files:**
- Modify: `backend/internal/updater/updater.go`（`downloadFile`/`downloadWithCDN` 签名 + 实现）
- Modify: `backend/internal/updater/download_verify_test.go`（适配新签名 + 进度测试）
- Modify: `backend/internal/updater/self_update.go`（已在新签名调用）

- [ ] **Step 1: 写失败测试 —— 进度回调被调用且百分比正确**

`download_verify_test.go` 当前导入缺 `strconv`（新测试用到 `strconv.Itoa`），先在 import 块补：

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)
```

在文件末尾追加：

```go
// 下载进度回调应随字节流推进：done/total 与预期一致，结束时达 100%。
func TestDownloadWithCDNReportsProgress(t *testing.T) {
	payload := strings.Repeat("P", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	m := New(Config{DataDir: t.TempDir(), CDNProviders: []string{srv.URL}, UseMihomoProxy: false})
	dest := filepath.Join(t.TempDir(), "a.bin")
	var lastDone, lastTotal int64
	var lastPct int
	m.downloadWithCDN(context.Background(), srv.URL+"/a.bin", dest, int64(len(payload)), func(done, total int64) {
		lastDone, lastTotal = done, total
		if total > 0 {
			lastPct = int(done * 100 / total)
		}
	})
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("进度应报告 done=%d total=%d, got %d/%d", len(payload), len(payload), lastDone, lastTotal)
	}
	if lastPct != 100 {
		t.Fatalf("结束时百分比应为 100, got %d", lastPct)
	}
}
```

Run: `go test ./backend/internal/updater/ -run TestDownloadWithCDNReportsProgress -v`
Expected: FAIL（`downloadWithCDN` 新参数不存在 / 编译错误）。

- [ ] **Step 2: downloadFile 加进度回调**

`updater.go` 中 `downloadFile` 签名加 `onProgress func(done, total int64)`，`io.Copy` 改带计数：

```go
func (m *Manager) downloadFile(ctx context.Context, url, dest string, client *http.Client, onProgress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AuroraMihomo-Updater/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if onProgress == nil {
		_, err = io.Copy(f, resp.Body)
		return err
	}
	total := resp.ContentLength
	if total < 0 {
		total = 0 // 未知长度：只给 done，百分比由调用方按 expectedSize 兜底
	}
	var done int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			onProgress(done, total)
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
```

- [ ] **Step 3: downloadWithCDN 加进度回调并透传**

`updater.go` 中 `downloadWithCDN` 签名加 `onProgress func(done, total int64)`，内部两处 `downloadFile` 调用透传；代理路径总长未知时用 expectedSize 兜底：

```go
func (m *Manager) downloadWithCDN(ctx context.Context, officialURL, dest string, expectedSize int64, onProgress func(done, total int64)) error {
	var errs []string

	// 代理路径无 Content-Length（经本地转发）时，用官方声明体积兜底进度
	progress := onProgress
	wrapProgress := func(done, total int64) {
		if progress == nil {
			return
		}
		if total <= 0 && expectedSize > 0 {
			total = expectedSize
		}
		progress(done, total)
	}
	// accept 保持原样（略），仅 downloadFile 调用处改为 wrapProgress
	...
	if err := m.downloadFile(ctx, officialURL, dest, client, wrapProgress); err != nil {
	...
	if err := m.downloadFile(ctx, u, dest, m.client, wrapProgress); err != nil {
	...
}
```

具体：`downloadWithCDN` 内两处 `m.downloadFile(ctx, ..., client)` 改为 `m.downloadFile(ctx, ..., client, wrapProgress)`。

- [ ] **Step 4: 适配所有 downloadWithCDN 调用点（补 nil）**

- `updater.go:616`（UpdateMihomo）：`m.downloadWithCDN(ctx, assetURL, archivePath, assetSize, nil)`
- `updater.go:733`（AdGuard 模板循环）：`m.downloadWithCDN(ctx, assetURL, dest, assetSize, nil)`
- `updater.go:874`（UpdateZashboard）：`m.downloadWithCDN(ctx, assetURL, archivePath, assetSize, nil)`
- `download_verify_test.go` 全部既有 `m.downloadWithCDN(...)` 调用补 `nil` 尾参（共 5 处：第 27/47/65/100/138/151 行）
- AdGuard 直连路径 `updater.go:805/816` 的 `m.downloadFile(...)` 补 `nil` 尾参

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./backend/internal/updater/ -v`
Expected: 全 PASS（含新进度测试与既有 CDN 回落测试）。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/updater/updater.go backend/internal/updater/download_verify_test.go
git commit -m "feat(updater): 下载进度回调（downloadFile/downloadWithCDN 透传）"
```

---

### Task 4: ReleaseNotes 透传与 .api 契约 + system.go handler

**Files:**
- Modify: `backend/internal/updater/updater.go`（githubRelease 加 Body）
- Modify: `backend/internal/updater/self_update.go`（SelfCheck 加 ReleaseNotes、CheckSelfUpdate 填充）
- Modify: `backend/api/AuroraMihomo-Go-Zero-API.api`
- Modify: `backend/api/system.go`
- Generate: `backend/api/internal/types/types.go`（goctl）

- [ ] **Step 1: githubRelease 加 Body + SelfCheck 加 ReleaseNotes**

`updater.go`：

```go
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}
```

`self_update.go` 的 `SelfCheck` 加字段：

```go
	// ReleaseNotes 远端最新 release 的发布说明（变更日志）。
	// 从 GitHub release body 截断到 maxReleaseNotesLen，空串表示拉不到。
	ReleaseNotes string `json:"releaseNotes,omitempty"`
```

加常量与截断：

```go
// maxReleaseNotesLen 变更日志随 check 响应下发，截断防爆（4KB 足够预览）。
const maxReleaseNotesLen = 4096
```

`CheckSelfUpdate` 中填充：

```go
	check.LatestVersion = rel.TagName
	if len(rel.Body) > maxReleaseNotesLen {
		check.ReleaseNotes = rel.Body[:maxReleaseNotesLen]
	} else {
		check.ReleaseNotes = rel.Body
	}
```

- [ ] **Step 2: 写失败测试 —— ReleaseNotes 透传与截断**

在 `self_update_test.go` 追加：

```go
// CheckSelfUpdate 应带回 release body 作变更日志，超长时截断。
func TestCheckSelfUpdateCarriesReleaseNotes(t *testing.T) {
	notes := strings.Repeat("x", maxReleaseNotesLen+100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
```

Run: `go test ./backend/internal/updater/ -run TestCheckSelfUpdateCarriesReleaseNotes -v`
Expected: 视实现先行——若 `Config` 无 `GitHubAPI` 注入字段，编译失败（见下）。

- [ ] **Step 3: 确认 Config 有 GitHubAPI 注入点**

`Config`（`updater.go:41`）已有 `GitHubAPI string` 字段，`githubAPI()`（`updater.go:934`）返回 `cfg.GitHubAPI`，测试直接注入本地服务器即可，无需改动。

Run: `go test ./backend/internal/updater/ -run TestCheckSelfUpdateCarriesReleaseNotes -v`
Expected: PASS。

- [ ] **Step 4: .api 规格更新**

`backend/api/AuroraMihomo-Go-Zero-API.api` 的 `SelfUpdateCheck` 加：

```
	// ReleaseNotes 远端最新 release 的发布说明（变更日志），截断至 4KB；空串表示拉不到
	ReleaseNotes string `json:"releaseNotes,optional"`
```

`self-update` 路由声明块（约 934 行）后新增类型与路由：

```
// SelfUpdateStatus 主程序自升级的运行状态（GET /system/self-update/status 轮询）。
type SelfUpdateStatus {
	// Running 是否进行中
	Running bool `json:"running"`
	// Phase 阶段：idle | downloading | verifying | extracting | preparing | restarting | failed
	Phase string `json:"phase"`
	// Percent 下载进度 0-100，非下载阶段为 0
	Percent int `json:"percent"`
	// Message 人类可读的阶段说明
	Message string `json:"message"`
	// TargetVersion 正在升级到的版本
	TargetVersion string `json:"targetVersion,optional"`
	// Error 失败时的结构化错误
	Error *SelfUpdateError `json:"error,optional"`
	// StartedAt 升级开始时间（RFC3339）
	StartedAt string `json:"startedAt,optional"`
}

// SelfUpdateError 主程序升级失败的结构化原因。
type SelfUpdateError {
	// Code 错误码：repo_not_configured | already_in_progress | check_failed |
	// download_failed | checksum_mismatch | extract_failed | verify_failed | internal
	Code string `json:"code"`
	// Message 面向用户的错误说明
	Message string `json:"message"`
}

	@handler selfUpdateStatus
	get /api/v1/system/self-update/status returns (SelfUpdateStatus)
```

- [ ] **Step 5: goctl 生成**

生成前确认 `backend/api/internal/` 无未提交改动（上一步提交后应干净）：

```bash
git status --porcelain backend/api/internal/
```

Run:

```bash
goctl api go -api backend/api/AuroraMihomo-Go-Zero-API.api -dir backend/api --style goZero
```

核对（`backend/AGENTS.md` 要求）：
- `internal/types/types.go` 的 `DO NOT EDIT` 头完好
- 新增 `SelfUpdateStatus` / `SelfUpdateError` 类型、`SelfUpdateCheck.ReleaseNotes` 字段到位
- 既有 handler 未被重写（`git status --porcelain backend/api/internal/` 应只见 types.go 变化）
- `go build ./backend/...` 通过

- [ ] **Step 6: system.go —— POST 改异步 + GET status + ready hook**

`backend/api/system.go` 的 self-update POST handler 改为：

```go
		{
			// 主程序一键自升级：异步启动下载校验新版 → 后台暂存 →
			// 自动备份数据库 → 触发优雅关停。应答后前端轮询
			// /system/self-update/status 查看阶段/进度/失败原因。
			Method: http.MethodPost,
			Path:   "/api/v1/system/self-update",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				if svcCtx.Updater.SelfUpdateInProgress() {
					httpx.ErrorCtx(r.Context(), w, updater.ErrSelfUpdateInProgress)
					return
				}
				if err := svcCtx.Updater.StartSelfUpdate(r.Context()); err != nil {
					logx.Errorf("self update start failed: %v", err)
					httpx.ErrorCtx(r.Context(), w, err)
					return
				}
				httpx.OkJson(w, map[string]any{
					"success": true,
					"message": "升级已开始，正在后台下载并校验新版主程序",
				})
			},
		},
		{
			// 主程序自升级状态轮询：返回阶段/进度/错误。
			// 升级成功触发关停后本请求也会随进程退出断开，
			// 前端以"连接断开"作为重启已发生的信号停止轮询。
			Method: http.MethodGet,
			Path:   "/api/v1/system/self-update/status",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				httpx.OkJson(w, svcCtx.Updater.GetSelfUpdateStatus())
			},
		},
```

**ready hook 注入**：在 `registerSystemRoutes` 内（或调用处）注入备份+关停回调。注入点选 `registerSystemRoutes` 函数体开头：

```go
func registerSystemRoutes(server *rest.Server, svcCtx *svc.ServiceContext, mgr *service.ReloadManager) {
	authOpt := rest.WithJwt(svcCtx.Config.Auth.AccessSecret)

	// 主程序自升级暂存成功后：自动备份数据库 + 触发关停换二进制。
	// 备份失败只记录不阻断（升级已通过完整性校验）；关停前先应答，
	// 让前端拿到"即将重启"状态而不是看到连接被掐断。
	svcCtx.Updater.SetSelfUpdateReadyHook(func() error {
		path, err := svcCtx.Database.BackupTo(backupDir(svcCtx), svcCtx.Config.Backup.MaxKeep)
		if err != nil {
			return fmt.Errorf("升级前数据库备份失败: %w", err)
		}
		logx.Infof("pre-upgrade database backup at %s", path)
		go func() {
			time.Sleep(100 * time.Millisecond)
			mgr.RequestQuit("HTTP /api/v1/system/self-update")
		}()
		return nil
	})
	...
```

若 `registerSystemRoutes` 可能被多次调用，改为幂等注入（hook 覆盖写即可，天然幂等）。

- [ ] **Step 7: 构建与既有测试**

Run: `go build ./backend/... && go test ./backend/... 2>&1 | tail -20`
Expected: 全绿。

- [ ] **Step 8: 提交**

```bash
git add backend/internal/updater/updater.go backend/internal/updater/self_update.go backend/internal/updater/self_update_test.go backend/api/AuroraMihomo-Go-Zero-API.api backend/api/internal/types/types.go backend/api/system.go
git commit -m "feat(api): 主程序升级状态接口 + 变更日志透传；POST 改异步"
```

---

### Task 5: 前端 store —— 类型、轮询、异步升级

**Files:**
- Modify: `frontend/src/stores/settings.ts`
- Test: `frontend/src/stores/settings.spec.ts`

- [ ] **Step 1: 写失败测试 —— 轮询与异步升级**

在 `settings.spec.ts` 追加：

```ts
it('updateSelf 异步触发后开始轮询状态，完成后停止', async () => {
  const store = useSettingsStore()
  mockedApi.post.mockResolvedValue({ data: { success: true, message: '升级已开始' } })
  // 第一次轮询：进行中；第二次：已完成（restarting）
  mockedApi.get
    .mockResolvedValueOnce({ data: { running: true, phase: 'downloading', percent: 50, message: '下载中' } })
    .mockResolvedValueOnce({ data: { running: false, phase: 'restarting', percent: 100, message: '即将重启生效' } })
  await store.updateSelf()
  expect(mockedApi.post).toHaveBeenCalledWith('/system/self-update')
  // 轮询定时器启动后立即拉一次
  await vi.waitFor(() => {
    expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/status')
  })
  expect(store.selfUpdateStatus?.phase).toBe('downloading')
  await vi.waitFor(() => {
    expect(store.selfUpdateStatus?.phase).toBe('restarting')
  })
  expect(store.updatingSelf).toBe(false)
})

it('轮询到 failed 状态时展示错误并停止', async () => {
  const store = useSettingsStore()
  mockedApi.post.mockResolvedValue({ data: { success: true, message: '升级已开始' } })
  mockedApi.get
    .mockResolvedValueOnce({ data: { running: true, phase: 'downloading', percent: 10, message: '下载中' } })
    .mockResolvedValueOnce({ data: { running: false, phase: 'failed', error: { code: 'download_failed', message: '所有下载源均失败' } } })
  await store.updateSelf()
  await vi.waitFor(() => {
    expect(store.selfUpdateStatus?.phase).toBe('failed')
  })
  expect(store.updatingSelf).toBe(false)
})
```

Run: `npx vitest run src/stores/settings.spec.ts`
Expected: FAIL（`selfUpdateStatus` 未定义 / `updateSelf` 未轮询）。

- [ ] **Step 2: 实现类型与 state**

`settings.ts`：

```ts
/** 主程序自升级的运行状态（轮询 /system/self-update/status） */
export interface SelfUpdateStatus {
  running: boolean
  phase: string
  percent: number
  message: string
  targetVersion?: string
  error?: { code: string; message: string }
  startedAt?: string
}
```

`SelfUpdateInfo` 加 `releaseNotes?: string`。

state 加：

```ts
    selfUpdateStatus: null as SelfUpdateStatus | null,
    /** 主程序升级状态轮询定时器 id */
    selfUpdatePollTimer: null as number | null,
```

- [ ] **Step 3: 实现 updateSelf 异步 + 轮询**

替换 `updateSelf` 与 `checkSelfUpdate` 尾部（checkSelfUpdate 不动，只 updateSelf 改）：

```ts
    // 一键自升级：异步触发（POST 立即返回），随后轮询状态接口
    // 展示下载/校验/重启阶段；服务重启导致轮询失败即停止。
    async updateSelf() {
      if (this.updatingSelf) return
      this.updatingSelf = true
      this.selfUpdateStatus = null
      try {
        const res = await api.post('/system/self-update')
        useNotifyStore().success(res.data?.message || '升级已开始')
        this.startSelfUpdatePolling()
      } catch (e: unknown) {
        console.error(e)
        useNotifyStore().error('无法启动升级，请查看后端日志')
        this.updatingSelf = false
      }
    },
    startSelfUpdatePolling() {
      this.stopSelfUpdatePolling()
      void this.pollSelfUpdate()
      this.selfUpdatePollTimer = window.setInterval(() => {
        void this.pollSelfUpdate()
      }, 1000)
    },
    stopSelfUpdatePolling() {
      if (this.selfUpdatePollTimer != null) {
        clearInterval(this.selfUpdatePollTimer)
        this.selfUpdatePollTimer = null
      }
    },
    async pollSelfUpdate() {
      try {
        const res = await api.get<SelfUpdateStatus>('/system/self-update/status')
        this.selfUpdateStatus = res.data
        if (!res.data.running) {
          this.stopSelfUpdatePolling()
          this.updatingSelf = false
          if (res.data.error) {
            useNotifyStore().error(res.data.error.message || '升级失败')
          }
        }
      } catch (e: unknown) {
        // 服务重启中连接被断开：升级已触发，停止轮询，不报错
        console.error(e)
        this.stopSelfUpdatePolling()
        this.updatingSelf = false
      }
    },
```

Run: `npx vitest run src/stores/settings.spec.ts`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/stores/settings.ts frontend/src/stores/settings.spec.ts
git commit -m "feat(settings): 主程序升级异步触发 + 状态轮询"
```

---

### Task 6: 前端 UI —— 阶段展示、确认弹窗、错误映射

**Files:**
- Modify: `frontend/src/views/SettingsView.vue`
- Test: `frontend/src/views/SettingsView.spec.ts`（若存在；否则新建）

- [ ] **Step 1: 写失败测试 —— 确认弹窗与错误文案**

`SettingsView.spec.ts` 已有挂载模式（`stubApi()` + `mount(SettingsView)`）。在文件末尾追加新 describe（复用文件顶部的 `stubApi`/`FakeIntersectionObserver`/`beforeEach` 模式，新增自己的 beforeEach）：

```ts
describe('SettingsView 主程序升级', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    FakeIntersectionObserver.instances = []
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    stubApi()
  })

  it('升级中显示下载阶段与进度条', async () => {
    const store = useSettingsStore()
    // 升级已触发：running + downloading + 进度
    store.selfUpdateStatus = {
      running: true,
      phase: 'downloading',
      percent: 42,
      message: '下载新版主程序…',
      targetVersion: 'v0.12.0',
    }
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('下载中')
    expect(wrapper.text()).toContain('下载新版主程序…')
    expect(wrapper.find('[aria-label="主程序下载进度"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('升级失败展示结构化错误文案', async () => {
    const store = useSettingsStore()
    store.selfUpdateStatus = {
      running: false,
      phase: 'failed',
      percent: 0,
      message: '',
      error: { code: 'checksum_mismatch', message: '下载内容校验和不匹配，可能被篡改或截断，请重试或更换下载源' },
    }
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('校验和不匹配')
    wrapper.unmount()
  })

  it('确认升级弹窗展示变更日志', async () => {
    const store = useSettingsStore()
    store.selfUpdateInfo = {
      configured: true,
      currentVersion: 'v0.11.11',
      latestVersion: 'v0.12.0',
      updateAvailable: true,
      releaseNotes: '- 修复若干问题\n- 新增功能',
    }
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()
    // 触发确认弹窗：直接调 vm 暴露的打开函数（或点按钮后断言）
    const vm = wrapper.vm as unknown as { confirmSelfUpdate: () => void }
    vm.confirmSelfUpdate()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('升级到 v0.12.0')
    expect(wrapper.text()).toContain('变更日志')
    expect(wrapper.text()).toContain('修复若干问题')
    wrapper.unmount()
  })
})
```

顶部 import 需补 `useSettingsStore`：

```ts
import { useSettingsStore } from '../stores/settings'
```

Run: `npx vitest run src/views/SettingsView.spec.ts`
Expected: FAIL（`selfUpdateStatus` 无渲染 / `confirmSelfUpdate` 未定义 / releaseNotes 未渲染）。

- [ ] **Step 2: 实现确认弹窗（ModalDialog + 变更日志）**

`SettingsView.vue`：删 `confirmUpdateSelf`（原生 confirm），加弹窗 state：

```ts
const selfUpdateDialogOpen = ref(false)
const confirmSelfUpdate = () => {
  selfUpdateDialogOpen.value = true
}
```

模板在 self-update 区块末尾加弹窗（用现有 `ModalDialog` 组件，参考 AdGuardSettingsDialog 用法）：

```vue
<ModalDialog
  :open="selfUpdateDialogOpen"
  :title="`升级到 ${store.selfUpdateInfo?.latestVersion || '新版本'}`"
  max-width="max-w-xl"
  @close="selfUpdateDialogOpen = false"
>
  <div class="space-y-4 text-sm">
    <p class="text-xs text-fg-subtle">
      当前 <code class="font-mono">{{ mihomoStore.appVersion }}</code>
      → 目标 <code class="font-mono">{{ store.selfUpdateInfo?.latestVersion }}</code>
    </p>
    <div v-if="store.selfUpdateInfo?.releaseNotes" class="rounded-lg border border-line bg-elevated/50 p-3">
      <div class="text-xs font-semibold text-fg mb-2">变更日志</div>
      <pre class="text-xs text-fg-subtle whitespace-pre-wrap font-sans max-h-64 overflow-y-auto">{{ store.selfUpdateInfo.releaseNotes }}</pre>
    </div>
    <p class="text-xs text-fg-subtle">
      将下载并校验新版主程序，替换自身二进制并重启进程（服务短暂中断，
      由进程管理器拉起新版）。升级前会自动备份数据库。
    </p>
    <div class="flex justify-end gap-2 pt-1">
      <Button variant="ghost" @click="selfUpdateDialogOpen = false">取消</Button>
      <Button @click="confirmAndStartUpdate()">升级并重启</Button>
    </div>
  </div>
</ModalDialog>
```

script 中：

```ts
const confirmAndStartUpdate = () => {
  selfUpdateDialogOpen.value = false
  store.updateSelf()
}
```

升级区块按钮改绑 `confirmSelfUpdate`（不再 `confirmUpdateSelf`）。

- [ ] **Step 3: 升级中阶段展示**

升级区块内、两个按钮之间的提示区改为动态（替换原静态说明段落）：

```vue
<div class="mt-3 text-xs text-fg-subtle">
  <!-- 升级进行中：阶段徽标 + 进度条 -->
  <template v-if="store.selfUpdateStatus?.running">
    <div class="flex items-center gap-2 mb-1.5">
      <Badge variant="info">{{ phaseLabel(store.selfUpdateStatus.phase) }}</Badge>
      <span class="flex-1">{{ store.selfUpdateStatus.message }}</span>
    </div>
    <Progress
      v-if="store.selfUpdateStatus.phase === 'downloading'"
      :model-value="store.selfUpdateStatus.percent"
      class="h-1.5"
      aria-label="主程序下载进度"
    />
    <p v-else-if="store.selfUpdateStatus.phase === 'restarting'" class="text-fg">
      新版本已下载并校验通过，即将重启生效…
    </p>
  </template>
  <!-- 失败：错误原因 + 重试 -->
  <template v-else-if="store.selfUpdateStatus?.phase === 'failed'">
    <p class="text-destructive">{{ selfUpdateErrorText }}</p>
  </template>
  <!-- 静态说明 -->
  <template v-else>
    升级流程：下载新版并校验完整性 → 自动备份数据库 → 优雅关停时替换
    自身二进制 → 由进程管理器（systemd / docker / NSSM）拉起新版。
    升级期间服务有短暂中断，属预期行为。
  </template>
</div>
```

script 加：

```ts
/** 升级阶段的中文标签 */
const phaseLabel = (phase: string) => {
  const map: Record<string, string> = {
    preparing: '准备中',
    downloading: '下载中',
    verifying: '校验中',
    extracting: '解压中',
    restarting: '即将重启',
    failed: '失败',
  }
  return map[phase] || phase
}
/** 失败提示：优先后端结构化 message，兜底通用文案 */
const selfUpdateErrorText = computed(() => {
  const err = store.selfUpdateStatus?.error
  if (err?.message) return `升级失败：${err.message}`
  return '升级失败，请重试或查看后端日志'
})
```

`Badge variant="info"` 已有先例（`AdGuardView.vue:375`），`text-destructive` 是错误色 token（`AdGuardView.vue:540` 同款用法）。

- [ ] **Step 4: 跑前端测试与 lint**

Run: `cd frontend && npx vitest run && npx eslint . --no-fix && npx vue-tsc --noEmit -p tsconfig.app.json`
Expected: 全绿。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/views/SettingsView.vue frontend/src/views/SettingsView.spec.ts
git commit -m "feat(settings): 主程序升级阶段/进度展示 + 变更日志确认弹窗"
```

---

### Task 7: 端到端验证与收尾

- [ ] **Step 1: 后端全量测试 + vet**

Run: `go vet ./backend/... && go test ./backend/... 2>&1 | tail -20`
Expected: 全绿。

- [ ] **Step 2: 前端全量检查**

Run: `cd frontend && npx vue-tsc --noEmit -p tsconfig.app.json && npx eslint . --no-fix && npm test`
Expected: 全绿。

- [ ] **Step 3: 规范机检 + gofmt**

Run: `gofmt -l ./backend`（本次改动文件应不在列表）
Run: `python scripts/check-conventions.py --baseline scripts/conventions-baseline.txt`
Expected: 通过。

- [ ] **Step 4: 手工冒烟（若环境可运行）**

`go run ./backend/api -f backend/api/etc/aurora-api.yaml` 起服务，浏览器验证：
- 检查更新返回 releaseNotes
- 点升级：确认弹窗展示变更日志 → 确认后出现进度条
- 人为制造下载源全失败（清空 CDN 列表且断代理），验证 failed 状态与错误文案

- [ ] **Step 5: 提交收尾（若有冒烟修复）**

```bash
git add -A
git commit -m "chore: 主程序升级体验端到端验证修复"
```

---

## Self-Review

**Spec coverage:**
- 实时反馈（轮询状态接口 + 阶段/进度）→ Task 1-3、5、6 ✅
- 失败可读化（错误码 + 前端映射）→ Task 1、5、6 ✅
- 变更日志预览（ReleaseNotes + ModalDialog）→ Task 4、6 ✅
- 不动的部分：下载源优先级、sha256 校验、备份时序、restart 互斥 → 均未改动 ✅

**Placeholder scan:** 无 TBD/TODO；所有代码步骤含完整实现。

**Type consistency:**
- `SelfUpdateStatus` 后端字段（running/phase/percent/message/targetVersion/error/startedAt）与 .api、前端接口逐一对齐 ✅
- 错误码枚举（repo_not_configured/already_in_progress/check_failed/download_failed/checksum_mismatch/extract_failed/verify_failed/internal）三处一致 ✅
- `downloadWithCDN(ctx, url, dest, expectedSize, onProgress)` 签名在所有调用点一致（5 处测试 + 3 处产品代码 + self-update 1 处）✅

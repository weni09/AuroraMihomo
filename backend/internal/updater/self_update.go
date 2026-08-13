package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/internal/version"
)

// ErrSelfRepoNotConfigured 表示主程序自升级未配置仓库。
// 默认值（weni09/AuroraMihomo）在 updater.New 兜底，运行期可在设置页
// 改成自建仓库；显式清空保存（DB 存空串）才会落到"未配置"并禁用自升级。
var ErrSelfRepoNotConfigured = errors.New("self update repo not configured")

// DefaultSelfRepo 是主程序自升级的默认发布仓库。
// 与本仓库 release.yml 的 GitHub 发布目标一致；fork 用户应在设置页改为
// 自己的 "owner/AuroraMihomo" 仓库（否则会从本仓库拉取二进制）。
const DefaultSelfRepo = "weni09/AuroraMihomo"

// ErrSelfUpdateInProgress 表示主程序自升级已在进行中（下载已成功、
// 等待关停换二进制，或第二次请求撞上第一次）。
// 调用方应拒绝重复触发，并禁用与关停竞态的 /system/restart。
var ErrSelfUpdateInProgress = errors.New("self update already in progress")

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
	errSelfCheckFailed      = errors.New("self update check failed")
	errSelfDownloadFailed   = errors.New("self update download failed")
	errSelfChecksumMismatch = errors.New("self update checksum mismatch")
	errSelfExtractFailed    = errors.New("self update extract failed")
	errSelfVerifyFailed     = errors.New("self update binary verify failed")
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

// SelfCheck 描述主程序自身的版本检查结果。
type SelfCheck struct {
	// Configured 是否已配置主程序仓库（未配置时仅此字段有意义）
	Configured bool `json:"configured"`
	// CurrentVersion 当前运行版本（version.Get()，未注入时为 "dev"）
	CurrentVersion string `json:"currentVersion"`
	// LatestVersion 远端最新 release tag，查询失败时为空串
	LatestVersion string `json:"latestVersion"`
	// UpdateAvailable 是否存在可升级的新版本
	UpdateAvailable bool `json:"updateAvailable"`
	// ReleaseNotes 远端最新 release 的发布说明（变更日志）。
	// 从 GitHub release body 截断到 maxReleaseNotesLen，空串表示拉不到。
	ReleaseNotes string `json:"releaseNotes,omitempty"`
	// Error 版本查询失败的原因（远端不可达等），为空串表示正常
	Error string `json:"error,omitempty"`
}

// maxReleaseNotesLen 变更日志随 check 响应下发，截断防爆（4KB 足够预览）。
const maxReleaseNotesLen = 4096

// SelfBinaryPath 返回主程序自身二进制路径。
func (m *Manager) SelfBinaryPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.SelfBinaryPath
}

// SelfRepoConfigured 是否已配置主程序仓库。
func (m *Manager) SelfRepoConfigured() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return strings.TrimSpace(m.cfg.SelfRepo) != ""
}

// CheckSelfUpdate 对比当前运行版本与 GitHub 最新 release。
//
// 未配置仓库时返回 ErrSelfRepoNotConfigured（由调用方决定如何呈现）；
// 配置了但查询失败时返回带 Error 字段的 SelfCheck，不把查询失败当作
// "无更新" 吞掉——面板需要向用户如实说明"暂时无法确认版本"。
func (m *Manager) CheckSelfUpdate(ctx context.Context) (SelfCheck, error) {
	check := SelfCheck{
		Configured:     m.SelfRepoConfigured(),
		CurrentVersion: version.Get(),
	}
	if !check.Configured {
		return check, ErrSelfRepoNotConfigured
	}
	rel, err := m.latestRelease(ctx, m.repoSelf())
	if err != nil {
		check.Error = err.Error()
		return check, nil
	}
	check.LatestVersion = rel.TagName
	if len(rel.Body) > maxReleaseNotesLen {
		check.ReleaseNotes = rel.Body[:maxReleaseNotesLen]
	} else {
		check.ReleaseNotes = rel.Body
	}
	// 主程序版本是精确的 tag（ldflags 注入），用等值比较而不是
	// versionMatches 的 Contains：后者会把 "v1.2.3" 误判成已覆盖
	// "v1.2.30"。开发构建（dev / dev-时间戳）不含 release tag，
	// 会正确地提示存在可升级版本。
	check.UpdateAvailable = !selfVersionEquals(check.CurrentVersion, rel.TagName)
	return check, nil
}

// selfVersionEquals 比较主程序当前版本与远端 tag 是否同一发布。
// 两侧都可能带或不带 "v" 前缀（用户手工改 AppVersion、或 tag 风格不一致），
// 统一去掉前缀再比；空 tag 视为"无法判断，当已最新"避免误升级。
func selfVersionEquals(local, latestTag string) bool {
	norm := func(s string) string {
		s = strings.TrimSpace(strings.ToLower(s))
		return strings.TrimPrefix(s, "v")
	}
	a, b := norm(local), norm(latestTag)
	if b == "" {
		return true
	}
	return a == b
}

// repoSelf 返回主程序所在仓库。
func (m *Manager) repoSelf() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return strings.TrimSpace(m.cfg.SelfRepo)
}

// selfAssetExt 返回主程序官方资产的扩展名。
// release.yml 打包：Windows 用 zip，其余平台 tar.gz（与 install.sh 契约一致）。
func selfAssetExt() string {
	if runtime.GOOS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

// selfArchiveName 构造主程序 release 资产的文件名。
// 命名与 release.yml 的打包步骤、install.sh 的下载 URL 完全一致：
//
//	auroramihomo_<tag>_<goos>_<goarch>.tar.gz|.zip
func selfArchiveName(tag string) string {
	return fmt.Sprintf("auroramihomo_%s_%s_%s%s", tag, runtime.GOOS, runtime.GOARCH, selfAssetExt())
}

// selfAssetSize 在 release 资产列表里按文件名找官方声明体积；
// 找不到返回 0（调用方退化为仅靠 sha256 校验）。
func selfAssetSize(rel *githubRelease, name string) int64 {
	if rel == nil {
		return 0
	}
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, name) {
			return a.Size
		}
	}
	return 0
}

// selfDownloadURL 构造主程序 release 资产的下载地址。
//
// 与组件资产不同，主程序资产按 install.sh 的同名契约拼接
// "<base>/<repo>/releases/download/<tag>/<name>"，不依赖 release JSON
// 的 browser_download_url——release 资产列表里可能混入其它同名变体，
// 按固定命名取最稳。base 默认官方 GitHub，测试注入本地服务器。
func (m *Manager) selfDownloadURL(tag, name string) string {
	m.mu.RLock()
	base := m.cfg.SelfDownloadBase
	repo := m.cfg.SelfRepo
	m.mu.RUnlock()
	if strings.TrimSpace(base) == "" {
		base = "https://github.com"
	}
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", strings.TrimRight(base, "/"), repo, tag, name)
}

// SelfUpdateInProgress 报告主程序自升级是否已进入"待关停换二进制"阶段。
// 供 /system/restart 与二次 /system/self-update 互斥查询。
func (m *Manager) SelfUpdateInProgress() bool {
	return m.selfUpdating.Load()
}

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
		// 若 release JSON 里刚好有同名资产，带上官方声明体积：
		// downloadWithCDN 会在 CDN 回落路径上拒绝体积不符的产物，
		// 作为 sha256 之外的第一道防线。找不到就传 0（只靠 sha256）。
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

// verifySelfChecksum 校验下载的主程序包与官方发布的 sha256 一致。
//
// 按两条路径取校验和：
//  1. 独立资产：<下载地址>.sha256（发布流程修复后每个包旁都有单个 .sha256）；
//  2. 汇总文件：<repo>/releases/download/<tag>/SHA256SUMS.txt（历史版本
//     release 只带这个汇总文件，内容为 "<hex>  <包名>" 逐行），按包名匹配。
//
// 两条路径都拿不到校验和时硬失败——主程序更新没有组件更新那样的
// 回滚兜底，不能在没有完整性校验的情况下换掉自己。
func (m *Manager) verifySelfChecksum(ctx context.Context, repo, tag, archiveURL, localPath string) error {
	archiveName := filepath.Base(archiveURL)

	want, err := m.fetchSelfChecksum(ctx, repo, tag, archiveURL, archiveName)
	if err != nil {
		return err
	}

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
}

// fetchSelfChecksum 依次尝试独立 .sha256 资产与 SHA256SUMS.txt，返回期望的
// 十六进制校验和。两条路径都按 downloadWithCDN 的出网顺序：代理 → 全局下载源。
func (m *Manager) fetchSelfChecksum(ctx context.Context, repo, tag, archiveURL, archiveName string) (string, error) {
	// 路径一：独立 .sha256 资产。
	if body, err := m.fetchBytesWithCDN(ctx, archiveURL+".sha256"); err == nil {
		if sum, err := parseChecksumFile(body, archiveName); err == nil {
			return sum, nil
		}
	}

	// 路径二：汇总文件 SHA256SUMS.txt。
	sumsURL := m.selfDownloadURL(tag, "SHA256SUMS.txt")
	body, err := m.fetchBytesWithCDN(ctx, sumsURL)
	if err != nil {
		return "", fmt.Errorf("%w: 无法获取主程序校验和（独立 .sha256 与 SHA256SUMS.txt 均不可用）: %v", errSelfDownloadFailed, err)
	}
	sum, err := parseChecksumFile(body, archiveName)
	if err != nil {
		return "", fmt.Errorf("%w: SHA256SUMS.txt 中未找到 %s 的校验和: %v", errSelfChecksumMismatch, archiveName, err)
	}
	return sum, nil
}

// fetchBytesWithCDN 按与 downloadWithCDN 相同的出网顺序取小文件：
// 先经 mihomo 代理拉官方地址，再按全局下载源（含官方兜底）直连。
// 不走 AdGuard 专用模板——那些是完整 AdGuard 下载 URL，不能当 GitHub 前缀。
func (m *Manager) fetchBytesWithCDN(ctx context.Context, officialURL string) ([]byte, error) {
	var errs []string
	if client, proxy := m.httpClient(); proxy != "" {
		body, err := m.fetchBytesOr(ctx, client, officialURL)
		if err == nil {
			return body, nil
		}
		errs = append(errs, fmt.Sprintf("mihomo 代理(%s) => %v", proxy, err))
		m.logger.Errorf("经 mihomo 代理拉取失败，改用 CDN 镜像: %v", err)
	}
	for _, p := range m.prioritizedCDNProviders() {
		u := cdnURLForProvider(officialURL, p)
		if u == "" {
			continue
		}
		body, err := m.fetchBytesOr(ctx, m.client, u)
		if err == nil {
			m.rememberLastCDN(p)
			return body, nil
		}
		errs = append(errs, fmt.Sprintf("%s => %v", u, err))
	}
	return nil, fmt.Errorf("all download sources failed: %s", strings.Join(errs, " | "))
}

// fetchBytesOr 下载并读取 url，失败返回错误。
func (m *Manager) fetchBytesOr(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512*1024))
}

// parseChecksumFile 从 "sha256sum -c" 兼容格式的内容中找出 name 对应的
// 十六进制校验和。内容形如 "<hex>  <文件名>"，逐行匹配。
func parseChecksumFile(body []byte, name string) (string, error) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// sha256sum -c 对带 * 前缀的是二进制模式，可能写成 "<hex> *name"
		filePart := strings.TrimPrefix(fields[1], "*")
		if filePart != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if _, err := hex.DecodeString(sum); err != nil {
			return "", fmt.Errorf("invalid checksum hex: %q", fields[0])
		}
		return sum, nil
	}
	return "", fmt.Errorf("entry not found")
}

// verifySelfBinary 运行新二进制的 -version 确认其可执行且能正常启动。
// 与 verifyAdGuardBinary 同理：进程能启动并给出输出即视为通过。
func verifySelfBinary(ctx context.Context, path string) error {
	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(execCtx, path, "-version")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.TrimSpace(string(out)) != "" {
		return nil
	}
	return fmt.Errorf("binary did not respond to -version: %w", err)
}

// stagedSelfStatus 判断 <target>.new 是否仍是「应继续完成」的升级。
//
// 崩溃恢复的设计本意：暂存后异常退出，下次启动应继续交换完成升级。但
// .new 可能已失去意义——损坏（无法执行 -version）、与当前版本相同、或比
// 当前版本旧（用户可能已在两次启动之间手工升级了主程序）——继续交换会
// 降级或换入坏版本，必须丢弃。返回 (是否保留, 原因)。
func (m *Manager) stagedSelfStatus(stage string) (keep bool, reason string) {
	out, err := m.selfBinaryVersion(stage)
	if err != nil {
		return false, "新二进制无法执行 -version（可能损坏），丢弃避免换入坏版本"
	}
	cur := version.Get()
	o, c := selfVersionNumeric(out), selfVersionNumeric(cur)
	if o == 0 && c == 0 {
		// 两侧都无版本号（dev 构建），无法判断新旧，保持旧的崩溃恢复语义
		return true, "暂存版本与当前均无版本号，无法比对，保持待交换"
	}
	if o <= c {
		return false, fmt.Sprintf("暂存版本 %s 不高于当前 %s，丢弃避免降级", out, cur)
	}
	return true, fmt.Sprintf("暂存版本 %s 高于当前 %s，继续完成升级", out, cur)
}

// selfBinaryVersion 运行二进制 -version 取版本输出。
// 语义与 verifySelfBinary 一致：进程能启动并给出输出即视为可用，输出为空
// 才判失败（兼容测试用 shell 脚本与 Windows 测试进程）。调用方只把它当
// 「能否运行」的判据，版本比对失败（解析不出数字）由 selfVersionNumeric
// 返回 0 兜底，不会造成误判降级。
func (m *Manager) selfBinaryVersion(path string) (string, error) {
	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(execCtx, path, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("binary did not respond to -version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// selfVersionNumeric 把版本串解析为可比较数值。
// 兼容 "v1.2.3"、"auroramihomo-v2.0.0"（-version 输出的常见形态）等：
// 从首个数字开始解析 主.次.补丁 三段；无数字（"dev"）返回 0。
func selfVersionNumeric(s string) int64 {
	idx := strings.IndexFunc(strings.TrimSpace(s), func(r rune) bool { return r >= '0' && r <= '9' })
	if idx < 0 {
		return 0
	}
	parts := strings.SplitN(s[idx:], ".", 3)
	var major, minor, patch int64
	if len(parts) > 0 {
		major, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) > 1 {
		minor, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) > 2 {
		patch, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	return major*1_000_000 + minor*1_000 + patch
}

// SwapSelfBinary 在关停时把 .new 换成自身二进制。无待生效的 .new 时为空操作。
//
// Unix 上运行中的可执行文件可以直接被 rename 覆盖（旧 inode 继续存活到
// 进程退出），一次 rename 即可原子切换；Windows 上运行中的 exe 无法覆盖，
// 但可以重命名自己——先把自己改成 .old，再把 .new 改名为自身。两步之间
// 若进程崩溃，留下的也是 .old/.new 二选一的中间态，由下次启动的
// CleanupStaleSelf 或重试收敛，不会出现损坏的自身二进制。
//
// 交换成功后强制 0755：从归档解出再 copyFile 的过程在部分文件系统上
// 可能丢掉可执行位，supervisor 随后 ExecStart 会直接 "Permission denied"，
// 表现为"服务没拉起、全面断网"。
func (m *Manager) SwapSelfBinary() error {
	target := m.SelfBinaryPath()
	if target == "" {
		return errors.New("cannot resolve current binary path for self update")
	}
	stage := target + ".new"
	if !fileExists(stage) {
		return nil // 没有待生效的更新，无需交换
	}

	// 交换前核验待生效版本：崩溃恢复语义是「继续完成上次没完成的升级」，
	// 但用户可能已在两次启动之间手工升级了主程序（或下载的 .new 损坏），
	// 此时继续交换会造成降级或换入坏版本，必须丢弃并跳过本次交换。
	if keep, reason := m.stagedSelfStatus(stage); !keep {
		m.logger.Infof("跳过自升级交换：%s", reason)
		if err := os.Remove(stage); err != nil {
			m.logger.Errorf("清理待生效 .new 失败: %v", err)
		}
		return nil
	}

	if runtime.GOOS == "windows" {
		if err := os.Rename(target, target+".old"); err != nil {
			// 重命名自身失败（权限/占用）时保留 .new 不动，
			// 调用方会记录错误，升级不生效但现有版本完好。
			return fmt.Errorf("rename current binary failed: %w", err)
		}
	}
	if err := os.Rename(stage, target); err != nil {
		// 目标改名失败：Unix 上可能是权限问题；Windows 上此时自身
		// 已被改成 .old，需尽力把自身改回来，避免运行目录里没有主程序。
		if runtime.GOOS == "windows" {
			_ = os.Rename(target+".old", target)
		}
		return fmt.Errorf("swap self binary failed: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o755); err != nil {
			m.logger.Errorf("设置新二进制可执行位失败: %v", err)
		}
	}
	m.logger.Infof("self binary swapped to %s", target)
	return nil
}

// CleanupStaleSelf 在启动早期清理自升级的残留文件。
//
//   - .old：上次 SwapSelfBinary 在 Windows 上成功后留下的旧版自身，
//     此时它已不再被运行中的进程引用，可以安全删除；
//   - .new：上次下载后未及交换（异常退出）的待生效版本。先核验它仍是
//     「高于当前版本的升级」才保留（崩溃恢复继续完成）；损坏或版本不高于
//     当前的丢弃——否则用户手工升级后，下一次关停会被这份陈旧 .new 降级。
//     若用户改主意放弃升级，手工删掉该文件即可。
func (m *Manager) CleanupStaleSelf() {
	target := m.SelfBinaryPath()
	if target == "" {
		return
	}
	if fileExists(target + ".old") {
		if err := os.Remove(target + ".old"); err != nil {
			m.logger.Errorf("清理 .old 残留失败: %v", err)
		} else {
			m.logger.Infof("已清理自升级残留 .old")
		}
	}
	if fileExists(target + ".new") {
		keep, reason := m.stagedSelfStatus(target + ".new")
		if keep {
			m.logger.Infof("检测到待生效的自升级 .new，将在下次关停时交换生效（%s）", reason)
		} else {
			if err := os.Remove(target + ".new"); err != nil {
				m.logger.Errorf("清理无效 .new 残留失败: %v", err)
			} else {
				m.logger.Infof("已清理无效 .new 残留：%s", reason)
			}
		}
	}
}

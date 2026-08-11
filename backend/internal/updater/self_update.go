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
	// Error 版本查询失败的原因（远端不可达等），为空串表示正常
	Error string `json:"error,omitempty"`
}

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

// UpdateSelf 下载并校验最新版主程序，暂存到 <自身路径>.new。
//
// 它不替换正在运行的二进制——运行中文件在多数平台无法直接覆盖，
// 替换动作留到关停时由 SwapSelfBinary 完成（关停流程能保证进程即将
// 退出，替换不再影响已加载的代码）。下载失败或校验不通过时绝不写入
// .new，磁盘上不会出现"半截新版本"。
//
// 成功暂存后会置 selfUpdating，拒绝后续二次升级与并发 restart；
// 失败路径不置位，调用方可安全重试。
func (m *Manager) UpdateSelf(ctx context.Context) error {
	// 在抢 updateMu 之前先 CAS 占位：第二次请求立刻拿到 InProgress，
	// 而不是排在第一次长达数分钟的下载后面空等。失败路径必须 Clear，
	// 成功路径保持 true 直到进程退出（关停换二进制）。
	if !m.selfUpdating.CompareAndSwap(false, true) {
		return ErrSelfUpdateInProgress
	}
	committed := false
	defer func() {
		if !committed {
			m.selfUpdating.Store(false)
		}
	}()

	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	if !m.SelfRepoConfigured() {
		return ErrSelfRepoNotConfigured
	}

	rel, err := m.latestRelease(ctx, m.repoSelf())
	if err != nil {
		return err
	}
	tag := rel.TagName
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
	// 完整性主路径是 GitHub 发布时附带的 .sha256 / SHA256SUMS.txt：
	// 下载后必须比对，不匹配即丢弃（防 CDN 篡改/截断）。
	if err := m.downloadWithCDN(ctx, officialURL, archivePath, assetSize); err != nil {
		return err
	}
	if err := m.verifySelfChecksum(ctx, m.repoSelf(), tag, officialURL, archivePath); err != nil {
		return err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		if err := unzip(archivePath, extractDir); err != nil {
			return err
		}
	} else {
		if err := untarGz(archivePath, extractDir); err != nil {
			return err
		}
	}
	binPath, err := findExtractedBinary(extractDir, "auroramihomo")
	if err != nil {
		return err
	}

	// 临时目录里先验证新二进制能跑通 -version，再动磁盘上的文件：
	// 校验失败立即返回，不会留下一个待生效的坏版本。
	if err := verifySelfBinary(ctx, binPath); err != nil {
		return err
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
	committed = true
	m.logger.Infof("auroramihomo %s staged to %s, will swap on shutdown", tag, stage)
	return nil
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
		return fmt.Errorf("checksum mismatch: want %s got %s", want, got)
	}
	return nil
}

// fetchSelfChecksum 依次尝试独立 .sha256 资产与 SHA256SUMS.txt，返回期望的
// 十六进制校验和。
func (m *Manager) fetchSelfChecksum(ctx context.Context, repo, tag, archiveURL, archiveName string) (string, error) {
	client, _ := m.httpClient()

	// 路径一：独立 .sha256 资产。
	if body, ok := m.fetchBytes(ctx, client, archiveURL+".sha256"); ok {
		if sum, err := parseChecksumFile(body, archiveName); err == nil {
			return sum, nil
		}
	}

	// 路径二：汇总文件 SHA256SUMS.txt。
	sumsURL := m.selfDownloadURL(tag, "SHA256SUMS.txt")
	body, err := m.fetchBytesOr(ctx, client, sumsURL)
	if err != nil {
		return "", fmt.Errorf("无法获取主程序校验和（独立 .sha256 与 SHA256SUMS.txt 均不可用）: %w", err)
	}
	sum, err := parseChecksumFile(body, archiveName)
	if err != nil {
		return "", fmt.Errorf("SHA256SUMS.txt 中未找到 %s 的校验和: %w", archiveName, err)
	}
	return sum, nil
}

// fetchBytes 尝试下载并返回 body；失败（网络错误或非 2xx）时返回 ok=false。
func (m *Manager) fetchBytes(ctx context.Context, client *http.Client, url string) ([]byte, bool) {
	body, err := m.fetchBytesOr(ctx, client, url)
	if err != nil {
		return nil, false
	}
	return body, true
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
//   - .new：上次下载但未及交换（异常退出）的待生效版本，保留它，
//     下次关停时 SwapSelfBinary 会继续完成升级；若用户改主意，
//     手工删掉该文件即可放弃本次升级。
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
		m.logger.Infof("检测到待生效的自升级 .new，将在下次关停时交换生效")
	}
}

package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

type Config struct {
	DataDir          string
	MihomoBinaryPath string
	ZashboardDir     string
	// AdGuardBinaryPath 为空时默认 DataDir/bin/adguardFileName()
	AdGuardBinaryPath string
	AutoUpdateEnabled bool
	AutoUpdateCron    string
	MihomoRepo        string
	ZashboardRepo     string
	// AdGuardRepo 为空时默认 AdguardTeam/AdGuardHome
	AdGuardRepo        string
	GitHubAPI          string
	HTTPTimeoutSeconds int
	// CDNProviders 为 GitHub Release 资产的下载源（内核与面板都以 Release 分发）
	CDNProviders []string
	// UseMihomoProxy 决定下载与版本查询是否优先经由本地 mihomo 代理。
	// 默认开启：内核跑起来后，走它出网通常比第三方镜像更快也更可靠。
	UseMihomoProxy bool
}

type RuntimeSettings struct {
	AutoUpdateEnabled bool     `json:"autoUpdateEnabled"`
	AutoUpdateCron    string   `json:"autoUpdateCron"`
	CDNProviders      []string `json:"cdnProviders"`
	// UseMihomoProxy 是否优先经由本地 mihomo 代理访问 GitHub
	UseMihomoProxy bool `json:"useMihomoProxy"`
	// MihomoProxyURL 为当前探测到的代理地址，未就绪时为空串
	MihomoProxyURL   string `json:"mihomoProxyUrl"`
	MihomoPath       string `json:"mihomoPath"`
	ZashboardDir     string `json:"zashboardDir"`
	MihomoPresent    bool   `json:"mihomoPresent"`
	ZashboardPresent bool   `json:"zashboardPresent"`
	// MihomoVersion 由 mihomo -v 探测，未知时为空串
	MihomoVersion string `json:"mihomoVersion"`
	// ZashboardVersion 为上次更新时记录的 release tag。
	// zashboard 是纯静态资源，本地没有可执行文件可查询版本，
	// 只能记录我们自己下载的那一次；历史安装会是空串。
	ZashboardVersion string `json:"zashboardVersion"`
}

type Manager struct {
	// updateMu 串行化 UpdateMihomo / UpdateZashboard / UpdateAdGuard。
	// 会覆盖二进制与面板目录，并发执行会写出内容交错的坏文件、并互相删掉备份。
	updateMu sync.Mutex

	cfg    Config
	client *http.Client
	logger logx.Logger
	mu     sync.RWMutex

	// zashboardVersion 记录上次成功更新的 release tag。
	// 由 SettingsService 在启动时从数据库回灌，更新成功后再写回，
	// 因此进程重启不会丢。受 mu 保护。
	zashboardVersion string
	// persistZashboardVersion 把面板版本写入持久层。
	// 用回调注入而非直接依赖 repository：updater 不应反向依赖数据层。
	persistZashboardVersion func(version string) error
	// proxyURLFn 返回本地 mihomo 的 HTTP 代理地址（如 http://127.0.0.1:7890）。
	// 用回调注入而非直接读配置文件：代理端口取决于当前生效的 config.yaml，
	// 由 ConfigService 负责解析，updater 不该重复一份解析逻辑。
	// 返回空串表示代理不可用（内核未运行或未开放混合端口）。
	proxyURLFn func() string
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset 是 release 里的单个可下载资产。
// 提为具名类型（原先是匿名结构体）便于测试构造资产列表。
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Size 为 GitHub 官方声明的字节数，用于校验经第三方 CDN
	// 下载回来的文件是否被篡改/截断（mihomo release 不提供 sha256 文件）
	Size int64 `json:"size"`
}

func New(cfg Config) *Manager {
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.MihomoBinaryPath == "" {
		cfg.MihomoBinaryPath = filepath.Join(cfg.DataDir, "bin", mihomoFileName())
	}
	if cfg.ZashboardDir == "" {
		cfg.ZashboardDir = filepath.Join(cfg.DataDir, "zashboard")
	}
	if cfg.MihomoRepo == "" {
		cfg.MihomoRepo = "MetaCubeX/mihomo"
	}
	if cfg.ZashboardRepo == "" {
		cfg.ZashboardRepo = "Zephyruso/zashboard"
	}
	if cfg.AdGuardRepo == "" {
		cfg.AdGuardRepo = "AdguardTeam/AdGuardHome"
	}
	if cfg.AdGuardBinaryPath == "" {
		cfg.AdGuardBinaryPath = filepath.Join(cfg.DataDir, "bin", adguardFileName())
	}
	if cfg.GitHubAPI == "" {
		cfg.GitHubAPI = "https://api.github.com"
	}
	if cfg.HTTPTimeoutSeconds <= 0 {
		cfg.HTTPTimeoutSeconds = 120
	}
	if cfg.AutoUpdateCron == "" {
		cfg.AutoUpdateCron = "0 0 4 * * *"
	}
	cfg.CDNProviders = normalizeCDNList(cfg.CDNProviders)

	return &Manager{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.HTTPTimeoutSeconds) * time.Second,
			// 直连路径也打面板专用 fwmark：透明代理 TProxy 模式下，
			// 不打标的话"直连"其实会被 mihomo 接管，用户关掉
			// UseMihomoProxy 的意图就被无声地绕过了。
			// 更要紧的是 mihomo 挂掉时这条路是下载内核的唯一通道，
			// 不能让它也依赖 mihomo。
			Transport: &http.Transport{
				DialContext: netcheck.MarkedDialContext(dialTimeout, logx.Errorf),
			},
		},
		logger: logx.WithContext(context.Background()),
	}
}

// dialTimeout 单次 TCP 建连超时。
// 与下面代理路径上用的是同一个值：都希望"连不上"能快速回落到下一个源，
// 而不是耗尽整体 Timeout。
const dialTimeout = 5 * time.Second

func mihomoFileName() string {
	if runtime.GOOS == "windows" {
		return "mihomo.exe"
	}
	return "mihomo"
}

func (m *Manager) MihomoBinaryPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.MihomoBinaryPath
}

func (m *Manager) AdGuardBinaryPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AdGuardBinaryPath
}

func (m *Manager) ZashboardDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.ZashboardDir
}

func (m *Manager) AutoUpdateCron() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AutoUpdateCron
}

func (m *Manager) AutoUpdateEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AutoUpdateEnabled
}

func (m *Manager) CDNProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.cfg.CDNProviders...)
}

// UseMihomoProxy 是否优先经由本地 mihomo 代理出网
func (m *Manager) UseMihomoProxy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.UseMihomoProxy
}

func (m *Manager) GetSettings() RuntimeSettings {
	m.mu.RLock()
	proxyEnabled := m.cfg.UseMihomoProxy
	st := RuntimeSettings{
		AutoUpdateEnabled: m.cfg.AutoUpdateEnabled,
		AutoUpdateCron:    m.cfg.AutoUpdateCron,
		CDNProviders:      append([]string{}, m.cfg.CDNProviders...),
		UseMihomoProxy:    proxyEnabled,
		MihomoPath:        m.cfg.MihomoBinaryPath,
		ZashboardDir:      m.cfg.ZashboardDir,
		MihomoPresent:     fileExists(m.cfg.MihomoBinaryPath),
		ZashboardPresent:  zashboardReady(m.cfg.ZashboardDir),
		ZashboardVersion:  m.zashboardVersion,
	}
	m.mu.RUnlock()

	// 代理地址在锁外探测：解析配置文件可能有 IO，不该阻塞状态查询
	if proxyEnabled {
		st.MihomoProxyURL = m.proxyURL()
	}
	return st
}

func (m *Manager) ApplySettings(enabled *bool, cron string, cdn []string, useProxy *bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if enabled != nil {
		m.cfg.AutoUpdateEnabled = *enabled
	}
	if strings.TrimSpace(cron) != "" {
		// basic validation: 5 or 6 fields
		fields := strings.Fields(cron)
		if len(fields) != 5 && len(fields) != 6 {
			return fmt.Errorf("invalid cron expression, expect 5 or 6 fields")
		}
		// robfig with seconds expects 6 fields; expand 5-field to 6-field
		if len(fields) == 5 {
			cron = "0 " + cron
		}
		m.cfg.AutoUpdateCron = cron
	}
	if cdn != nil {
		m.cfg.CDNProviders = normalizeCDNList(cdn)
	}
	if useProxy != nil {
		m.cfg.UseMihomoProxy = *useProxy
	}
	return nil
}

// SetProxyURLFunc 注入本地 mihomo 代理地址的查询回调。
func (m *Manager) SetProxyURLFunc(fn func() string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxyURLFn = fn
}

// proxyURL 返回当前可用的 mihomo 代理地址，不可用时为空串。
func (m *Manager) proxyURL() string {
	m.mu.RLock()
	fn := m.proxyURLFn
	enabled := m.cfg.UseMihomoProxy
	m.mu.RUnlock()
	if !enabled || fn == nil {
		return ""
	}
	return strings.TrimSpace(fn())
}

// httpClient 返回本次请求应使用的客户端。
//
// 开启代理且代理可用时返回走代理的客户端，否则返回直连客户端。
// 每次调用都重新判断：内核可能在运行期被启停，缓存客户端会让
// 「内核起来后仍走直连」或反之。构造 http.Client 本身开销可忽略。
func (m *Manager) httpClient() (*http.Client, string) {
	proxy := m.proxyURL()
	if proxy == "" {
		return m.client, ""
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		m.logger.Errorf("mihomo 代理地址无法解析，改为直连: %q", proxy)
		return m.client, ""
	}
	return &http.Client{
		Timeout: m.client.Timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
			// 代理不可用时快速失败，好尽早回落到直连或镜像；
			// 不设的话默认无连接超时，会一直等到整体 Timeout。
			//
			// 这里同样打面板 fwmark：拨的是到 mihomo 混合端口的本地连接，
			// 目标虽是回环、本不会被 TPROXY 抓，但打标不影响它，
			// 而两条路径用同一个构造方式能避免"改了一处忘了另一处"。
			DialContext: netcheck.MarkedDialContext(dialTimeout, logx.Errorf),
		},
	}, proxy
}

// SetZashboardVersion 记录面板版本。启动时由设置服务回灌历史值，
// 更新成功后写入新 tag。
func (m *Manager) SetZashboardVersion(v string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zashboardVersion = strings.TrimSpace(v)
}

// ZashboardVersion 返回已记录的面板版本，未知时为空串
func (m *Manager) ZashboardVersion() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.zashboardVersion
}

// SetZashboardVersionPersister 注入版本落库回调，使版本在进程重启后仍可显示。
func (m *Manager) SetZashboardVersionPersister(fn func(version string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistZashboardVersion = fn
}

// versionPersister 在锁内取出回调，避免在持锁期间执行落库（会阻塞状态查询）
func (m *Manager) versionPersister() func(string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.persistZashboardVersion
}

func (m *Manager) DefaultCDNProviders() []string {
	return append([]string{}, DefaultCDNProviders...)
}

// ComponentCheck 描述一个组件（mihomo / zashboard / AdGuardHome）的本地就绪状态与远端最新版本对比结果。
type ComponentCheck struct {
	Present       bool   `json:"present"`       // 本地是否已就绪
	LocalVersion  string `json:"localVersion"`  // 本地已安装版本（未知时为空串）
	LatestVersion string `json:"latestVersion"` // 远端最新版本，查询失败时为空串
	UpdateNeeded  bool   `json:"updateNeeded"`  // 本地缺失，或本地版本与远端不一致
	Error         string `json:"error,omitempty"`
}

// CheckLatest 查询 mihomo / zashboard / AdGuardHome 在 GitHub 上的最新 release，
// 与本地已记录的版本比对，得出是否需要更新。
// localVersion 为空串时（如从未成功探测过版本）只能判断"是否存在"，
// 无法判断"是否需要更新"。
//
// AdGuard 为可选组件，不在 EnsureComponents 里自动下载，但检查更新时一并返回状态。
func (m *Manager) CheckLatest(ctx context.Context, mihomoLocalVersion, adguardLocalVersion string) (mihomo, zashboard, adguard ComponentCheck) {
	mihomo.Present = fileExists(m.MihomoBinaryPath())
	mihomo.LocalVersion = mihomoLocalVersion
	if rel, err := m.latestRelease(ctx, m.repoMihomo()); err != nil {
		mihomo.Error = err.Error()
	} else {
		mihomo.LatestVersion = rel.TagName
	}
	mihomo.UpdateNeeded = !mihomo.Present ||
		(mihomo.LatestVersion != "" && mihomo.LocalVersion != "" && !versionMatches(mihomo.LocalVersion, mihomo.LatestVersion))

	zashboard.Present = zashboardReady(m.ZashboardDir())
	// zashboard 是纯静态资源，无可执行文件可查版本，只能用我们下载时记下的 tag。
	// 历史安装（或手工放入的目录）没有记录，此时仍只能判断"是否已下载"。
	zashboard.LocalVersion = m.ZashboardVersion()
	if rel, err := m.latestRelease(ctx, m.repoZashboard()); err != nil {
		zashboard.Error = err.Error()
	} else {
		zashboard.LatestVersion = rel.TagName
	}
	zashboard.UpdateNeeded = !zashboard.Present ||
		(zashboard.LatestVersion != "" && zashboard.LocalVersion != "" && !versionMatches(zashboard.LocalVersion, zashboard.LatestVersion))

	adguard.Present = fileExists(m.AdGuardBinaryPath())
	adguard.LocalVersion = adguardLocalVersion
	if rel, err := m.latestRelease(ctx, m.repoAdGuard()); err != nil {
		adguard.Error = err.Error()
	} else {
		adguard.LatestVersion = rel.TagName
	}
	adguard.UpdateNeeded = !adguard.Present ||
		(adguard.LatestVersion != "" && adguard.LocalVersion != "" && !versionMatches(adguard.LocalVersion, adguard.LatestVersion))
	return mihomo, zashboard, adguard
}

// versionMatches 判断本地版本字符串（通常形如 "Mihomo Meta v1.19.29 ..."）
// 是否包含远端 release 的 tag（如 "v1.19.29"）。
func versionMatches(local, latestTag string) bool {
	tag := strings.TrimSpace(latestTag)
	if tag == "" {
		return true
	}
	return strings.Contains(local, tag)
}

// EnsureComponents checks local assets and downloads missing ones.
func (m *Manager) EnsureComponents(ctx context.Context) error {
	m.mu.RLock()
	bin := m.cfg.MihomoBinaryPath
	zdir := m.cfg.ZashboardDir
	m.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(zdir, 0o755); err != nil {
		return err
	}

	if !fileExists(bin) {
		m.logger.Infof("mihomo binary missing, downloading...")
		if err := m.UpdateMihomo(ctx); err != nil {
			return fmt.Errorf("ensure mihomo failed: %w", err)
		}
	} else {
		m.logger.Infof("mihomo binary found: %s", bin)
	}

	if !zashboardReady(zdir) {
		m.logger.Infof("zashboard assets missing, downloading...")
		if err := m.UpdateZashboard(ctx); err != nil {
			return fmt.Errorf("ensure zashboard failed: %w", err)
		}
	} else {
		m.logger.Infof("zashboard assets found: %s", zdir)
	}
	return nil
}

func (m *Manager) UpdateMihomo(ctx context.Context) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	rel, err := m.latestRelease(ctx, m.repoMihomo())
	if err != nil {
		return err
	}
	assetURL, assetName, assetSize, err := pickMihomoAsset(rel)
	if err != nil {
		return err
	}
	m.logger.Infof("downloading mihomo %s (%s)", rel.TagName, assetName)

	tmpDir, err := os.MkdirTemp("", "aurora-mihomo-*")
	if err != nil {
		return err
	}
	// 临时目录清理属于尽力而为，失败也没有补救动作
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, assetName)
	if err := m.downloadWithCDN(ctx, assetURL, archivePath, assetSize); err != nil {
		return err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}

	// .gz（Linux/macOS）是单个裸二进制，解出来就是可执行文件本身，
	// 没有目录结构可遍历；.zip（Windows）才是归档。
	var binPath string
	if strings.HasSuffix(strings.ToLower(assetName), ".gz") {
		binPath = filepath.Join(extractDir, mihomoFileName())
		if err := gunzipFile(archivePath, binPath); err != nil {
			return err
		}
	} else {
		if err := unzip(archivePath, extractDir); err != nil {
			return err
		}
		var err error
		binPath, err = findExtractedBinary(extractDir, "mihomo")
		if err != nil {
			return err
		}
	}

	target := m.MihomoBinaryPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := copyFile(binPath, target); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(target, 0o755)
	}

	cmd := exec.CommandContext(ctx, target, "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mihomo binary invalid: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	m.logger.Infof("mihomo ready: %s", strings.TrimSpace(string(out)))
	return nil
}

// UpdateAdGuard 下载并安装最新 AdGuardHome 二进制。
// 可选组件：仅在显式调用时安装，EnsureComponents 不会自动拉取。
func (m *Manager) UpdateAdGuard(ctx context.Context) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	rel, err := m.latestRelease(ctx, m.repoAdGuard())
	if err != nil {
		return err
	}
	assetURL, assetName, assetSize, err := pickAdGuardAsset(rel)
	if err != nil {
		return err
	}
	m.logger.Infof("downloading AdGuardHome %s (%s)", rel.TagName, assetName)

	tmpDir, err := os.MkdirTemp("", "aurora-adguard-*")
	if err != nil {
		return err
	}
	// 临时目录清理属于尽力而为，失败也没有补救动作
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, assetName)
	if err := m.downloadWithCDN(ctx, assetURL, archivePath, assetSize); err != nil {
		return err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}

	lowerName := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		if err := unzip(archivePath, extractDir); err != nil {
			return err
		}
	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		if err := untarGz(archivePath, extractDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported AdGuardHome archive format: %s", assetName)
	}

	// keyword 小写：findExtractedBinary 会对文件名 ToLower 后再 Contains
	binPath, err := findExtractedBinary(extractDir, "adguardhome")
	if err != nil {
		return err
	}

	target := m.AdGuardBinaryPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	// 已有文件先备份，便于安装失败或版本回退时手工恢复
	if fileExists(target) {
		if err := copyFile(target, target+".bak"); err != nil {
			m.logger.Errorf("备份旧 AdGuardHome 失败: %v", err)
		}
	}
	if err := copyFile(binPath, target); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(target, 0o755)
	}

	// 校验：文件可执行且体积非空；再尝试 --version / --help。
	// 校验失败仍保留已写入的文件（用户可能手工修复权限），但向上返回错误。
	st, err := os.Stat(target)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("AdGuardHome binary invalid: empty or missing after install")
	}
	if err := verifyAdGuardBinary(ctx, target); err != nil {
		return fmt.Errorf("AdGuardHome binary invalid: %w", err)
	}
	m.logger.Infof("AdGuardHome ready: %s (%s)", target, rel.TagName)
	return nil
}

// verifyAdGuardBinary 尝试启动二进制确认它能跑。
// --version 与 --help 任一能跑通即可；有的构建对未知 flag 返回非 0 但仍会打印帮助。
// 不访问网络。
func verifyAdGuardBinary(ctx context.Context, path string) error {
	var lastErr error
	for _, args := range [][]string{{"--version"}, {"--help"}, {"-h"}} {
		cmd := exec.CommandContext(ctx, path, args...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		// 部分平台/构建对 --help 返回 exit 1/2 但仍是合法二进制；
		// 只要进程确实启动过（有输出），就视为通过。
		if strings.TrimSpace(string(out)) != "" {
			return nil
		}
		lastErr = fmt.Errorf("%v (%s)", err, strings.TrimSpace(string(out)))
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("binary did not respond to --version/--help")
}

func (m *Manager) UpdateZashboard(ctx context.Context) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	rel, err := m.latestRelease(ctx, m.repoZashboard())
	if err != nil {
		return err
	}
	assetURL, assetName, assetSize, err := pickZashboardAsset(rel)
	if err != nil {
		return err
	}
	m.logger.Infof("downloading zashboard %s (%s)", rel.TagName, assetName)

	tmpDir, err := os.MkdirTemp("", "aurora-zashboard-*")
	if err != nil {
		return err
	}
	// 临时目录清理属于尽力而为，失败也没有补救动作
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, assetName)
	if err := m.downloadWithCDN(ctx, assetURL, archivePath, assetSize); err != nil {
		return err
	}
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := unzip(archivePath, extractDir); err != nil {
		return err
	}

	webRoot, err := findWebRoot(extractDir)
	if err != nil {
		return err
	}

	target := m.ZashboardDir()
	backup := target + ".bak"
	_ = os.RemoveAll(backup)
	if dirExists(target) {
		_ = os.Rename(target, backup)
	}
	if err := copyDir(webRoot, target); err != nil {
		_ = os.RemoveAll(target)
		if dirExists(backup) {
			_ = os.Rename(backup, target)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	// 只在替换成功后记录版本，失败已回滚旧目录，写入新 tag 会谎报
	m.SetZashboardVersion(rel.TagName)
	if persist := m.versionPersister(); persist != nil {
		if err := persist(rel.TagName); err != nil {
			// 落库失败只影响重启后的版本显示，面板本身已更新成功，不该整体报错
			m.logger.Errorf("记录 zashboard 版本失败: %v", err)
		}
	}
	m.logger.Infof("zashboard ready at %s (%s)", target, rel.TagName)
	return nil
}

func (m *Manager) repoMihomo() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.MihomoRepo
}

func (m *Manager) repoZashboard() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.ZashboardRepo
}

func (m *Manager) repoAdGuard() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.AdGuardRepo
}

func (m *Manager) githubAPI() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.GitHubAPI
}

// latestRelease 查询指定仓库的最新 release。
//
// 只走官方 api.github.com，不套任何镜像前缀：ghproxy 系镜像只代理
// github.com 的下载路径，jsdelivr 只镜像仓库内文件，两者都不代理 REST API，
// 套上去只会产生一串必然 404 的请求。
//
// 直连不通的情况由 mihomo 代理解决：开启后先经代理请求，失败再直连兜底。
// 这也顺带避免了从第三方镜像取元数据的信任问题——asset 的下载地址与体积
// 都来自这份 JSON，被篡改则后续的体积校验形同虚设。
func (m *Manager) latestRelease(ctx context.Context, repo string) (*githubRelease, error) {
	apiBase := strings.TrimRight(m.githubAPI(), "/")
	official := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)

	client, proxy := m.httpClient()
	if proxy != "" {
		rel, err := m.fetchReleaseJSON(ctx, official, client)
		if err == nil {
			return rel, nil
		}
		m.logger.Errorf("经 mihomo 代理(%s)查询 release 失败，改为直连: %v", proxy, err)
	}

	rel, err := m.fetchReleaseJSON(ctx, official, m.client)
	if err != nil {
		return nil, fmt.Errorf("查询 %s 的最新版本失败: %w", repo, err)
	}
	return rel, nil
}

func (m *Manager) fetchReleaseJSON(ctx context.Context, apiURL string, client *http.Client) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AuroraMihomo-Updater/0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" || len(rel.Assets) == 0 {
		return nil, fmt.Errorf("invalid release metadata")
	}
	return &rel, nil
}

// mihomoAssetExt 返回当前平台对应的官方资产扩展名。
//
// 官方 release 只给 Windows 发 .zip，Linux / macOS 一律是 .gz（单个裸二进制
// 经 gzip 压缩，不是 tar 归档）。此前这里统一按 .zip 过滤，导致在 Linux 上
// 匹配不到任何资产、内核根本装不上——透明代理依赖内核，必须先修这个。
//
// 同一 release 里还有 .deb / .rpm / .pkg.tar.zst 等发行版包，它们需要包管理器
// 安装且落盘路径不受我们控制，一律不用。
func mihomoAssetExt() string {
	if runtime.GOOS == "windows" {
		return ".zip"
	}
	return ".gz"
}

// 返回 (下载地址, 文件名, 官方声明体积, error)
func pickMihomoAsset(rel *githubRelease) (string, string, int64, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	var candidates []string
	switch {
	case goos == "windows" && goarch == "amd64":
		candidates = []string{"windows-amd64", "windows-x86_64"}
	case goos == "windows" && goarch == "arm64":
		candidates = []string{"windows-arm64"}
	case goos == "linux" && goarch == "amd64":
		candidates = []string{"linux-amd64", "linux-x86_64"}
	case goos == "linux" && goarch == "arm64":
		candidates = []string{"linux-arm64", "linux-aarch64"}
	case goos == "darwin" && goarch == "amd64":
		candidates = []string{"darwin-amd64", "macos-amd64"}
	case goos == "darwin" && goarch == "arm64":
		candidates = []string{"darwin-arm64", "macos-arm64"}
	default:
		return "", "", 0, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}

	wantExt := mihomoAssetExt()
	// compatible 构建（不依赖较新 CPU 指令集）优先，兼容性更好
	var preferred, secondary []struct {
		url, name string
		size      int64
	}
	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if !strings.HasSuffix(name, wantExt) || !strings.Contains(name, "mihomo") {
			continue
		}
		// .pkg.tar.zst 也以 .zst 结尾不会误入，但 Arch 的包名里同样含
		// linux-<arch>，显式排除发行版包更稳妥
		if isDistroPackage(name) {
			continue
		}
		// 排除特化变体，理由见 isSpecializedVariant
		if isSpecializedVariant(name) {
			continue
		}
		matched := false
		for _, c := range candidates {
			if strings.Contains(name, c) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		item := struct {
			url, name string
			size      int64
		}{asset.BrowserDownloadURL, asset.Name, asset.Size}
		if strings.Contains(name, "compatible") {
			preferred = append(preferred, item)
		} else {
			secondary = append(secondary, item)
		}
	}
	if len(preferred) > 0 {
		return preferred[0].url, preferred[0].name, preferred[0].size, nil
	}
	if len(secondary) > 0 {
		return secondary[0].url, secondary[0].name, secondary[0].size, nil
	}
	return "", "", 0, fmt.Errorf("no mihomo %s asset matched for %s/%s in %s", wantExt, goos, goarch, rel.TagName)
}

// isDistroPackage 识别发行版包。这类资产需要包管理器安装、落盘路径不由我们
// 决定，与"下载到 data/bin 下自管"的模型不符。
func isDistroPackage(lowerName string) bool {
	for _, suffix := range []string{".deb", ".rpm", ".pkg.tar.zst", ".apk"} {
		if strings.HasSuffix(lowerName, suffix) {
			return true
		}
	}
	return false
}

// specializedVariantRe 匹配官方为特定环境额外编译的变体：
//
//	-go120- / -go122- / -go124-   用旧 Go 版本构建，供旧内核/旧 macOS 使用
//	-v1- / -v2- / -v3-            x86-64 微架构等级（v3 需要 AVX2 等指令集）
//
// 同一平台下这些变体和基础版并存（v1.19.29 的 linux-amd64 就有 11 个 .gz），
// 若按"第一个匹配"来取，结果取决于 GitHub 返回顺序：既不稳定，还可能选到
// 当前 CPU 跑不了的 v3。这里一律跳过，只在基础版与 compatible 之间选，
// 后者本身就是官方给的"兼容性优先"构建。
var specializedVariantRe = regexp.MustCompile(`-(go1[0-9]{2}|v[123])-`)

func isSpecializedVariant(lowerName string) bool {
	return specializedVariantRe.MatchString(lowerName)
}

// 返回 (下载地址, 文件名, 官方声明体积, error)
func pickZashboardAsset(rel *githubRelease) (string, string, int64, error) {
	preferred := []string{"dist.zip", "zashboard.zip", "release.zip"}
	for _, p := range preferred {
		for _, asset := range rel.Assets {
			if strings.EqualFold(asset.Name, p) {
				return asset.BrowserDownloadURL, asset.Name, asset.Size, nil
			}
		}
	}
	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".zip") && (strings.Contains(name, "dist") || strings.Contains(name, "zashboard") || strings.Contains(name, "web")) {
			return asset.BrowserDownloadURL, asset.Name, asset.Size, nil
		}
	}
	for _, asset := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") {
			return asset.BrowserDownloadURL, asset.Name, asset.Size, nil
		}
	}
	return "", "", 0, fmt.Errorf("no zashboard zip asset in %s", rel.TagName)
}

// downloadWithCDN 依次尝试各下载源。expectedSize 来自 GitHub 官方 API
// 声明的 asset 体积（0 表示未知）：由于 mihomo release 不提供 sha256 校验文件，
// 体积比对是我们能对经第三方镜像下载的产物做的最强完整性校验。
// downloadWithCDN 下载 Release 资产。
//
// 顺序是「先经 mihomo 代理直取官方地址，再依次尝试各 CDN 镜像」：
// 内核已在运行时走它出网通常比第三方镜像更快，且拿到的是官方原始文件，
// 无需担心镜像返回被篡改或截断的内容。代理不可用（内核未运行、未开放
// 混合端口）时自动跳过这一步，回落到原有的镜像列表。
func (m *Manager) downloadWithCDN(ctx context.Context, officialURL, dest string, expectedSize int64) error {
	var errs []string

	// 校验并接受一次下载产物；返回 false 表示该源不可信，已清理落盘文件
	accept := func(source string) bool {
		st, err := os.Stat(dest)
		if err != nil || st.Size() < 1024 {
			errs = append(errs, fmt.Sprintf("%s => invalid file size", source))
			_ = os.Remove(dest)
			return false
		}
		if expectedSize > 0 && st.Size() != expectedSize {
			// 体积与官方声明不一致，说明该来源返回的内容不可信，
			// 直接丢弃并换下一个源，绝不能落盘后拿去执行
			errs = append(errs, fmt.Sprintf("%s => 体积与官方声明不符(期望 %d, 实际 %d)", source, expectedSize, st.Size()))
			m.logger.Errorf("拒绝来自 %s 的下载产物：体积与官方声明不符（期望 %d，实际 %d）", source, expectedSize, st.Size())
			_ = os.Remove(dest)
			return false
		}
		m.logger.Infof("download success via %s (%d bytes)", source, st.Size())
		return true
	}

	if client, proxy := m.httpClient(); proxy != "" {
		m.logger.Infof("尝试经 mihomo 代理(%s)下载: %s", proxy, officialURL)
		if err := m.downloadFile(ctx, officialURL, dest, client); err != nil {
			errs = append(errs, fmt.Sprintf("mihomo 代理(%s) => %v", proxy, err))
			m.logger.Errorf("经 mihomo 代理下载失败，改用 CDN 镜像: %v", err)
		} else if accept("mihomo 代理 " + proxy) {
			return nil
		}
	}

	urls := buildCDNURLs(officialURL, m.CDNProviders())
	for i, u := range urls {
		m.logger.Infof("trying download source [%d/%d]: %s", i+1, len(urls), u)
		if err := m.downloadFile(ctx, u, dest, m.client); err != nil {
			errs = append(errs, fmt.Sprintf("%s => %v", u, err))
			m.logger.Errorf("download failed via %s: %v", u, err)
			continue
		}
		if accept(u) {
			return nil
		}
	}
	return fmt.Errorf("all download sources failed: %s", strings.Join(errs, " | "))
}

func (m *Manager) downloadFile(ctx context.Context, url, dest string, client *http.Client) error {
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
	_, err = io.Copy(f, resp.Body)
	return err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) && filepath.Clean(target) != filepath.Clean(dest) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// untarGz 解压 .tar.gz / .tgz 归档到 dest。
// 与 unzip 相同，拒绝写出提取目录之外的路径（路径穿越）。
func untarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = zr.Close() }()

	tr := tar.NewReader(zr)
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// 忽略绝对路径前缀，统一按相对路径处理
		name := hdr.Name
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
			name = strings.TrimLeft(name, `/\`)
		}
		target := filepath.Join(dest, name)
		// 防止 ../ 写出 dest 之外（与 unzip 同一策略）
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) &&
			filepath.Clean(target) != filepath.Clean(dest) {
			return fmt.Errorf("illegal file path in tar: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := hdr.FileInfo().Mode()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			// 跳过链接等特殊类型，官方 AdGuard 包只含目录与普通文件
			continue
		}
	}
}

// gunzipFile 解压单文件 gzip 流。
//
// 与 unzip 不同，这里没有归档条目、也就没有路径穿越风险：目标路径由调用方
// 指定，gzip 头里的原始文件名一律忽略（官方资产名形如
// mihomo-linux-amd64-v1.19.29.gz，解出来就是二进制本身）。
//
// 直接建成 0o755：这是要执行的内核二进制，调用方随后还会 chmod 一次，
// 但先给出可执行位可以避免中间态出现不可执行的文件。
func gunzipFile(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = zr.Close() }()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, zr); err != nil {
		_ = out.Close()
		return fmt.Errorf("gunzip %s: %w", filepath.Base(src), err)
	}
	return out.Close()
}

func findExtractedBinary(root, keyword string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := strings.ToLower(d.Name())
		if !strings.Contains(name, keyword) {
			return nil
		}
		if runtime.GOOS == "windows" {
			if strings.HasSuffix(name, ".exe") {
				found = path
				return io.EOF
			}
			return nil
		}
		if !strings.Contains(name, ".") {
			found = path
			return io.EOF
		}
		return nil
	})
	// io.EOF 是上面用来提前终止遍历的哨兵，不是真错误
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("binary %s not found in archive", keyword)
	}
	return found, nil
}

func findWebRoot(root string) (string, error) {
	if fileExists(filepath.Join(root, "index.html")) {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if fileExists(filepath.Join(p, "index.html")) {
			return p, nil
		}
		sub, _ := os.ReadDir(p)
		for _, s := range sub {
			if s.IsDir() {
				sp := filepath.Join(p, s.Name())
				if fileExists(filepath.Join(sp, "index.html")) {
					return sp, nil
				}
			}
		}
	}
	return "", fmt.Errorf("index.html not found in zashboard archive")
}

func zashboardReady(dir string) bool {
	return fileExists(filepath.Join(dir, "index.html"))
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

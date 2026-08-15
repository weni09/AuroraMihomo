package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"auroramihomo/backend/internal/applog"
	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/updater"

	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

const (
	settingAutoUpdateEnabled = "auto_update.enabled"
	settingAutoUpdateCron    = "auto_update.cron"
	// settingCDNProviders 为 Release 资产（内核/面板二进制）的下载源
	settingCDNProviders = "auto_update.cdn_providers"
	// settingLastCDNProvider 上次成功的全局下载源，下次优先尝试
	settingLastCDNProvider = "auto_update.last_cdn_provider"
	// settingRawCDNProviders 为 raw.githubusercontent.com 内容的加速下载源
	//（模板转换远程地址、订阅远程源等）
	settingRawCDNProviders = "auto_update.raw_cdn_providers"
	// settingLastRawCDNProvider 上次成功的 raw 加速源，下次优先尝试
	settingLastRawCDNProvider = "auto_update.last_raw_cdn_provider"
	// settingUseMihomoProxy 是否优先经由本地 mihomo 代理访问 GitHub
	settingUseMihomoProxy = "auto_update.use_mihomo_proxy"
	// settingSelfRepo 主程序（AuroraMihomo 自身）的仓库，运行期可配置。
	// 空串 = 显式停用面板内自升级；未设置（首次启动）时用 updater 的默认值。
	settingSelfRepo = "auto_update.self_repo"
	// settingZashboardVersion 记录面板上次更新到的 release tag。
	// 面板是纯静态资源，本地无法反查版本，只能在下载时记账。
	settingZashboardVersion = "zashboard.version"
	// settingLogRetentionDays 应用日志文件的保留天数。
	// 只影响轮转归档，不影响内存缓冲与当前正在写的文件。
	settingLogRetentionDays = "applog.retention_days"
	// settingLogCleanupCron 清理任务的调度表达式；
	// settingLogCleanupEnabled 为 "0" 时停掉定时清理（仍可靠大小轮转兜底）。
	settingLogCleanupCron     = "applog.cleanup_cron"
	settingLogCleanupEnabled  = "applog.cleanup_enabled"
	settingMergePolicyProxy   = "merge.policy.proxy"
	settingMergePolicyRule    = "merge.policy.rule"
	settingMergePolicyDNS     = "merge.policy.dns"
	settingMergePolicyTUN     = "merge.policy.tun"
	settingMergePolicyGeneral = "merge.policy.general"
	settingRemoteSourceType   = "remote.source.type"
	settingRemoteSourceID     = "remote.source.id"
	settingRemoteSourceURL    = "remote.source.url"
	// settingRemoteSourceCron 远程来源的定时拉取调度表达式。
	// 与系统设置里的自动更新用同一套 Cron 语法，由调度器统一驱动。
	settingRemoteSourceCron = "remote.source.cron"
	// settingRemoteSourceEnabled 是否启用远程拉取的定时任务
	settingRemoteSourceEnabled = "remote.source.cron_enabled"
	// settingMonitorEnabled 服务器资源监控总开关（控制台资源卡片）。
	// settingMonitorIntervalSec 为卡片的刷新间隔（秒），取值见 MonitorIntervalOptions。
	settingMonitorEnabled     = "monitor.enabled"
	settingMonitorIntervalSec = "monitor.interval_sec"
)

// MonitorIntervalOptions 是允许的刷新间隔（秒）。
// 前端设置页的下拉框必须与此保持一致：新增档位要两边同步改。
var MonitorIntervalOptions = []int{1, 3, 5, 10, 30}

// defaultMonitorIntervalSec 是资源卡片的默认刷新间隔。
// 取 3s：比旧版 60s 有实时感，又不会像 1s 那样放大网络与采样开销。
const defaultMonitorIntervalSec = 3

// 远程拉取的默认调度：每小时的第 0 分 0 秒。
const defaultRemoteSourceCron = "0 0 * * * *"

type SettingsService struct {
	db       *repository.Database
	updater  *updater.Manager
	reloadFn func(enabled bool, cronExpr string) error
	// remotePullReloadFn 重装远程拉取的定时任务，由 API 层注入
	remotePullReloadFn func(enabled bool, cronExpr string) error
	logCleanupReloadFn func(enabled bool, cronExpr string) error
	// mihomoVersionFn 查询内核版本，由 API 层注入
	mihomoVersionFn func() string
	// rawCDNFn 把当前 raw 加速源推给所有 fetcher 实例（模板/订阅拉取）。
	// 设置变更后需即时生效，而 fetcher 实例散落在各 service 内，
	// 由 API 层统一注入、遍历注册。
	rawCDNFn func(providers []string)
	logger   logx.Logger
}

func NewSettingsService(db *repository.Database, upd *updater.Manager) *SettingsService {
	return &SettingsService{
		db:      db,
		updater: upd,
		logger:  logx.WithContext(context.Background()),
	}
}

func (s *SettingsService) SetReloadFunc(fn func(enabled bool, cronExpr string) error) {
	s.reloadFn = fn
}

// SetRawCDNFunc 注入 raw 加速源推送回调。设置变更或启动装载后触发，
// 由 API 层把清洗后的列表推给所有 fetcher 实例。
func (s *SettingsService) SetRawCDNFunc(fn func(providers []string)) {
	s.rawCDNFn = fn
}

// pushRawCDN 把当前生效的 raw 加速源推给注册的接收方。
func (s *SettingsService) pushRawCDN() {
	if s.rawCDNFn != nil {
		s.rawCDNFn(s.updater.RawCDNProviders())
	}
}

func (s *SettingsService) LoadAndApply() error {
	enabled := s.updater.AutoUpdateEnabled()
	cronExpr := s.updater.AutoUpdateCron()
	cdn := s.updater.CDNProviders()
	rawCDN := s.updater.RawCDNProviders()
	useProxy := s.updater.UseMihomoProxy()

	if v, err := s.db.GetSetting(settingAutoUpdateEnabled); err == nil {
		enabled = v == "1" || strings.EqualFold(v, "true")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if v, err := s.db.GetSetting(settingAutoUpdateCron); err == nil && strings.TrimSpace(v) != "" {
		cronExpr = v
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if arr, err := s.loadCDNSetting(settingCDNProviders); err != nil {
		return err
	} else if arr != nil {
		cdn = arr
	}
	if arr, err := s.loadCDNSetting(settingRawCDNProviders); err != nil {
		return err
	} else if arr != nil {
		rawCDN = arr
	}
	if v, err := s.db.GetSetting(settingUseMihomoProxy); err == nil {
		useProxy = v == "1" || strings.EqualFold(v, "true")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// 主程序仓库：读不到（首次启动）保持 updater.New 兜底的默认值；
	// 存了空串 = 用户显式停用自升级，同样写回 updater。
	if v, err := s.db.GetSetting(settingSelfRepo); err == nil {
		s.updater.SetSelfRepo(v)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// 日志保留天数：读不到（首次启动）时沿用默认值，不报错
	if v, err := s.db.GetSetting(settingLogRetentionDays); err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(v)); convErr == nil {
			applog.SetRetentionDays(n)
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// 面板版本回灌：进程重启后仍能在组件状态里显示上次更新到的版本
	if v, err := s.db.GetSetting(settingZashboardVersion); err == nil {
		s.updater.SetZashboardVersion(v)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	s.updater.SetZashboardVersionPersister(func(version string) error {
		return s.db.SetSetting(settingZashboardVersion, version)
	})
	// 上次成功下载源：回灌后挂落库，下载成功即记住，重启仍优先该源
	if v, err := s.db.GetSetting(settingLastCDNProvider); err == nil {
		s.updater.SetLastCDNProvider(v)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	s.updater.SetLastCDNPersister(func(provider string) error {
		return s.db.SetSetting(settingLastCDNProvider, provider)
	})
	// 上次成功的 raw 加速源：同样回灌 + 落库，重启后仍优先该源
	if v, err := s.db.GetSetting(settingLastRawCDNProvider); err == nil {
		s.updater.SetLastRawCDNProvider(v)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	s.updater.SetLastRawCDNPersister(func(provider string) error {
		return s.db.SetSetting(settingLastRawCDNProvider, provider)
	})

	if err := s.updater.ApplySettings(&enabled, cronExpr, cdn, rawCDN, &useProxy); err != nil {
		return err
	}
	s.pushRawCDN()
	if s.reloadFn != nil {
		return s.reloadFn(enabled, s.updater.AutoUpdateCron())
	}
	return nil
}

// loadCDNSetting 读取一组 CDN 配置。
// 返回 nil 表示未设置（调用方应沿用现值），而非「设置为空列表」。
func (s *SettingsService) loadCDNSetting(key string) ([]string, error) {
	v, err := s.db.GetSetting(key)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var arr []string
	if json.Unmarshal([]byte(v), &arr) != nil || len(arr) == 0 {
		return nil, nil
	}
	return arr, nil
}

type UpdateSettingsInput struct {
	AutoUpdateEnabled *bool    `json:"autoUpdateEnabled,optional"`
	AutoUpdateCron    string   `json:"autoUpdateCron,optional"`
	CDNProviders      []string `json:"cdnProviders,optional"`
	// RawCDNProviders 为 raw.githubusercontent.com 内容的加速下载源，
	// 空串列表表示清除加速、回落直连官方。
	RawCDNProviders []string `json:"rawCdnProviders,optional"`
	// UseMihomoProxy 是否优先经由本地 mihomo 代理出网，nil 表示不修改
	UseMihomoProxy *bool `json:"useMihomoProxy,optional"`
	// SelfRepo 主程序（AuroraMihomo 自身）仓库，nil 表示不修改。
	// 传空串 = 显式停用面板内自升级；传仓库地址即切换（可改成自建 fork）。
	SelfRepo *string `json:"selfRepo,optional"`
	// LogRetentionDays 应用日志文件保留天数，nil 表示不修改。
	// 取值被夹到 [1, 365]，只影响轮转归档，不影响内存缓冲。
	LogRetentionDays *int `json:"logRetentionDays,optional"`
	// LogCleanupCron 清理任务的调度表达式，空串表示不修改
	LogCleanupCron string `json:"logCleanupCron,optional"`
	// LogCleanupEnabled 是否启用定时清理，nil 表示不修改
	LogCleanupEnabled *bool `json:"logCleanupEnabled,optional"`
	// MonitorEnabled 服务器资源监控总开关，nil 表示不修改
	MonitorEnabled *bool `json:"monitorEnabled,optional"`
	// MonitorIntervalSec 资源卡片刷新间隔（秒），nil 表示不修改。
	// 只接受 MonitorIntervalOptions 内的档位，其余值回退默认 3s。
	MonitorIntervalSec *int `json:"monitorIntervalSec,optional"`
}

// Get 返回运行期设置，并补上需要实时探测的 mihomo 版本。
//
// 版本不进 updater 的配置：它由内核二进制决定，缓存在 mihomo 进程管理器里，
// 让 updater 也存一份会产生两个可能不一致的来源。
func (s *SettingsService) Get() updater.RuntimeSettings {
	st := s.updater.GetSettings()
	if s.mihomoVersionFn != nil {
		st.MihomoVersion = s.mihomoVersionFn()
	}
	return st
}

// SetMihomoVersionFunc 注入 mihomo 版本查询回调。
// 用回调而非直接依赖 mihomo.Manager，避免 service 层与进程管理层互相引用。
func (s *SettingsService) SetMihomoVersionFunc(fn func() string) {
	s.mihomoVersionFn = fn
}

// NormalizeCron 校验并规范化 Cron 表达式。
//
// 调度器启用了秒级字段（cron.WithSeconds），因此内部统一用 6 段式。
// 用户习惯写 5 段（分 时 日 月 周），这里自动补上秒位的 "0"，
// 避免「我按标准 crontab 写的为什么报错」。
//
// 返回规范化后的 6 段表达式；空串原样返回（由调用方决定是否使用默认值）。
func NormalizeCron(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	if len(strings.Fields(expr)) == 5 {
		expr = "0 " + expr
	}
	if len(strings.Fields(expr)) != 6 {
		return "", fmt.Errorf("定时表达式需为 5 段（分 时 日 月 周）或 6 段（含秒），当前 %d 段",
			len(strings.Fields(expr)))
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(expr); err != nil {
		return "", fmt.Errorf("定时表达式非法: %w", err)
	}
	return expr, nil
}

func (s *SettingsService) Update(in UpdateSettingsInput) (updater.RuntimeSettings, error) {
	// validate cron early
	cronExpr, err := NormalizeCron(in.AutoUpdateCron)
	if err != nil {
		return updater.RuntimeSettings{}, err
	}
	// 日志清理表达式同样先校验再落库：写进去再报错会留下一个
	// 界面显示、实际装载不上的坏值
	cleanupCron, err := NormalizeCron(in.LogCleanupCron)
	if err != nil {
		return updater.RuntimeSettings{}, fmt.Errorf("日志清理%w", err)
	}

	if err := s.updater.ApplySettings(in.AutoUpdateEnabled, cronExpr, in.CDNProviders, in.RawCDNProviders, in.UseMihomoProxy); err != nil {
		return updater.RuntimeSettings{}, err
	}
	// 主程序仓库先应用再取快照：st.SelfRepo 必须是 SetSelfRepo 之后的新值，
	// 否则下面落库会写进旧值（trim 前的原始输入）。
	// 空串 = 显式停用，落库后下次启动 LoadAndApply 同样读到空串。
	// 仅当请求显式带 SelfRepo 时才改写与落库：nil 表示不碰该项，
	// 避免「只改监控/日志」的部分更新把默认仓库强行写进 settings 表。
	if in.SelfRepo != nil {
		s.updater.SetSelfRepo(*in.SelfRepo)
	}

	// persist
	st := s.updater.GetSettings()
	if err := s.db.SetSetting(settingAutoUpdateEnabled, boolToStr(st.AutoUpdateEnabled)); err != nil {
		return updater.RuntimeSettings{}, err
	}
	if err := s.db.SetSetting(settingAutoUpdateCron, st.AutoUpdateCron); err != nil {
		return updater.RuntimeSettings{}, err
	}
	// 落库归一化后的结果（而非入参），保证读回与运行期一致
	b, _ := json.Marshal(st.CDNProviders)
	if err := s.db.SetSetting(settingCDNProviders, string(b)); err != nil {
		return updater.RuntimeSettings{}, err
	}
	rb, _ := json.Marshal(st.RawCDNProviders)
	if err := s.db.SetSetting(settingRawCDNProviders, string(rb)); err != nil {
		return updater.RuntimeSettings{}, err
	}
	if err := s.db.SetSetting(settingUseMihomoProxy, boolToStr(st.UseMihomoProxy)); err != nil {
		return updater.RuntimeSettings{}, err
	}
	if in.SelfRepo != nil {
		if err := s.db.SetSetting(settingSelfRepo, st.SelfRepo); err != nil {
			return updater.RuntimeSettings{}, err
		}
	}
	// 日志保留天数不属于 updater 的运行期设置（它管的是组件更新），
	// 故单独应用与落库。落库的是夹取后的值，保证读回与生效值一致。
	if in.LogRetentionDays != nil {
		days := applog.SetRetentionDays(*in.LogRetentionDays)
		if err := s.db.SetSetting(settingLogRetentionDays, strconv.Itoa(days)); err != nil {
			return updater.RuntimeSettings{}, err
		}
	}
	// 清理调度的改动需要重装任务；先落库再重装，
	// 保证重装读到的就是刚保存的值
	scheduleChanged := false
	if strings.TrimSpace(in.LogCleanupCron) != "" {
		if err := s.db.SetSetting(settingLogCleanupCron, cleanupCron); err != nil {
			return updater.RuntimeSettings{}, err
		}
		scheduleChanged = true
	}
	if in.LogCleanupEnabled != nil {
		if err := s.db.SetSetting(settingLogCleanupEnabled, boolToStr(*in.LogCleanupEnabled)); err != nil {
			return updater.RuntimeSettings{}, err
		}
		scheduleChanged = true
	}
	if scheduleChanged {
		if err := s.ApplyLogCleanupSchedule(); err != nil {
			return updater.RuntimeSettings{}, fmt.Errorf("重装日志清理任务失败: %w", err)
		}
	}

	// 资源监控设置：纯展示配置（前端轮询节奏），无调度任务可重装，落库即生效
	if in.MonitorEnabled != nil {
		if err := s.db.SetSetting(settingMonitorEnabled, boolToStr(*in.MonitorEnabled)); err != nil {
			return updater.RuntimeSettings{}, err
		}
	}
	if in.MonitorIntervalSec != nil {
		sec := *in.MonitorIntervalSec
		if !validMonitorInterval(sec) {
			sec = defaultMonitorIntervalSec
		}
		if err := s.db.SetSetting(settingMonitorIntervalSec, strconv.Itoa(sec)); err != nil {
			return updater.RuntimeSettings{}, err
		}
	}

	s.pushRawCDN()
	if s.reloadFn != nil {
		if err := s.reloadFn(st.AutoUpdateEnabled, st.AutoUpdateCron); err != nil {
			return updater.RuntimeSettings{}, err
		}
	}
	return s.Get(), nil
}

func boolToStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// ---- 服务器资源监控 ----

// MonitorEnabled 返回资源监控总开关，未设置时默认开启。
// 默认开启：这是面板的轻量展示功能，新装用户开箱即用更符合预期。
func (s *SettingsService) MonitorEnabled() bool {
	v, err := s.db.GetSetting(settingMonitorEnabled)
	if err != nil {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true")
}

// MonitorIntervalSec 返回资源卡片的刷新间隔（秒）。
// 未设置或值不在合法档位内时回退默认 3s：非法值只可能来自手工改库，
// 回退到确定的默认值比「猜一个最近的档位」更可预期。
func (s *SettingsService) MonitorIntervalSec() int {
	v, err := s.db.GetSetting(settingMonitorIntervalSec)
	if err != nil {
		return defaultMonitorIntervalSec
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(v))
	if convErr != nil || !validMonitorInterval(n) {
		return defaultMonitorIntervalSec
	}
	return n
}

// validMonitorInterval 判断间隔是否在允许的档位集合内。
func validMonitorInterval(sec int) bool {
	for _, opt := range MonitorIntervalOptions {
		if sec == opt {
			return true
		}
	}
	return false
}

// ---- Merge Policy (设计 §16：用户可配置合并策略) ----

// GetMergePolicy 读取用户配置的合并策略，缺省回落到默认值
func (s *SettingsService) GetMergePolicy() domain.MergePolicy {
	p := domain.DefaultMergePolicy()
	if v, err := s.db.GetSetting(settingMergePolicyProxy); err == nil && v != "" {
		p.ProxyPriority = normalizeStaleManual(v)
	}
	if v, err := s.db.GetSetting(settingMergePolicyRule); err == nil && v != "" {
		p.RulePriority = normalizeStaleManual(v)
	}
	if v, err := s.db.GetSetting(settingMergePolicyDNS); err == nil && v != "" {
		p.DNSPriority = v
	}
	if v, err := s.db.GetSetting(settingMergePolicyTUN); err == nil && v != "" {
		p.TUNPriority = v
	}
	if v, err := s.db.GetSetting(settingMergePolicyGeneral); err == nil && v != "" {
		p.GeneralPriority = v
	}
	return p
}

// normalizeStaleManual 把旧版允许保存的 manual 策略归一化回默认 local。
// manual 已移除（冲突统一自动解决），存量值继续使用会让引擎产生
// unresolved 冲突、控制台显示无法处理的「待处理」。
func normalizeStaleManual(v string) string {
	if v == "manual" {
		return "local"
	}
	return v
}

// SetMergePolicy 持久化用户选择的合并策略
func (s *SettingsService) SetMergePolicy(proxy, rule, dns, tun, general string) (domain.MergePolicy, error) {
	// manual 已移除：冲突全部按策略自动解决，没有手动处理入口，
	// 选 manual 只会让控制台显示无法处理的「待处理」。
	valid := map[string]bool{"local": true, "remote": true, "merge": true}
	// dns/tun 只是系统级配置的 Local/Remote First 切换（设计 §11/§16），
	// 不支持 merge/manual 这类需要对象级合并策略的语义
	simpleValid := map[string]bool{"local": true, "remote": true}

	if proxy != "" {
		if !valid[proxy] {
			return domain.MergePolicy{}, fmt.Errorf("proxy 策略非法: %s", proxy)
		}
		if err := s.db.SetSetting(settingMergePolicyProxy, proxy); err != nil {
			return domain.MergePolicy{}, err
		}
	}
	if rule != "" {
		if !valid[rule] {
			return domain.MergePolicy{}, fmt.Errorf("rule 策略非法: %s", rule)
		}
		if err := s.db.SetSetting(settingMergePolicyRule, rule); err != nil {
			return domain.MergePolicy{}, err
		}
	}
	if dns != "" {
		if !simpleValid[dns] {
			return domain.MergePolicy{}, fmt.Errorf("dns 策略非法: %s", dns)
		}
		if err := s.db.SetSetting(settingMergePolicyDNS, dns); err != nil {
			return domain.MergePolicy{}, err
		}
	}
	if tun != "" {
		if !simpleValid[tun] {
			return domain.MergePolicy{}, fmt.Errorf("tun 策略非法: %s", tun)
		}
		if err := s.db.SetSetting(settingMergePolicyTUN, tun); err != nil {
			return domain.MergePolicy{}, err
		}
	}
	if general != "" {
		if !simpleValid[general] {
			return domain.MergePolicy{}, fmt.Errorf("general 策略非法: %s", general)
		}
		if err := s.db.SetSetting(settingMergePolicyGeneral, general); err != nil {
			return domain.MergePolicy{}, err
		}
	}
	return s.GetMergePolicy(), nil
}

// ---- 远程配置来源 ----

// GetRemoteSource 读取用户选定的远程配置来源。
// 未设置或数据非法时回落为「不使用远程配置」（默认不填）。
func (s *SettingsService) GetRemoteSource() domain.RemoteSource {
	src := domain.DefaultRemoteSource()
	if v, err := s.db.GetSetting(settingRemoteSourceType); err == nil && strings.TrimSpace(v) != "" {
		src.Type = strings.TrimSpace(v)
	}
	if v, err := s.db.GetSetting(settingRemoteSourceID); err == nil && strings.TrimSpace(v) != "" {
		if id, convErr := strconv.ParseInt(strings.TrimSpace(v), 10, 64); convErr == nil {
			src.ID = id
		}
	}
	if v, err := s.db.GetSetting(settingRemoteSourceURL); err == nil {
		src.URL = strings.TrimSpace(v)
	}
	src.Cron = s.remoteSourceCron()
	src.CronEnabled = s.remoteSourceCronEnabled()
	// 存量数据可能因实体被删而失效，回落保证合并链路始终可用。
	//
	// Valid() 只校验形状（type 合法、id>0 / url 非空），查不出
	// 「id 指向的实体已被删除」：设置时校验过存在性，但之后实体可能被删，
	// 而删除路径不会回头修正这条设置。此时 Valid() 依旧为真，悬空引用会
	// 一路带到合并阶段才炸（buildRemoteConfig 报「指定作为远程来源的
	// 订阅(N)不存在或已禁用」），表现为「拉取并合并」永久失败；更难查的是
	// 界面下拉框因候选项里没有该 id 而显示空白，用户看不出问题在哪。
	// 所以这里必须连实体是否还在一起判断。
	if !src.Valid() || !s.remoteSourceEntityExists(src) {
		def := domain.DefaultRemoteSource()
		// 调度设置与来源本身相互独立，来源失效不应把用户配的 Cron 一起丢掉
		def.Cron = src.Cron
		def.CronEnabled = src.CronEnabled
		// 把回落持久化，否则每次读取都要重新发现一遍失效，
		// 且界面仍显示那个已不存在的 id，不手动改设置就一直卡在报错上
		s.persistRemoteSourceFallback(src)
		return def
	}
	return src
}

// remoteSourceEntityExists 判断来源指向的实体是否仍存在且可用。
// none/all/url 不依赖实体，恒为真。
func (s *SettingsService) remoteSourceEntityExists(src domain.RemoteSource) bool {
	switch src.Type {
	case domain.RemoteSourceSubscription:
		sub, err := s.db.GetSubscription(src.ID)
		// 被禁用的订阅同样无法充当远程来源（buildRemoteConfig 会跳过它，
		// 最终报的正是"不存在或已禁用"），故一并视为失效
		return err == nil && sub != nil && sub.Enabled == 1
	case domain.RemoteSourceCollection:
		c, err := s.db.GetCollection(src.ID)
		return err == nil && c != nil && c.Enabled == 1
	case domain.RemoteSourceFile:
		f, err := s.db.GetFile(src.ID)
		// 与 SetRemoteSource 的校验保持一致：只有 mihomo 配置类型的
		// 文件模板能充当远程层
		return err == nil && f != nil && f.ConfigType == model.FileConfigTypeMihomo
	default:
		return true
	}
}

// persistRemoteSourceFallback 把失效的来源设置改写为「不使用远程配置」。
// 已是默认值时直接返回，避免每次读取都产生无意义的写入。
func (s *SettingsService) persistRemoteSourceFallback(src domain.RemoteSource) {
	if src.Type == domain.RemoteSourceNone && src.ID == 0 && src.URL == "" {
		return
	}
	s.logger.Errorf("远程来源已失效（type=%s id=%d），自动回落为不使用远程配置", src.Type, src.ID)
	if err := s.db.SetSetting(settingRemoteSourceType, domain.RemoteSourceNone); err != nil {
		s.logger.Errorf("回落远程来源类型失败: %v", err)
	}
	if err := s.db.SetSetting(settingRemoteSourceID, "0"); err != nil {
		s.logger.Errorf("回落远程来源 id 失败: %v", err)
	}
	if err := s.db.SetSetting(settingRemoteSourceURL, ""); err != nil {
		s.logger.Errorf("回落远程来源 url 失败: %v", err)
	}
}

// remoteSourceCron 读取远程拉取的调度表达式，未设置或非法时回落到默认值。
func (s *SettingsService) remoteSourceCron() string {
	v, err := s.db.GetSetting(settingRemoteSourceCron)
	if err != nil || strings.TrimSpace(v) == "" {
		return defaultRemoteSourceCron
	}
	normalized, nerr := NormalizeCron(v)
	if nerr != nil || normalized == "" {
		return defaultRemoteSourceCron
	}
	return normalized
}

// remoteSourceCronEnabled 是否启用远程拉取的定时任务。
// 默认启用：用户配了远程来源，通常就是希望它能自动更新。
func (s *SettingsService) remoteSourceCronEnabled() bool {
	v, err := s.db.GetSetting(settingRemoteSourceEnabled)
	if err != nil || strings.TrimSpace(v) == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true")
}

// SetRemoteSource 持久化远程配置来源。
//
// 校验时要求非 all 类型必须带有效 ID，并确认目标实体确实存在——
// 否则用户会得到一份静默退化为"仅本地配置"的结果，
// 而界面上仍显示他选中的来源。
// RemoteSourceInput 描述一次远程来源设置。
// 用结构体而非长参数列表：字段会继续增加，位置参数在调用点无法自解释。
type RemoteSourceInput struct {
	Type string
	ID   int64
	URL  string
	// Cron 留空表示不修改，沿用已存的值
	Cron string
	// CronEnabled 为 nil 表示不修改
	CronEnabled *bool
}

func (s *SettingsService) SetRemoteSource(in RemoteSourceInput) (domain.RemoteSource, error) {
	sourceType, id, rawURL := in.Type, in.ID, in.URL
	src := domain.RemoteSource{
		Type: strings.TrimSpace(sourceType),
		ID:   id,
		URL:  strings.TrimSpace(rawURL),
	}
	// 留空即「不使用远程配置」，最终配置等于配置中心的本地配置
	if src.Type == "" {
		src.Type = domain.RemoteSourceNone
	}
	if !src.Valid() {
		if src.Type == domain.RemoteSourceURL {
			return domain.RemoteSource{}, fmt.Errorf("选择外部订阅链接时必须填写地址")
		}
		return domain.RemoteSource{}, fmt.Errorf("远程来源配置非法: type=%s id=%d", src.Type, src.ID)
	}

	switch src.Type {
	case domain.RemoteSourceSubscription:
		if _, err := s.db.GetSubscription(src.ID); err != nil {
			return domain.RemoteSource{}, fmt.Errorf("订阅(%d)不存在", src.ID)
		}
	case domain.RemoteSourceCollection:
		if _, err := s.db.GetCollection(src.ID); err != nil {
			return domain.RemoteSource{}, fmt.Errorf("组合(%d)不存在", src.ID)
		}
	case domain.RemoteSourceFile:
		f, err := s.db.GetFile(src.ID)
		if err != nil {
			return domain.RemoteSource{}, fmt.Errorf("文件(%d)不存在", src.ID)
		}
		// 只有 mihomo 类型的文件模板才会渲染出可合并的配置；
		// 原样输出型文件（规则片段等）不能充当远程配置层。
		if f.ConfigType != model.FileConfigTypeMihomo {
			return domain.RemoteSource{}, fmt.Errorf("文件「%s」的配置类型不是 Mihomo 配置，无法作为远程来源", f.Name)
		}
	case domain.RemoteSourceURL:
		// 与 fetcher 保持一致：只允许 http/https。
		// 提前在设置阶段拦掉，避免存下一个每次合并都必然失败的地址。
		u, perr := url.Parse(src.URL)
		if perr != nil {
			return domain.RemoteSource{}, fmt.Errorf("订阅链接无法解析: %w", perr)
		}
		if sc := strings.ToLower(u.Scheme); sc != "http" && sc != "https" {
			return domain.RemoteSource{}, fmt.Errorf("订阅链接仅支持 http/https，当前为 %q", u.Scheme)
		}
		if strings.TrimSpace(u.Host) == "" {
			return domain.RemoteSource{}, fmt.Errorf("订阅链接缺少主机名")
		}
	}

	if err := s.db.SetSetting(settingRemoteSourceType, src.Type); err != nil {
		return domain.RemoteSource{}, err
	}
	if err := s.db.SetSetting(settingRemoteSourceID, strconv.FormatInt(src.ID, 10)); err != nil {
		return domain.RemoteSource{}, err
	}
	if err := s.db.SetSetting(settingRemoteSourceURL, src.URL); err != nil {
		return domain.RemoteSource{}, err
	}
	// 空串表示不修改，沿用已存的调度
	if strings.TrimSpace(in.Cron) != "" {
		normalized, cerr := NormalizeCron(in.Cron)
		if cerr != nil {
			return domain.RemoteSource{}, cerr
		}
		if err := s.db.SetSetting(settingRemoteSourceCron, normalized); err != nil {
			return domain.RemoteSource{}, err
		}
	}
	if in.CronEnabled != nil {
		flag := "0"
		if *in.CronEnabled {
			flag = "1"
		}
		if err := s.db.SetSetting(settingRemoteSourceEnabled, flag); err != nil {
			return domain.RemoteSource{}, err
		}
	}

	out := s.GetRemoteSource()
	// 调度参数变了要立刻重装任务，否则得等进程重启才生效
	if s.remotePullReloadFn != nil {
		if err := s.remotePullReloadFn(out.CronEnabled && out.Type != domain.RemoteSourceNone, out.Cron); err != nil {
			s.logger.Errorf("重装远程拉取定时任务失败: %v", err)
		}
	}
	return out, nil
}

// SetRemotePullReloadFunc 注入「重装远程拉取定时任务」的回调。
// 设置变更后需即时生效，而调度器由 API 层持有，故用回调解耦。
func (s *SettingsService) SetRemotePullReloadFunc(fn func(enabled bool, cronExpr string) error) {
	s.remotePullReloadFn = fn
}

// ApplyRemotePullSchedule 按当前设置装载远程拉取任务，供启动时调用。
func (s *SettingsService) ApplyRemotePullSchedule() error {
	if s.remotePullReloadFn == nil {
		return nil
	}
	src := s.GetRemoteSource()
	// 未配置远程来源时不必挂任务，省掉每轮的空转
	return s.remotePullReloadFn(src.CronEnabled && src.Type != domain.RemoteSourceNone, src.Cron)
}

// DefaultLogCleanupCron 是日志清理的默认调度：每天凌晨 3:30。
//
// 选凌晨低峰：清理要遍历目录并删文件。分钟取 30 而非 0，
// 避开整点——整点通常已经堆了别的定时任务。
const DefaultLogCleanupCron = "0 30 3 * * *"

// SetLogCleanupReloadFunc 注册日志清理任务的装载回调。
// 与自动更新、远程拉取同一套模式：调度由数据库里的设置决定，
// 改动后调用 ApplyLogCleanupSchedule 即时重装，无需重启进程。
func (s *SettingsService) SetLogCleanupReloadFunc(fn func(enabled bool, cronExpr string) error) {
	s.logCleanupReloadFn = fn
}

// LogCleanupCron 返回当前生效的清理调度，未设置时给默认值。
func (s *SettingsService) LogCleanupCron() string {
	v, err := s.db.GetSetting(settingLogCleanupCron)
	if err != nil || strings.TrimSpace(v) == "" {
		return DefaultLogCleanupCron
	}
	// 存量值也过一遍校验：数据库可能被手工改坏，
	// 此时回退默认值而不是让任务装载失败、清理彻底停摆
	if norm, err := NormalizeCron(v); err == nil && norm != "" {
		return norm
	}
	logx.Errorf("数据库中的日志清理表达式非法，回退默认值 %s", DefaultLogCleanupCron)
	return DefaultLogCleanupCron
}

// LogCleanupEnabled 返回是否启用定时清理，未设置时默认启用。
func (s *SettingsService) LogCleanupEnabled() bool {
	v, err := s.db.GetSetting(settingLogCleanupEnabled)
	if err != nil {
		return true // 首次启动：默认开启
	}
	return v != "0" && !strings.EqualFold(v, "false")
}

// ApplyLogCleanupSchedule 按当前设置装载清理任务。
func (s *SettingsService) ApplyLogCleanupSchedule() error {
	if s.logCleanupReloadFn == nil {
		return nil
	}
	return s.logCleanupReloadFn(s.LogCleanupEnabled(), s.LogCleanupCron())
}

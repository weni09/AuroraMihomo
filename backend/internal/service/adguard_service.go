package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/netcheck"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/updater"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// AdGuardStatusDTO 是面板 AdGuard 页与 API 共用的状态视图。
type AdGuardStatusDTO struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	WorkDir   string `json:"workDir"`
	WebAddr   string `json:"webAddr"`
	DNSPort   int    `json:"dnsPort"`
	// ComponentEnabled 产品化组件总开关（默认 false）
	ComponentEnabled bool `json:"componentEnabled"`
	// DnsMode 0 未托管 / 1 绑定 53 / 2 重定向 53→AGH
	DnsMode int `json:"dnsMode"`
	// Wiring 为 "off" / "on"；展示层可把 on 渲染成「已对接」
	Wiring      string `json:"wiring"`
	WiringLabel string `json:"wiringLabel"` // "未对接" / "已对接"
	LastError   string `json:"lastError,omitempty"`
	// EntryPath 同源反代路径，前端 iframe 用
	EntryPath string `json:"entryPath"`
	// Snapshot 仅 wiring=on 时附带，便于 UI 展示可回滚内容
	Snapshot *WiringPlan `json:"snapshot,omitempty"`
	// CdnProviders AdGuard 升级专用镜像列表（空则用全局 CDN）
	CdnProviders []string `json:"cdnProviders,omitempty"`
	// AutoUpdate 是否启用 AdGuard 独立自动更新
	AutoUpdate bool `json:"autoUpdate"`
	// AutoUpdateCron AdGuard 自动更新表达式（6 段）；空则默认
	AutoUpdateCron string `json:"autoUpdateCron,omitempty"`
	// Username AGH 管理员用户名（settings 优先，否则 yaml）
	Username string `json:"username,omitempty"`
	// DesiredRunning 用户期望的运行态（enabled_at_boot）。
	// 面板重启后据此决定是否自启；与实时 Running 可能短暂不一致。
	DesiredRunning bool `json:"desiredRunning"`
	// ManagedBy 运行形态："process"（面板托管子进程）/ "systemd" / "openrc"。
	// 服务模式下进程由系统服务看护，面板退出后 DNS 仍常驻。
	ManagedBy string `json:"managedBy"`
}

// AdGuardService 编排 AdGuard 安装/启停与 DNS 一键对接。
//
// 进程细节在 adguard.Manager；下载在 updater；本服务负责 settings、
// TransparentService 端口覆盖、ConfigService base dns.listen 与快照回滚。
// dnsRedirectApplier 无 TProxy 时下发/拆除「53→AGH 端口」专用规则。
type dnsRedirectApplier interface {
	ApplyDNSRedirect(ctx context.Context, p netcheck.DNSRedirectParams) error
	TeardownDNSRedirect(ctx context.Context) error
}

type AdGuardService struct {
	db      *repository.Database
	updater *updater.Manager
	mgr     *adguard.Manager
	transp  *TransparentService
	cfgSvc  *ConfigService
	// dnsRedir 可选：Linux 上注入 netcheck.Applier；非 Linux 为 nil 时模式 2 无 TProxy 会明确报错
	dnsRedir dnsRedirectApplier
	workDir  string
	webAddr  string
	logger   logx.Logger
	// scheduleReload 在自动更新开关/cron 或组件开关变化后重装调度任务。
	scheduleReload func() error
	// sso 可选：首次引导随机口令写入永久免密凭据。
	sso *adguard.SessionBridge
}

// SetScheduleReloadFunc 由 API 层注入：把 AdGuard 自动更新登记进 Scheduler。
func (s *AdGuardService) SetScheduleReloadFunc(fn func() error) {
	if s == nil {
		return
	}
	s.scheduleReload = fn
}

// SetSSO 注入 SessionBridge，供首次随机口令落库免密接管。
func (s *AdGuardService) SetSSO(bridge *adguard.SessionBridge) {
	if s == nil {
		return
	}
	s.sso = bridge
}

func (s *AdGuardService) reloadSchedule() {
	if s == nil || s.scheduleReload == nil {
		return
	}
	if err := s.scheduleReload(); err != nil {
		s.logger.Errorf("重装 AdGuard 自动更新调度失败: %v", err)
	}
}

// SetDNSRedirectApplier 注入仅 DNS 重定向能力（与全量 TProxy 表分离）。
func (s *AdGuardService) SetDNSRedirectApplier(a dnsRedirectApplier) {
	s.dnsRedir = a
}

// NewAdGuardService 构造编排服务。mgr / db 不可为 nil。
func NewAdGuardService(
	db *repository.Database,
	upd *updater.Manager,
	mgr *adguard.Manager,
	transp *TransparentService,
	cfgSvc *ConfigService,
	workDir, webAddr string,
) *AdGuardService {
	if webAddr == "" {
		webAddr = "127.0.0.1:3000"
	}
	return &AdGuardService{
		db:      db,
		updater: upd,
		mgr:     mgr,
		transp:  transp,
		cfgSvc:  cfgSvc,
		workDir: workDir,
		webAddr: webAddr,
		logger:  logx.WithContext(context.Background()),
	}
}

// Status 汇总进程状态、DNS 端口与 wiring 对接信息。
func (s *AdGuardService) Status(ctx context.Context) (*AdGuardStatusDTO, error) {
	_ = ctx
	st := s.mgr.Status()
	dto := &AdGuardStatusDTO{
		Installed:        st.Installed,
		Running:          st.Running,
		PID:              st.PID,
		Version:          st.Version,
		WorkDir:          st.WorkDir,
		WebAddr:          st.WebAddr,
		ComponentEnabled: s.ComponentEnabled(),
		DnsMode:          int(s.DNSMode()),
		LastError:        st.LastError,
		EntryPath:        "/adguard-ui/",
		Wiring:           adguardWiringOff,
		WiringLabel:      "未对接",
	}
	if dto.WorkDir == "" {
		dto.WorkDir = s.workDir
	}
	// Web 地址优先 settings / yaml 端口，保证改端口后 Status 与反代一致
	if v := strings.TrimSpace(s.getSetting(settingAdGuardWebAddr, "")); v != "" {
		dto.WebAddr = v
	} else if port, err := adguard.ReadWebPort(s.workDir); err == nil && port > 0 {
		dto.WebAddr = fmt.Sprintf("127.0.0.1:%d", port)
	} else if dto.WebAddr == "" {
		dto.WebAddr = s.webAddr
	}
	dto.CdnProviders = s.CDNProviders()
	dto.AutoUpdate = s.AutoUpdateEnabled()
	dto.AutoUpdateCron = s.AutoUpdateCron()
	dto.Username = s.AdminUsername()
	dto.DesiredRunning = s.DesiredRunning()
	dto.ManagedBy = s.mgr.ManagedBy()
	// 版本优先 settings（安装时记下的 tag），进程 Status 可能尚未探测
	if v := s.getSetting(settingAdGuardVersion, ""); v != "" && dto.Version == "" {
		dto.Version = v
	}
	dnsPort, err := adguard.ReadDNSPort(s.workDir)
	if err != nil {
		s.logger.Errorf("读取 AdGuard DNS 端口失败: %v", err)
	}
	if dnsPort <= 0 {
		if p := s.getSettingInt(settingAdGuardDNSPort, 0); p > 0 {
			dnsPort = p
		} else {
			dnsPort = adguard.DefaultDNSPort
		}
	}
	dto.DNSPort = dnsPort

	wiring := s.getSetting(settingAdGuardWiring, adguardWiringOff)
	if wiring == adguardWiringOn {
		dto.Wiring = adguardWiringOn
		dto.WiringLabel = "已对接"
		if raw := s.getSetting(settingAdGuardSnapshot, ""); raw != "" {
			if plan, err := unmarshalWiringSnapshot(raw); err == nil {
				dto.Snapshot = &plan
			}
		}
	}
	return dto, nil
}

// Install 下载安装最新 AdGuardHome 并记录版本 tag。
// 服务模式下额外注册系统服务单元（写一次，此后改端口/更新不再动它）。
func (s *AdGuardService) Install(ctx context.Context) error {
	if s.updater == nil {
		return errors.New("更新器未初始化，无法安装 AdGuard Home")
	}
	// 安装/更新前推送 AdGuard 专用 CDN（空则回落全局）
	s.applyAdGuardCDNToUpdater()
	if err := s.updater.UpdateAdGuard(ctx); err != nil {
		return err
	}
	// 记录版本：CheckLatest 的 local 侧之后会用 settings；此处尽量写入 tag
	_, _, agh := s.updater.CheckLatest(ctx, "", "")
	if agh.LatestVersion != "" {
		_ = s.db.SetSetting(settingAdGuardVersion, agh.LatestVersion)
		s.mgr.SetVersion(agh.LatestVersion)
	}
	// 首次安装确保 bind 回环，避免 AGH 默认暴露到局域网
	if err := adguard.EnsureBindLocalhost(s.workDir); err != nil {
		s.logger.Errorf("确保 AdGuard bind_host=127.0.0.1 失败: %v", err)
	}
	if port, err := adguard.ReadDNSPort(s.workDir); err == nil && port > 0 {
		_ = s.db.SetSetting(settingAdGuardDNSPort, strconv.Itoa(port))
	}
	// 服务模式：注册系统服务单元。失败返回 error——否则用户看到「安装成功」
	// 后点启动才 systemctl 失败，排查成本高。
	if err := s.registerServiceUnit(ctx); err != nil {
		return err
	}
	return nil
}

// registerServiceUnit 服务模式下注册系统服务单元（幂等）。
// unit 内容只含安装期不变的绝对路径参数，此后改端口/更新不再动它。
// exec 模式（controller nil）为 no-op。
func (s *AdGuardService) registerServiceUnit(ctx context.Context) error {
	c := s.serviceController()
	if c == nil {
		return nil
	}
	if s.updater == nil {
		return errors.New("更新器未初始化，无法注册 AdGuard 系统服务")
	}
	bin := s.updater.AdGuardBinaryPath()
	if bin == "" {
		return errors.New("AdGuard 二进制路径为空，无法注册系统服务")
	}
	if err := c.Install(ctx, bin, s.workDir, s.mgr.ConfigFilePath()); err != nil {
		return fmt.Errorf("注册 AdGuard 系统服务失败: %w", err)
	}
	return nil
}

// EnsureServiceUnitOnBoot 面板启动时调用：存量升级后若已安装 AGH 但尚无 unit，
// 补写 unit 并按「组件开 + 期望运行」决定是否 enable+start。
//
// 背景：服务化前 AGH 是面板子进程；升级后 ServiceMode=true 且 ShouldStartAtBoot
// 恒 false，若不补注册，已装用户的 AGH 会停在那起不来。
// 失败只记日志，不拖垮面板启动。
func (s *AdGuardService) EnsureServiceUnitOnBoot(ctx context.Context) {
	if s == nil || s.serviceController() == nil {
		return
	}
	if !s.BinaryPresent() {
		return
	}
	if err := s.registerServiceUnit(ctx); err != nil {
		s.logger.Errorf("启动时补注册 AdGuard 系统服务失败: %v", err)
		return
	}
	// 组件未启用：只写 unit，不 enable/start（与「关组件」语义一致）
	if !s.ComponentEnabled() {
		return
	}
	// 期望运行：settings 里的 enabled_at_boot（不用 DesiredRunning——
	// 服务模式下它读 systemctl is-enabled，unit 刚写完可能仍是 disabled）
	if !s.desiredRunningFromSettings() {
		return
	}
	if err := s.Start(ctx); err != nil {
		s.logger.Errorf("启动时按期望运行拉起 AdGuard 系统服务失败: %v", err)
	}
}

// desiredRunningFromSettings 只读 settings，不查 systemctl。
// 供升级迁移：unit 尚未 enable 时 DesiredRunning() 会误报 false。
func (s *AdGuardService) desiredRunningFromSettings() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardBoot, ""))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on") || v == "yes"
}

// serviceController 返回注入到进程 Manager 的系统服务控制器（nil 为 exec 模式）。
func (s *AdGuardService) serviceController() adguard.ServiceController {
	if s == nil || s.mgr == nil {
		return nil
	}
	return s.mgr.Controller()
}

// 面板开机自启：首次尝试前等待、失败后有限次重试（指数退避）。
// 总预算留给调用方 context；默认参数覆盖「mihomo/磁盘抢资源」类瞬时失败。
const (
	adguardBootStartInitialDelay = 800 * time.Millisecond
	adguardBootStartMaxAttempts  = 3
	adguardBootStartRetryBase    = 2 * time.Second
)

// Start 拉起 AdGuard 子进程，成功后记录「期望运行」以便面板重启后自启。
//
// 组件总开关关闭时直接拒绝：避免 API/调用方在 component_enabled=false 时仍把
// 进程拉起并写入 enabled_at_boot，造成「关了组件却在跑 / 期望态与开关矛盾」。
// 面板开机自启走 ShouldStartAtBoot，本身已要求组件开启；此处与之对齐。
func (s *AdGuardService) Start(ctx context.Context) error {
	if !s.ComponentEnabled() {
		return errors.New("AdGuard 组件未启用，请先在系统设置中开启组件后再启动")
	}
	if err := adguard.EnsureBindLocalhost(s.workDir); err != nil {
		s.logger.Errorf("启动前确保 bind_host 失败: %v", err)
	}
	if err := s.mgr.Start(ctx); err != nil {
		return err
	}
	// 首次完整引导生成的随机口令：落库免密 + 日志提示（明文在 workDir/initial_admin_password.txt）
	if pass := s.mgr.TakeInitialAdminPassword(); pass != "" {
		user := s.AdminUsername()
		if user == "" {
			user = "admin"
		}
		if s.sso != nil {
			s.sso.SetUsername(user)
			if err := s.sso.PersistCredentials(user, pass); err != nil {
				s.logger.Errorf("首次 AGH 口令免密落库失败: %v", err)
			} else if s.mgr.Status().Running {
				_ = s.sso.Establish(ctx, "1", user, pass)
			}
		}
		s.logger.Infof("AdGuard 首次管理员口令已生成（用户 %s），见 %s/initial_admin_password.txt", user, s.workDir)
	}
	// 仅在真正启动成功（含已在跑幂等）后落库，避免失败仍被当成要自启
	if err := s.setDesiredRunning(true); err != nil {
		s.logger.Errorf("记录 AdGuard 期望运行状态失败: %v", err)
	}
	return nil
}

// StartWithBootRetry 供面板进程启动时异步调用：先短暂让出资源，再有限次 Start。
//
// 与用户点击「启动」不同——那条路径失败应立刻返回，由前端提示；
// 开机自启面对的是 mihomo/磁盘/瞬态端口 等竞争，一次失败不代表用户意图改变，
// 故在 ctx 未取消前按 2s、4s… 退避重试，最多 adguardBootStartMaxAttempts 次。
//
// 任一次成功即返回 nil；全部失败返回最后一次 error（调用方只记日志，不拖垮面板）。
func (s *AdGuardService) StartWithBootRetry(ctx context.Context) error {
	return s.startWithBootRetry(ctx, adguardBootStartInitialDelay, adguardBootStartMaxAttempts, adguardBootStartRetryBase)
}

// startWithBootRetry 可注入延迟参数，便于单测不真 sleep 数秒。
func (s *AdGuardService) startWithBootRetry(ctx context.Context, initialDelay time.Duration, maxAttempts int, retryBase time.Duration) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("开机自启取消（初始等待）: %w", ctx.Err())
		case <-time.After(initialDelay):
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("开机自启取消（已失败 %d 次，末次: %s）: %w", attempt-1, lastErr.Error(), err)
			}
			return fmt.Errorf("开机自启取消: %w", err)
		}
		// 中途用户关组件 / 清 desired：不再重试，避免与「不要跑」意图打架
		if !s.ShouldStartAtBoot() {
			if lastErr != nil {
				return fmt.Errorf("开机自启中止（组件或期望运行已关闭，末次失败: %w）", lastErr)
			}
			return errors.New("开机自启中止（组件未启用、未期望运行或未安装）")
		}

		err := s.Start(ctx)
		if err == nil {
			if attempt > 1 {
				s.logger.Infof("AdGuard Home 开机自启在第 %d 次尝试成功", attempt)
			}
			return nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		s.logger.Errorf("AdGuard Home 开机自启第 %d/%d 次失败: %v，将重试", attempt, maxAttempts, err)
		// 指数退避：2s、4s、…（受 ctx 限制）
		delay := retryBase * time.Duration(1<<(attempt-1))
		select {
		case <-ctx.Done():
			return fmt.Errorf("开机自启取消（重试等待中，末次: %s）: %w", lastErr.Error(), ctx.Err())
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("开机自启 %d 次均失败: %w", maxAttempts, lastErr)
}

// Stop 用户主动停止。
// exec 模式：停进程并清除「期望运行」，面板重启后不再自启。
// 服务模式：systemctl stop 已保留 enable——用户临时停 ≠ 取消开机自启，
// settings 不动（DesiredRunning 读系统真实状态）。
func (s *AdGuardService) Stop(ctx context.Context) error {
	if err := s.mgr.Stop(ctx); err != nil {
		return err
	}
	if s.serviceController() != nil {
		return nil
	}
	if err := s.setDesiredRunning(false); err != nil {
		s.logger.Errorf("清除 AdGuard 期望运行状态失败: %v", err)
	}
	return nil
}

// SetBootEnabled 控制「开机自启」，不启停进程。
// 服务模式：驱动 systemctl enable/disable / rc-update（系统真实状态）。
// exec 模式：写 settings（面板重启后据此决定是否拉起）。
func (s *AdGuardService) SetBootEnabled(ctx context.Context, enabled bool) error {
	if c := s.serviceController(); c != nil {
		if err := c.SetBootEnabled(ctx, enabled); err != nil {
			return err
		}
	}
	if err := s.setDesiredRunning(enabled); err != nil {
		return err
	}
	return nil
}

// StopProcess 仅停止进程，不改期望运行态。
// 供自动更新、二进制升级等「临时停机再拉起」路径使用——若误走 Stop，
// 会把 enabled_at_boot 清掉，发版/重启面板后 AdGuard 就不再自启。
func (s *AdGuardService) StopProcess(ctx context.Context) error {
	return s.mgr.Stop(ctx)
}

// Restart 重启子进程；成功则保持/写入期望运行为 true。
func (s *AdGuardService) Restart(ctx context.Context) error {
	if err := s.mgr.Restart(ctx); err != nil {
		return err
	}
	if err := s.setDesiredRunning(true); err != nil {
		s.logger.Errorf("记录 AdGuard 期望运行状态失败: %v", err)
	}
	return nil
}

// WiringPreview 生成对接计划（不落库、不改配置）。
func (s *AdGuardService) WiringPreview(ctx context.Context) (*WiringPlan, error) {
	_ = ctx
	cur, err := s.collectDNSState()
	if err != nil {
		return nil, err
	}
	// 预检默认勾选：TProxy 启用则 Redirect；有 mihomo DNS 则 Resolve+Upstream
	opts := WiringOptions{
		RedirectTProxy:  cur.TProxyEnabled,
		ResolveConflict: true,
		PatchUpstream:   cur.MihomoDNSEnabled || cur.MihomoDNSPort > 0,
		WeakenTUNHijack: false,
	}
	plan, err := buildWiringPlan(opts, cur)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// WiringApply 先写 snapshot，再按 plan 执行；失败尽量自动回滚。
func (s *AdGuardService) WiringApply(ctx context.Context, opts WiringOptions) (*WiringPlan, error) {
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
		return nil, errors.New("DNS 已对接，请先解除对接再重新应用")
	}
	cur, err := s.collectDNSState()
	if err != nil {
		return nil, err
	}
	plan, err := buildWiringPlan(opts, cur)
	if err != nil {
		return nil, err
	}
	if len(plan.Actions) == 0 {
		return &plan, errors.New("没有可执行的对接动作，请检查勾选与当前状态")
	}

	// 先快照：失败时 rollback 只依赖这份 JSON
	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		return nil, err
	}
	if err := s.db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		return nil, fmt.Errorf("写入 wiring 快照失败: %w", err)
	}

	if err := s.applyWiringPlan(ctx, plan, cur); err != nil {
		// 尽力回滚到 apply 前；忽略二次错误，把原始失败抛给调用方
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		return &plan, err
	}

	if err := s.db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		return &plan, fmt.Errorf("标记 wiring=on 失败: %w", err)
	}
	// 记下 AGH 端口，供 dnsPortOverride 与重启后恢复
	_ = s.db.SetSetting(settingAdGuardDNSPort, strconv.Itoa(plan.AGHDNSPort))

	// 刷新 override：wiring 刚打开，TProxy 规则应指向 AGH
	s.refreshDNSPortOverride()

	if s.transp != nil {
		if err := s.transp.Resync(ctx); err != nil {
			s.logger.Errorf("wiring apply 后 Resync 失败，自动回滚: %v", err)
			// Resync 失败说明防火墙规则未跟上，对接处于半生效；
			// 与 applyWiringPlan 失败路径一致：回滚配置并清 wiring 标记。
			_ = s.rollbackWiringPlan(ctx, plan)
			_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
			_ = s.db.SetSetting(settingAdGuardSnapshot, "")
			s.clearDNSPortOverride()
			return &plan, fmt.Errorf("对接写入后 TProxy 规则同步失败，已回滚: %w", err)
		}
	}
	return &plan, nil
}

// WiringRollback 按快照恢复，并 Resync。
func (s *AdGuardService) WiringRollback(ctx context.Context) error {
	raw := s.getSetting(settingAdGuardSnapshot, "")
	if raw == "" && s.getSetting(settingAdGuardWiring, adguardWiringOff) != adguardWiringOn {
		return errors.New("当前未对接，无需回滚")
	}
	plan, err := unmarshalWiringSnapshot(raw)
	if err != nil {
		// 快照损坏时仍清掉 override 与 wiring 标记，避免卡在半对接
		s.clearDNSPortOverride()
		_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
		return err
	}
	if err := s.rollbackWiringPlan(ctx, plan); err != nil {
		return err
	}
	_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
	_ = s.db.SetSetting(settingAdGuardSnapshot, "")
	s.clearDNSPortOverride()

	if s.transp != nil {
		if err := s.transp.Resync(ctx); err != nil {
			return fmt.Errorf("回滚完成，但 TProxy 规则同步失败: %w", err)
		}
	}
	return nil
}

// ApplyWiring 是 WiringApply 的别名语义入口（计划已算好时由内部复用）。
// 保留导出名以符合任务描述；外部应优先走 WiringApply(opts)。
func (s *AdGuardService) ApplyWiring(ctx context.Context, plan WiringPlan, cur currentDNSState) error {
	return s.applyWiringPlan(ctx, plan, cur)
}

// Rollback 按 plan 恢复（不清理 settings 标记；WiringRollback 会清理）。
func (s *AdGuardService) Rollback(ctx context.Context, plan WiringPlan) error {
	return s.rollbackWiringPlan(ctx, plan)
}

// refreshDNSPortOverride 根据 settings 决定是否覆盖 TProxy DNS 目标。
// 启动时与 apply/rollback 后调用，保证进程内状态与 KV 一致。
//
// 仅当 wiring=on 且快照里 DidRedirect（勾选了 TProxy DNS→AGH）时覆盖；
// 只做上游补丁/冲突解决而不劫持 TProxy 的对接，不应改防火墙 DNS 目标。
func (s *AdGuardService) refreshDNSPortOverride() {
	if s.transp == nil {
		return
	}
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) != adguardWiringOn {
		s.transp.SetDNSPortOverride(nil)
		return
	}
	raw := s.getSetting(settingAdGuardSnapshot, "")
	if raw != "" {
		if plan, err := unmarshalWiringSnapshot(raw); err == nil && !plan.DidRedirect {
			s.transp.SetDNSPortOverride(nil)
			return
		}
	}
	s.transp.SetDNSPortOverride(func() int {
		if p, err := adguard.ReadDNSPort(s.workDir); err == nil && p > 0 {
			return p
		}
		return s.getSettingInt(settingAdGuardDNSPort, adguard.DefaultDNSPort)
	})
}

func (s *AdGuardService) clearDNSPortOverride() {
	if s.transp != nil {
		s.transp.SetDNSPortOverride(nil)
	}
}

// RestoreWiringOverrideOnBoot 供 ServiceContext 在启动时调用：
// 若上次退出时 wiring=on，恢复 dnsPortOverride，使 TProxy Resync 仍指向 AGH。
func (s *AdGuardService) RestoreWiringOverrideOnBoot() {
	s.refreshDNSPortOverride()
}

// ComponentEnabled 读取组件总开关；缺省或非法值视为 false。
// 真值：true / 1 / yes（大小写不敏感）。
func (s *AdGuardService) ComponentEnabled() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardComponent, "false"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// SetComponentEnabled 开关组件。
//
// enabled=true：仅写 settings，不自动安装/启动。
// enabled=false：强制 DNS 模式 0（含 wiring 回滚，失败则不写 false）；
// 再 Stop（失败则不写 false）；最后写 component_enabled=false。
func (s *AdGuardService) SetComponentEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		if s.db == nil {
			return errors.New("数据库未初始化")
		}
		if err := s.db.SetSetting(settingAdGuardComponent, "true"); err != nil {
			return err
		}
		s.reloadSchedule()
		return nil
	}

	// 关闭前：强制退出 DNS 模式（回滚劫持 + dns_mode=0）
	if err := s.SetDNSMode(ctx, DNSModeNone); err != nil {
		return fmt.Errorf("关闭组件前退出 DNS 模式失败: %w", err)
	}
	// 服务模式：先 disable 开机自启。Stop 只停进程保留 enable——
	// 若不 disable，机器重启后 systemd 仍会拉起 AGH，与「组件关」矛盾。
	if err := s.SetBootEnabled(ctx, false); err != nil {
		return fmt.Errorf("关闭组件时取消开机自启失败: %w", err)
	}
	if err := s.Stop(ctx); err != nil {
		return fmt.Errorf("关闭组件时停止 AdGuard 失败: %w", err)
	}
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	if err := s.db.SetSetting(settingAdGuardComponent, "false"); err != nil {
		return err
	}
	s.reloadSchedule()
	return nil
}

// Uninstall 彻底卸载 AdGuard Home。必须 confirm=true。
//
// 顺序：
//  1. wiring=on 时 WiringRollback（失败则中止，不删文件）
//  2. Stop
//  3. 服务模式注销系统服务（stop + disable + 删 unit）——必须在删二进制前
//  4. 删除二进制与 .bak
//  5. RemoveAll workDir
//  6. 清除 adguard.* settings
//  7. 强制 component_enabled=false
func (s *AdGuardService) Uninstall(ctx context.Context, confirm bool) error {
	if !confirm {
		return errors.New("请确认卸载（confirm=true）")
	}

	// 1. 优先保证 DNS 回滚成功再删文件
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
		if err := s.WiringRollback(ctx); err != nil {
			return fmt.Errorf("卸载前解除 DNS 对接失败: %w", err)
		}
	}

	// 2. Stop（未运行时 Manager.Stop 返回 nil）
	if err := s.Stop(ctx); err != nil {
		return fmt.Errorf("卸载时停止 AdGuard 失败: %w", err)
	}

	// 3. 服务模式：注销系统服务。disable 必须先于删二进制，
	// 否则开机按残留 enable 拉起已不存在的二进制。
	if c := s.serviceController(); c != nil {
		if err := c.Uninstall(ctx); err != nil {
			return fmt.Errorf("注销 AdGuard 系统服务失败: %w", err)
		}
	}

	// 4. 删除二进制与备份
	if s.updater != nil {
		bin := s.updater.AdGuardBinaryPath()
		if bin != "" {
			if err := os.Remove(bin); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("删除 AdGuard 二进制失败: %w", err)
			}
			if err := os.Remove(bin + ".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("删除 AdGuard 备份失败: %w", err)
			}
		}
	}

	// 5. 删除工作目录（配置、日志等）
	if s.workDir != "" {
		if err := os.RemoveAll(s.workDir); err != nil {
			return fmt.Errorf("删除 AdGuard 工作目录失败: %w", err)
		}
	}

	// 6. 清除 adguard.* settings
	if err := s.clearAdGuardSettings(); err != nil {
		return err
	}

	// 7. 强制关闭组件
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	return s.db.SetSetting(settingAdGuardComponent, "false")
}

// knownAdGuardSettingKeys 是卸载时要清除的已知键（含尚未落地的产品化键）。
// component_enabled 由 Uninstall 最后单独写 false，不在此清空为空串。
var knownAdGuardSettingKeys = []string{
	settingAdGuardBoot,
	settingAdGuardWebAddr,
	settingAdGuardDNSPort,
	settingAdGuardVersion,
	settingAdGuardWiring,
	settingAdGuardSnapshot,
	settingAdGuardUsername,
	settingAdGuardDNSMode,
	settingAdGuardAutoUpdate,
	settingAdGuardAutoUpdateCron,
	settingAdGuardCDNProviders,
	settingAdGuardSSOPasswordEnc,
	// 历史键：密码同步功能已移除，卸载时仍清掉残留
	"adguard.sync_password",
}

func (s *AdGuardService) clearAdGuardSettings() error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	seen := make(map[string]struct{}, len(knownAdGuardSettingKeys)+8)
	keys := make([]string, 0, len(knownAdGuardSettingKeys)+8)
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || k == settingAdGuardComponent {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for _, k := range knownAdGuardSettingKeys {
		add(k)
	}
	if m, err := s.db.GetSettings("adguard."); err == nil {
		for k := range m {
			add(k)
		}
	}
	for _, k := range keys {
		if err := s.db.SetSetting(k, ""); err != nil {
			return fmt.Errorf("清除设置 %s 失败: %w", k, err)
		}
	}
	return nil
}

// ShouldStartAtBoot 需组件开启、用户期望运行（enabled_at_boot）且二进制存在。
// 发版/面板重启后据此决定是否拉起 AdGuard，与用户上次点「启动/停止」对齐。
// 服务模式下恒 false：开机自启由 systemctl enable / rc-update 负责，
// 面板不再拉起，避免面板重启后与系统服务双重拉起竞争。
func (s *AdGuardService) ShouldStartAtBoot() bool {
	if s.mgr != nil && s.mgr.ServiceMode() {
		return false
	}
	if !s.ComponentEnabled() {
		return false
	}
	if !s.DesiredRunning() {
		return false
	}
	st := s.mgr.Status()
	return st.Installed
}

// DesiredRunning 读取用户期望的运行态（settings: adguard.enabled_at_boot）。
// 服务模式下以系统真实 enable 状态为准（settings 可能滞后，或用户
// 在 systemctl 侧直接改过）。
func (s *AdGuardService) DesiredRunning() bool {
	if s.mgr != nil && s.mgr.ServiceMode() {
		return s.mgr.ServiceEnabled(context.Background())
	}
	v := strings.TrimSpace(s.getSetting(settingAdGuardBoot, ""))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on") || v == "yes"
}

// setDesiredRunning 持久化期望运行态。失败只返回 error，由调用方记日志。
func (s *AdGuardService) setDesiredRunning(want bool) error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	val := "false"
	if want {
		val = "true"
	}
	return s.db.SetSetting(settingAdGuardBoot, val)
}

func (s *AdGuardService) collectDNSState() (currentDNSState, error) {
	cur := currentDNSState{}
	port, err := adguard.ReadDNSPort(s.workDir)
	if err != nil {
		return cur, fmt.Errorf("读取 AdGuard DNS 端口失败: %w", err)
	}
	cur.AGHDNSPort = port
	if cur.AGHDNSPort <= 0 {
		cur.AGHDNSPort = s.getSettingInt(settingAdGuardDNSPort, adguard.DefaultDNSPort)
	}

	if s.cfgSvc != nil {
		if listen, err := s.cfgSvc.BaseDNSListen(); err == nil {
			cur.MihomoDNSListen = listen
		}
		if p, err := s.cfgSvc.KernelDNSPort(); err == nil && p > 0 {
			cur.MihomoDNSPort = p
		} else if cur.MihomoDNSListen != "" {
			cur.MihomoDNSPort = parseListenPort(cur.MihomoDNSListen)
		}
		// dns.enable：从 base 粗判（最终配置更准，但 base 足够做预检默认勾选）
		if raw, err := s.cfgSvc.GetBaseConfig(); err == nil && strings.Contains(raw, "enable: true") {
			// 过于宽松，但预检只影响默认勾选；真正 patch 看端口是否可得
			cur.MihomoDNSEnabled = strings.Contains(raw, "dns:")
		}
		if cur.MihomoDNSPort > 0 {
			cur.MihomoDNSEnabled = true
		}
	}

	if s.transp != nil {
		st, _ := s.transp.Status()
		if st != nil {
			cur.TProxyEnabled = st.Enabled && st.Mode == "tproxy"
			cur.TUNEnabled = st.Enabled && st.Mode == "tun"
		}
		// 也认「面板托管的 TProxy 规则」——即使 pending 确认中
		if s.transp.TProxyManaged() {
			cur.TProxyEnabled = true
		}
	}
	return cur, nil
}

func (s *AdGuardService) applyWiringPlan(ctx context.Context, plan WiringPlan, cur currentDNSState) error {
	// B. 改 mihomo dns.listen（必须先于 upstream，因 upstream 可能指向新端口）
	if plan.DidResolveConflict && plan.MihomoDNSListen != "" {
		if s.cfgSvc == nil {
			return errors.New("ConfigService 未注入，无法改 dns.listen")
		}
		if err := s.cfgSvc.SetBaseDNSListen(plan.MihomoDNSListen); err != nil {
			return fmt.Errorf("改 mihomo dns.listen 失败: %w", err)
		}
		// 让最终配置跟上 base，否则 KernelDNSPort / 上游补丁仍看旧端口
		if _, err := s.cfgSvc.ApplyLocalOnly(ctx); err != nil {
			return fmt.Errorf("应用 dns.listen 变更失败: %w", err)
		}
	}

	// C. 补丁 AGH upstream
	if plan.DidPatchUpstream {
		upPort := cur.MihomoDNSPort
		if plan.DidResolveConflict {
			upPort = parseListenPort(plan.MihomoDNSListen)
		}
		upstreams := wiringUpstreamForPlan(plan, upPort)
		if len(upstreams) == 0 {
			return errors.New("无法确定 mihomo DNS 端口，上游补丁中止")
		}
		prev, err := adguard.PatchUpstreamDNS(s.workDir, upstreams)
		if err != nil {
			return fmt.Errorf("补丁 AdGuard 上游失败: %w", err)
		}
		// 把真实 previous 写回 snapshot，便于精确回滚
		plan.OriginalUpstream = prev
		if snap, err := marshalWiringSnapshot(plan); err == nil {
			_ = s.db.SetSetting(settingAdGuardSnapshot, snap)
		}
	}

	// D. TUN dns-hijack 弱化：清空 base 里 tun.dns-hijack（先快照原文）
	if plan.DidWeakenTUN {
		if s.cfgSvc == nil {
			return errors.New("ConfigService 未注入，无法弱化 TUN dns-hijack")
		}
		raw, err := s.cfgSvc.GetBaseConfig()
		if err != nil {
			return err
		}
		if len(plan.OriginalDNSHijack) == 0 {
			plan.OriginalDNSHijack = readDNSHijackFromBaseYAML(raw)
		}
		patched, err := patchBaseYAML(raw, "tun.dns-hijack", nil)
		if err != nil {
			return fmt.Errorf("清空 tun.dns-hijack 失败: %w", err)
		}
		if err := s.cfgSvc.UpdateBaseConfig(patched); err != nil {
			return err
		}
		if _, err := s.cfgSvc.ApplyLocalOnly(ctx); err != nil {
			return fmt.Errorf("应用 dns-hijack 变更失败: %w", err)
		}
		if snap, err := marshalWiringSnapshot(plan); err == nil {
			_ = s.db.SetSetting(settingAdGuardSnapshot, snap)
		}
	}

	// A. Redirect 只靠 settings + dnsPortOverride，无额外 IO
	_ = plan.DidRedirect
	return nil
}

func (s *AdGuardService) rollbackWiringPlan(ctx context.Context, plan WiringPlan) error {
	var errs []string

	// 无 TProxy 的 DNS 专用表优先拆除，避免回滚中途仍劫持 53
	if plan.DidDNSOnlyRedirect && s.dnsRedir != nil {
		if err := s.dnsRedir.TeardownDNSRedirect(ctx); err != nil {
			errs = append(errs, "拆除 aurora_agh_dns: "+err.Error())
		}
	}

	// 逆序：先 upstream / listen，最后清 override（由调用方做）
	if plan.DidPatchUpstream {
		if err := adguard.RestoreUpstreamDNS(s.workDir, plan.OriginalUpstream); err != nil {
			errs = append(errs, "恢复 AdGuard 上游: "+err.Error())
		}
	}
	if plan.DidResolveConflict && plan.OriginalMihomoListen != "" {
		if s.cfgSvc != nil {
			if err := s.cfgSvc.SetBaseDNSListen(plan.OriginalMihomoListen); err != nil {
				errs = append(errs, "恢复 mihomo dns.listen: "+err.Error())
			} else if _, err := s.cfgSvc.ApplyLocalOnly(ctx); err != nil {
				errs = append(errs, "应用 dns.listen 回滚: "+err.Error())
			}
		}
	}
	// 恢复 tun.dns-hijack（若快照里保存了原文，含空列表表示本来就是空）
	if plan.DidWeakenTUN && s.cfgSvc != nil {
		raw, err := s.cfgSvc.GetBaseConfig()
		if err != nil {
			errs = append(errs, "读 base 以恢复 dns-hijack: "+err.Error())
		} else {
			var val interface{}
			if len(plan.OriginalDNSHijack) > 0 {
				val = plan.OriginalDNSHijack
			} else {
				val = nil
			}
			patched, err := patchBaseYAML(raw, "tun.dns-hijack", val)
			if err != nil {
				errs = append(errs, "恢复 dns-hijack 补丁: "+err.Error())
			} else if err := s.cfgSvc.UpdateBaseConfig(patched); err != nil {
				errs = append(errs, "写回 dns-hijack: "+err.Error())
			} else if _, err := s.cfgSvc.ApplyLocalOnly(ctx); err != nil {
				errs = append(errs, "应用 dns-hijack 回滚: "+err.Error())
			}
		}
	}
	// DidBind53：退出模式 1 时不在此恢复 AGH 端口（由 enterDNSMode0/其它模式重写）

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (s *AdGuardService) getSetting(key, def string) string {
	if s.db == nil {
		return def
	}
	v, err := s.db.GetSetting(key)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return def
		}
		return def
	}
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func (s *AdGuardService) getSettingInt(key string, def int) int {
	v := s.getSetting(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// BinaryPresent 报告 AdGuard 二进制是否在磁盘上（供 boot 判断）。
func (s *AdGuardService) BinaryPresent() bool {
	if s.updater == nil {
		return s.mgr.Status().Installed
	}
	path := s.updater.AdGuardBinaryPath()
	if path == "" {
		return s.mgr.Status().Installed
	}
	_, err := os.Stat(path)
	return err == nil
}

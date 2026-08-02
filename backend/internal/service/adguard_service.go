package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"auroramihomo/backend/internal/adguard"
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
}

// AdGuardService 编排 AdGuard 安装/启停与 DNS 一键对接。
//
// 进程细节在 adguard.Manager；下载在 updater；本服务负责 settings、
// TransparentService 端口覆盖、ConfigService base dns.listen 与快照回滚。
type AdGuardService struct {
	db      *repository.Database
	updater *updater.Manager
	mgr     *adguard.Manager
	transp  *TransparentService
	cfgSvc  *ConfigService
	workDir string
	webAddr string
	logger  logx.Logger
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
		EntryPath:        "/adguard/",
		Wiring:           adguardWiringOff,
		WiringLabel:      "未对接",
	}
	if dto.WorkDir == "" {
		dto.WorkDir = s.workDir
	}
	if dto.WebAddr == "" {
		dto.WebAddr = s.webAddr
	}
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
			dnsPort = 1053
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
func (s *AdGuardService) Install(ctx context.Context) error {
	if s.updater == nil {
		return errors.New("更新器未初始化，无法安装 AdGuard Home")
	}
	if err := s.updater.UpdateAdGuard(ctx); err != nil {
		return err
	}
	// 记录版本：CheckLatest 的 local 侧之后会用 settings；此处尽量写入 tag
	_, _, agh := s.updater.CheckLatest(ctx, "", "")
	if agh.LatestVersion != "" {
		_ = s.db.SetSetting(settingAdGuardVersion, agh.LatestVersion)
	}
	// 首次安装确保 bind 回环，避免 AGH 默认暴露到局域网
	if err := adguard.EnsureBindLocalhost(s.workDir); err != nil {
		s.logger.Errorf("确保 AdGuard bind_host=127.0.0.1 失败: %v", err)
	}
	if port, err := adguard.ReadDNSPort(s.workDir); err == nil && port > 0 {
		_ = s.db.SetSetting(settingAdGuardDNSPort, strconv.Itoa(port))
	}
	return nil
}

// Start 拉起 AdGuard 子进程。
func (s *AdGuardService) Start(ctx context.Context) error {
	if err := adguard.EnsureBindLocalhost(s.workDir); err != nil {
		s.logger.Errorf("启动前确保 bind_host 失败: %v", err)
	}
	return s.mgr.Start(ctx)
}

// Stop 停止 AdGuard 子进程。
func (s *AdGuardService) Stop(ctx context.Context) error {
	return s.mgr.Stop(ctx)
}

// Restart 重启 AdGuard 子进程。
func (s *AdGuardService) Restart(ctx context.Context) error {
	return s.mgr.Restart(ctx)
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
		return s.getSettingInt(settingAdGuardDNSPort, 1053)
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
		return s.db.SetSetting(settingAdGuardComponent, "true")
	}

	// 关闭前：强制退出 DNS 模式（回滚劫持 + dns_mode=0）
	if err := s.SetDNSMode(ctx, DNSModeNone); err != nil {
		return fmt.Errorf("关闭组件前退出 DNS 模式失败: %w", err)
	}
	if err := s.Stop(ctx); err != nil {
		return fmt.Errorf("关闭组件时停止 AdGuard 失败: %w", err)
	}
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	return s.db.SetSetting(settingAdGuardComponent, "false")
}

// Uninstall 彻底卸载 AdGuard Home。必须 confirm=true。
//
// 顺序：
//  1. wiring=on 时 WiringRollback（失败则中止，不删文件）
//  2. Stop
//  3. 删除二进制与 .bak
//  4. RemoveAll workDir
//  5. 清除 adguard.* settings
//  6. 强制 component_enabled=false
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

	// 3. 删除二进制与备份
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

	// 4. 删除工作目录（配置、日志等）
	if s.workDir != "" {
		if err := os.RemoveAll(s.workDir); err != nil {
			return fmt.Errorf("删除 AdGuard 工作目录失败: %w", err)
		}
	}

	// 5. 清除 adguard.* settings
	if err := s.clearAdGuardSettings(); err != nil {
		return err
	}

	// 6. 强制关闭组件
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
	"adguard.sync_password",
	"adguard.username",
	settingAdGuardDNSMode,
	"adguard.auto_update",
	"adguard.cdn_providers",
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

// ShouldStartAtBoot 需组件开启、enabled_at_boot 且二进制存在。
func (s *AdGuardService) ShouldStartAtBoot() bool {
	if !s.ComponentEnabled() {
		return false
	}
	v := strings.TrimSpace(s.getSetting(settingAdGuardBoot, ""))
	if v != "1" && !strings.EqualFold(v, "true") && v != "on" {
		return false
	}
	st := s.mgr.Status()
	return st.Installed
}

func (s *AdGuardService) collectDNSState() (currentDNSState, error) {
	cur := currentDNSState{}
	port, err := adguard.ReadDNSPort(s.workDir)
	if err != nil {
		return cur, fmt.Errorf("读取 AdGuard DNS 端口失败: %w", err)
	}
	cur.AGHDNSPort = port
	if cur.AGHDNSPort <= 0 {
		cur.AGHDNSPort = s.getSettingInt(settingAdGuardDNSPort, 1053)
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

	// D. TUN dns-hijack 弱化：清空 base 里 tun.dns-hijack
	if plan.DidWeakenTUN {
		if s.cfgSvc == nil {
			return errors.New("ConfigService 未注入，无法弱化 TUN dns-hijack")
		}
		raw, err := s.cfgSvc.GetBaseConfig()
		if err != nil {
			return err
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
	}

	// A. Redirect 只靠 settings + dnsPortOverride，无额外 IO
	_ = plan.DidRedirect
	return nil
}

func (s *AdGuardService) rollbackWiringPlan(ctx context.Context, plan WiringPlan) error {
	var errs []string

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
	// DidWeakenTUN：P1 不自动恢复原 dns-hijack 列表（快照未存原文）；
	// 只记日志，避免用空列表覆盖用户事后手改。
	if plan.DidWeakenTUN {
		s.logger.Infof("wiring 回滚：曾弱化 TUN dns-hijack，请按需在配置中心手动恢复")
	}

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

package service

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/adguard"
)

// 一键「入口 DNS」预设：TUN / TProxy 通用。
// AGH 听 53，解析经 1053 交给 mihomo。
//
// bootstrap / fallback 禁止填裸 8.8.8.8、1.1.1.1：国内 UDP 会被污染
// （AAAA 常见 2001::1）。mihomo 短暂无应答时 AGH 若回落到这些地址，
// 污染会重新进客户端缓存。bootstrap 只用国内纯 IP；fallback 只回 mihomo。
var (
	entryPresetAGHUpstream  = []string{"127.0.0.1:1053"}
	entryPresetAGHFallback  = []string{"127.0.0.1:1053"}
	entryPresetAGHBootstrap = []string{"223.5.5.5", "119.29.29.29"}
	entryPresetMihomoListen = "0.0.0.0:1053"
)

// ApplyEntryDNSPreset 一键写入入口 DNS 方案（TUN/TProxy 通用）：
//
//	AdGuard: port=53；upstream/fallback=127.0.0.1:1053；
//	  bootstrap=223.5.5.5,119.29.29.29（国内纯 IP，不用境外裸 DNS）
//	mihomo:  dns.enable=true；dns.listen=0.0.0.0:1053
//	并写入与 mode1 相同的 wiring 快照，便于 mode0/卸载回滚。
//
// 同时尽量清空 tun.dns-hijack，避免 TUN 把 53 抢走。
// 53 被其它进程占用时失败；AdGuard 自身占用视为可继续。
func (s *AdGuardService) ApplyEntryDNSPreset(ctx context.Context) error {
	if s.workDir == "" {
		return fmt.Errorf("AdGuard 工作目录未配置")
	}
	if s.cfgSvc == nil {
		return fmt.Errorf("配置服务未初始化，无法改写 mihomo dns.listen")
	}
	if s.db == nil {
		return fmt.Errorf("数据库未初始化")
	}

	// 1) 校验 53 可用
	aghRunning := s.mgr != nil && s.mgr.Status().Running
	curPort := 0
	if p, err := adguard.ReadDNSPort(s.workDir); err == nil {
		curPort = p
	}
	if p := s.getSettingInt(settingAdGuardDNSPort, 0); p > 0 {
		curPort = p
	}
	if _, _, err := adguard.CheckDNSPortAvailability(53, aghRunning, curPort); err != nil {
		return err
	}

	// 先退出模式 2 / 旧 wiring，避免劫持与 bind53 叠加
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn ||
		strings.TrimSpace(s.getSetting(settingAdGuardSnapshot, "")) != "" {
		if err := s.WiringRollback(ctx); err != nil {
			return fmt.Errorf("应用入口方案前解除对接失败: %w", err)
		}
	}

	// 2) mihomo DNS 必须可用：enable + 固定 0.0.0.0:1053（LAN 可连）
	if err := s.cfgSvc.SetBaseDNSEnable(true); err != nil {
		return fmt.Errorf("开启 mihomo dns.enable 失败: %w", err)
	}
	if err := s.cfgSvc.SetBaseDNSListen(entryPresetMihomoListen); err != nil {
		return fmt.Errorf("设置 mihomo dns.listen=%s 失败: %w", entryPresetMihomoListen, err)
	}
	if _, err := s.cfgSvc.ApplyLocalOnly(ctx); err != nil {
		return fmt.Errorf("应用 mihomo DNS 变更失败: %w", err)
	}

	cur, err := s.collectDNSState()
	if err != nil {
		return err
	}

	plan := WiringPlan{
		Actions: []string{
			"AdGuard 监听 :53（客户端入口 DNS，完整日志与拦截）",
			"确保 mihomo dns.enable=true、dns.listen=0.0.0.0:1053",
			"AdGuard 上游/后备 → 127.0.0.1:1053；Bootstrap → 国内纯 IP",
		},
		AGHDNSPort: 53,
		// 保存进入入口模式前 AGH 的端口，退出时据此恢复。
		OriginalDNSPort:      cur.AGHDNSPort,
		OriginalMihomoListen: cur.MihomoDNSListen,
		OriginalUpstream:     append([]string(nil), cur.OriginalUpstream...),
		MihomoDNSListen:      entryPresetMihomoListen,
		WiringOn:             true,
		DidBind53:            true,
		DidResolveConflict:   true, // listen 已由上面强制写入；标记便于回滚 OriginalMihomoListen
		DidPatchUpstream:     true,
	}

	// 3) 清空 tun.dns-hijack
	if raw, err := s.cfgSvc.GetBaseConfig(); err == nil {
		hijack := readDNSHijackFromBaseYAML(raw)
		if len(hijack) > 0 {
			plan.DidWeakenTUN = true
			plan.OriginalDNSHijack = hijack
			plan.Actions = append(plan.Actions,
				"清空 tun.dns-hijack（避免 TUN 抢走 53，保证客户端 DNS 先到 AdGuard）")
		}
	}

	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		return fmt.Errorf("写入入口方案快照失败: %w", err)
	}

	// 4) 弱化 TUN（listen 已写，跳过 DidResolveConflict 再写 listen——但 apply 会对 DidResolveConflict 再 SetBaseDNSListen，幂等 OK）
	//    上游由下面 PatchDNSResolvers 完整写入，这里先只做 TUN 弱化，避免 apply 里 PatchUpstream 与完整 resolvers 打架。
	//    简化：复用 applyWiringPlan，再覆盖完整 resolvers。
	if err := s.applyWiringPlan(ctx, plan, cur); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
		return err
	}

	// 5) AGH：端口 + 完整 resolvers（upstream/fallback/bootstrap）
	if err := adguard.SetDNSPort(s.workDir, 53); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		return fmt.Errorf("写入 AdGuard dns.port=53 失败: %w", err)
	}
	if err := adguard.PatchDNSResolvers(s.workDir, entryPresetAGHUpstream, entryPresetAGHFallback, entryPresetAGHBootstrap); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		return fmt.Errorf("写入 AdGuard 上游/后备/Bootstrap 失败: %w", err)
	}

	_ = s.db.SetSetting(settingAdGuardDNSPort, "53")
	if err := s.db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		return err
	}
	if err := s.db.SetSetting(settingAdGuardDNSMode, "1"); err != nil {
		return fmt.Errorf("写入 dns_mode 失败: %w", err)
	}

	// 6) 运行中则重启使监听与 resolvers 生效
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("入口方案已写入，但重启 AdGuard 失败: %w", err)
		}
	}
	return nil
}

// EntryDNSPresetSummary 返回方案说明（API 成功文案 / 前端展示用）。
func EntryDNSPresetSummary() string {
	return strings.Join([]string{
		"AdGuard DNS :53",
		"上游 127.0.0.1:1053",
		"后备 127.0.0.1:1053",
		"Bootstrap 223.5.5.5 / 119.29.29.29",
		"mihomo dns.enable=true、dns.listen 0.0.0.0:1053",
	}, "；")
}

// hardenAGHResolversForMihomo 把 AGH upstream/fallback 指到本机 mihomo，bootstrap 用国内纯 IP。
// mode1 与入口预设共用，避免只改 upstream 时残留裸 8.8.8.8 fallback。
func hardenAGHResolversForMihomo(workDir string, mihomoPort int) error {
	if mihomoPort <= 0 {
		mihomoPort = 1053
	}
	up := fmt.Sprintf("127.0.0.1:%d", mihomoPort)
	return adguard.PatchDNSResolvers(workDir,
		[]string{up},
		[]string{up},
		[]string{"223.5.5.5", "119.29.29.29"},
	)
}

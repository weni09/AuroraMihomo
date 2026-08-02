package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/netcheck"
)

// DNSMode 是 AdGuard DNS 服务模式。
//
//	0 未托管：不劫持，AGH 可用高位端口
//	1 绑定 53：AGH 直接听 UDP/TCP :53
//	2 重定向：TProxy/规则把 53 转到 AGH 高位端口（复用 wiring）
type DNSMode int

const (
	DNSModeNone     DNSMode = 0
	DNSModeBind53   DNSMode = 1
	DNSModeRedirect DNSMode = 2
)

// 可替换探测函数，单测可 mock 53 口占用情况。
var udpPortInUseFn = adguard.UDPPortInUse

// DNSMode 读取当前 DNS 服务模式；缺省 "0"。
// 迁移：旧版仅有 dns_wiring=on 且未写 dns_mode 时视为模式 2。
func (s *AdGuardService) DNSMode() DNSMode {
	v := strings.TrimSpace(s.getSetting(settingAdGuardDNSMode, "0"))
	switch v {
	case "1":
		return DNSModeBind53
	case "2":
		return DNSModeRedirect
	case "0", "":
		if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
			return DNSModeRedirect
		}
		return DNSModeNone
	default:
		if n, err := strconv.Atoi(v); err == nil {
			switch DNSMode(n) {
			case DNSModeBind53:
				return DNSModeBind53
			case DNSModeRedirect:
				return DNSModeRedirect
			case DNSModeNone:
				if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
					return DNSModeRedirect
				}
				return DNSModeNone
			}
		}
		if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
			return DNSModeRedirect
		}
		return DNSModeNone
	}
}

// SetDNSMode 切换 DNS 服务模式 0/1/2。
//
//	0：若 wiring=on 或当前为模式 2，先 WiringRollback；写 dns_mode=0；清 override
//	1：预检 UDP 53；占用则中文错误；yaml 写 dns.port=53；dns_mode=1；运行中则 Restart
//	2：WiringApply(Redirect+ResolveConflict+PatchUpstream)；dns_mode=2
func (s *AdGuardService) SetDNSMode(ctx context.Context, mode DNSMode) error {
	if mode < DNSModeNone || mode > DNSModeRedirect {
		return fmt.Errorf("无效的 DNS 模式: %d（支持 0/1/2）", int(mode))
	}
	if s.db == nil {
		return fmt.Errorf("数据库未初始化")
	}

	switch mode {
	case DNSModeNone:
		return s.enterDNSMode0(ctx)
	case DNSModeBind53:
		return s.enterDNSMode1(ctx)
	case DNSModeRedirect:
		return s.enterDNSMode2(ctx)
	default:
		return fmt.Errorf("无效的 DNS 模式: %d", int(mode))
	}
}

func (s *AdGuardService) enterDNSMode0(ctx context.Context) error {
	wiringOn := s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn
	hasSnapshot := strings.TrimSpace(s.getSetting(settingAdGuardSnapshot, "")) != ""
	// 模式 2 / 旧 wiring：必须先回滚劫持与 listen/upstream
	if wiringOn || hasSnapshot {
		if err := s.WiringRollback(ctx); err != nil {
			return fmt.Errorf("退出 DNS 模式时解除对接失败: %w", err)
		}
	} else {
		s.clearDNSPortOverride()
		// 即使无 snapshot，也尽力拆掉可能残留的仅 DNS 表
		if s.dnsRedir != nil {
			_ = s.dnsRedir.TeardownDNSRedirect(ctx)
		}
	}
	return s.db.SetSetting(settingAdGuardDNSMode, "0")
}

func (s *AdGuardService) enterDNSMode1(ctx context.Context) error {
	// 先退出模式 2 / 旧 wiring，避免劫持与 bind53 叠加
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn ||
		strings.TrimSpace(s.getSetting(settingAdGuardSnapshot, "")) != "" {
		if err := s.WiringRollback(ctx); err != nil {
			return fmt.Errorf("切换到入口模式前解除对接失败: %w", err)
		}
	}

	if udpPortInUseFn(53) {
		return fmt.Errorf("UDP 53 端口已被占用，无法将 AdGuard 绑定到 53。" +
			"请检查 systemd-resolved、dnsmasq 或其他 DNS 服务是否占用该端口，" +
			"在系统上让路（例如禁用/停止占用进程）后重试")
	}

	// —— 入口模式（不是防火墙把 AGH 转到 1053）——
	// AdGuard 听 53；放行查询由 AGH「上游 DNS」转发到 mihomo 高位端口（如 1053）。
	// 同时清空 tun.dns-hijack，避免 TUN 把 53 抢进 mihomo 内部。

	cur, err := s.collectDNSState()
	if err != nil {
		return err
	}

	plan := WiringPlan{
		Actions:              make([]string, 0, 6),
		AGHDNSPort:           53,
		OriginalDNSPort:      cur.MihomoDNSPort,
		OriginalMihomoListen: cur.MihomoDNSListen,
		OriginalUpstream:     append([]string(nil), cur.OriginalUpstream...),
		WiringOn:             true,
		DidBind53:            true,
	}

	// 1) mihomo 必须提供高位 DNS 口给 AGH 当上游
	mihomoPort := cur.MihomoDNSPort
	mihomoListen := cur.MihomoDNSListen
	if mihomoPort <= 0 {
		mihomoPort = parseListenPort(mihomoListen)
	}
	if mihomoPort <= 0 || mihomoPort == 53 {
		// 未配置或误占 53：落到 127.0.0.1:1053
		plan.DidResolveConflict = true
		plan.MihomoDNSListen = "127.0.0.1:1053"
		plan.Actions = append(plan.Actions, "确保 mihomo dns.listen = 127.0.0.1:1053（供 AdGuard 上游）")
		mihomoPort = 1053
	} else {
		plan.Actions = append(plan.Actions, fmt.Sprintf("mihomo DNS 上游目标 :%d", mihomoPort))
	}

	// 2) AGH 上游 → 127.0.0.1:mihomoPort（配置级「转发」，非 nft 重定向）
	plan.DidPatchUpstream = true
	plan.Actions = append(plan.Actions,
		fmt.Sprintf("AdGuard 上游 DNS → 127.0.0.1:%d（mihomo；入口仍为 :53）", mihomoPort))

	// 3) 清空 tun.dns-hijack，否则 TUN 下查询进 mihomo 内部、AGH 当不了入口
	if s.cfgSvc != nil {
		if raw, err := s.cfgSvc.GetBaseConfig(); err == nil {
			hijack := readDNSHijackFromBaseYAML(raw)
			if len(hijack) > 0 {
				plan.DidWeakenTUN = true
				plan.OriginalDNSHijack = hijack
				plan.Actions = append(plan.Actions,
					"清空 tun.dns-hijack（避免 TUN 抢走 53，保证客户端 DNS 先到 AdGuard）")
			}
		}
	}

	plan.Actions = append([]string{
		"AdGuard 监听 :53（客户端入口 DNS，完整日志与拦截）",
	}, plan.Actions...)

	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		return fmt.Errorf("写入入口模式快照失败: %w", err)
	}

	// 执行：mihomo listen → AGH upstream → 清 hijack
	if err := s.applyWiringPlan(ctx, plan, cur); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
		return err
	}

	// AGH 绑 53（在 upstream 补丁之后，避免中途端口错乱）
	if err := adguard.SetDNSPort(s.workDir, 53); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		return fmt.Errorf("写入 AdGuard dns.port=53 失败: %w", err)
	}
	_ = s.db.SetSetting(settingAdGuardDNSPort, "53")

	if err := s.db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		return err
	}
	if err := s.db.SetSetting(settingAdGuardDNSMode, "1"); err != nil {
		return fmt.Errorf("写入 dns_mode 失败: %w", err)
	}

	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("入口模式已写入，但重启 AdGuard 失败: %w", err)
		}
	}
	return nil
}

func (s *AdGuardService) enterDNSMode2(ctx context.Context) error {
	// 已对接：仅对齐 dns_mode 标记（幂等）
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
		return s.db.SetSetting(settingAdGuardDNSMode, "2")
	}

	// 重定向目标必须是 AGH 高位端口（≠53）。未配置则用 1053。
	port := s.resolveRedirectDNSPort()
	if err := s.ensureAGHListenPort(ctx, port); err != nil {
		return err
	}

	cur, err := s.collectDNSState()
	if err != nil {
		return err
	}
	cur.AGHDNSPort = port

	if cur.TProxyEnabled {
		opts := WiringOptions{
			RedirectTProxy:  true,
			ResolveConflict: true,
			PatchUpstream:   true,
			WeakenTUNHijack: false,
		}
		if _, err := s.WiringApply(ctx, opts); err != nil {
			return err
		}
		return s.db.SetSetting(settingAdGuardDNSMode, "2")
	}

	// 无 TProxy：独立 DNS 重定向表（仍要求先启动 AGH）
	return s.enterDNSMode2WithoutTProxy(ctx, port, cur)
}

// resolveRedirectDNSPort 模式 2 用的 AGH 监听端口：settings > yaml > 1053；禁止 53。
func (s *AdGuardService) resolveRedirectDNSPort() int {
	if p := s.getSettingInt(settingAdGuardDNSPort, 0); p > 0 && p != 53 {
		return p
	}
	if p, err := adguard.ReadDNSPort(s.workDir); err == nil && p > 0 && p != 53 {
		return p
	}
	return 1053
}

// ensureAGHListenPort 把 AGH dns.port 写成指定高位端口，运行中则重启使监听生效。
func (s *AdGuardService) ensureAGHListenPort(ctx context.Context, port int) error {
	if port <= 0 || port == 53 {
		return fmt.Errorf("重定向模式要求 AdGuard DNS 使用高位端口（如 1053），不能是 %d", port)
	}
	if err := adguard.SetDNSPort(s.workDir, port); err != nil {
		return fmt.Errorf("写入 AdGuard dns.port=%d 失败: %w", port, err)
	}
	if s.db != nil {
		_ = s.db.SetSetting(settingAdGuardDNSPort, strconv.Itoa(port))
	}
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("已写入 dns.port=%d，但重启 AdGuard 失败（重定向前需要进程监听该端口）: %w", port, err)
		}
	}
	return nil
}

// enterDNSMode2WithoutTProxy 在未启用 TProxy 时用 aurora_agh_dns 劫持 53。
func (s *AdGuardService) enterDNSMode2WithoutTProxy(ctx context.Context, port int, cur currentDNSState) error {
	if s.dnsRedir == nil {
		return fmt.Errorf("当前环境无法下发 DNS 重定向规则（需要 Linux + nft）。请启用 TProxy 后重试，或改用「使用 53 端口」模式")
	}
	// 真实环境下 ApplyDNSRedirect 会探测 UDP 端口是否有监听；
	// 此处不强制 Status.Running，以便单测注入假 applier，并允许「先下发规则、监听稍后就绪」的竞态由 applier 拦截。

	plan, err := buildWiringPlan(WiringOptions{
		RedirectTProxy:  false,
		ResolveConflict: true,
		PatchUpstream:   true,
		WeakenTUNHijack: false,
	}, cur)
	if err != nil {
		return err
	}
	plan.DidDNSOnlyRedirect = true
	plan.DidRedirect = false
	plan.AGHDNSPort = port
	plan.Actions = append([]string{
		fmt.Sprintf("无 TProxy：nft 表 aurora_agh_dns 将 53 重定向到 AdGuard :%d", port),
	}, plan.Actions...)

	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		return fmt.Errorf("写入快照失败: %w", err)
	}

	if err := s.applyWiringPlan(ctx, plan, cur); err != nil {
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		return err
	}

	if err := s.dnsRedir.ApplyDNSRedirect(ctx, netcheck.DNSRedirectParams{
		DNSPort:    port,
		EnableIPv6: true,
	}); err != nil {
		_ = s.rollbackWiringPlan(ctx, plan)
		_ = s.db.SetSetting(settingAdGuardSnapshot, "")
		_ = s.db.SetSetting(settingAdGuardWiring, adguardWiringOff)
		return err
	}

	if err := s.db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		_ = s.dnsRedir.TeardownDNSRedirect(ctx)
		_ = s.rollbackWiringPlan(ctx, plan)
		return err
	}
	_ = s.db.SetSetting(settingAdGuardDNSPort, strconv.Itoa(port))
	return s.db.SetSetting(settingAdGuardDNSMode, "2")
}

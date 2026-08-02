package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"auroramihomo/backend/internal/adguard"
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
	}
	return s.db.SetSetting(settingAdGuardDNSMode, "0")
}

func (s *AdGuardService) enterDNSMode1(ctx context.Context) error {
	// 先退出模式 2 / wiring，避免劫持与 bind53 叠加
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn ||
		strings.TrimSpace(s.getSetting(settingAdGuardSnapshot, "")) != "" {
		if err := s.WiringRollback(ctx); err != nil {
			return fmt.Errorf("切换到 53 端口前解除对接失败: %w", err)
		}
	}

	if udpPortInUseFn(53) {
		return fmt.Errorf("UDP 53 端口已被占用，无法将 AdGuard 绑定到 53。" +
			"请检查 systemd-resolved、dnsmasq 或其他 DNS 服务是否占用该端口，" +
			"在系统上让路（例如禁用/停止占用进程）后重试")
	}

	if err := adguard.SetDNSPort(s.workDir, 53); err != nil {
		return fmt.Errorf("写入 AdGuard dns.port=53 失败: %w", err)
	}
	_ = s.db.SetSetting(settingAdGuardDNSPort, "53")

	if err := s.db.SetSetting(settingAdGuardDNSMode, "1"); err != nil {
		return fmt.Errorf("写入 dns_mode 失败: %w", err)
	}

	// 运行中需重启以应用新端口
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("已写入 dns.port=53，但重启 AdGuard 失败: %w", err)
		}
	}
	return nil
}

func (s *AdGuardService) enterDNSMode2(ctx context.Context) error {
	// 已对接：仅对齐 dns_mode 标记（幂等）
	if s.getSetting(settingAdGuardWiring, adguardWiringOff) == adguardWiringOn {
		return s.db.SetSetting(settingAdGuardDNSMode, "2")
	}

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

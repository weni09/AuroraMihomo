package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/updater"
)

// AdminUsername 返回 settings 中的 AGH 用户名；空则尝试读 yaml；再空则 "admin"。
func (s *AdGuardService) AdminUsername() string {
	if u := strings.TrimSpace(s.getSetting(settingAdGuardUsername, "")); u != "" {
		return u
	}
	if name, err := adguard.ReadUsername(s.workDir); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "admin"
}

// SetCredentials 写入 AGH yaml 管理员口令（bcrypt），并持久化用户名。
// 不在 SQLite 存密码明文。若进程在跑则 Restart 使 users 生效。
func (s *AdGuardService) SetCredentials(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if password == "" {
		return errors.New("密码不能为空")
	}
	if err := adguard.SetUserPassword(s.workDir, username, password); err != nil {
		return err
	}
	if s.db != nil {
		if err := s.db.SetSetting(settingAdGuardUsername, username); err != nil {
			return fmt.Errorf("保存用户名失败: %w", err)
		}
	}
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("账号已写入，但重启失败（新口令需重启后生效）: %w", err)
		}
	}
	return nil
}

// SetWebPort 只改端口，保留现有监听 host（以 yaml 为准）；若进程在跑则 Restart。
func (s *AdGuardService) SetWebPort(ctx context.Context, port int) error {
	host, _, err := adguard.ReadWebListen(s.workDir)
	if err != nil || strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	// 注意：不以 settings 为准——yaml 是监听地址的唯一事实来源
	// （用户可能直接改过 yaml 而 settings 未同步，反代/SSO 读的都是 yaml）。
	return s.SetWebListen(ctx, host, port)
}

// SetWebListen 设置 Web 管理监听地址（host + port）。
//
// host 空 → 127.0.0.1（安全默认）；允许 0.0.0.0 / 具体网卡 IP。
// 服务化后 AGH 可独立于面板对外提供管理面，不再强制回环。
// 若进程在跑则 Restart 使新监听生效。
func (s *AdGuardService) SetWebListen(ctx context.Context, host string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("web 端口无效: %d（须为 1-65535）", port)
	}
	norm, err := adguard.NormalizeWebHost(host)
	if err != nil {
		return err
	}
	if err := adguard.SetWebListen(s.workDir, norm, port); err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", norm, port)
	s.webAddr = addr
	if s.mgr != nil {
		// Manager 回显 / 探活用完整地址；反代上游另经 LocalProxyUpstream 归一
		s.mgr.SetWebAddr(addr)
	}
	if s.db != nil {
		if err := s.db.SetSetting(settingAdGuardWebAddr, addr); err != nil {
			return fmt.Errorf("保存 web_addr 失败: %w", err)
		}
	}
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("监听地址已写入，但重启失败: %w", err)
		}
	}
	return nil
}

// SetDNSListenPort 设置 AdGuard DNS 监听端口（含 53）。
//
// 占用校验：空闲或 AdGuard 自身占用 → 成功；其它进程占用 → 失败。
// 写入 yaml 后若进程在跑则重启以应用监听。
func (s *AdGuardService) SetDNSListenPort(ctx context.Context, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("DNS 端口无效: %d（须为 1-65535）", port)
	}
	aghRunning := s.mgr != nil && s.mgr.Status().Running
	curPort := 0
	if p, err := adguard.ReadDNSPort(s.workDir); err == nil {
		curPort = p
	}
	if p := s.getSettingInt(settingAdGuardDNSPort, 0); p > 0 {
		curPort = p
	}
	av, owner, err := adguard.CheckDNSPortAvailability(port, aghRunning, curPort)
	if err != nil {
		return err
	}
	_ = av
	_ = owner

	if err := adguard.SetDNSPort(s.workDir, port); err != nil {
		return fmt.Errorf("写入 AdGuard dns.port=%d 失败: %w", port, err)
	}
	if s.db != nil {
		_ = s.db.SetSetting(settingAdGuardDNSPort, fmt.Sprintf("%d", port))
	}
	// 与「入口 53」相关的 dns_mode 标记：53 → 1，其它高位 → 保持/置 0 不强制改模式业务
	if s.db != nil {
		if port == 53 {
			_ = s.db.SetSetting(settingAdGuardDNSMode, "1")
		} else if s.getSetting(settingAdGuardDNSMode, "0") == "1" {
			_ = s.db.SetSetting(settingAdGuardDNSMode, "0")
		}
	}
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("端口已写入，但重启 AdGuard 失败: %w", err)
		}
	}
	return nil
}

// CDNProviders 读取 adguard.cdn_providers JSON 数组；损坏或空则返回 nil。
func (s *AdGuardService) CDNProviders() []string {
	raw := strings.TrimSpace(s.getSetting(settingAdGuardCDNProviders, ""))
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		s.logger.Errorf("解析 adguard.cdn_providers 失败: %v", err)
		return nil
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetCDNProviders 持久化 AdGuard 专用升级镜像列表（按序回落）；空/nil 表示用全局 CDN。
func (s *AdGuardService) SetCDNProviders(providers []string) error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	clean := make([]string, 0, len(providers))
	for _, p := range providers {
		p = strings.TrimSpace(p)
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		if err := s.db.SetSetting(settingAdGuardCDNProviders, ""); err != nil {
			return err
		}
		s.applyAdGuardCDNToUpdater()
		return nil
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(settingAdGuardCDNProviders, string(b)); err != nil {
		return err
	}
	s.applyAdGuardCDNToUpdater()
	return nil
}

// AutoUpdateEnabled 是否启用 AdGuard 独立自动更新。
func (s *AdGuardService) AutoUpdateEnabled() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardAutoUpdate, ""))
	return v == "1" || strings.EqualFold(v, "true") || v == "on"
}

// AutoUpdateCron 返回 AdGuard 自动更新 cron（已规范化 6 段）；空则默认。
func (s *AdGuardService) AutoUpdateCron() string {
	raw := strings.TrimSpace(s.getSetting(settingAdGuardAutoUpdateCron, ""))
	if raw == "" {
		return defaultAdGuardAutoUpdateCron
	}
	if norm, err := NormalizeCron(raw); err == nil && norm != "" {
		return norm
	}
	return defaultAdGuardAutoUpdateCron
}

// ShouldRunAutoUpdate 组件启用且开关打开时才真正调度。
func (s *AdGuardService) ShouldRunAutoUpdate() bool {
	return s != nil && s.ComponentEnabled() && s.AutoUpdateEnabled()
}

// SetAutoUpdateSettings 写入开关与/或 cron，并触发调度重装。
// enabled 为 nil 表示不改开关；cron 空串表示不改表达式。
func (s *AdGuardService) SetAutoUpdateSettings(enabled *bool, cronExpr string) error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	if cronExpr = strings.TrimSpace(cronExpr); cronExpr != "" {
		norm, err := NormalizeCron(cronExpr)
		if err != nil {
			return err
		}
		if err := s.db.SetSetting(settingAdGuardAutoUpdateCron, norm); err != nil {
			return fmt.Errorf("保存自动更新 cron 失败: %w", err)
		}
	}
	if enabled != nil {
		val := "false"
		if *enabled {
			val = "true"
		}
		if err := s.db.SetSetting(settingAdGuardAutoUpdate, val); err != nil {
			return fmt.Errorf("保存自动更新开关失败: %w", err)
		}
	}
	s.reloadSchedule()
	return nil
}

// SetAutoUpdate 仅写开关（兼容旧调用），并重装调度。
func (s *AdGuardService) SetAutoUpdate(enabled bool) error {
	return s.SetAutoUpdateSettings(&enabled, "")
}

// CheckUpdateOnly 只检查 AdGuard Home 是否有新版本（不查 mihomo/zashboard）。
func (s *AdGuardService) CheckUpdateOnly(ctx context.Context) (updater.ComponentCheck, error) {
	if s.updater == nil {
		return updater.ComponentCheck{}, errors.New("更新器未初始化")
	}
	local := strings.TrimSpace(s.getSetting(settingAdGuardVersion, ""))
	if local == "" && s.mgr != nil {
		local = strings.TrimSpace(s.mgr.Status().Version)
	}
	_, _, agh := s.updater.CheckLatest(ctx, "", local)
	return agh, nil
}

// applyAdGuardCDNToUpdater 把专用列表推给 updater；空列表清除覆盖以回落全局。
func (s *AdGuardService) applyAdGuardCDNToUpdater() {
	if s.updater == nil {
		return
	}
	s.updater.SetAdGuardCDNProviders(s.CDNProviders())
}

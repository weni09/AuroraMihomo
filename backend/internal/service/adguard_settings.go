package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/adguard"
)

// PasswordSyncEnabled 是否在 Aurora 改密后自动同步到 AGH。
func (s *AdGuardService) PasswordSyncEnabled() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardSyncPassword, ""))
	return v == "1" || strings.EqualFold(v, "true") || v == "on"
}

// SetPasswordSync 写入 adguard.sync_password。
func (s *AdGuardService) SetPasswordSync(ctx context.Context, enabled bool) error {
	_ = ctx
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	return s.db.SetSetting(settingAdGuardSyncPassword, val)
}

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

// SyncPasswordFromAurora 在 sync 开启时，用 Aurora 新明文口令更新 AGH。
// sync 关闭则 no-op（返回 nil）。用户名取 settings 或默认 admin。
func (s *AdGuardService) SyncPasswordFromAurora(ctx context.Context, plainPassword string) error {
	if !s.PasswordSyncEnabled() {
		return nil
	}
	if strings.TrimSpace(plainPassword) == "" {
		return errors.New("同步密码为空")
	}
	return s.SetCredentials(ctx, s.AdminUsername(), plainPassword)
}

// SetWebPort 校验端口、写 AGH yaml（强制 127.0.0.1）、落库 web_addr；
// 若进程在跑则 Restart 使新端口生效。
func (s *AdGuardService) SetWebPort(ctx context.Context, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("web 端口无效: %d（须为 1-65535）", port)
	}
	if err := adguard.SetWebPort(s.workDir, port); err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	s.webAddr = addr
	if s.mgr != nil {
		s.mgr.SetWebAddr(addr)
	}
	if s.db != nil {
		if err := s.db.SetSetting(settingAdGuardWebAddr, addr); err != nil {
			return fmt.Errorf("保存 web_addr 失败: %w", err)
		}
	}
	if s.mgr != nil && s.mgr.Status().Running {
		if err := s.Restart(ctx); err != nil {
			return fmt.Errorf("端口已写入，但重启失败: %w", err)
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

// AutoUpdateEnabled 是否参加系统自动更新（与全局 cron 同一套）。
func (s *AdGuardService) AutoUpdateEnabled() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardAutoUpdate, ""))
	return v == "1" || strings.EqualFold(v, "true") || v == "on"
}

// SetAutoUpdate 写入 adguard.auto_update。
func (s *AdGuardService) SetAutoUpdate(enabled bool) error {
	if s.db == nil {
		return errors.New("数据库未初始化")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	return s.db.SetSetting(settingAdGuardAutoUpdate, val)
}

// applyAdGuardCDNToUpdater 把专用列表推给 updater；空列表清除覆盖以回落全局。
func (s *AdGuardService) applyAdGuardCDNToUpdater() {
	if s.updater == nil {
		return
	}
	s.updater.SetAdGuardCDNProviders(s.CDNProviders())
}

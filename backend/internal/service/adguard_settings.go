package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/adguard"
)

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

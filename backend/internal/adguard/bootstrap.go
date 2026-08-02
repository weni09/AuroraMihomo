package adguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// schemaVersionForEmbedded 与当前嵌入使用的 AGH 稳定版（v0.107.x）对齐。
// 过低会触发迁移，过高可能拒绝；29 在 0.107 系列可迁移到最新。
const schemaVersionForEmbedded = 29

// EnsureBootstrapConfig 保证 work-dir 内有一份「已完成安装」的可用配置，
// 避免 AGH 一直停在 install.html 向导（真机：残缺 yaml 会被当成首次启动）。
//
// 若已有 schema_version 与 users，仅补齐 http/dns 关键字段，不覆盖用户密码。
func EnsureBootstrapConfig(workDir, webAddr string, dnsPort int, adminUser, adminPass string) error {
	if workDir == "" {
		return fmt.Errorf("work dir empty")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(workDir, "data"), 0o700)

	if webAddr == "" {
		webAddr = fmt.Sprintf("%s:%d", localhostBind, defaultWebPort)
	}
	webAddr = strings.TrimPrefix(strings.TrimPrefix(webAddr, "http://"), "https://")
	if dnsPort <= 0 {
		dnsPort = defaultDNSPort
	}
	if adminUser == "" {
		adminUser = "admin"
	}

	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}

	needFull := false
	if _, ok := m["schema_version"]; !ok {
		needFull = true
	}
	users := m["users"]
	if users == nil {
		needFull = true
	}

	if needFull {
		// 保留已有 users 密码哈希，避免每次引导覆盖
		existingUsers := m["users"]
		hash := ""
		if existingUsers == nil {
			if adminPass != "" && !strings.HasPrefix(adminPass, "$2") {
				b, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
				if err != nil {
					return fmt.Errorf("bcrypt: %w", err)
				}
				hash = string(b)
			} else if strings.HasPrefix(adminPass, "$2") {
				hash = adminPass
			} else {
				b, err := bcrypt.GenerateFromPassword([]byte("AuroraChangeMe"), bcrypt.DefaultCost)
				if err != nil {
					return err
				}
				hash = string(b)
			}
			existingUsers = []any{
				map[string]any{"name": adminUser, "password": hash},
			}
		}
		m = map[string]any{
			"schema_version": schemaVersionForEmbedded,
			"users":          existingUsers,
			"http": map[string]any{
				"address":     webAddr,
				"session_ttl": "720h",
			},
			"dns": map[string]any{
				"bind_hosts":               []any{"0.0.0.0"},
				"port":                     dnsPort,
				"upstream_mode":            "load_balance",
				"upstream_dns":             []any{defaultMihomoUpstream, "https://dns.alidns.com/dns-query"},
				"bootstrap_dns":            []any{"223.5.5.5", "1.1.1.1"},
				"protection_enabled":       true,
				"ratelimit":                20,
				"upstream_timeout":         "10s",
				"cache_size":               4194304,
				"cache_optimistic":         true,
				"use_private_ptr_resolvers": true,
			},
			"querylog": map[string]any{
				"enabled": true, "file_enabled": true, "interval": "2160h", "size_memory": 1000,
			},
			"statistics": map[string]any{"enabled": true, "interval": "24h"},
			"filtering":  map[string]any{"protection_enabled": true, "filters_update_interval": 24},
			"filters": []any{
				map[string]any{
					"enabled": true,
					"url":     "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt",
					"name":    "AdGuard DNS filter",
					"id":      1,
				},
			},
			"clients": map[string]any{
				"runtime_sources": map[string]any{
					"whois": true, "arp": true, "rdns": true, "dhcp": true, "hosts": true,
				},
			},
			"log": map[string]any{"enabled": true, "file": ""},
		}
		return saveConfigMap(workDir, m)
	}

	// 已有完整安装：只保证 web 回环与 dns 段存在
	httpM := asMap(m["http"])
	if httpM == nil {
		httpM = map[string]any{}
	}
	httpM["address"] = webAddr
	m["http"] = httpM
	delete(m, "bind_host") // 废弃字段易干扰

	dnsM := asMap(m["dns"])
	if dnsM == nil {
		dnsM = map[string]any{}
	}
	if _, ok := dnsM["bind_hosts"]; !ok {
		dnsM["bind_hosts"] = []any{"0.0.0.0"}
	}
	if _, ok := dnsM["port"]; !ok {
		dnsM["port"] = dnsPort
	}
	if _, ok := dnsM["upstream_dns"]; !ok {
		dnsM["upstream_dns"] = []any{defaultMihomoUpstream}
	}
	if _, ok := dnsM["bootstrap_dns"]; !ok {
		dnsM["bootstrap_dns"] = []any{"223.5.5.5", "1.1.1.1"}
	}
	m["dns"] = dnsM
	if _, ok := m["schema_version"]; !ok {
		m["schema_version"] = schemaVersionForEmbedded
	}
	return saveConfigMap(workDir, m)
}

// IsBootstrapComplete 判断配置是否足以跳过官方 install 向导。
func IsBootstrapComplete(workDir string) bool {
	m, missing, err := loadConfigMap(workDir)
	if err != nil || missing {
		return false
	}
	if _, ok := m["schema_version"]; !ok {
		return false
	}
	users, ok := m["users"]
	if !ok || users == nil {
		return false
	}
	switch u := users.(type) {
	case []any:
		return len(u) > 0
	case []map[string]any:
		return len(u) > 0
	default:
		return false
	}
}

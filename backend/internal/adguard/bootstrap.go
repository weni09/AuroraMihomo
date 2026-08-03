package adguard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// schemaVersionForEmbedded 与当前嵌入使用的 AGH 稳定版（v0.107.x）对齐。
// 过低会触发迁移，过高可能拒绝；29 在 0.107 系列可迁移到最新。
const schemaVersionForEmbedded = 29

// publicDNSPollutionRisk 国内直连易被污染的公共 DNS，不得出现在 AGH bootstrap/fallback。
var publicDNSPollutionRisk = map[string]struct{}{
	"8.8.8.8": {}, "8.8.4.4": {},
	"1.1.1.1": {}, "1.0.0.1": {},
	"9.9.9.9": {},
}

// EnsureBootstrapConfig 保证 work-dir 内有一份「已完成安装」的可用配置，
// 避免 AGH 一直停在 install.html 向导（真机：残缺 yaml 会被当成首次启动）。
//
// 若已有 schema_version 与 users，仅补齐 http/dns 关键字段，不覆盖用户密码。
//
// 返回值 initialPassword：仅在「首次完整引导且调用方未传入密码」时非空，
// 为随机生成的管理员明文口令，调用方应 Persist 到 SSO 并写入 initial 文件供用户查看。
// 已有用户或调用方自带密码时返回空串。
func EnsureBootstrapConfig(workDir, webAddr string, dnsPort int, adminUser, adminPass string) (initialPassword string, err error) {
	if workDir == "" {
		return "", fmt.Errorf("work dir empty")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
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
		return "", err
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
		generated := ""
		if existingUsers == nil {
			plain := strings.TrimSpace(adminPass)
			if plain == "" {
				// 禁止固定 AuroraChangeMe：首次安装生成随机口令
				p, gerr := generateRandomPassword(16)
				if gerr != nil {
					return "", gerr
				}
				plain = p
				generated = p
			} else if strings.HasPrefix(plain, "$2") {
				// 调用方误传 bcrypt 串：直接当哈希用，不再生成
				hash = plain
				plain = ""
			}
			if hash == "" {
				b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
				if err != nil {
					return "", fmt.Errorf("bcrypt: %w", err)
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
				"bind_hosts":    []any{"0.0.0.0"},
				"port":          dnsPort,
				"upstream_mode": "load_balance",
				"upstream_dns":  []any{defaultMihomoUpstream},
				// bootstrap 只用国内纯 IP：裸 1.1.1.1/8.8.8.8 在国内会被 UDP 污染
				"bootstrap_dns":             []any{"223.5.5.5", "119.29.29.29"},
				"fallback_dns":              []any{defaultMihomoUpstream},
				"protection_enabled":        true,
				"ratelimit":                 20,
				"upstream_timeout":          "10s",
				"cache_size":                4194304,
				"cache_optimistic":          true,
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
		if err := saveConfigMap(workDir, m); err != nil {
			return "", err
		}
		if generated != "" {
			_ = writeInitialAdminPasswordFile(workDir, adminUser, generated)
		}
		return generated, nil
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
		dnsM["bootstrap_dns"] = []any{"223.5.5.5", "119.29.29.29"}
	}
	m["dns"] = dnsM
	if _, ok := m["schema_version"]; !ok {
		m["schema_version"] = schemaVersionForEmbedded
	}
	if err := saveConfigMap(workDir, m); err != nil {
		return "", err
	}
	// 存量配置：清洗 bootstrap/fallback 里易污染的公共 DNS
	_ = SanitizePollutionProneDNS(workDir)
	return "", nil
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

// SanitizePollutionProneDNS 去掉 AGH bootstrap_dns / fallback_dns 中易被国内污染的裸公共 DNS。
// 不改 upstream_dns（入口方案会指向 mihomo）。启动与引导补齐路径都会调用。
func SanitizePollutionProneDNS(workDir string) error {
	m, missing, err := loadConfigMap(workDir)
	if err != nil || missing {
		return err
	}
	dns := asMap(m["dns"])
	if dns == nil {
		return nil
	}
	changed := false
	if cleaned, n := scrubPublicDNSList(asStringList(dns["bootstrap_dns"]), []string{"223.5.5.5", "119.29.29.29"}); n > 0 {
		dns["bootstrap_dns"] = toAnyList(cleaned)
		changed = true
	}
	if cleaned, n := scrubPublicDNSList(asStringList(dns["fallback_dns"]), []string{defaultMihomoUpstream}); n > 0 {
		dns["fallback_dns"] = toAnyList(cleaned)
		changed = true
	}
	if !changed {
		return nil
	}
	m["dns"] = dns
	return saveConfigMap(workDir, m)
}

func scrubPublicDNSList(in, fallbackIfEmpty []string) (out []string, removed int) {
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// 只匹配纯 IP 形式；带 # 或 https:// 的用户自定义上游保留
		host := s
		if i := strings.IndexAny(s, "/#:"); i >= 0 && !strings.Contains(s, "://") {
			// "8.8.8.8:53" → 取 host；"https://" 整段保留
			if strings.Count(s, ":") == 1 && !strings.Contains(s, "/") {
				host = s[:strings.Index(s, ":")]
			}
		}
		if _, bad := publicDNSPollutionRisk[host]; bad {
			removed++
			continue
		}
		// 也拦 "8.8.8.8" 完全相等
		if _, bad := publicDNSPollutionRisk[s]; bad {
			removed++
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 && removed > 0 {
		out = append(out, fallbackIfEmpty...)
	}
	return out, removed
}

func toAnyList(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func generateRandomPassword(nbytes int) (string, error) {
	if nbytes < 8 {
		nbytes = 8
	}
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	// hex 可读、无 shell 特殊字符；16 字节 → 32 字符
	return hex.EncodeToString(b), nil
}

func writeInitialAdminPasswordFile(workDir, user, pass string) error {
	path := filepath.Join(workDir, "initial_admin_password.txt")
	content := fmt.Sprintf("AdGuard Home 首次引导自动生成的管理员口令（请登录面板 AdGuard 设置中保存账号以启用免密，并尽快修改）。\n用户: %s\n密码: %s\n", user, pass)
	return os.WriteFile(path, []byte(content), 0o600)
}

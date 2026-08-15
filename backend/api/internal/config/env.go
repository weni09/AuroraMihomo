package config

import (
	"os"
	"strconv"
	"strings"
)

// ApplyEnvOverrides 允许用环境变量覆盖配置文件中的关键项。
// 容器部署时敏感信息不应写进镜像里的 yaml，需要能从外部注入。
// 环境变量优先级高于配置文件。
func (c *Config) ApplyEnvOverrides() {
	if v := env("AURORA_JWT_SECRET"); v != "" {
		c.Auth.AccessSecret = v
	}
	if v := env("AURORA_JWT_EXPIRE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.Auth.AccessExpire = n
		}
	}
	if v := env("AURORA_DATA_SOURCE"); v != "" {
		c.DataSource = v
	}
	if v := env("AURORA_CONFIG_DIR"); v != "" {
		c.Mihomo.ConfigDir = v
	}
	if v := env("AURORA_MIHOMO_BINARY"); v != "" {
		c.Mihomo.BinaryPath = v
	}
	if v := env("AURORA_HOST"); v != "" {
		c.Host = v
	}
	if v := env("AURORA_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Port = n
		}
	}
	if v := env("AURORA_AUTO_UPDATE"); v != "" {
		c.AutoUpdate.Enabled = isTruthy(v)
	}
	if v := env("AURORA_AUTO_UPDATE_CRON"); v != "" {
		c.AutoUpdate.Cron = v
	}
	if v := env("AURORA_GITHUB_API"); v != "" {
		c.Updater.GitHubAPI = v
	}
	// 逗号分隔的 CDN 列表，便于在内网环境替换为私有镜像
	if out := csvEnv("AURORA_CDN_PROVIDERS"); len(out) > 0 {
		c.Updater.CDNProviders = out
	}
	// 逗号分隔的 raw 加速源列表（raw.githubusercontent.com 内容）
	if out := csvEnv("AURORA_RAW_CDN_PROVIDERS"); len(out) > 0 {
		c.Updater.RawCDNProviders = out
	}
	if v := env("AURORA_USE_MIHOMO_PROXY"); v != "" {
		c.Updater.UseMihomoProxy = isTruthy(v)
	}
}

// csvEnv 读取逗号分隔的环境变量，去空白并丢弃空项。
func csvEnv(key string) []string {
	v := env(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

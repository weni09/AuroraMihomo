package adguard

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	aghConfigFile    = "AdGuardHome.yaml"
	defaultDNSPort   = 1053
	defaultWebPort   = 80
	localhostBind    = "127.0.0.1"
)

// configPath 返回 work-dir 下的 AdGuardHome.yaml 路径。
func configPath(workDir string) string {
	return filepath.Join(workDir, aghConfigFile)
}

// loadConfigMap 读取 AGH yaml 为 map。文件不存在时返回 empty map 与 notExist=true。
func loadConfigMap(workDir string) (m map[string]any, notExist bool, err error) {
	path := configPath(workDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, true, nil
		}
		return nil, false, fmt.Errorf("read AdGuardHome.yaml: %w", err)
	}
	m = map[string]any{}
	if len(raw) == 0 {
		return m, false, nil
	}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, false, fmt.Errorf("parse AdGuardHome.yaml: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, false, nil
}

// saveConfigMap 将 map 写回 AdGuardHome.yaml。
func saveConfigMap(workDir string, m map[string]any) error {
	if workDir != "" {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return fmt.Errorf("create work dir: %w", err)
		}
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal AdGuardHome.yaml: %w", err)
	}
	path := configPath(workDir)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write AdGuardHome.yaml: %w", err)
	}
	return nil
}

func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	// yaml.v3 偶尔会给出 map[interface{}]interface{}（旧路径），兼容一下
	if m, ok := v.(map[any]any); ok {
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				continue
			}
			out[ks] = val
		}
		return out
	}
	return nil
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func asStringList(v any) []string {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		out := make([]string, len(s))
		copy(out, s)
		return out
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			} else {
				out = append(out, fmt.Sprint(item))
			}
		}
		return out
	default:
		return nil
	}
}

// ReadDNSPort 读取 dns.port；文件或键缺失时默认 1053。
func ReadDNSPort(workDir string) (int, error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return 0, err
	}
	dns := asMap(m["dns"])
	if dns == nil {
		return defaultDNSPort, nil
	}
	if p, ok := asInt(dns["port"]); ok && p > 0 {
		return p, nil
	}
	return defaultDNSPort, nil
}

// ReadWebPort 读取 http.port；文件或键缺失时默认 80。
func ReadWebPort(workDir string) (int, error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return 0, err
	}
	httpSec := asMap(m["http"])
	if httpSec == nil {
		return defaultWebPort, nil
	}
	if p, ok := asInt(httpSec["port"]); ok && p > 0 {
		return p, nil
	}
	return defaultWebPort, nil
}

// EnsureBindLocalhost 将顶层 bind_host 设为 127.0.0.1，保留其它键。
// 文件不存在时创建仅含 bind_host 的最小配置。
func EnsureBindLocalhost(workDir string) error {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	m["bind_host"] = localhostBind
	return saveConfigMap(workDir, m)
}

// PatchUpstreamDNS 仅改写 dns.upstream_dns，保留其它键。
// 返回改写前的 upstream 列表（可能为 nil/empty），供回滚。
func PatchUpstreamDNS(workDir string, upstreams []string) (previous []string, err error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return nil, err
	}
	dns := asMap(m["dns"])
	if dns == nil {
		dns = map[string]any{}
	}
	previous = asStringList(dns["upstream_dns"])
	// 写入时用 []any 更贴近 yaml 列表节点形态，避免某些消费者挑剔类型
	list := make([]any, len(upstreams))
	for i, u := range upstreams {
		list[i] = u
	}
	dns["upstream_dns"] = list
	m["dns"] = dns
	if err := saveConfigMap(workDir, m); err != nil {
		return nil, err
	}
	return previous, nil
}

// RestoreUpstreamDNS 将 dns.upstream_dns 恢复为 previous。
// previous 为 nil 时写空列表（与「原先无 upstream」对齐）。
func RestoreUpstreamDNS(workDir string, previous []string) error {
	if previous == nil {
		previous = []string{}
	}
	_, err := PatchUpstreamDNS(workDir, previous)
	return err
}

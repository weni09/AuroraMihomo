package adguard

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	aghConfigFile  = "AdGuardHome.yaml"
	defaultDNSPort = 1053
	// defaultWebPort 与面板默认反代上游 127.0.0.1:3000 对齐；
	// AGH 官方默认 80，但嵌入场景用 3000 避免与宿主机其它 Web 冲突。
	defaultWebPort = 3000
	localhostBind  = "127.0.0.1"
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

// ReadWebPort 读取 Web 监听端口。
// 优先解析现代 AGH 的 http.address（如 "127.0.0.1:3000"），
// 其次 http.port；均缺失时默认 3000。
func ReadWebPort(workDir string) (int, error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return 0, err
	}
	httpSec := asMap(m["http"])
	if httpSec == nil {
		return defaultWebPort, nil
	}
	if addr, ok := httpSec["address"].(string); ok {
		if p := portFromAddress(addr); p > 0 {
			return p, nil
		}
	}
	if p, ok := asInt(httpSec["port"]); ok && p > 0 {
		return p, nil
	}
	return defaultWebPort, nil
}

// portFromAddress 从 "host:port" / ":port" / "port" 中抽出端口。
func portFromAddress(addr string) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 0
	}
	// net.SplitHostPort 要求带括号的 IPv6；纯数字则直接当端口
	if p, err := strconv.Atoi(addr); err == nil && p > 0 && p <= 65535 {
		return p
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// 可能是 "0.0.0.0" 无端口
		_ = host
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return 0
	}
	return p
}

// EnsureBindLocalhost 将顶层 bind_host 与 http.address 固定到回环。
//
// - bind_host: 127.0.0.1
// - http.address: 127.0.0.1:<port>，port 优先取现有 address / http.port，否则 3000
//
// 文件不存在时创建最小安全配置。启动前必须调用，防止 AGH 默认 0.0.0.0 暴露。
func EnsureBindLocalhost(workDir string) error {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	m["bind_host"] = localhostBind

	httpSec := asMap(m["http"])
	if httpSec == nil {
		httpSec = map[string]any{}
	}
	port := defaultWebPort
	if addr, ok := httpSec["address"].(string); ok {
		if p := portFromAddress(addr); p > 0 {
			port = p
		}
	} else if p, ok := asInt(httpSec["port"]); ok && p > 0 {
		port = p
	}
	httpSec["address"] = fmt.Sprintf("%s:%d", localhostBind, port)
	m["http"] = httpSec

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

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
	aghConfigFile = "AdGuardHome.yaml"
	// DefaultDNSPort 是 AGH 自身默认监听端口（可被 service 层引用）。
	// 勿用 1053：与 mihomo 默认 dns.listen 冲突，
	// 真机表现为 starting dns server: bind address already in use 后进程退出。
	DefaultDNSPort = 5353
	defaultDNSPort = DefaultDNSPort
	// defaultWebPort 与面板默认反代上游 127.0.0.1:3000 对齐。
	defaultWebPort = 3000
	localhostBind  = "127.0.0.1"
	// defaultMihomoUpstream 嵌入场景下 AGH 默认把解析转给本机 mihomo DNS。
	defaultMihomoUpstream = "127.0.0.1:1053"
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

// ReadDNSPort 读取 dns.port；文件或键缺失时默认 defaultDNSPort(5353)。
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

// SetDNSPort 写入 dns.port；文件不存在时创建含 dns 段的最小配置。
func SetDNSPort(workDir string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("无效的 DNS 端口: %d", port)
	}
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	dns := asMap(m["dns"])
	if dns == nil {
		dns = map[string]any{}
	}
	dns["port"] = port
	m["dns"] = dns
	return saveConfigMap(workDir, m)
}

// ReadWebPort 读取 Web 监听端口。
// 优先解析现代 AGH 的 http.address（如 "127.0.0.1:3000"），
// 其次 http.port；均缺失时默认 3000。
func ReadWebPort(workDir string) (int, error) {
	_, port, err := ReadWebListen(workDir)
	return port, err
}

// ReadWebListen 读取 Web 监听 host+port。
// host 缺省为 127.0.0.1；port 缺省为 3000。
func ReadWebListen(workDir string) (host string, port int, err error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return "", 0, err
	}
	httpSec := asMap(m["http"])
	if httpSec == nil {
		return localhostBind, defaultWebPort, nil
	}
	if addr, ok := httpSec["address"].(string); ok {
		h, p := splitHostPort(addr)
		if p > 0 {
			if h == "" {
				h = localhostBind
			}
			return h, p, nil
		}
		if h != "" {
			// 仅 host、无端口
			if p2, ok := asInt(httpSec["port"]); ok && p2 > 0 {
				return h, p2, nil
			}
			return h, defaultWebPort, nil
		}
	}
	port = defaultWebPort
	if p, ok := asInt(httpSec["port"]); ok && p > 0 {
		port = p
	}
	// 兼容旧 bind_host
	if bh, ok := m["bind_host"].(string); ok && strings.TrimSpace(bh) != "" {
		return strings.TrimSpace(bh), port, nil
	}
	return localhostBind, port, nil
}

// portFromAddress 从 "host:port" / ":port" / "port" 中抽出端口。
func portFromAddress(addr string) int {
	_, p := splitHostPort(addr)
	return p
}

// splitHostPort 解析 "host:port" / "[::1]:port" / ":port" / "port"。
// host 可能为空（仅端口时）。
func splitHostPort(addr string) (host string, port int) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0
	}
	if p, err := strconv.Atoi(addr); err == nil && p > 0 && p <= 65535 {
		return "", p
	}
	h, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		// 可能是纯 host（无端口）
		if ip := net.ParseIP(addr); ip != nil {
			return addr, 0
		}
		// 宽松：看起来像 host 名则当 host
		if !strings.Contains(addr, ":") {
			return addr, 0
		}
		return "", 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return h, 0
	}
	return h, p
}

// NormalizeWebHost 规范化 Web 监听地址主机部分。
// 空串 → 127.0.0.1（安全默认）；允许 0.0.0.0 / :: / 回环 / 单播 IP。
// IPv6 返回规范带括号形式（"[::]"/"[::1]"），供 SetWebListen 拼 "host:port"。
// 拒绝空格、路径字符与明显非法值。
func NormalizeWebHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return localhostBind, nil
	}
	// 去掉误传的 scheme / 路径
	host = strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	if i := strings.IndexAny(host, "/#?"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return localhostBind, nil
	}
	// 剥掉可能误传的 IPv6 括号，统一校验与输出
	bracketed := false
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		bracketed = true
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	// 若误带端口，只取 host
	if h, p := splitHostPort(host); p > 0 && h != "" {
		host = h
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			bracketed = true
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
	}
	switch strings.ToLower(host) {
	case "localhost":
		return localhostBind, nil
	case "*", "any":
		return "0.0.0.0", nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("Web 监听地址无效: %q（请用 127.0.0.1、0.0.0.0 或具体 IP）", host)
	}
	_ = bracketed
	if ip.To4() == nil {
		// IPv6 字面量必须带括号，否则 "::3000" 是非法地址
		return "[" + host + "]", nil
	}
	return host, nil
}

// LocalProxyUpstream 根据 AGH 对外监听地址，返回面板反代应连接的本机上游。
// 0.0.0.0/::/空 → 127.0.0.1:port；其余 host 原样（绑定单网卡 IP 时只能连该 IP）。
func LocalProxyUpstream(host string, port int) string {
	if port <= 0 {
		port = defaultWebPort
	}
	host = strings.TrimSpace(host)
	// 剥 IPv6 括号再判断通配
	bare := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" || bare == "0.0.0.0" || bare == "::" {
		host = localhostBind
	}
	// IPv6 字面量加括号（可能来自 ReadWebListen 已剥括号的 host）
	if ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")); ip != nil && ip.To4() == nil {
		return fmt.Sprintf("[%s]:%d", strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// EnsureWebListenPresent 保证 http.address 存在；已有配置不改 host。
//
// 与旧 EnsureBindLocalhost 的区别：不再每次启动把监听打回 127.0.0.1——
// 服务化后用户可把 Web 绑到 0.0.0.0/局域网 IP，启动路径必须保留该选择。
// 仅在完全缺失 address 时写入安全默认 127.0.0.1:3000。
func EnsureWebListenPresent(workDir string) error {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	httpSec := asMap(m["http"])
	if httpSec == nil {
		httpSec = map[string]any{}
	}
	if addr, ok := httpSec["address"].(string); ok && strings.TrimSpace(addr) != "" {
		// 已有监听：同步废弃 bind_host 与 address 的 host 一致（不改用户 host）
		h, p := splitHostPort(addr)
		if h != "" {
			m["bind_host"] = h
		}
		if p <= 0 {
			if p2, ok := asInt(httpSec["port"]); ok && p2 > 0 {
				p = p2
			} else {
				p = defaultWebPort
			}
			if h == "" {
				h = localhostBind
			}
			httpSec["address"] = fmt.Sprintf("%s:%d", h, p)
			m["http"] = httpSec
		}
		return saveConfigMap(workDir, m)
	}
	port := defaultWebPort
	if p, ok := asInt(httpSec["port"]); ok && p > 0 {
		port = p
	}
	host := localhostBind
	if bh, ok := m["bind_host"].(string); ok && strings.TrimSpace(bh) != "" {
		if h, err := NormalizeWebHost(bh); err == nil {
			host = h
		}
	}
	httpSec["address"] = fmt.Sprintf("%s:%d", host, port)
	m["http"] = httpSec
	m["bind_host"] = host
	return saveConfigMap(workDir, m)
}

// EnsureBindLocalhost 历史名：现仅保证 http.address 存在，不再强制回环。
// 保留导出名以免外部调用方编译失败；新代码请用 EnsureWebListenPresent。
func EnsureBindLocalhost(workDir string) error {
	return EnsureWebListenPresent(workDir)
}

// SetWebListen 写入 Web 管理监听 host:port（服务化后可绑 0.0.0.0 / 局域网 IP）。
// host 空则 127.0.0.1。同步旧字段 bind_host。
func SetWebListen(workDir, host string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("web 端口无效: %d（须为 1-65535）", port)
	}
	host, err := NormalizeWebHost(host)
	if err != nil {
		return err
	}
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	m["bind_host"] = host
	httpSec := asMap(m["http"])
	if httpSec == nil {
		httpSec = map[string]any{}
	}
	httpSec["address"] = fmt.Sprintf("%s:%d", host, port)
	m["http"] = httpSec
	return saveConfigMap(workDir, m)
}

// SetWebPort 只改端口，保留现有 host（无配置时默认 127.0.0.1）。
func SetWebPort(workDir string, port int) error {
	host, _, err := ReadWebListen(workDir)
	if err != nil {
		host = localhostBind
	}
	return SetWebListen(workDir, host, port)
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

// PatchDNSResolvers 一次写入 AGH 上游 / 后备 / Bootstrap 列表。
// 任一切片为 nil 表示不改该字段；空切片表示清空。
func PatchDNSResolvers(workDir string, upstream, fallback, bootstrap []string) error {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	dns := asMap(m["dns"])
	if dns == nil {
		dns = map[string]any{}
	}
	toAny := func(in []string) []any {
		out := make([]any, len(in))
		for i, u := range in {
			out[i] = u
		}
		return out
	}
	if upstream != nil {
		dns["upstream_dns"] = toAny(upstream)
	}
	if fallback != nil {
		dns["fallback_dns"] = toAny(fallback)
	}
	if bootstrap != nil {
		dns["bootstrap_dns"] = toAny(bootstrap)
	}
	m["dns"] = dns
	return saveConfigMap(workDir, m)
}

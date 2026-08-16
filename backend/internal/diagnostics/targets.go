// Package diagnostics 提供网络诊断能力。
//
// 本文件实现预设目标：DefaultTargets 返回固定清单（GitHub API/raw、
// 公共 DNS 及其 TCP 端口），可选追加代理连通性探测目标。
package diagnostics

import (
	"net/url"
	"strconv"
)

// DefaultProxyPort 是代理目标未显式声明端口时的默认端口（mihomo 常见 HTTP 代理端口）。
const DefaultProxyPort = 7890

// DefaultTargets 返回预设诊断目标列表：GitHub API/raw 与公共 DNS。
//
// proxyURLFn 非 nil 时，取其返回值解析出代理主机与端口（端口缺省用
// DefaultProxyPort），追加一个 TCP 代理连通性探测目标；解析失败或缺少
// 主机名时静默跳过，不影响预设清单。
func DefaultTargets(proxyURLFn func() string) []DiagnosticTarget {
	targets := []DiagnosticTarget{
		{Type: TypeDNS, Target: "api.github.com"},
		{Type: TypeTCP, Target: "api.github.com", Port: 443},
		{Type: TypeHTTP, Target: "https://api.github.com/"},
		{Type: TypeTCP, Target: "raw.githubusercontent.com", Port: 443},
		{Type: TypeHTTP, Target: "https://raw.githubusercontent.com/"},
		{Type: TypeDNS, Target: "1.1.1.1"},
		{Type: TypeTCP, Target: "1.1.1.1", Port: 53},
		{Type: TypeDNS, Target: "8.8.8.8"},
		{Type: TypeTCP, Target: "8.8.8.8", Port: 53},
		{Type: TypeDNS, Target: "223.5.5.5"},
		{Type: TypeTCP, Target: "223.5.5.5", Port: 53},
	}
	if proxyURLFn == nil {
		return targets
	}
	if t, ok := proxyTarget(proxyURLFn()); ok {
		targets = append(targets, t)
	}
	return targets
}

// proxyTarget 把代理地址字符串解析为 TCP 诊断目标。
//
// 返回的 ok 为 false 表示无法解析出主机+端口（空地址、非法 URL、
// 缺少主机名），调用方应跳过该目标。端口缺省时取 DefaultProxyPort。
func proxyTarget(raw string) (DiagnosticTarget, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return DiagnosticTarget{}, false
	}
	port := DefaultProxyPort
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	return DiagnosticTarget{Type: TypeTCP, Target: u.Hostname(), Port: port}, true
}

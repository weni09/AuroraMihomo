package auth

import (
	"net"
	"net/http"
	"strings"
)

// DirectIP 返回与服务器直连的对端 IP（去掉端口）。无法解析时返回 RemoteAddr 原串。
func DirectIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	direct := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		direct = host
	}
	return direct
}

// IsTrustedProxy 判断直连来源是否在可信代理白名单内，支持单 IP 与 CIDR。
// 与登录限流、Cookie Secure 共用同一信任模型，避免分叉。
func IsTrustedProxy(ip string, trusted []string) bool {
	if len(trusted) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	for _, entry := range trusted {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil || parsed == nil {
				continue
			}
			if network.Contains(parsed) {
				return true
			}
			continue
		}
		if entry == ip {
			return true
		}
		if other := net.ParseIP(entry); other != nil && parsed != nil && other.Equal(parsed) {
			return true
		}
	}
	return false
}

// RequestIsHTTPS 判断是否应按 HTTPS 设置 Cookie Secure 等标志。
//
//   - 直连 TLS（r.TLS != nil）→ true
//   - 否则仅当对端在 TrustedProxies 内，且 X-Forwarded-Proto=https
//     或 X-Forwarded-Ssl=on 时采信 → true
//
// 未受信来源伪造的转发头一律忽略，与登录 clientIP 策略一致。
func RequestIsHTTPS(r *http.Request, trustedProxies []string) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !IsTrustedProxy(DirectIP(r), trustedProxies) {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	// 反代可能写成 "https,http"；取最左侧
	if i := strings.Index(proto, ","); i >= 0 {
		proto = strings.TrimSpace(proto[:i])
	}
	if proto == "https" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on") {
		return true
	}
	return false
}

// IsLocalDialableHost 判断 host 是否为本机可 dial 的上游（回环 / localhost /
// 本机接口 IP）。同源反代（/adguard-ui、/mihomo-api）用它做 SSRF 防线：
// 上游须为本机可达的 IP 字面量，拒绝域名与外网 IP——否则配置被改写成
// 外网地址时反代会变成 SSRF 跳板。
func IsLocalDialableHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		var ifIP net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ifIP = v.IP
		case *net.IPAddr:
			ifIP = v.IP
		}
		if ifIP != nil && ifIP.Equal(ip) {
			return true
		}
	}
	return false
}

// IsAllowedKernelUpstream 判断 mihomo 反代上游是否允许：回环 / localhost /
// 本机接口 IP / 私网地址（RFC1918、ULA、链路本地）。域名与公网 IP 拒绝。
//
// 与 IsLocalDialableHost 的区别：external-controller 可能配置为「nginx/内核在
// 出网口机器、管理端在另一台」这类分机部署下的私网地址——此时回环与本机
// 接口都不满足，但该地址仍是用户自己配置的本地内核服务，应当放行。
// 公网 IP 与域名继续拒绝：external-controller 来自 config.yaml，攻击者无法
// 通过请求参数指定上游，真正的风险是配置被订阅/污染改写，私网放行不影响
// 这层防护，公网仍被挡在门外。
func IsAllowedKernelUpstream(host string) bool {
	if IsLocalDialableHost(host) {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

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
// - 直连 TLS（r.TLS != nil）→ true
// - 否则仅当对端在 TrustedProxies 内，且 X-Forwarded-Proto=https
//   或 X-Forwarded-Ssl=on 时采信 → true
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

package parser

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ===== 扩展协议解析（对齐 Sub-Store 的协议覆盖）=====

// parseHysteria 解析 hysteria:// (v1)
// hysteria://host:port?protocol=udp&auth=xxx&peer=sni&insecure=1&upmbps=100&downmbps=100#name
func parseHysteria(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("hysteria-%s-%d", h, p)
	}

	n := newNode(name, "hysteria", h, p, source)
	q := u.Query()

	if v := q.Get("auth"); v != "" {
		n.Extra["auth-str"] = v
	}
	if v := q.Get("peer"); v != "" {
		n.Extra["sni"] = v
	}
	if v := q.Get("obfs"); v != "" {
		n.Extra["obfs"] = v
	}
	if v := q.Get("protocol"); v != "" {
		n.Extra["protocol"] = v
	}
	if v := q.Get("insecure"); v == "1" || strings.EqualFold(v, "true") {
		n.Extra["skip-cert-verify"] = true
	}
	if v := q.Get("upmbps"); v != "" {
		if x, err := strconv.Atoi(v); err == nil {
			n.Extra["up"] = x
		}
	}
	if v := q.Get("downmbps"); v != "" {
		if x, err := strconv.Atoi(v); err == nil {
			n.Extra["down"] = x
		}
	}
	if v := q.Get("alpn"); v != "" {
		n.Extra["alpn"] = strings.Split(v, ",")
	}
	return n, nil
}

// parseTUIC 解析 tuic:// (v5)
// tuic://uuid:password@host:port?sni=xxx&congestion_control=bbr&alpn=h3#name
func parseTUIC(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("tuic-%s-%d", h, p)
	}

	n := newNode(name, "tuic", h, p, source)
	if u.User != nil {
		n.Extra["uuid"] = u.User.Username()
		if pw, ok := u.User.Password(); ok && pw != "" {
			n.Extra["password"] = pw
		}
	}

	q := u.Query()
	if v := q.Get("sni"); v != "" {
		n.Extra["sni"] = v
	}
	if v := q.Get("congestion_control"); v != "" {
		n.Extra["congestion-controller"] = v
	}
	if v := q.Get("udp_relay_mode"); v != "" {
		n.Extra["udp-relay-mode"] = v
	}
	if v := q.Get("alpn"); v != "" {
		n.Extra["alpn"] = strings.Split(v, ",")
	}
	if v := q.Get("allow_insecure"); v == "1" || strings.EqualFold(v, "true") {
		n.Extra["skip-cert-verify"] = true
	}
	return n, nil
}

// parseWireGuard 解析 wireguard:// / wg://
// wireguard://privateKey@host:port?publickey=xxx&address=10.0.0.2/32&reserved=0,0,0#name
func parseWireGuard(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("wg-%s-%d", h, p)
	}

	n := newNode(name, "wireguard", h, p, source)
	if u.User != nil {
		n.Extra["private-key"] = u.User.Username()
	}

	q := u.Query()
	if v := q.Get("publickey"); v != "" {
		n.Extra["public-key"] = v
	}
	if v := q.Get("address"); v != "" {
		// address 可能是 "10.0.0.2/32,fd00::2/128"
		for _, addr := range strings.Split(v, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			if strings.Contains(addr, ":") {
				n.Extra["ipv6"] = strings.SplitN(addr, "/", 2)[0]
			} else {
				n.Extra["ip"] = strings.SplitN(addr, "/", 2)[0]
			}
		}
	}
	if v := q.Get("presharedkey"); v != "" {
		n.Extra["pre-shared-key"] = v
	}
	if v := q.Get("mtu"); v != "" {
		if x, err := strconv.Atoi(v); err == nil {
			n.Extra["mtu"] = x
		}
	}
	if v := q.Get("reserved"); v != "" {
		parts := strings.Split(v, ",")
		nums := make([]int, 0, len(parts))
		for _, s := range parts {
			if x, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				nums = append(nums, x)
			}
		}
		if len(nums) > 0 {
			n.Extra["reserved"] = nums
		}
	}
	return n, nil
}

// parseAnyTLS 解析 anytls://
// anytls://password@host:port?sni=xxx&insecure=1#name
func parseAnyTLS(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("anytls-%s-%d", h, p)
	}

	n := newNode(name, "anytls", h, p, source)
	if u.User != nil {
		n.Extra["password"] = u.User.Username()
	}

	q := u.Query()
	if v := q.Get("sni"); v != "" {
		n.Extra["sni"] = v
	}
	if v := q.Get("insecure"); v == "1" || strings.EqualFold(v, "true") {
		n.Extra["skip-cert-verify"] = true
	}
	if v := q.Get("udp"); v == "1" || strings.EqualFold(v, "true") {
		n.UDP = true
	}
	return n, nil
}

// parseSocks 解析 socks5:// / socks5+tls://
func parseSocks(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("socks5-%s-%d", h, p)
	}

	n := newNode(name, "socks5", h, p, source)
	if u.User != nil {
		n.Extra["username"] = u.User.Username()
		if pw, ok := u.User.Password(); ok && pw != "" {
			n.Extra["password"] = pw
		}
	}
	if strings.Contains(strings.ToLower(u.Scheme), "tls") {
		n.Extra["tls"] = true
	}
	return n, nil
}

// parseHTTPProxy 解析 http:// / https:// 形式的代理节点
func parseHTTPProxy(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("http-%s-%d", h, p)
	}

	n := newNode(name, "http", h, p, source)
	if u.User != nil {
		n.Extra["username"] = u.User.Username()
		if pw, ok := u.User.Password(); ok && pw != "" {
			n.Extra["password"] = pw
		}
	}
	if strings.EqualFold(u.Scheme, "https") {
		n.Extra["tls"] = true
	}
	return n, nil
}

var _ = base64.StdEncoding

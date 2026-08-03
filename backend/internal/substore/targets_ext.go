package substore

import (
	"fmt"
	"strings"
)

// NodesToStash 输出 Stash 配置（Clash 系，字段与 mihomo 基本一致）
func NodesToStash(nodes []Node) (string, error) {
	proxies := make([]map[string]interface{}, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		item := map[string]interface{}{
			"name":   n.Name,
			"type":   n.Type,
			"server": n.Server,
			"port":   n.Port,
		}
		if n.UDP {
			item["udp"] = true
		}
		for k, v := range n.Extra {
			if strings.HasPrefix(k, "_") {
				continue // 跳过内部字段（如 _origin_server）
			}
			item[k] = v
		}
		// Stash 同为 Clash 系，reality 对 uTLS 指纹的依赖与 mihomo 一致
		applyClientFingerprint(n, item)
		applyPacketEncoding(n, item)
		proxies = append(proxies, item)
		names = append(names, n.Name)
	}

	cfg := map[string]interface{}{
		"proxies": proxies,
		"proxy-groups": []map[string]interface{}{
			{"name": "Proxy", "type": "select", "proxies": names},
		},
		"rules": []string{"MATCH,Proxy"},
	}
	return marshalYAML(cfg)
}

// NodesToSurfboard 输出 Surfboard 代理段（语法基于 Surge）
func NodesToSurfboard(nodes []Node) string {
	// Surfboard 与 Surge 语法兼容，直接复用
	return NodesToSurge(nodes)
}

// NodesToShadowrocket 输出 Shadowrocket 订阅（分享链接明文列表）
func NodesToShadowrocket(nodes []Node) string {
	return NodesToShareLinks(nodes)
}

// NodesToEgern 输出 Egern 配置（YAML 结构）
func NodesToEgern(nodes []Node) (string, error) {
	proxies := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		typ := strings.ToLower(n.Type)
		item := map[string]interface{}{}

		switch typ {
		case "ss", "shadowsocks":
			inner := map[string]interface{}{
				"name":     n.Name,
				"server":   n.Server,
				"port":     n.Port,
				"method":   n.Extra["cipher"],
				"password": n.Extra["password"],
			}
			item["shadowsocks"] = inner
		case "trojan":
			inner := map[string]interface{}{
				"name":     n.Name,
				"server":   n.Server,
				"port":     n.Port,
				"password": n.Extra["password"],
			}
			if v, ok := n.Extra["sni"]; ok {
				inner["sni"] = v
			}
			item["trojan"] = inner
		case "vmess":
			inner := map[string]interface{}{
				"name":   n.Name,
				"server": n.Server,
				"port":   n.Port,
				"uuid":   extraString(n, "uuid"),
			}
			if v := extraString(n, "cipher"); v != "" {
				inner["security"] = v
			}
			egernApplyTLS(n, inner, extraBool(n, "tls"))
			egernApplyTransport(n, inner)
			item["vmess"] = inner
		case "vless":
			inner := map[string]interface{}{
				"name":   n.Name,
				"server": n.Server,
				"port":   n.Port,
				"uuid":   extraString(n, "uuid"),
			}
			if v := extraString(n, "flow"); v != "" {
				inner["flow"] = v
			}
			egernApplyTLS(n, inner, true)
			egernApplyTransport(n, inner)
			item["vless"] = inner
		case "hysteria2", "hy2":
			inner := map[string]interface{}{
				"name":     n.Name,
				"server":   n.Server,
				"port":     n.Port,
				"password": firstExtraString(n, "password", "auth"),
			}
			if v := extraString(n, "obfs"); v != "" {
				inner["obfs"] = v
				if pw := extraString(n, "obfs-password"); pw != "" {
					inner["obfs_password"] = pw
				}
			}
			egernApplyTLS(n, inner, true)
			item["hysteria2"] = inner
		case "tuic":
			inner := map[string]interface{}{
				"name":     n.Name,
				"server":   n.Server,
				"port":     n.Port,
				"uuid":     extraString(n, "uuid"),
				"password": extraString(n, "password"),
			}
			if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
				inner["alpn"] = alpn
			}
			egernApplyTLS(n, inner, true)
			item["tuic"] = inner
		case "http", "https", "socks5", "socks":
			inner := map[string]interface{}{
				"name":   n.Name,
				"server": n.Server,
				"port":   n.Port,
			}
			if u := firstExtraString(n, "username", "user"); u != "" {
				inner["username"] = u
			}
			if p := extraString(n, "password"); p != "" {
				inner["password"] = p
			}
			key := "http"
			if typ == "socks5" || typ == "socks" {
				key = "socks5"
			}
			if typ == "https" || (key == "http" && extraBool(n, "tls")) {
				egernApplyTLS(n, inner, true)
			}
			item[key] = inner
		default:
			// ssr / snell / ssh / mieru / wireguard / anytls：Egern 无对应类型
			continue
		}
		proxies = append(proxies, item)
	}

	return marshalYAML(map[string]interface{}{"proxies": proxies})
}

// egernApplyTLS 写入 Egern 的 TLS 相关字段。
func egernApplyTLS(n Node, inner map[string]interface{}, force bool) {
	if !force && !extraBool(n, "tls") {
		return
	}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		inner["sni"] = sni
	}
	if extraBool(n, "skip-cert-verify") {
		inner["skip_cert_verify"] = true
	}
	if ro, ok := n.Extra["reality-opts"].(map[string]interface{}); ok && len(ro) > 0 {
		reality := map[string]interface{}{}
		if v := mapString(ro, "public-key"); v != "" {
			reality["public_key"] = v
		}
		if v := mapString(ro, "short-id"); v != "" {
			reality["short_id"] = v
		}
		inner["reality"] = reality
	}
}

// egernApplyTransport 写入 ws / grpc 传输层配置。
func egernApplyTransport(n Node, inner map[string]interface{}) {
	switch extraString(n, "network") {
	case "ws":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		ws := map[string]interface{}{}
		if path := mapString(opts, "path"); path != "" {
			ws["path"] = path
		}
		if host := headerHost(opts); host != "" {
			ws["host"] = host
		}
		inner["websocket"] = ws
	case "grpc":
		opts, _ := n.Extra["grpc-opts"].(map[string]interface{})
		g := map[string]interface{}{}
		if name := mapString(opts, "grpc-service-name"); name != "" {
			g["service_name"] = name
		}
		inner["grpc"] = g
	}
}

var _ = fmt.Sprint

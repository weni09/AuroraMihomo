package substore

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ===== 多客户端输出目标（对齐 Sub-Store 的 Output Targets）=====

// NodesToSurge 输出 Surge 代理段
func NodesToSurge(nodes []Node) string {
	return nodesToSurgeFlavor(nodes, false)
}

// NodesToSurgeMac 输出 SurgeMac 代理段。
// 相比 iOS 版，SurgeMac 额外支持 external 类型（调用本地二进制），
// 因此 snell / ssh / mieru 等协议可通过 external 落地。
func NodesToSurgeMac(nodes []Node) string {
	return nodesToSurgeFlavor(nodes, true)
}

func nodesToSurgeFlavor(nodes []Node, mac bool) string {
	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		var parts []string
		switch strings.ToLower(n.Type) {
		case "ss", "shadowsocks":
			parts = []string{"ss", n.Server, fmt.Sprint(n.Port)}
			if v, ok := n.Extra["cipher"].(string); ok && v != "" {
				parts = append(parts, "encrypt-method="+v)
			}
			if v, ok := n.Extra["password"].(string); ok && v != "" {
				parts = append(parts, "password="+v)
			}
		case "trojan":
			parts = []string{"trojan", n.Server, fmt.Sprint(n.Port)}
			if v := extraString(n, "password"); v != "" {
				parts = append(parts, "password="+v)
			}
			parts = append(parts, surgeTLSParams(n)...)
			parts = append(parts, surgeWSParams(n)...)
		case "vmess":
			parts = []string{"vmess", n.Server, fmt.Sprint(n.Port)}
			if v := extraString(n, "uuid"); v != "" {
				parts = append(parts, "username="+v)
			}
			if extraBool(n, "tls") {
				parts = append(parts, "tls=true")
				parts = append(parts, surgeTLSParams(n)...)
			}
			parts = append(parts, surgeWSParams(n)...)
		case "vless":
			// Surge 5 起支持 vless；reality 不受支持，带 reality-opts 的
			// 节点输出后无法连接，故跳过而非输出半成品
			if ro, ok := n.Extra["reality-opts"].(map[string]interface{}); ok && len(ro) > 0 {
				continue
			}
			parts = []string{"vless", n.Server, fmt.Sprint(n.Port)}
			if v := extraString(n, "uuid"); v != "" {
				parts = append(parts, "username="+v)
			}
			parts = append(parts, "tls=true")
			parts = append(parts, surgeTLSParams(n)...)
			parts = append(parts, surgeWSParams(n)...)
		case "hysteria2", "hy2":
			parts = []string{"hysteria2", n.Server, fmt.Sprint(n.Port)}
			if v := firstExtraString(n, "password", "auth"); v != "" {
				parts = append(parts, "password="+v)
			}
			if v := extraString(n, "down"); v != "" {
				parts = append(parts, "download-bandwidth="+v)
			}
			parts = append(parts, surgeTLSParams(n)...)
		case "tuic":
			parts = []string{"tuic", n.Server, fmt.Sprint(n.Port)}
			if v := extraString(n, "uuid"); v != "" {
				parts = append(parts, "uuid="+v)
			}
			if v := extraString(n, "password"); v != "" {
				parts = append(parts, "password="+v)
			}
			if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
				parts = append(parts, "alpn="+strings.Join(alpn, ","))
			}
			parts = append(parts, surgeTLSParams(n)...)
		case "snell":
			parts = []string{"snell", n.Server, fmt.Sprint(n.Port)}
			if v, ok := n.Extra["psk"].(string); ok && v != "" {
				parts = append(parts, "psk="+v)
			}
			if v, ok := n.Extra["version"].(int); ok && v > 0 {
				parts = append(parts, fmt.Sprintf("version=%d", v))
			}
			if opts, ok := n.Extra["obfs-opts"].(map[string]interface{}); ok {
				if mode, ok := opts["mode"].(string); ok && mode != "" {
					parts = append(parts, "obfs="+mode)
				}
				if host, ok := opts["host"].(string); ok && host != "" {
					parts = append(parts, "obfs-host="+host)
				}
			}
		case "http", "https", "socks5":
			parts = []string{strings.ToLower(n.Type), n.Server, fmt.Sprint(n.Port)}
		default:
			if !mac {
				continue // Surge iOS 不支持的协议跳过
			}
			ext := surgeExternal(n)
			if ext == nil {
				continue
			}
			parts = ext
		}
		if n.UDP {
			parts = append(parts, "udp-relay=true")
		}
		lines = append(lines, fmt.Sprintf("%s = %s", n.Name, strings.Join(parts, ", ")))
	}
	return strings.Join(lines, "\n")
}

// surgeExternal 为 SurgeMac 生成 external 形式的代理定义，
// 无法映射到本地可执行程序的协议返回 nil 以跳过。
func surgeExternal(n Node) []string {
	binary, args := "", []string(nil)
	switch strings.ToLower(n.Type) {
	case "ssh":
		binary = "/usr/bin/ssh"
		user, _ := n.Extra["username"].(string)
		target := n.Server
		if user != "" {
			target = user + "@" + n.Server
		}
		args = []string{"-N", "-D", fmt.Sprint(n.Port), "-p", fmt.Sprint(n.Port), target}
	case "hysteria", "hysteria2", "tuic", "mieru", "anytls", "wireguard":
		// 这些协议需借助 sing-box 等外部内核，交由用户配置可执行路径
		binary = "/usr/local/bin/sing-box"
		args = []string{"run", "-c", fmt.Sprintf("%s.json", n.Name)}
	default:
		return nil
	}

	parts := []string{"external", "exec=" + quoteSurgeValue(binary)}
	for _, a := range args {
		parts = append(parts, "args="+quoteSurgeValue(a))
	}
	parts = append(parts, "addresses="+n.Server)
	return parts
}

func quoteSurgeValue(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}

// surgeTLSParams 输出 Surge 的 SNI 与证书校验参数。
func surgeTLSParams(n Node) []string {
	out := []string{}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		out = append(out, "sni="+sni)
	}
	if extraBool(n, "skip-cert-verify") {
		out = append(out, "skip-cert-verify=true")
	}
	return out
}

// surgeWSParams 输出 Surge 的 WebSocket 参数。
// Surge 只支持 ws，grpc/h2 无对应写法，返回 nil 让其退化为直连传输。
func surgeWSParams(n Node) []string {
	if extraString(n, "network") != "ws" {
		return nil
	}
	opts, _ := n.Extra["ws-opts"].(map[string]interface{})
	out := []string{"ws=true"}
	if path := mapString(opts, "path"); path != "" {
		out = append(out, "ws-path="+path)
	}
	if host := headerHost(opts); host != "" {
		// Surge 的 ws headers 用 KEY:VALUE|KEY:VALUE 形式
		out = append(out, "ws-headers=Host:"+host)
	}
	return out
}

// NodesToLoon 输出 Loon 代理段。
//
// Loon 的位置参数是「协议,服务器,端口,[加密],[凭据]」，其后跟 key=value；
// 传输层与 TLS 参数此前完全没输出，走 ws/tls 的节点导入后连不上。
func NodesToLoon(nodes []Node) string {
	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		var parts []string
		switch strings.ToLower(n.Type) {
		case "ss", "shadowsocks":
			parts = []string{"Shadowsocks", n.Server, fmt.Sprint(n.Port),
				extraString(n, "cipher"), extraString(n, "password")}
			parts = append(parts, loonSSPlugin(n)...)
		case "trojan":
			parts = []string{"trojan", n.Server, fmt.Sprint(n.Port), extraString(n, "password")}
			parts = append(parts, loonTLSParams(n, true)...)
			parts = append(parts, loonTransportParams(n)...)
		case "vmess":
			parts = []string{"vmess", n.Server, fmt.Sprint(n.Port),
				firstNonEmpty(extraString(n, "cipher"), "auto"), extraString(n, "uuid")}
			parts = append(parts, loonTransportParams(n)...)
			parts = append(parts, loonTLSParams(n, false)...)
		case "vless":
			parts = []string{"vless", n.Server, fmt.Sprint(n.Port), extraString(n, "uuid")}
			if flow := extraString(n, "flow"); flow != "" {
				parts = append(parts, "flow="+flow)
			}
			parts = append(parts, loonTransportParams(n)...)
			// vless 基本都跑在 TLS/reality 上，强制输出 TLS 参数
			parts = append(parts, loonTLSParams(n, true)...)
		case "hysteria2", "hy2":
			parts = []string{"Hysteria2", n.Server, fmt.Sprint(n.Port),
				firstExtraString(n, "password", "auth")}
			parts = append(parts, loonTLSParams(n, true)...)
		case "tuic":
			parts = []string{"tuic", n.Server, fmt.Sprint(n.Port),
				extraString(n, "uuid"), extraString(n, "password")}
			if v := extraString(n, "alpn"); v != "" {
				parts = append(parts, "alpn="+strings.Join(extraStringSlice(n, "alpn"), ","))
			}
			parts = append(parts, loonTLSParams(n, true)...)
		case "http", "https":
			proto := "http"
			if strings.EqualFold(n.Type, "https") || extraBool(n, "tls") {
				proto = "https"
			}
			parts = []string{proto, n.Server, fmt.Sprint(n.Port)}
			parts = append(parts, loonUserPass(n)...)
		case "socks5", "socks":
			proto := "socks5"
			if extraBool(n, "tls") {
				proto = "socks5-tls"
			}
			parts = []string{proto, n.Server, fmt.Sprint(n.Port)}
			parts = append(parts, loonUserPass(n)...)
		default:
			// ssr / snell / ssh / mieru / wireguard / anytls：
			// Loon 无对应协议或需额外配置，跳过
			continue
		}
		if n.UDP {
			parts = append(parts, "udp=true")
		}
		lines = append(lines, fmt.Sprintf("%s = %s", n.Name, strings.Join(parts, ",")))
	}
	return strings.Join(lines, "\n")
}

func loonUserPass(n Node) []string {
	out := []string{}
	if u := firstExtraString(n, "username", "user"); u != "" {
		out = append(out, u)
		if p := extraString(n, "password"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loonSSPlugin(n Node) []string {
	if extraString(n, "plugin") != "obfs" {
		return nil
	}
	opts, _ := n.Extra["plugin-opts"].(map[string]interface{})
	out := []string{}
	if mode := mapString(opts, "mode"); mode != "" {
		out = append(out, "obfs-name="+mode)
	}
	if host := mapString(opts, "host"); host != "" {
		out = append(out, "obfs-host="+host)
	}
	return out
}

func loonTLSParams(n Node, force bool) []string {
	if !force && !extraBool(n, "tls") {
		return nil
	}
	out := []string{}
	// SNI 优先取顶层 servername/sni；很多 CDN 节点只在 ws-opts 的
	// Host 头里给了域名，此时必须回退到它，否则 Loon 会拿 IP 去做
	// TLS 校验而握手失败。
	sni := firstExtraString(n, "servername", "sni")
	if sni == "" {
		sni = tlsHostFallback(n)
	}
	if sni != "" {
		out = append(out, "tls-name="+sni)
	}
	if extraBool(n, "skip-cert-verify") {
		out = append(out, "skip-cert-verify=true")
	}
	return out
}

// tlsHostFallback 从传输层的 Host 头里取域名，作为 SNI 的兜底。
func tlsHostFallback(n Node) string {
	for _, key := range []string{"ws-opts", "http-opts", "h2-opts"} {
		opts, _ := n.Extra[key].(map[string]interface{})
		if host := headerHost(opts); host != "" {
			return host
		}
		if hosts := stringSlice(mapValue(opts, "host")); len(hosts) > 0 {
			return hosts[0]
		}
	}
	return ""
}

func loonTransportParams(n Node) []string {
	switch extraString(n, "network") {
	case "ws":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		out := []string{"transport=ws"}
		if path := mapString(opts, "path"); path != "" {
			out = append(out, "path="+path)
		}
		if host := headerHost(opts); host != "" {
			out = append(out, "host="+host)
		}
		return out
	case "http":
		return []string{"transport=http"}
	case "tcp", "":
		return []string{"transport=tcp"}
	default:
		// grpc/h2 等 Loon 不支持的传输层不输出 transport，
		// 让客户端按默认 tcp 处理，至少不会因未知值直接报错
		return nil
	}
}

// NodesToQuantumultX 输出 QuantumultX 代理段。
//
// QX 用 over-tls / tls-host / obfs 等键描述 TLS 与传输层，
// 此前一律未输出，导致 ws/tls 节点导入后无法握手。
func NodesToQuantumultX(nodes []Node) string {
	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		var parts []string
		switch strings.ToLower(n.Type) {
		case "ss", "shadowsocks":
			parts = append(parts, fmt.Sprintf("shadowsocks=%s:%d", n.Server, n.Port))
			if v := extraString(n, "cipher"); v != "" {
				parts = append(parts, "method="+v)
			}
			if v := extraString(n, "password"); v != "" {
				parts = append(parts, "password="+v)
			}
			parts = append(parts, qxSSObfs(n)...)
		case "trojan":
			parts = append(parts, fmt.Sprintf("trojan=%s:%d", n.Server, n.Port))
			if v := extraString(n, "password"); v != "" {
				parts = append(parts, "password="+v)
			}
			// trojan 恒为 TLS
			parts = append(parts, "over-tls=true")
			parts = append(parts, qxTLSParams(n)...)
			parts = append(parts, qxObfsParams(n)...)
		case "vmess":
			parts = append(parts, fmt.Sprintf("vmess=%s:%d", n.Server, n.Port))
			if v := extraString(n, "uuid"); v != "" {
				parts = append(parts, "password="+v)
			}
			parts = append(parts, "method="+firstNonEmpty(extraString(n, "cipher"), "none"))
			if extraBool(n, "tls") {
				parts = append(parts, "over-tls=true")
				parts = append(parts, qxTLSParams(n)...)
			}
			parts = append(parts, qxObfsParams(n)...)
		case "vless":
			parts = append(parts, fmt.Sprintf("vless=%s:%d", n.Server, n.Port))
			if v := extraString(n, "uuid"); v != "" {
				parts = append(parts, "password="+v)
			}
			parts = append(parts, "method=none")
			parts = append(parts, "over-tls=true")
			parts = append(parts, qxTLSParams(n)...)
			parts = append(parts, qxObfsParams(n)...)
		case "http", "https":
			parts = append(parts, fmt.Sprintf("http=%s:%d", n.Server, n.Port))
			parts = append(parts, qxUserPass(n)...)
			if strings.EqualFold(n.Type, "https") || extraBool(n, "tls") {
				parts = append(parts, "over-tls=true")
				parts = append(parts, qxTLSParams(n)...)
			}
		case "socks5", "socks":
			parts = append(parts, fmt.Sprintf("socks5=%s:%d", n.Server, n.Port))
			parts = append(parts, qxUserPass(n)...)
			if extraBool(n, "tls") {
				parts = append(parts, "over-tls=true")
				parts = append(parts, qxTLSParams(n)...)
			}
		default:
			// ssr/snell/ssh/mieru/wireguard/hysteria*/tuic/anytls：QX 不支持
			continue
		}
		if n.UDP {
			parts = append(parts, "udp-relay=true")
		}
		parts = append(parts, "tag="+n.Name)
		lines = append(lines, strings.Join(parts, ", "))
	}
	return strings.Join(lines, "\n")
}

func qxUserPass(n Node) []string {
	out := []string{}
	if u := firstExtraString(n, "username", "user"); u != "" {
		out = append(out, "username="+u)
	}
	if p := extraString(n, "password"); p != "" {
		out = append(out, "password="+p)
	}
	return out
}

func qxTLSParams(n Node) []string {
	out := []string{}
	if sni := firstExtraString(n, "servername", "sni"); sni != "" {
		out = append(out, "tls-host="+sni)
	}
	if extraBool(n, "skip-cert-verify") {
		out = append(out, "tls-verification=false")
	}
	return out
}

// qxObfsParams 把 ws/http 传输层映射成 QX 的 obfs 键。
func qxObfsParams(n Node) []string {
	network := extraString(n, "network")
	switch network {
	case "ws":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		obfs := "ws"
		if extraBool(n, "tls") {
			obfs = "wss"
		}
		out := []string{"obfs=" + obfs}
		if path := mapString(opts, "path"); path != "" {
			out = append(out, "obfs-uri="+path)
		}
		if host := headerHost(opts); host != "" {
			out = append(out, "obfs-host="+host)
		}
		return out
	case "h2":
		opts, _ := n.Extra["h2-opts"].(map[string]interface{})
		out := []string{"obfs=h2"}
		if path := mapString(opts, "path"); path != "" {
			out = append(out, "obfs-uri="+path)
		}
		return out
	}
	return nil
}

func qxSSObfs(n Node) []string {
	if extraString(n, "plugin") != "obfs" {
		return nil
	}
	opts, _ := n.Extra["plugin-opts"].(map[string]interface{})
	out := []string{}
	if mode := mapString(opts, "mode"); mode != "" {
		out = append(out, "obfs="+mode)
	}
	if host := mapString(opts, "host"); host != "" {
		out = append(out, "obfs-host="+host)
	}
	return out
}

// NodesToSingBox 输出 sing-box outbounds JSON。
//
// 字段名遵循 sing-box 的 snake_case 约定（server_port / tls.server_name 等），
// 与 Clash 系的 kebab-case 不同，不能直接透传 Extra。
func NodesToSingBox(nodes []Node) (string, error) {
	outbounds := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		ob := map[string]interface{}{
			"tag":         n.Name,
			"server":      n.Server,
			"server_port": n.Port,
		}
		switch strings.ToLower(n.Type) {
		case "ss", "shadowsocks":
			ob["type"] = "shadowsocks"
			ob["method"] = extraString(n, "cipher")
			ob["password"] = extraString(n, "password")
		case "trojan":
			ob["type"] = "trojan"
			ob["password"] = extraString(n, "password")
			singBoxApplyTLS(n, ob, true)
			singBoxApplyTransport(n, ob)
		case "vmess":
			ob["type"] = "vmess"
			ob["uuid"] = extraString(n, "uuid")
			ob["security"] = firstNonEmpty(extraString(n, "cipher"), "auto")
			if aid := extraInt(n, "alterId"); aid > 0 {
				ob["alter_id"] = aid
			}
			singBoxApplyTLS(n, ob, false)
			singBoxApplyTransport(n, ob)
		case "vless":
			ob["type"] = "vless"
			ob["uuid"] = extraString(n, "uuid")
			if flow := extraString(n, "flow"); flow != "" {
				ob["flow"] = flow
			}
			singBoxApplyTLS(n, ob, false)
			singBoxApplyTransport(n, ob)
		case "hysteria2", "hy2":
			ob["type"] = "hysteria2"
			ob["password"] = firstExtraString(n, "password", "auth")
			if obfs := extraString(n, "obfs"); obfs != "" {
				o := map[string]interface{}{"type": obfs}
				if pw := extraString(n, "obfs-password"); pw != "" {
					o["password"] = pw
				}
				ob["obfs"] = o
			}
			if v := extraString(n, "up"); v != "" {
				ob["up_mbps"] = extraInt(n, "up")
			}
			if v := extraString(n, "down"); v != "" {
				ob["down_mbps"] = extraInt(n, "down")
			}
			singBoxApplyTLS(n, ob, true)
		case "hysteria", "hy":
			ob["type"] = "hysteria"
			if v := firstExtraString(n, "auth-str", "auth_str", "auth"); v != "" {
				ob["auth_str"] = v
			}
			if v := extraString(n, "up"); v != "" {
				ob["up_mbps"] = extraInt(n, "up")
			}
			if v := extraString(n, "down"); v != "" {
				ob["down_mbps"] = extraInt(n, "down")
			}
			singBoxApplyTLS(n, ob, true)
		case "tuic":
			ob["type"] = "tuic"
			ob["uuid"] = extraString(n, "uuid")
			ob["password"] = extraString(n, "password")
			if v := extraString(n, "congestion-controller"); v != "" {
				ob["congestion_control"] = v
			}
			if v := extraString(n, "udp-relay-mode"); v != "" {
				ob["udp_relay_mode"] = v
			}
			singBoxApplyTLS(n, ob, true)
		case "anytls":
			ob["type"] = "anytls"
			ob["password"] = extraString(n, "password")
			singBoxApplyTLS(n, ob, true)
		case "socks5", "socks":
			ob["type"] = "socks"
			ob["version"] = "5"
			singBoxApplyUserPass(n, ob)
		case "http", "https":
			ob["type"] = "http"
			singBoxApplyUserPass(n, ob)
			if strings.EqualFold(n.Type, "https") || extraBool(n, "tls") {
				singBoxApplyTLS(n, ob, true)
			}
		case "ssh":
			ob["type"] = "ssh"
			if v := firstExtraString(n, "username", "user"); v != "" {
				ob["user"] = v
			}
			if v := extraString(n, "password"); v != "" {
				ob["password"] = v
			}
		default:
			// ssr / snell / mieru / wireguard 在 sing-box 里或无对应
			// outbound，或需要额外密钥材料，跳过而非输出不可用条目
			continue
		}
		outbounds = append(outbounds, ob)
	}
	b, err := json.MarshalIndent(map[string]interface{}{"outbounds": outbounds}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// singBoxApplyTLS 写入 sing-box 的 tls 段。
// force 用于 trojan/hysteria2/tuic 这类协议——它们本身即基于 TLS，
// 即使节点没显式标 tls 也必须开启，否则 sing-box 无法握手。
func singBoxApplyTLS(n Node, ob map[string]interface{}, force bool) {
	enabled := force || extraBool(n, "tls")
	if !enabled {
		return
	}
	tls := map[string]interface{}{"enabled": true}
	if sni := firstExtraString(n, "servername", "sni"); sni != "" {
		tls["server_name"] = sni
	}
	if extraBool(n, "skip-cert-verify") {
		tls["insecure"] = true
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if fp := extraString(n, "client-fingerprint"); fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}
	if ro, ok := n.Extra["reality-opts"].(map[string]interface{}); ok && len(ro) > 0 {
		reality := map[string]interface{}{"enabled": true}
		if v := mapString(ro, "public-key"); v != "" {
			reality["public_key"] = v
		}
		if v := mapString(ro, "short-id"); v != "" {
			reality["short_id"] = v
		}
		tls["reality"] = reality
	}
	ob["tls"] = tls
}

// singBoxApplyTransport 写入 transport 段（ws/grpc/http/httpupgrade）。
func singBoxApplyTransport(n Node, ob map[string]interface{}) {
	switch extraString(n, "network") {
	case "ws":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		tr := map[string]interface{}{"type": "ws"}
		if path := mapString(opts, "path"); path != "" {
			tr["path"] = path
		}
		if host := headerHost(opts); host != "" {
			tr["headers"] = map[string]interface{}{"Host": host}
		}
		ob["transport"] = tr
	case "grpc":
		opts, _ := n.Extra["grpc-opts"].(map[string]interface{})
		tr := map[string]interface{}{"type": "grpc"}
		if name := mapString(opts, "grpc-service-name"); name != "" {
			tr["service_name"] = name
		}
		ob["transport"] = tr
	case "h2":
		opts, _ := n.Extra["h2-opts"].(map[string]interface{})
		tr := map[string]interface{}{"type": "http"}
		if path := mapString(opts, "path"); path != "" {
			tr["path"] = path
		}
		if hosts := stringSlice(mapValue(opts, "host")); len(hosts) > 0 {
			tr["host"] = hosts
		}
		ob["transport"] = tr
	case "httpupgrade":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		tr := map[string]interface{}{"type": "httpupgrade"}
		if path := mapString(opts, "path"); path != "" {
			tr["path"] = path
		}
		if host := headerHost(opts); host != "" {
			tr["host"] = host
		}
		ob["transport"] = tr
	}
}

func singBoxApplyUserPass(n Node, ob map[string]interface{}) {
	if v := firstExtraString(n, "username", "user"); v != "" {
		ob["username"] = v
	}
	if v := extraString(n, "password"); v != "" {
		ob["password"] = v
	}
}

// NodesToV2RayJSON 输出 V2Ray outbounds JSON。
//
// 之前只写了 protocol 与凭据，没有 streamSettings：这会让所有走 TLS 或
// ws/grpc 传输的节点退化成明文 TCP 直连，客户端导入后必然连不上。
func NodesToV2RayJSON(nodes []Node) (string, error) {
	outbounds := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		typ := strings.ToLower(n.Type)
		ob := map[string]interface{}{"tag": n.Name}

		switch typ {
		case "vmess", "vless":
			ob["protocol"] = typ
			user := map[string]interface{}{"id": extraString(n, "uuid")}
			if typ == "vmess" {
				user["alterId"] = extraInt(n, "alterId")
				user["security"] = firstNonEmpty(extraString(n, "cipher"), "auto")
			} else {
				// vless 用 encryption 而非 security，缺省必须显式写 none
				user["encryption"] = "none"
				if flow := extraString(n, "flow"); flow != "" {
					user["flow"] = flow
				}
			}
			ob["settings"] = map[string]interface{}{
				"vnext": []map[string]interface{}{{
					"address": n.Server,
					"port":    n.Port,
					"users":   []map[string]interface{}{user},
				}},
			}
			if ss := v2rayStreamSettings(n); ss != nil {
				ob["streamSettings"] = ss
			}
		case "trojan":
			ob["protocol"] = "trojan"
			ob["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  n.Server,
					"port":     n.Port,
					"password": extraString(n, "password"),
				}},
			}
			// trojan 基于 TLS，即使节点未显式标注也要生成 tls streamSettings
			if ss := v2rayStreamSettings(n); ss != nil {
				ob["streamSettings"] = ss
			} else {
				ob["streamSettings"] = map[string]interface{}{
					"network":     firstNonEmpty(extraString(n, "network"), "tcp"),
					"security":    "tls",
					"tlsSettings": v2rayTLSSettings(n),
				}
			}
		case "ss", "shadowsocks":
			ob["protocol"] = "shadowsocks"
			ob["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  n.Server,
					"port":     n.Port,
					"password": extraString(n, "password"),
					"method":   extraString(n, "cipher"),
				}},
			}
		case "socks5", "socks":
			ob["protocol"] = "socks"
			srv := map[string]interface{}{"address": n.Server, "port": n.Port}
			if u := firstExtraString(n, "username", "user"); u != "" {
				srv["users"] = []map[string]interface{}{{
					"user": u, "pass": extraString(n, "password"),
				}}
			}
			ob["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{srv},
			}
		case "http", "https":
			ob["protocol"] = "http"
			srv := map[string]interface{}{"address": n.Server, "port": n.Port}
			if u := firstExtraString(n, "username", "user"); u != "" {
				srv["users"] = []map[string]interface{}{{
					"user": u, "pass": extraString(n, "password"),
				}}
			}
			ob["settings"] = map[string]interface{}{
				"servers": []map[string]interface{}{srv},
			}
			if typ == "https" || extraBool(n, "tls") {
				ob["streamSettings"] = map[string]interface{}{
					"network": "tcp", "security": "tls",
					"tlsSettings": v2rayTLSSettings(n),
				}
			}
		default:
			// hysteria/hysteria2/tuic/anytls 等基于 QUIC 或自有协议，
			// V2Ray 核心不支持，跳过
			continue
		}
		outbounds = append(outbounds, ob)
	}
	b, err := json.MarshalIndent(map[string]interface{}{"outbounds": outbounds}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// v2rayStreamSettings 组装 streamSettings；无 TLS 也无特殊传输时返回 nil。
func v2rayStreamSettings(n Node) map[string]interface{} {
	network := firstNonEmpty(extraString(n, "network"), "tcp")
	reality, hasReality := n.Extra["reality-opts"].(map[string]interface{})
	tlsOn := extraBool(n, "tls")

	if network == "tcp" && !tlsOn && !hasReality {
		return nil
	}

	ss := map[string]interface{}{"network": network}
	switch {
	case hasReality && len(reality) > 0:
		ss["security"] = "reality"
		rs := map[string]interface{}{"show": false}
		if sni := firstExtraString(n, "servername", "sni"); sni != "" {
			rs["serverName"] = sni
		}
		if v := mapString(reality, "public-key"); v != "" {
			rs["publicKey"] = v
		}
		if v := mapString(reality, "short-id"); v != "" {
			rs["shortId"] = v
		}
		if fp := extraString(n, "client-fingerprint"); fp != "" {
			rs["fingerprint"] = fp
		}
		ss["realitySettings"] = rs
	case tlsOn:
		ss["security"] = "tls"
		ss["tlsSettings"] = v2rayTLSSettings(n)
	default:
		ss["security"] = "none"
	}

	switch network {
	case "ws":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		ws := map[string]interface{}{}
		if path := mapString(opts, "path"); path != "" {
			ws["path"] = path
		}
		if host := headerHost(opts); host != "" {
			ws["headers"] = map[string]interface{}{"Host": host}
		}
		ss["wsSettings"] = ws
	case "grpc":
		opts, _ := n.Extra["grpc-opts"].(map[string]interface{})
		g := map[string]interface{}{}
		if name := mapString(opts, "grpc-service-name"); name != "" {
			g["serviceName"] = name
		}
		ss["grpcSettings"] = g
	case "h2":
		opts, _ := n.Extra["h2-opts"].(map[string]interface{})
		h := map[string]interface{}{}
		if path := mapString(opts, "path"); path != "" {
			h["path"] = path
		}
		if hosts := stringSlice(mapValue(opts, "host")); len(hosts) > 0 {
			h["host"] = hosts
		}
		ss["httpSettings"] = h
	}
	return ss
}

func v2rayTLSSettings(n Node) map[string]interface{} {
	tls := map[string]interface{}{}
	if sni := firstExtraString(n, "servername", "sni"); sni != "" {
		tls["serverName"] = sni
	}
	if extraBool(n, "skip-cert-verify") {
		tls["allowInsecure"] = true
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if fp := extraString(n, "client-fingerprint"); fp != "" {
		tls["fingerprint"] = fp
	}
	return tls
}

// NodesToPlainJSON 输出统一节点模型 JSON（Sub-Store: Plain JSON）
func NodesToPlainJSON(nodes []Node) (string, error) {
	items := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]interface{}{
			"name":   n.Name,
			"type":   n.Type,
			"server": n.Server,
			"port":   n.Port,
		}
		if n.UDP {
			m["udp"] = true
		}
		for k, v := range n.Extra {
			m[k] = v
		}
		items = append(items, m)
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

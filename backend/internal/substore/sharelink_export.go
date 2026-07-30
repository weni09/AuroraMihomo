package substore

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// 分享链接导出：把统一节点模型还原成各协议的 xxx:// 分享链接。
//
// 这一份被三个对外格式共用：base64-links（整体 base64）、share-links（明文）
// 与 Shadowrocket 订阅。此前只支持 ss / trojan，导致节点里常见的
// vmess / vless / hysteria2 / tuic 等一律被丢弃——对外表现为
// 「HTTP 200 但内容为空」，用户无从判断是没节点还是不支持。
//
// 字段名严格对齐 parser 侧写入 Extra 的键（如 vless 的 servername /
// reality-opts / ws-opts），避免导出的链接与解析结果不自洽。

// NodesToShareLinks 输出每行一条分享链接；无法表达为链接的协议会被跳过。
func NodesToShareLinks(nodes []Node) string {
	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if link := nodeToShareLink(n); link != "" {
			lines = append(lines, link)
		}
	}
	return strings.Join(lines, "\n")
}

// nodeToShareLink 返回单个节点的分享链接，不支持的协议返回空串。
func nodeToShareLink(n Node) string {
	switch strings.ToLower(n.Type) {
	case "ss", "shadowsocks":
		return ssShareLink(n)
	case "trojan":
		return trojanShareLink(n)
	case "vmess":
		return vmessShareLink(n)
	case "vless":
		return vlessShareLink(n)
	case "hysteria2", "hy2":
		return hysteria2ShareLink(n)
	case "hysteria", "hy":
		return hysteriaShareLink(n)
	case "tuic":
		return tuicShareLink(n)
	case "anytls":
		return anytlsShareLink(n)
	case "socks5", "socks":
		return socksShareLink(n)
	case "http", "https":
		return httpShareLink(n)
	default:
		// ssr / snell / ssh / mieru / wireguard 没有被客户端普遍接受的
		// 分享链接形式（或需要额外密钥材料），保持跳过而非输出半成品链接。
		return ""
	}
}

func ssShareLink(n Node) string {
	cipher := extraString(n, "cipher")
	password := extraString(n, "password")
	if cipher == "" || password == "" {
		return ""
	}
	// SIP002：userinfo 用 base64(method:password)，历史客户端兼容性最好
	user := base64.RawURLEncoding.EncodeToString([]byte(cipher + ":" + password))
	link := fmt.Sprintf("ss://%s@%s", user, hostPort(n))
	if q := ssPluginQuery(n); q != "" {
		link += "?" + q
	}
	return link + fragment(n.Name)
}

// ssPluginQuery 还原 obfs / v2ray-plugin 插件参数。
func ssPluginQuery(n Node) string {
	plugin := extraString(n, "plugin")
	if plugin == "" {
		return ""
	}
	opts, _ := n.Extra["plugin-opts"].(map[string]interface{})
	parts := make([]string, 0, 4)
	switch plugin {
	case "obfs":
		parts = append(parts, "obfs-local")
		if mode := mapString(opts, "mode"); mode != "" {
			parts = append(parts, "obfs="+mode)
		}
		if host := mapString(opts, "host"); host != "" {
			parts = append(parts, "obfs-host="+host)
		}
	case "v2ray-plugin":
		parts = append(parts, "v2ray-plugin")
		if mode := mapString(opts, "mode"); mode != "" {
			parts = append(parts, "mode="+mode)
		}
		if host := mapString(opts, "host"); host != "" {
			parts = append(parts, "host="+host)
		}
		if path := mapString(opts, "path"); path != "" {
			parts = append(parts, "path="+path)
		}
		if tlsOn, _ := opts["tls"].(bool); tlsOn {
			parts = append(parts, "tls")
		}
	default:
		return ""
	}
	return "plugin=" + url.QueryEscape(strings.Join(parts, ";"))
}

func trojanShareLink(n Node) string {
	password := extraString(n, "password")
	if password == "" {
		return ""
	}
	q := url.Values{}
	// trojan 的 SNI 在 parser 里落在 sni；部分上游用 servername
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		q.Set("sni", sni)
	}
	if network := extraString(n, "network"); network != "" && network != "tcp" {
		q.Set("type", network)
		applyTransportQuery(n, network, q)
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		q.Set("alpn", strings.Join(alpn, ","))
	}
	if extraBool(n, "skip-cert-verify") {
		q.Set("allowInsecure", "1")
	}
	return "trojan://" + url.QueryEscape(password) + "@" + hostPort(n) + query(q) + fragment(n.Name)
}

// vmessShareLink 输出 v2rayN 风格的 base64(JSON) 形式，
// 这是 vmess 事实上的通用分享格式。
func vmessShareLink(n Node) string {
	uuid := extraString(n, "uuid")
	if uuid == "" {
		return ""
	}
	network := extraString(n, "network")
	if network == "" {
		network = "tcp"
	}
	m := map[string]interface{}{
		"v":    "2",
		"ps":   n.Name,
		"add":  n.Server,
		"port": fmt.Sprint(n.Port),
		"id":   uuid,
		"aid":  fmt.Sprint(extraInt(n, "alterId")),
		"scy":  firstNonEmpty(extraString(n, "cipher"), "auto"),
		"net":  network,
		"type": "none",
	}
	if extraBool(n, "tls") {
		m["tls"] = "tls"
	} else {
		m["tls"] = ""
	}
	if sni := firstExtraString(n, "servername", "sni"); sni != "" {
		m["sni"] = sni
	}
	// host/path 依传输层而定，取自对应的 xxx-opts
	host, path := transportHostPath(n, network)
	if host != "" {
		m["host"] = host
	}
	if path != "" {
		m["path"] = path
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func vlessShareLink(n Node) string {
	uuid := extraString(n, "uuid")
	if uuid == "" {
		return ""
	}
	q := url.Values{}
	network := extraString(n, "network")
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)

	// security：reality 优先（有 reality-opts 即视为 reality）
	realityOpts, hasReality := n.Extra["reality-opts"].(map[string]interface{})
	switch {
	case hasReality && len(realityOpts) > 0:
		q.Set("security", "reality")
		if pbk := mapString(realityOpts, "public-key"); pbk != "" {
			q.Set("pbk", pbk)
		}
		if sid := mapString(realityOpts, "short-id"); sid != "" {
			q.Set("sid", sid)
		}
	case extraBool(n, "tls"):
		q.Set("security", "tls")
	default:
		q.Set("security", "none")
	}
	if sni := firstExtraString(n, "servername", "sni"); sni != "" {
		q.Set("sni", sni)
	}
	if flow := extraString(n, "flow"); flow != "" {
		q.Set("flow", flow)
	}
	if fp := extraString(n, "client-fingerprint"); fp != "" {
		q.Set("fp", fp)
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		q.Set("alpn", strings.Join(alpn, ","))
	}
	applyTransportQuery(n, network, q)
	return "vless://" + uuid + "@" + hostPort(n) + query(q) + fragment(n.Name)
}

func hysteria2ShareLink(n Node) string {
	password := firstExtraString(n, "password", "auth")
	if password == "" {
		return ""
	}
	q := url.Values{}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		q.Set("sni", sni)
	}
	if obfs := extraString(n, "obfs"); obfs != "" {
		q.Set("obfs", obfs)
		if pw := extraString(n, "obfs-password"); pw != "" {
			q.Set("obfs-password", pw)
		}
	}
	if extraBool(n, "skip-cert-verify") {
		q.Set("insecure", "1")
	}
	return "hysteria2://" + url.QueryEscape(password) + "@" + hostPort(n) + query(q) + fragment(n.Name)
}

func hysteriaShareLink(n Node) string {
	q := url.Values{}
	if auth := firstExtraString(n, "auth-str", "auth_str", "auth"); auth != "" {
		q.Set("auth", auth)
	}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		q.Set("peer", sni)
	}
	if v := extraString(n, "protocol"); v != "" {
		q.Set("protocol", v)
	}
	if v := extraString(n, "up"); v != "" {
		q.Set("upmbps", v)
	}
	if v := extraString(n, "down"); v != "" {
		q.Set("downmbps", v)
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		q.Set("alpn", strings.Join(alpn, ","))
	}
	if extraBool(n, "skip-cert-verify") {
		q.Set("insecure", "1")
	}
	return "hysteria://" + hostPort(n) + query(q) + fragment(n.Name)
}

func tuicShareLink(n Node) string {
	uuid := extraString(n, "uuid")
	password := extraString(n, "password")
	// tuic v5 以 uuid:password 作为凭据；两者缺一则无法连接
	if uuid == "" || password == "" {
		return ""
	}
	q := url.Values{}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		q.Set("sni", sni)
	}
	if v := extraString(n, "congestion-controller"); v != "" {
		q.Set("congestion_control", v)
	}
	if v := extraString(n, "udp-relay-mode"); v != "" {
		q.Set("udp_relay_mode", v)
	}
	if alpn := extraStringSlice(n, "alpn"); len(alpn) > 0 {
		q.Set("alpn", strings.Join(alpn, ","))
	}
	if extraBool(n, "skip-cert-verify") {
		q.Set("allow_insecure", "1")
	}
	cred := url.QueryEscape(uuid) + ":" + url.QueryEscape(password)
	return "tuic://" + cred + "@" + hostPort(n) + query(q) + fragment(n.Name)
}

func anytlsShareLink(n Node) string {
	password := extraString(n, "password")
	if password == "" {
		return ""
	}
	q := url.Values{}
	if sni := firstExtraString(n, "sni", "servername"); sni != "" {
		q.Set("sni", sni)
	}
	if extraBool(n, "skip-cert-verify") {
		q.Set("insecure", "1")
	}
	if n.UDP {
		q.Set("udp", "1")
	}
	return "anytls://" + url.QueryEscape(password) + "@" + hostPort(n) + query(q) + fragment(n.Name)
}

func socksShareLink(n Node) string {
	return userinfoShareLink(n, "socks5")
}

func httpShareLink(n Node) string {
	scheme := "http"
	// tls 为真时按 https 输出，否则客户端会以明文连接
	if extraBool(n, "tls") || strings.EqualFold(n.Type, "https") {
		scheme = "https"
	}
	return userinfoShareLink(n, scheme)
}

// userinfoShareLink 拼 scheme://[user:pass@]host:port#name，
// socks5 与 http/https 共用这一形式。
func userinfoShareLink(n Node, scheme string) string {
	user := firstExtraString(n, "username", "user")
	password := extraString(n, "password")
	auth := ""
	switch {
	case user != "" && password != "":
		auth = url.QueryEscape(user) + ":" + url.QueryEscape(password) + "@"
	case user != "":
		auth = url.QueryEscape(user) + "@"
	}
	return scheme + "://" + auth + hostPort(n) + fragment(n.Name)
}

// applyTransportQuery 把传输层参数写进分享链接的 query。
func applyTransportQuery(n Node, network string, q url.Values) {
	switch network {
	case "ws", "httpupgrade":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		if opts == nil {
			opts, _ = n.Extra["http-opts"].(map[string]interface{})
		}
		if path := mapString(opts, "path"); path != "" {
			q.Set("path", path)
		}
		if host := headerHost(opts); host != "" {
			q.Set("host", host)
		}
	case "grpc":
		opts, _ := n.Extra["grpc-opts"].(map[string]interface{})
		if name := mapString(opts, "grpc-service-name"); name != "" {
			q.Set("serviceName", name)
		}
	case "h2":
		opts, _ := n.Extra["h2-opts"].(map[string]interface{})
		if path := mapString(opts, "path"); path != "" {
			q.Set("path", path)
		}
		if hosts := stringSlice(mapValue(opts, "host")); len(hosts) > 0 {
			q.Set("host", strings.Join(hosts, ","))
		}
	}
}

// transportHostPath 取传输层的 host 与 path，供 vmess 的 JSON 形式使用。
func transportHostPath(n Node, network string) (string, string) {
	switch network {
	case "ws", "httpupgrade":
		opts, _ := n.Extra["ws-opts"].(map[string]interface{})
		if opts == nil {
			opts, _ = n.Extra["http-opts"].(map[string]interface{})
		}
		return headerHost(opts), mapString(opts, "path")
	case "grpc":
		opts, _ := n.Extra["grpc-opts"].(map[string]interface{})
		return "", mapString(opts, "grpc-service-name")
	case "h2":
		opts, _ := n.Extra["h2-opts"].(map[string]interface{})
		hosts := stringSlice(mapValue(opts, "host"))
		return strings.Join(hosts, ","), mapString(opts, "path")
	}
	return "", ""
}

// headerHost 从 xxx-opts.headers 里取 Host（大小写都可能出现）。
func headerHost(opts map[string]interface{}) string {
	if opts == nil {
		return ""
	}
	switch h := opts["headers"].(type) {
	case map[string]interface{}:
		for _, k := range []string{"Host", "host"} {
			if v, ok := h[k].(string); ok && v != "" {
				return v
			}
		}
	case map[string]string:
		for _, k := range []string{"Host", "host"} {
			if v := h[k]; v != "" {
				return v
			}
		}
	}
	return ""
}

func hostPort(n Node) string {
	// IPv6 字面量必须加方括号，否则 host:port 无法解析
	if strings.Contains(n.Server, ":") && !strings.HasPrefix(n.Server, "[") {
		return fmt.Sprintf("[%s]:%d", n.Server, n.Port)
	}
	return fmt.Sprintf("%s:%d", n.Server, n.Port)
}

func query(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// fragment 输出 #节点名。
//
// 刻意不用 url.QueryEscape：它会把中文与 emoji 全部转成 %XX，
// 而节点名里 emoji 国旗极常见，全量转义后链接对人不可读，
// 也与真实机场的输出习惯不符。
// 但解析侧按空格切分链接（见 parser.ParseShareLinks），因此空格与
// 换行必须转义，否则一个带空格的节点名会被拆成两条无效链接；
// '#' 也要转义，否则会被当成新的 fragment 起点。
func fragment(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('#')
	for _, r := range name {
		switch r {
		case ' ':
			b.WriteString("%20")
		case '#':
			b.WriteString("%23")
		case '\n':
			b.WriteString("%0A")
		case '\r':
			b.WriteString("%0D")
		case '\t':
			b.WriteString("%09")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

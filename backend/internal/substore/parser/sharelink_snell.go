package parser

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// parseSnell 解析 snell://
// snell://psk@host:port?version=4&obfs=http&obfs-host=xxx#name
func parseSnell(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("snell-%s-%d", h, p)
	}

	n := newNode(name, "snell", h, p, source)
	q := u.Query()

	psk := ""
	if u.User != nil {
		psk = u.User.Username()
	}
	if psk == "" {
		psk = q.Get("psk")
	}
	if psk == "" {
		return Node{}, fmt.Errorf("snell: missing psk")
	}
	n.Extra["psk"] = psk

	// Snell v1/v2/v3/v4，默认按 mihomo 习惯取 4
	version := q.Get("version")
	if version == "" {
		version = q.Get("v")
	}
	if version == "" {
		version = "4"
	}
	if iv, err := strconv.Atoi(version); err == nil && iv > 0 {
		n.Extra["version"] = iv
	}

	// obfs 相关参数在 mihomo 中收敛到 obfs-opts
	obfs := q.Get("obfs")
	if obfs == "" {
		obfs = q.Get("obfs-mode")
	}
	if obfs != "" && !strings.EqualFold(obfs, "none") {
		opts := map[string]interface{}{"mode": obfs}
		if host := firstNonEmpty(q.Get("obfs-host"), q.Get("host")); host != "" {
			opts["host"] = host
		}
		n.Extra["obfs-opts"] = opts
	}

	if v := q.Get("udp"); v == "1" || strings.EqualFold(v, "true") {
		n.UDP = true
	}
	return n, nil
}

// parseSSH 解析 ssh://
// ssh://user:password@host:port?privateKey=xxx&host-key-algorithms=xxx#name
func parseSSH(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPortDefault(u.Host, 22)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("ssh-%s-%d", h, p)
	}

	n := newNode(name, "ssh", h, p, source)
	if u.User != nil {
		n.Extra["username"] = u.User.Username()
		if pw, ok := u.User.Password(); ok && pw != "" {
			n.Extra["password"] = pw
		}
	}

	q := u.Query()
	if v := firstNonEmpty(q.Get("privateKey"), q.Get("private-key")); v != "" {
		key, _ := url.QueryUnescape(v)
		n.Extra["private-key"] = key
	}
	if v := firstNonEmpty(q.Get("privateKeyPassphrase"), q.Get("private-key-passphrase")); v != "" {
		n.Extra["private-key-passphrase"] = v
	}
	if v := firstNonEmpty(q.Get("hostKey"), q.Get("host-key")); v != "" {
		n.Extra["host-key"] = splitCSVTrimmed(v)
	}
	if v := firstNonEmpty(q.Get("hostKeyAlgorithms"), q.Get("host-key-algorithms")); v != "" {
		n.Extra["host-key-algorithms"] = splitCSVTrimmed(v)
	}
	return n, nil
}

// parseMieru 解析 mieru://
// mieru://username:password@host:port?transport=TCP&profile=xxx#name
// 也兼容 mieru://base64(username:password)@host:port#name
func parseMieru(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("mieru-%s-%d", h, p)
	}

	n := newNode(name, "mieru", h, p, source)

	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
		// 无显式密码时尝试 base64(user:pass) 形式
		if password == "" {
			if decoded, err := decodeB64(username); err == nil {
				if idx := strings.Index(string(decoded), ":"); idx > 0 {
					s := string(decoded)
					username, password = s[:idx], s[idx+1:]
				}
			}
		}
	}
	if username == "" || password == "" {
		return Node{}, fmt.Errorf("mieru: missing credentials")
	}
	n.Extra["username"] = username
	n.Extra["password"] = password

	q := u.Query()
	transport := q.Get("transport")
	if transport == "" {
		transport = "TCP"
	}
	n.Extra["transport"] = strings.ToUpper(transport)

	// mieru 支持 port-range 多端口跳跃，与 port 互斥
	// （上游会报 "port and port-range cannot be set at the same time"）。
	// 端口存在 Node.Port 字段而非 Extra 里，必须清零结构体字段；
	// 否则渲染时照样写出 port，内核直接拒绝加载。
	if v := firstNonEmpty(q.Get("port-range"), q.Get("portRange")); v != "" {
		n.Extra["port-range"] = v
		n.Port = 0
	}
	if v := q.Get("multiplexing"); v != "" {
		n.Extra["multiplexing"] = v
	}
	return n, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// splitHostPortDefault 在链接省略端口时回落到协议默认端口
func splitHostPortDefault(hostport string, def int) (string, int, error) {
	if h, p, err := splitHostPort(hostport); err == nil {
		return h, p, nil
	}
	h := strings.Trim(hostport, "[]")
	if h == "" {
		return "", 0, fmt.Errorf("invalid hostport")
	}
	return h, def, nil
}

func splitCSVTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

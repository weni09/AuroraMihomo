package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func ParseShareLinks(raw string, source string) ([]Node, error) {
	// support plain multi-line and base64 bulk
	text := strings.TrimSpace(raw)
	if dec, err := decodeB64(text); err == nil {
		ds := strings.TrimSpace(string(dec))
		if strings.Contains(ds, "://") {
			text = ds
		}
	}
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' || r == ' ' })
	out := make([]Node, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := parseOneShareLink(line, source)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid share links")
	}
	return out, nil
}

func parseOneShareLink(link, source string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, err
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "ss":
		return parseSS(u, source)
	case "ssr":
		return parseSSR(link, source)
	case "vmess":
		return parseVMess(link, source)
	case "vless":
		return parseVLESS(u, source)
	case "trojan":
		return parseTrojan(u, source)
	case "hysteria2", "hy2":
		return parseHysteria2(u, source)
	case "hysteria", "hy":
		return parseHysteria(u, source)
	case "tuic":
		return parseTUIC(u, source)
	case "wireguard", "wg":
		return parseWireGuard(u, source)
	case "anytls":
		return parseAnyTLS(u, source)
	case "socks5", "socks", "socks5+tls":
		return parseSocks(u, source)
	case "snell":
		return parseSnell(u, source)
	case "ssh":
		return parseSSH(u, source)
	case "mieru":
		return parseMieru(u, source)
	case "http", "https":
		return parseHTTPProxy(u, source)
	default:
		return Node{}, fmt.Errorf("unsupported scheme %s", scheme)
	}
}

func parseSS(u *url.URL, source string) (Node, error) {
	// ss://base64(method:pass)@host:port#name or ss://base64(method:pass@host:port)
	name, _ := url.QueryUnescape(u.Fragment)
	host := u.Host
	method, password := "", ""
	if u.User != nil {
		// user may be base64 method:pass
		user := u.User.String()
		if dec, err := decodeB64(user); err == nil && strings.Contains(string(dec), ":") {
			parts := strings.SplitN(string(dec), ":", 2)
			method, password = parts[0], parts[1]
		} else {
			method = u.User.Username()
			password, _ = u.User.Password()
		}
	} else {
		// all-in-base64
		payload := strings.TrimPrefix(u.String(), "ss://")
		payload = strings.SplitN(payload, "#", 2)[0]
		dec, err := decodeB64(payload)
		if err != nil {
			return Node{}, err
		}
		// method:pass@host:port
		sp := strings.SplitN(string(dec), "@", 2)
		if len(sp) != 2 {
			return Node{}, fmt.Errorf("invalid ss link")
		}
		mp := strings.SplitN(sp[0], ":", 2)
		if len(mp) != 2 {
			return Node{}, fmt.Errorf("invalid ss method/pass")
		}
		method, password = mp[0], mp[1]
		host = sp[1]
	}
	h, p, err := splitHostPort(host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("ss-%s-%d", h, p)
	}
	n := newNode(name, "ss", h, p, source)
	n.Extra["cipher"] = method
	n.Extra["password"] = password
	return n, nil
}

func parseSSR(link, source string) (Node, error) {
	payload := strings.TrimPrefix(link, "ssr://")
	dec, err := decodeB64(payload)
	if err != nil {
		return Node{}, err
	}
	// host:port:protocol:method:obfs:password_base64/?params
	main := string(dec)
	parts := strings.SplitN(main, "/?", 2)
	head := strings.Split(parts[0], ":")
	if len(head) < 6 {
		return Node{}, fmt.Errorf("invalid ssr")
	}
	pass, _ := decodeB64(head[5])
	port, _ := strconv.Atoi(head[1])
	name := fmt.Sprintf("ssr-%s-%d", head[0], port)
	if len(parts) == 2 {
		q, _ := url.ParseQuery(parts[1])
		if rn := q.Get("remarks"); rn != "" {
			if d, err := decodeB64(rn); err == nil {
				name = string(d)
			}
		}
	}
	n := newNode(name, "ssr", head[0], port, source)
	n.Extra["protocol"] = head[2]
	n.Extra["cipher"] = head[3]
	n.Extra["obfs"] = head[4]
	n.Extra["password"] = string(pass)
	return n, nil
}

func parseVMess(link, source string) (Node, error) {
	payload := strings.TrimPrefix(link, "vmess://")
	dec, err := decodeB64(payload)
	if err != nil {
		return Node{}, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(dec, &m); err != nil {
		return Node{}, err
	}
	name, _ := m["ps"].(string)
	server, _ := m["add"].(string)
	port := asInt(m["port"])
	if name == "" {
		name = fmt.Sprintf("vmess-%s-%d", server, port)
	}
	n := newNode(name, "vmess", server, port, source)
	n.Extra["uuid"] = fmt.Sprintf("%v", m["id"])
	n.Extra["alterId"] = asInt(m["aid"])
	n.Extra["cipher"] = firstString(m["scy"], "auto")
	n.Extra["network"] = firstString(m["net"], "tcp")
	n.Extra["tls"] = firstString(m["tls"], "") != ""
	if host, ok := m["host"].(string); ok && host != "" {
		n.Extra["servername"] = host
	}
	return n, nil
}

func parseVLESS(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("vless-%s-%d", h, p)
	}
	n := newNode(name, "vless", h, p, source)
	n.Extra["uuid"] = u.User.Username()
	q := u.Query()

	// Security
	security := q.Get("security")
	if security != "" {
		if security == "tls" || security == "reality" {
			n.Extra["tls"] = true
		}
		if security == "reality" {
			n.Extra["servername"] = q.Get("sni")
			realityOpts := map[string]interface{}{}
			if pbk := q.Get("pbk"); pbk != "" {
				realityOpts["public-key"] = pbk
			}
			if sid := q.Get("sid"); sid != "" {
				realityOpts["short-id"] = sid
			}
			n.Extra["reality-opts"] = realityOpts
		} else {
			n.Extra["servername"] = q.Get("sni")
		}
	} else if sni := q.Get("sni"); sni != "" {
		n.Extra["servername"] = sni
	}

	// Flow
	if flow := q.Get("flow"); flow != "" {
		n.Extra["flow"] = flow
	}

	// Network / Transport
	network := q.Get("type")
	if network != "" {
		n.Extra["network"] = network
		switch network {
		case "ws":
			wsOpts := map[string]interface{}{}
			if path := q.Get("path"); path != "" {
				wsOpts["path"] = path
			}
			if host := q.Get("host"); host != "" {
				wsOpts["headers"] = map[string]string{"Host": host}
			}
			n.Extra["ws-opts"] = wsOpts
		case "grpc":
			grpcOpts := map[string]interface{}{}
			if serviceName := q.Get("serviceName"); serviceName != "" {
				grpcOpts["grpc-service-name"] = serviceName
			}
			n.Extra["grpc-opts"] = grpcOpts
		}
	}

	if alpn := q.Get("alpn"); alpn != "" {
		n.Extra["alpn"] = strings.Split(alpn, ",")
	}

	return n, nil
}

func parseTrojan(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("trojan-%s-%d", h, p)
	}
	n := newNode(name, "trojan", h, p, source)
	n.Extra["password"] = u.User.Username()

	q := u.Query()
	if sni := q.Get("sni"); sni != "" {
		n.Extra["sni"] = sni
	}

	network := q.Get("type")
	if network != "" && network != "tcp" {
		n.Extra["network"] = network
		switch network {
		case "ws":
			wsOpts := map[string]interface{}{}
			if path := q.Get("path"); path != "" {
				wsOpts["path"] = path
			}
			if host := q.Get("host"); host != "" {
				wsOpts["headers"] = map[string]string{"Host": host}
			}
			n.Extra["ws-opts"] = wsOpts
		case "grpc":
			grpcOpts := map[string]interface{}{}
			if serviceName := q.Get("serviceName"); serviceName != "" {
				grpcOpts["grpc-service-name"] = serviceName
			}
			n.Extra["grpc-opts"] = grpcOpts
		}
	}

	if alpn := q.Get("alpn"); alpn != "" {
		n.Extra["alpn"] = strings.Split(alpn, ",")
	}

	return n, nil
}

func parseHysteria2(u *url.URL, source string) (Node, error) {
	name, _ := url.QueryUnescape(u.Fragment)
	h, p, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, err
	}
	if name == "" {
		name = fmt.Sprintf("hy2-%s-%d", h, p)
	}
	n := newNode(name, "hysteria2", h, p, source)
	n.Extra["password"] = u.User.Username()
	if sni := u.Query().Get("sni"); sni != "" {
		n.Extra["sni"] = sni
	}
	return n, nil
}

func splitHostPort(hostport string) (string, int, error) {
	// handle ipv6 [..]
	if strings.HasPrefix(hostport, "[") {
		i := strings.LastIndex(hostport, "]:")
		if i < 0 {
			return "", 0, fmt.Errorf("invalid hostport")
		}
		h := hostport[1:i]
		p, err := strconv.Atoi(hostport[i+2:])
		return h, p, err
	}
	i := strings.LastIndex(hostport, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("invalid hostport")
	}
	p, err := strconv.Atoi(hostport[i+1:])
	return hostport[:i], p, err
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

func firstString(v interface{}, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

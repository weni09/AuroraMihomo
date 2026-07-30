package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSurge parses lines like:
// ProxySS = ss, 1.2.3.4, 443, encrypt-method=aes-128-gcm, password=pwd
func ParseSurge(raw, source string) ([]Node, error) {
	out := make([]Node, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		name := strings.TrimSpace(kv[0])
		rest := strings.TrimSpace(kv[1])
		parts := splitCSV(rest)
		if len(parts) < 3 {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(parts[0]))
		server := strings.TrimSpace(parts[1])
		port, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		if server == "" || port == 0 {
			continue
		}
		mapped := mapSurgeType(typ)
		if mapped == "" {
			continue
		}
		n := newNode(name, mapped, server, port, source)
		for _, p := range parts[3:] {
			if !strings.Contains(p, "=") {
				continue
			}
			pp := strings.SplitN(p, "=", 2)
			k := strings.TrimSpace(pp[0])
			v := strings.TrimSpace(pp[1])
			switch k {
			case "encrypt-method", "method":
				n.Extra["cipher"] = v
			case "password":
				n.Extra["password"] = v
			case "udp-relay":
				n.UDP = strings.EqualFold(v, "true")
			case "sni":
				n.Extra["sni"] = v
			default:
				n.Extra[k] = v
			}
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no surge proxies")
	}
	return out, nil
}

// ParseQuantumultX parses lines like:
// shadowsocks=1.2.3.4:443, method=aes-128-gcm, password=pwd, tag=HK
func ParseQuantumultX(raw, source string) ([]Node, error) {
	out := make([]Node, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := splitCSV(line)
		if len(parts) == 0 {
			continue
		}
		head := strings.SplitN(parts[0], "=", 2)
		if len(head) != 2 {
			continue
		}
		typ := mapQXType(strings.TrimSpace(head[0]))
		hostport := strings.TrimSpace(head[1])
		if typ == "" || hostport == "" {
			continue
		}
		h, p, err := parseHostPortSimple(hostport)
		if err != nil {
			continue
		}
		n := newNode("", typ, h, p, source)
		for _, part := range parts[1:] {
			if !strings.Contains(part, "=") {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			switch k {
			case "tag":
				n.Name = v
			case "method":
				n.Extra["cipher"] = v
			case "password":
				n.Extra["password"] = v
			case "obfs":
				n.Extra["obfs"] = v
			default:
				n.Extra[k] = v
			}
		}
		if n.Name == "" {
			n.Name = fmt.Sprintf("%s-%s-%d", typ, h, p)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no quantumultx proxies")
	}
	return out, nil
}

func mapSurgeType(t string) string {
	switch t {
	case "ss", "shadowsocks":
		return "ss"
	case "vmess":
		return "vmess"
	case "trojan":
		return "trojan"
	case "http", "https", "socks5":
		return t
	default:
		return ""
	}
}

func mapQXType(t string) string {
	switch strings.ToLower(t) {
	case "shadowsocks", "ss":
		return "ss"
	case "vmess":
		return "vmess"
	case "trojan":
		return "trojan"
	case "http", "socks5":
		return strings.ToLower(t)
	default:
		return ""
	}
}

func splitCSV(s string) []string {
	out := []string{}
	cur := strings.Builder{}
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	out = append(out, cur.String())
	return out
}

func parseHostPortSimple(s string) (string, int, error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("bad hostport")
	}
	p, err := strconv.Atoi(s[i+1:])
	return s[:i], p, err
}

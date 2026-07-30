package substore

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

type Format string

const (
	FormatClashYAML   Format = "clash-yaml"
	FormatV2RayJSON   Format = "v2ray-json"
	FormatShareLinks  Format = "share-links"
	FormatSIP008      Format = "sip008"
	FormatSurge       Format = "surge"
	FormatQuantumultX Format = "quantumultx"
	FormatUnknown     Format = "unknown"
)

func DetectFormat(raw []byte) Format {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return FormatUnknown
	}

	// base64 bulk links
	if decoded, err := decodeMaybeBase64(s); err == nil {
		ds := strings.TrimSpace(string(decoded))
		if looksLikeShareLinks(ds) {
			return FormatShareLinks
		}
		// recurse on decoded content once
		if f := DetectFormat(decoded); f != FormatUnknown && f != FormatShareLinks {
			return f
		}
	}

	if looksLikeShareLinks(s) {
		return FormatShareLinks
	}

	// json
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var anyJSON interface{}
		if json.Unmarshal([]byte(s), &anyJSON) == nil {
			// sip008: {"version":1,"servers":[...]}
			if m, ok := anyJSON.(map[string]interface{}); ok {
				if _, hasServers := m["servers"]; hasServers {
					return FormatSIP008
				}
				if _, hasOutbounds := m["outbounds"]; hasOutbounds {
					return FormatV2RayJSON
				}
			}
			if arr, ok := anyJSON.([]interface{}); ok && len(arr) > 0 {
				if m, ok := arr[0].(map[string]interface{}); ok {
					if _, ok := m["server"]; ok {
						// could be sip008 servers array-ish or custom
						if _, ok2 := m["method"]; ok2 {
							return FormatSIP008
						}
					}
				}
			}
			return FormatV2RayJSON
		}
	}

	// surge / qx lines
	if strings.Contains(s, "= ss") || strings.Contains(s, "= vmess") || strings.Contains(s, "= trojan") || strings.Contains(s, "= shadowsocks") {
		if strings.Contains(s, "tag=") || strings.Contains(s, "obfs=") {
			return FormatQuantumultX
		}
		return FormatSurge
	}

	// yaml clash
	var y map[string]interface{}
	if yaml.Unmarshal([]byte(s), &y) == nil {
		if _, ok := y["proxies"]; ok {
			return FormatClashYAML
		}
		if _, ok := y["proxy-groups"]; ok {
			return FormatClashYAML
		}
	}
	return FormatUnknown
}

func decodeMaybeBase64(s string) ([]byte, error) {
	clean := strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
	clean = strings.TrimSpace(clean)
	// rough heuristic
	if len(clean) < 16 {
		return nil, base64.CorruptInputError(0)
	}
	if dec, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return dec, nil
	}
	return base64.RawStdEncoding.DecodeString(clean)
}

func looksLikeShareLinks(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "ss://") ||
		strings.Contains(ls, "ssr://") ||
		strings.Contains(ls, "vmess://") ||
		strings.Contains(ls, "vless://") ||
		strings.Contains(ls, "trojan://") ||
		strings.Contains(ls, "hysteria2://") ||
		strings.Contains(ls, "hy2://") ||
		strings.Contains(ls, "hysteria://") ||
		strings.Contains(ls, "tuic://") ||
		strings.Contains(ls, "wireguard://") ||
		strings.Contains(ls, "wg://") ||
		strings.Contains(ls, "anytls://") ||
		strings.Contains(ls, "socks5://")
}

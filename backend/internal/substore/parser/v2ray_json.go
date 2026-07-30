package parser

import (
	"encoding/json"
	"fmt"
)

func ParseV2RayJSON(raw []byte, source string) ([]Node, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		// maybe array of outbounds
		var arr []map[string]interface{}
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			return nil, err
		}
		return outboundsToNodes(arr, source)
	}
	if outs, ok := root["outbounds"].([]interface{}); ok {
		arr := make([]map[string]interface{}, 0, len(outs))
		for _, o := range outs {
			if m, ok := o.(map[string]interface{}); ok {
				arr = append(arr, m)
			}
		}
		return outboundsToNodes(arr, source)
	}
	return nil, fmt.Errorf("no outbounds in v2ray json")
}

func outboundsToNodes(arr []map[string]interface{}, source string) ([]Node, error) {
	out := make([]Node, 0)
	for _, m := range arr {
		protocol, _ := m["protocol"].(string)
		protocol = stringsToLower(protocol)
		if protocol == "freedom" || protocol == "blackhole" || protocol == "dns" || protocol == "direct" {
			continue
		}
		tag, _ := m["tag"].(string)
		settings, _ := m["settings"].(map[string]interface{})
		vnext, _ := settings["vnext"].([]interface{})
		servers, _ := settings["servers"].([]interface{})
		switch protocol {
		case "vmess", "vless":
			if len(vnext) == 0 {
				continue
			}
			vn, _ := vnext[0].(map[string]interface{})
			addr, _ := vn["address"].(string)
			port := asInt(vn["port"])
			users, _ := vn["users"].([]interface{})
			uuid := ""
			if len(users) > 0 {
				if u, ok := users[0].(map[string]interface{}); ok {
					uuid = fmt.Sprintf("%v", u["id"])
				}
			}
			name := tag
			if name == "" {
				name = fmt.Sprintf("%s-%s-%d", protocol, addr, port)
			}
			n := newNode(name, protocol, addr, port, source)
			n.Extra["uuid"] = uuid
			out = append(out, n)
		case "trojan", "shadowsocks":
			if len(servers) == 0 {
				continue
			}
			sv, _ := servers[0].(map[string]interface{})
			addr, _ := sv["address"].(string)
			if addr == "" {
				addr, _ = sv["server"].(string)
			}
			port := asInt(sv["port"])
			name := tag
			if name == "" {
				name = fmt.Sprintf("%s-%s-%d", protocol, addr, port)
			}
			typ := protocol
			if protocol == "shadowsocks" {
				typ = "ss"
			}
			n := newNode(name, typ, addr, port, source)
			if protocol == "trojan" {
				n.Extra["password"] = fmt.Sprintf("%v", sv["password"])
			} else {
				n.Extra["cipher"] = fmt.Sprintf("%v", firstNonNil(sv["method"], sv["cipher"]))
				n.Extra["password"] = fmt.Sprintf("%v", sv["password"])
			}
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no supported outbounds")
	}
	return out, nil
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func firstNonNil(vs ...interface{}) interface{} {
	for _, v := range vs {
		if v != nil {
			return v
		}
	}
	return ""
}

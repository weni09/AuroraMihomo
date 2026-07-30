package parser

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func ParseClashYAML(raw []byte, source string) ([]Node, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	arr, _ := root["proxies"].([]interface{})
	out := make([]Node, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		typ, _ := m["type"].(string)
		server, _ := m["server"].(string)
		port := asInt(m["port"])
		if name == "" || typ == "" || server == "" || port == 0 {
			continue
		}
		n := newNode(name, typ, server, port, source)
		if udp, ok := m["udp"].(bool); ok {
			n.UDP = udp
		}
		for k, v := range m {
			if k == "name" || k == "type" || k == "server" || k == "port" || k == "udp" {
				continue
			}
			n.Extra[k] = v
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no proxies found in clash yaml")
	}
	return out, nil
}

func asInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		var x int
		// 解析失败时 x 保持 0，与 default 分支的行为一致
		if _, err := fmt.Sscanf(t, "%d", &x); err != nil {
			return 0
		}
		return x
	default:
		return 0
	}
}

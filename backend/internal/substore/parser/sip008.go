package parser

import (
	"encoding/json"
	"fmt"
)

func ParseSIP008(raw []byte, source string) ([]Node, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	servers, _ := root["servers"].([]interface{})
	out := make([]Node, 0, len(servers))
	for _, s := range servers {
		m, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		server, _ := m["server"].(string)
		port := asInt(m["server_port"])
		if port == 0 {
			port = asInt(m["port"])
		}
		method, _ := m["method"].(string)
		password, _ := m["password"].(string)
		name, _ := m["remarks"].(string)
		if name == "" {
			name = fmt.Sprintf("ss-%s-%d", server, port)
		}
		if server == "" || port == 0 {
			continue
		}
		n := newNode(name, "ss", server, port, source)
		n.Extra["cipher"] = method
		n.Extra["password"] = password
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no sip008 servers")
	}
	return out, nil
}

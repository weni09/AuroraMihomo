package substore

import (
	"context"
	"strings"
	"testing"
)

// ===== 扩展协议解析 =====

func TestParseExtendedProtocols(t *testing.T) {
	cases := []struct {
		name     string
		link     string
		wantType string
		check    func(t *testing.T, n Node)
	}{
		{
			name:     "hysteria v1",
			link:     "hysteria://1.1.1.1:443?auth=mypass&peer=example.com&insecure=1&upmbps=100&downmbps=200#HY-Node",
			wantType: "hysteria",
			check: func(t *testing.T, n Node) {
				if n.Extra["auth-str"] != "mypass" {
					t.Errorf("auth-str 解析失败: %v", n.Extra["auth-str"])
				}
				if n.Extra["sni"] != "example.com" {
					t.Errorf("sni 解析失败: %v", n.Extra["sni"])
				}
				if n.Extra["skip-cert-verify"] != true {
					t.Error("insecure 未映射为 skip-cert-verify")
				}
			},
		},
		{
			name:     "tuic v5",
			link:     "tuic://uuid-123:pass456@2.2.2.2:8443?sni=tuic.example.com&congestion_control=bbr&alpn=h3#TUIC-Node",
			wantType: "tuic",
			check: func(t *testing.T, n Node) {
				if n.Extra["uuid"] != "uuid-123" {
					t.Errorf("uuid 解析失败: %v", n.Extra["uuid"])
				}
				if n.Extra["password"] != "pass456" {
					t.Errorf("password 解析失败: %v", n.Extra["password"])
				}
				if n.Extra["congestion-controller"] != "bbr" {
					t.Errorf("拥塞控制解析失败: %v", n.Extra["congestion-controller"])
				}
			},
		},
		{
			name:     "wireguard",
			link:     "wireguard://privkey123@3.3.3.3:51820?publickey=pubkey456&address=10.0.0.2/32&mtu=1420#WG-Node",
			wantType: "wireguard",
			check: func(t *testing.T, n Node) {
				if n.Extra["private-key"] != "privkey123" {
					t.Errorf("private-key 解析失败: %v", n.Extra["private-key"])
				}
				if n.Extra["public-key"] != "pubkey456" {
					t.Errorf("public-key 解析失败: %v", n.Extra["public-key"])
				}
				if n.Extra["ip"] != "10.0.0.2" {
					t.Errorf("address 解析失败: %v", n.Extra["ip"])
				}
			},
		},
		{
			name:     "anytls",
			link:     "anytls://mypassword@4.4.4.4:8443?sni=any.example.com&insecure=1#AnyTLS-Node",
			wantType: "anytls",
			check: func(t *testing.T, n Node) {
				if n.Extra["password"] != "mypassword" {
					t.Errorf("password 解析失败: %v", n.Extra["password"])
				}
				if n.Extra["skip-cert-verify"] != true {
					t.Error("insecure 未映射")
				}
			},
		},
		{
			name:     "socks5",
			link:     "socks5://user:pass@5.5.5.5:1080#SOCKS-Node",
			wantType: "socks5",
			check: func(t *testing.T, n Node) {
				if n.Extra["username"] != "user" || n.Extra["password"] != "pass" {
					t.Errorf("认证信息解析失败: %v", n.Extra)
				}
			},
		},
	}

	e := NewEngine()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := e.Convert(context.Background(),
				ConvertRequest{Content: c.link}, nil, nil, "mihomo-yaml", "")
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(res.Nodes) != 1 {
				t.Fatalf("期望 1 个节点，实际 %d", len(res.Nodes))
			}
			n := res.Nodes[0]
			if n.Type != c.wantType {
				t.Fatalf("类型错误: 期望 %s，实际 %s", c.wantType, n.Type)
			}
			c.check(t, n)
		})
	}
}

// ===== 区域过滤器 =====

func TestApplyRegionFilterKeep(t *testing.T) {
	nodes := []Node{
		{Name: "🇭🇰 香港 01", Server: "1.1.1.1", Port: 443},
		{Name: "JP-Tokyo-02", Server: "2.2.2.2", Port: 443},
		{Name: "US-LA-03", Server: "3.3.3.3", Port: 443},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegion,
		Enabled: true,
		Payload: map[string]interface{}{
			"action":  "keep",
			"regions": []interface{}{"HK", "JP"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("区域保留失败，期望 2 个，实际 %d: %v", len(out), names(out))
	}
	for _, n := range out {
		if strings.Contains(n.Name, "US") {
			t.Fatal("US 节点应被剔除")
		}
	}
}

func TestApplyRegionFilterDrop(t *testing.T) {
	nodes := []Node{
		{Name: "🇭🇰 香港 01", Server: "1.1.1.1", Port: 443},
		{Name: "US-LA-03", Server: "3.3.3.3", Port: 443},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegion,
		Enabled: true,
		Payload: map[string]interface{}{
			"action":  "drop",
			"regions": []interface{}{"US"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Name, "香港") {
		t.Fatalf("区域剔除失败: %v", names(out))
	}
}

// ===== 新输出目标 =====

func TestNewOutputTargets(t *testing.T) {
	nodes := []Node{
		{
			Name: "HK-01", Type: "ss", Server: "1.1.1.1", Port: 443,
			Extra: map[string]interface{}{"cipher": "aes-128-gcm", "password": "pwd"},
		},
	}

	targets := []struct {
		name     string
		contains string
	}{
		{"stash", "proxies:"},
		{"surfboard", "HK-01 = ss"},
		{"shadowrocket", "ss://"},
		{"egern", "shadowsocks:"},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := RenderTemplate(tc.name, "", nodes)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			if !strings.Contains(out, tc.contains) {
				t.Fatalf("输出不含 %q:\n%s", tc.contains, out)
			}
		})
	}
}

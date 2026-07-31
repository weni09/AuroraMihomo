package parser

import "testing"

func TestParseShareSS(t *testing.T) {
	// method:pass = aes-128-gcm:pwd => YWVzLTEyOC1nY206cHdk
	nodes, err := ParseShareLinks("ss://YWVzLTEyOC1nY206cHdk@1.2.3.4:443#NodeA", "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Type != "ss" || nodes[0].Server != "1.2.3.4" {
		t.Fatalf("%+v", nodes)
	}
}

func TestParseSIP008(t *testing.T) {
	raw := []byte(`{"version":1,"servers":[{"server":"9.9.9.9","server_port":8388,"method":"aes-256-gcm","password":"x","remarks":"S1"}]}`)
	nodes, err := ParseSIP008(raw, "t")
	if err != nil || len(nodes) != 1 || nodes[0].Name != "S1" {
		t.Fatalf("err=%v nodes=%+v", err, nodes)
	}
}

func TestParseSurge(t *testing.T) {
	raw := "ProxySS = ss, 8.8.8.8, 443, encrypt-method=aes-128-gcm, password=pwd"
	nodes, err := ParseSurge(raw, "t")
	if err != nil || len(nodes) != 1 || nodes[0].Type != "ss" {
		t.Fatalf("err=%v nodes=%+v", err, nodes)
	}
}

// TestParseShareLinkClientFingerprint 覆盖曾经被漏读的 fp 参数。
// 漏读的后果不是少一个字段：reality 节点没有 uTLS 指纹就无法握手，
// 表现为「订阅拉到了、节点也在、一连就失败」。
func TestParseShareLinkClientFingerprint(t *testing.T) {
	cases := []struct {
		name string
		link string
	}{
		{
			"vless reality",
			"vless://1ff35761@cdsa.650998.xyz:42345?encryption=none&security=reality&sni=www.icloud.com&pbk=PUB&sid=93c8&fp=chrome&type=tcp&flow=xtls-rprx-vision#Dmit",
		},
		{
			"trojan",
			"trojan://pwd@t.example.com:443?sni=t.example.com&fp=firefox#T1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, err := ParseShareLinks(c.link, "t")
			if err != nil {
				t.Fatal(err)
			}
			if len(nodes) != 1 {
				t.Fatalf("期望 1 个节点，实际 %d", len(nodes))
			}
			if got := nodes[0].Extra["client-fingerprint"]; got == "" || got == nil {
				t.Errorf("fp 未解析成 client-fingerprint: %#v", nodes[0].Extra)
			}
		})
	}
}

// TestParseVMessFingerprint 单列出来：vmess 是 base64(JSON) 形式，
// 与上面的 query 参数走完全不同的解析分支。
func TestParseVMessFingerprint(t *testing.T) {
	// {"v":"2","ps":"V1","add":"v.example.com","port":"443","id":"uuid-1","net":"tcp","fp":"safari"}
	link := "vmess://eyJ2IjoiMiIsInBzIjoiVjEiLCJhZGQiOiJ2LmV4YW1wbGUuY29tIiwicG9ydCI6IjQ0MyIsImlkIjoidXVpZC0xIiwibmV0IjoidGNwIiwiZnAiOiJzYWZhcmkifQ"
	nodes, err := ParseShareLinks(link, "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("期望 1 个节点，实际 %d", len(nodes))
	}
	if got := nodes[0].Extra["client-fingerprint"]; got != "safari" {
		t.Errorf("vmess fp 未解析: %#v", nodes[0].Extra)
	}
}

// TestParseShareLinkWithoutFingerprint 确认不凭空造字段：
// 解析阶段只如实反映原文，补默认值是输出阶段的事（见 substore/fingerprint.go）。
func TestParseShareLinkWithoutFingerprint(t *testing.T) {
	nodes, err := ParseShareLinks("vless://u@a.example.com:443?security=tls&sni=a.example.com#N", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := nodes[0].Extra["client-fingerprint"]; ok {
		t.Errorf("原文无 fp 时不应写入该键: %#v", nodes[0].Extra)
	}
}

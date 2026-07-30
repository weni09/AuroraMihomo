package parser

import (
	"testing"
)

func TestParseSnellSSHMieru(t *testing.T) {
	links := "snell://mypsk@1.2.3.4:44046?version=4&obfs=http&obfs-host=bing.com#Snell%E8%8A%82%E7%82%B9\n" +
		"ssh://root:toor@5.6.7.8:2222#SSH%E8%8A%82%E7%82%B9\n" +
		"mieru://user1:pass1@9.9.9.9:7788?transport=TCP#Mieru%E8%8A%82%E7%82%B9\n"

	nodes, err := ParseShareLinks(links, "test")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("期望 3 个节点，实际 %d", len(nodes))
	}

	snell := nodes[0]
	if snell.Type != "snell" || snell.Name != "Snell节点" || snell.Port != 44046 {
		t.Fatalf("snell 基础字段错误: %+v", snell)
	}
	if snell.Extra["psk"] != "mypsk" {
		t.Fatalf("snell psk 错误: %v", snell.Extra["psk"])
	}
	if snell.Extra["version"] != 4 {
		t.Fatalf("snell version 应为 int 4, 实际 %#v", snell.Extra["version"])
	}
	obfs, ok := snell.Extra["obfs-opts"].(map[string]interface{})
	if !ok || obfs["mode"] != "http" || obfs["host"] != "bing.com" {
		t.Fatalf("snell obfs-opts 错误: %#v", snell.Extra["obfs-opts"])
	}

	ssh := nodes[1]
	if ssh.Type != "ssh" || ssh.Port != 2222 {
		t.Fatalf("ssh 基础字段错误: %+v", ssh)
	}
	if ssh.Extra["username"] != "root" || ssh.Extra["password"] != "toor" {
		t.Fatalf("ssh 凭据错误: %+v", ssh.Extra)
	}

	mieru := nodes[2]
	if mieru.Type != "mieru" || mieru.Port != 7788 {
		t.Fatalf("mieru 基础字段错误: %+v", mieru)
	}
	if mieru.Extra["username"] != "user1" || mieru.Extra["password"] != "pass1" {
		t.Fatalf("mieru 凭据错误: %+v", mieru.Extra)
	}
	if mieru.Extra["transport"] != "TCP" {
		t.Fatalf("mieru transport 错误: %v", mieru.Extra["transport"])
	}
}

// SSH 省略端口时应回落到 22
func TestParseSSHDefaultPort(t *testing.T) {
	nodes, err := ParseShareLinks("ssh://admin:pwd@10.0.0.1#NoPort\n", "test")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if nodes[0].Port != 22 {
		t.Fatalf("期望默认端口 22，实际 %d", nodes[0].Port)
	}
}

// mieru 端口跳跃时应输出 port-range 而非固定 port
func TestParseMieruPortRange(t *testing.T) {
	nodes, err := ParseShareLinks("mieru://u:p@1.1.1.1:2000?port-range=2000-3000#PR\n", "test")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if nodes[0].Extra["port-range"] != "2000-3000" {
		t.Fatalf("port-range 缺失: %+v", nodes[0].Extra)
	}
}

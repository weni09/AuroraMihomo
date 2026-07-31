package substore

import (
	"encoding/base64"
	"strings"
	"testing"

	"auroramihomo/backend/internal/substore/parser"
)

// exportRoundTrip 是最有力的正确性检查：导出的分享链接必须能被本项目的
// 解析器重新解析回等价节点。只断言"输出非空"会漏掉字段名写错、
// 转义缺失这类问题——链接看着有内容，客户端却连不上。
func exportRoundTrip(t *testing.T, n Node) parser.Node {
	t.Helper()
	link := nodeToShareLink(n)
	if link == "" {
		t.Fatalf("%s 节点未能导出分享链接", n.Type)
	}
	got, err := parser.ParseShareLinks(link, "test")
	if err != nil {
		t.Fatalf("导出的链接无法被解析回节点: %v\nlink=%s", err, link)
	}
	if len(got) != 1 {
		t.Fatalf("期望解析出 1 个节点，实际 %d 个\nlink=%s", len(got), link)
	}
	round := got[0]
	if round.Server != n.Server {
		t.Errorf("server 不一致: got %q want %q\nlink=%s", round.Server, n.Server, link)
	}
	if round.Port != n.Port {
		t.Errorf("port 不一致: got %d want %d\nlink=%s", round.Port, n.Port, link)
	}
	if round.Name != n.Name {
		t.Errorf("name 不一致: got %q want %q\nlink=%s", round.Name, n.Name, link)
	}
	return round
}

func TestShareLinkExportSS(t *testing.T) {
	n := Node{Name: "香港 01", Type: "ss", Server: "a.example.com", Port: 8443,
		Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p@ss:word"}}
	round := exportRoundTrip(t, n)
	if round.Extra["cipher"] != "aes-256-gcm" {
		t.Errorf("cipher 丢失: %#v", round.Extra["cipher"])
	}
	if round.Extra["password"] != "p@ss:word" {
		t.Errorf("password 丢失或被截断: %#v", round.Extra["password"])
	}
}

func TestShareLinkExportVMess(t *testing.T) {
	n := Node{Name: "vmess-ws", Type: "vmess", Server: "b.example.com", Port: 443,
		Extra: map[string]interface{}{
			"uuid": "11111111-2222-3333-4444-555555555555", "alterId": 0,
			"cipher": "auto", "network": "ws", "tls": true,
			"ws-opts": map[string]interface{}{
				"path":    "/ray",
				"headers": map[string]string{"Host": "cdn.example.com"},
			},
		}}
	round := exportRoundTrip(t, n)
	if round.Extra["uuid"] != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("uuid 丢失: %#v", round.Extra["uuid"])
	}
	if round.Extra["network"] != "ws" {
		t.Errorf("network 丢失: %#v", round.Extra["network"])
	}
	if tls, _ := round.Extra["tls"].(bool); !tls {
		t.Errorf("tls 丢失: %#v", round.Extra["tls"])
	}
}

func TestShareLinkExportVLESSReality(t *testing.T) {
	n := Node{Name: "vless reality", Type: "vless", Server: "c.example.com", Port: 42345,
		Extra: map[string]interface{}{
			"uuid": "1ff35761-7e07-4724-bc72-58160ea5fe8c",
			"tls":  true, "network": "tcp", "flow": "xtls-rprx-vision",
			"servername": "www.icloud.com",
			"reality-opts": map[string]interface{}{
				"public-key": "PUBKEY123", "short-id": "93c8",
			},
		}}
	round := exportRoundTrip(t, n)
	if round.Extra["uuid"] != "1ff35761-7e07-4724-bc72-58160ea5fe8c" {
		t.Errorf("uuid 丢失: %#v", round.Extra["uuid"])
	}
	if round.Extra["flow"] != "xtls-rprx-vision" {
		t.Errorf("flow 丢失: %#v", round.Extra["flow"])
	}
	if round.Extra["servername"] != "www.icloud.com" {
		t.Errorf("servername 丢失: %#v", round.Extra["servername"])
	}
	ro, ok := round.Extra["reality-opts"].(map[string]interface{})
	if !ok {
		t.Fatalf("reality-opts 未还原: %#v", round.Extra["reality-opts"])
	}
	if ro["public-key"] != "PUBKEY123" || ro["short-id"] != "93c8" {
		t.Errorf("reality-opts 内容不对: %#v", ro)
	}
}

// TestShareLinkRoundTripClientFingerprint 守住导出与解析的对称性。
// 曾经导出侧写 fp、解析侧不读，于是分享出去的链接再导入回来指纹就没了，
// reality 节点随之失效。
func TestShareLinkRoundTripClientFingerprint(t *testing.T) {
	n := Node{Name: "vless reality fp", Type: "vless", Server: "c.example.com", Port: 42345,
		Extra: map[string]interface{}{
			"uuid": "1ff35761-7e07-4724-bc72-58160ea5fe8c",
			"tls":  true, "network": "tcp", "servername": "www.icloud.com",
			"client-fingerprint": "firefox",
			"reality-opts": map[string]interface{}{
				"public-key": "PUBKEY123", "short-id": "93c8",
			},
		}}
	round := exportRoundTrip(t, n)
	if round.Extra["client-fingerprint"] != "firefox" {
		t.Errorf("client-fingerprint 未往返: %#v", round.Extra["client-fingerprint"])
	}
}

func TestShareLinkExportVLESSWebSocket(t *testing.T) {
	n := Node{Name: "vless ws", Type: "vless", Server: "d.example.com", Port: 443,
		Extra: map[string]interface{}{
			"uuid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"tls":  true, "network": "ws",
			"ws-opts": map[string]interface{}{
				"path":    "/3ijfs99wqeq",
				"headers": map[string]interface{}{"Host": "edge.example.com"},
			},
		}}
	round := exportRoundTrip(t, n)
	if round.Extra["network"] != "ws" {
		t.Errorf("network 丢失: %#v", round.Extra["network"])
	}
	wo, ok := round.Extra["ws-opts"].(map[string]interface{})
	if !ok {
		t.Fatalf("ws-opts 未还原: %#v", round.Extra["ws-opts"])
	}
	if wo["path"] != "/3ijfs99wqeq" {
		t.Errorf("ws path 丢失: %#v", wo)
	}
}

func TestShareLinkExportTrojan(t *testing.T) {
	n := Node{Name: "trojan 节点", Type: "trojan", Server: "e.example.com", Port: 443,
		Extra: map[string]interface{}{"password": "pw/with?special&chars", "sni": "sni.example.com"}}
	round := exportRoundTrip(t, n)
	if round.Extra["password"] != "pw/with?special&chars" {
		t.Errorf("password 未正确转义/还原: %#v", round.Extra["password"])
	}
}

func TestShareLinkExportHysteria2(t *testing.T) {
	n := Node{Name: "hy2", Type: "hysteria2", Server: "f.example.com", Port: 8443,
		Extra: map[string]interface{}{"password": "hy2pass", "sni": "h.example.com"}}
	round := exportRoundTrip(t, n)
	if round.Extra["password"] != "hy2pass" {
		t.Errorf("password 丢失: %#v", round.Extra["password"])
	}
	if round.Extra["sni"] != "h.example.com" {
		t.Errorf("sni 丢失: %#v", round.Extra["sni"])
	}
}

func TestShareLinkExportTUIC(t *testing.T) {
	n := Node{Name: "tuic", Type: "tuic", Server: "g.example.com", Port: 443,
		Extra: map[string]interface{}{
			"uuid": "uuid-1234", "password": "tuicpass",
			"sni": "t.example.com", "congestion-controller": "bbr",
			"alpn": []string{"h3"},
		}}
	round := exportRoundTrip(t, n)
	if round.Extra["uuid"] != "uuid-1234" {
		t.Errorf("uuid 丢失: %#v", round.Extra["uuid"])
	}
	if round.Extra["password"] != "tuicpass" {
		t.Errorf("password 丢失: %#v", round.Extra["password"])
	}
	if round.Extra["congestion-controller"] != "bbr" {
		t.Errorf("congestion-controller 丢失: %#v", round.Extra["congestion-controller"])
	}
}

func TestShareLinkExportAnyTLS(t *testing.T) {
	n := Node{Name: "anytls", Type: "anytls", Server: "i.example.com", Port: 443,
		Extra: map[string]interface{}{"password": "atpass", "sni": "a.example.com"}}
	round := exportRoundTrip(t, n)
	if round.Extra["password"] != "atpass" {
		t.Errorf("password 丢失: %#v", round.Extra["password"])
	}
}

// TestShareLinkExportSkipsUnsupported 锁定「无法表达为链接的协议必须跳过」。
// 输出半成品链接比不输出更糟：客户端会导入一个连不上的节点。
func TestShareLinkExportSkipsUnsupported(t *testing.T) {
	for _, typ := range []string{"ssr", "snell", "ssh", "mieru", "wireguard"} {
		n := Node{Name: typ, Type: typ, Server: "x.example.com", Port: 1080,
			Extra: map[string]interface{}{"password": "p"}}
		if link := nodeToShareLink(n); link != "" {
			t.Errorf("%s 应被跳过，却导出了: %s", typ, link)
		}
	}
}

// TestShareLinkExportSkipsIncomplete 缺少必要凭据时不应输出链接。
func TestShareLinkExportSkipsIncomplete(t *testing.T) {
	cases := []Node{
		{Name: "ss-no-cipher", Type: "ss", Server: "a", Port: 1, Extra: map[string]interface{}{"password": "p"}},
		{Name: "vmess-no-uuid", Type: "vmess", Server: "a", Port: 1, Extra: map[string]interface{}{}},
		{Name: "tuic-no-pass", Type: "tuic", Server: "a", Port: 1, Extra: map[string]interface{}{"uuid": "u"}},
	}
	for _, n := range cases {
		if link := nodeToShareLink(n); link != "" {
			t.Errorf("%s 凭据不全应跳过，却导出了: %s", n.Name, link)
		}
	}
}

// TestNodesToShareLinksMixed 混合协议时，支持的全部导出、不支持的静默跳过。
func TestNodesToShareLinksMixed(t *testing.T) {
	nodes := []Node{
		{Name: "n1", Type: "vless", Server: "a.com", Port: 443, Extra: map[string]interface{}{"uuid": "u1", "tls": true}},
		{Name: "n2", Type: "ssr", Server: "b.com", Port: 443, Extra: map[string]interface{}{}},
		{Name: "n3", Type: "hysteria2", Server: "c.com", Port: 443, Extra: map[string]interface{}{"password": "p"}},
	}
	out := NodesToShareLinks(nodes)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("应导出 2 条（ssr 跳过），实际 %d 条:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "vless://") {
		t.Errorf("第一条应为 vless: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "hysteria2://") {
		t.Errorf("第二条应为 hysteria2: %s", lines[1])
	}
}

// TestNodesToShareLinksIPv6 IPv6 地址必须加方括号，否则 host:port 无法解析。
func TestNodesToShareLinksIPv6(t *testing.T) {
	n := Node{Name: "v6", Type: "hysteria2", Server: "2001:db8::1", Port: 443,
		Extra: map[string]interface{}{"password": "p"}}
	link := nodeToShareLink(n)
	if !strings.Contains(link, "[2001:db8::1]:443") {
		t.Errorf("IPv6 未加方括号: %s", link)
	}
	exportRoundTrip(t, n)
}

// TestBase64LinksTargetNotEmpty 回归本次的起因：vless/vmess 这类节点在
// base64-links 目标下曾输出空内容（HTTP 200 但 size=0）。
func TestBase64LinksTargetNotEmpty(t *testing.T) {
	nodes := []Node{
		{Name: "v1", Type: "vless", Server: "a.com", Port: 443, Extra: map[string]interface{}{"uuid": "u1", "tls": true}},
		{Name: "v2", Type: "vmess", Server: "b.com", Port: 443, Extra: map[string]interface{}{"uuid": "u2"}},
	}
	target, body, err := RenderTemplate("base64-links", "", nodes)
	if err != nil {
		t.Fatal(err)
	}
	if target != "base64-links" {
		t.Errorf("target = %q", target)
	}
	if body == "" {
		t.Fatal("base64-links 输出为空——全 vless/vmess 的订阅会得到空响应")
	}
	dec, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("输出不是合法 base64: %v", err)
	}
	if !strings.Contains(string(dec), "vless://") || !strings.Contains(string(dec), "vmess://") {
		t.Errorf("解码后缺少预期链接:\n%s", dec)
	}
}

package substore

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 这些测试针对「导出器的协议覆盖」：此前多数导出器只认 ss/trojan/vmess，
// 遇到 vless / hysteria2 / tuic 等一律 continue，对外表现为
// 「HTTP 200 但内容为空或缺节点」。除了断言节点没被丢弃，
// 还要断言 TLS 与传输层字段真的写出来了——只输出协议和凭据的话，
// 客户端能导入但连不上，这种"假成功"比空输出更难排查。

// sampleNodes 覆盖真实订阅里的常见形态：reality、ws+TLS、QUIC 系协议。
func sampleNodes() []Node {
	return []Node{
		{Name: "ss-node", Type: "ss", Server: "s1.example.com", Port: 8388,
			Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "sspw"}},
		{Name: "trojan-node", Type: "trojan", Server: "s2.example.com", Port: 443,
			Extra: map[string]interface{}{"password": "tjpw", "sni": "t.example.com"}},
		{Name: "vmess-ws", Type: "vmess", Server: "s3.example.com", Port: 443,
			Extra: map[string]interface{}{
				"uuid": "vm-uuid", "cipher": "auto", "network": "ws", "tls": true,
				"ws-opts": map[string]interface{}{
					"path": "/vmpath", "headers": map[string]interface{}{"Host": "vm.example.com"},
				},
			}},
		{Name: "vless-reality", Type: "vless", Server: "s4.example.com", Port: 42345,
			Extra: map[string]interface{}{
				"uuid": "vl-uuid", "tls": true, "network": "tcp", "flow": "xtls-rprx-vision",
				"servername":   "www.icloud.com",
				"reality-opts": map[string]interface{}{"public-key": "PBK", "short-id": "sid1"},
			}},
		{Name: "vless-ws", Type: "vless", Server: "s5.example.com", Port: 443,
			Extra: map[string]interface{}{
				"uuid": "vl2-uuid", "tls": true, "network": "ws",
				"servername": "w.example.com",
				"ws-opts": map[string]interface{}{
					"path": "/vlpath", "headers": map[string]interface{}{"Host": "w.example.com"},
				},
			}},
		{Name: "hy2-node", Type: "hysteria2", Server: "s6.example.com", Port: 8443,
			Extra: map[string]interface{}{"password": "hy2pw", "sni": "h.example.com"}},
		{Name: "tuic-node", Type: "tuic", Server: "s7.example.com", Port: 443,
			Extra: map[string]interface{}{
				"uuid": "tu-uuid", "password": "tupw", "sni": "u.example.com",
				"congestion-controller": "bbr", "alpn": []string{"h3"},
			}},
		{Name: "anytls-node", Type: "anytls", Server: "s8.example.com", Port: 443,
			Extra: map[string]interface{}{"password": "atpw", "sni": "a.example.com"}},
		{Name: "socks-node", Type: "socks5", Server: "s9.example.com", Port: 1080,
			Extra: map[string]interface{}{"username": "u1", "password": "p1"}},
	}
}

func TestSingBoxCoversModernProtocols(t *testing.T) {
	out, err := NodesToSingBox(sampleNodes())
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 JSON: %v", err)
	}
	byTag := map[string]map[string]interface{}{}
	for _, ob := range cfg.Outbounds {
		byTag[ob["tag"].(string)] = ob
	}

	for _, tag := range []string{"ss-node", "trojan-node", "vmess-ws", "vless-reality",
		"vless-ws", "hy2-node", "tuic-node", "anytls-node", "socks-node"} {
		if _, ok := byTag[tag]; !ok {
			t.Errorf("节点 %q 被丢弃", tag)
		}
	}

	// vless + reality：tls.reality 必须落地，否则连不上
	if ob := byTag["vless-reality"]; ob != nil {
		if ob["type"] != "vless" {
			t.Errorf("vless type 错误: %v", ob["type"])
		}
		if ob["flow"] != "xtls-rprx-vision" {
			t.Errorf("flow 丢失: %v", ob["flow"])
		}
		tls, ok := ob["tls"].(map[string]interface{})
		if !ok {
			t.Fatalf("vless 缺 tls 段: %#v", ob)
		}
		if tls["server_name"] != "www.icloud.com" {
			t.Errorf("server_name 错误: %v", tls["server_name"])
		}
		reality, ok := tls["reality"].(map[string]interface{})
		if !ok {
			t.Fatalf("缺 reality 段: %#v", tls)
		}
		if reality["public_key"] != "PBK" || reality["short_id"] != "sid1" {
			t.Errorf("reality 字段错误: %#v", reality)
		}
	}

	// ws 传输层必须写成 transport
	if ob := byTag["vmess-ws"]; ob != nil {
		tr, ok := ob["transport"].(map[string]interface{})
		if !ok {
			t.Fatalf("vmess 缺 transport: %#v", ob)
		}
		if tr["type"] != "ws" || tr["path"] != "/vmpath" {
			t.Errorf("ws transport 错误: %#v", tr)
		}
	}

	// hysteria2/tuic 恒基于 TLS，必须自动开启
	for _, tag := range []string{"hy2-node", "tuic-node", "anytls-node", "trojan-node"} {
		ob := byTag[tag]
		if ob == nil {
			continue
		}
		if _, ok := ob["tls"].(map[string]interface{}); !ok {
			t.Errorf("%s 应自动开启 tls，实际: %#v", tag, ob)
		}
	}
	if ob := byTag["tuic-node"]; ob != nil {
		if ob["congestion_control"] != "bbr" {
			t.Errorf("tuic congestion_control 丢失: %v", ob["congestion_control"])
		}
	}
}

func TestSingBoxSkipsUnsupported(t *testing.T) {
	nodes := []Node{
		{Name: "ssr", Type: "ssr", Server: "a", Port: 1, Extra: map[string]interface{}{}},
		{Name: "snell", Type: "snell", Server: "a", Port: 1, Extra: map[string]interface{}{"psk": "k"}},
	}
	out, err := NodesToSingBox(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\"ssr\"") || strings.Contains(out, "\"snell\"") {
		t.Errorf("不支持的协议应跳过:\n%s", out)
	}
}

func TestV2RayStreamSettings(t *testing.T) {
	out, err := NodesToV2RayJSON(sampleNodes())
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 JSON: %v", err)
	}
	byTag := map[string]map[string]interface{}{}
	for _, ob := range cfg.Outbounds {
		byTag[ob["tag"].(string)] = ob
	}

	// reality 必须写进 realitySettings，否则退化为明文
	if ob := byTag["vless-reality"]; ob != nil {
		ss, ok := ob["streamSettings"].(map[string]interface{})
		if !ok {
			t.Fatalf("vless 缺 streamSettings: %#v", ob)
		}
		if ss["security"] != "reality" {
			t.Errorf("security 应为 reality: %v", ss["security"])
		}
		rs, ok := ss["realitySettings"].(map[string]interface{})
		if !ok {
			t.Fatalf("缺 realitySettings: %#v", ss)
		}
		if rs["publicKey"] != "PBK" || rs["shortId"] != "sid1" {
			t.Errorf("realitySettings 错误: %#v", rs)
		}
	} else {
		t.Error("vless-reality 被丢弃")
	}

	// ws + tls
	if ob := byTag["vmess-ws"]; ob != nil {
		ss := ob["streamSettings"].(map[string]interface{})
		if ss["network"] != "ws" || ss["security"] != "tls" {
			t.Errorf("vmess streamSettings 错误: %#v", ss)
		}
		ws, ok := ss["wsSettings"].(map[string]interface{})
		if !ok || ws["path"] != "/vmpath" {
			t.Errorf("wsSettings 错误: %#v", ss["wsSettings"])
		}
	}

	// trojan 即使未显式标 tls，也必须输出 tls streamSettings
	if ob := byTag["trojan-node"]; ob != nil {
		ss, ok := ob["streamSettings"].(map[string]interface{})
		if !ok || ss["security"] != "tls" {
			t.Errorf("trojan 应输出 tls streamSettings: %#v", ob["streamSettings"])
		}
	}

	// socks5 应导出
	if _, ok := byTag["socks-node"]; !ok {
		t.Error("socks-node 被丢弃")
	}

	// V2Ray 不支持 QUIC 系协议，应跳过
	for _, tag := range []string{"hy2-node", "tuic-node", "anytls-node"} {
		if _, ok := byTag[tag]; ok {
			t.Errorf("%s 不应出现在 V2Ray 产物里", tag)
		}
	}
}

func TestLoonCoversModernProtocols(t *testing.T) {
	out := NodesToLoon(sampleNodes())
	lines := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, " = "); i > 0 {
			lines[l[:i]] = l[i+3:]
		}
	}
	for _, name := range []string{"ss-node", "trojan-node", "vmess-ws", "vless-reality",
		"vless-ws", "hy2-node", "tuic-node", "socks-node"} {
		if _, ok := lines[name]; !ok {
			t.Errorf("节点 %q 被丢弃:\n%s", name, out)
		}
	}
	if v := lines["vless-ws"]; !strings.Contains(v, "transport=ws") || !strings.Contains(v, "path=/vlpath") {
		t.Errorf("vless ws 传输层缺失: %s", v)
	}
	if v := lines["vmess-ws"]; !strings.Contains(v, "tls-name=vm.example.com") {
		t.Errorf("vmess TLS SNI 缺失: %s", v)
	}
	if v := lines["hy2-node"]; !strings.HasPrefix(v, "Hysteria2,") {
		t.Errorf("hysteria2 协议名错误: %s", v)
	}
	// Loon 不支持 anytls
	if _, ok := lines["anytls-node"]; ok {
		t.Error("anytls 不应出现在 Loon 产物里")
	}
}

func TestQuantumultXCoversModernProtocols(t *testing.T) {
	out := NodesToQuantumultX(sampleNodes())
	byTag := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		for _, p := range strings.Split(l, ", ") {
			if strings.HasPrefix(p, "tag=") {
				byTag[strings.TrimPrefix(p, "tag=")] = l
			}
		}
	}
	for _, name := range []string{"ss-node", "trojan-node", "vmess-ws", "vless-reality",
		"vless-ws", "socks-node"} {
		if _, ok := byTag[name]; !ok {
			t.Errorf("节点 %q 被丢弃:\n%s", name, out)
		}
	}
	if v := byTag["vless-ws"]; !strings.Contains(v, "over-tls=true") || !strings.Contains(v, "obfs=wss") {
		t.Errorf("vless ws+tls 参数缺失: %s", v)
	}
	if v := byTag["trojan-node"]; !strings.Contains(v, "over-tls=true") {
		t.Errorf("trojan 应带 over-tls: %s", v)
	}
	// QX 不支持 QUIC 系
	for _, name := range []string{"hy2-node", "tuic-node", "anytls-node"} {
		if _, ok := byTag[name]; ok {
			t.Errorf("%s 不应出现在 QX 产物里", name)
		}
	}
}

func TestSurgeCoversModernProtocols(t *testing.T) {
	out := NodesToSurge(sampleNodes())
	lines := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, " = "); i > 0 {
			lines[l[:i]] = l[i+3:]
		}
	}
	for _, name := range []string{"ss-node", "trojan-node", "vmess-ws", "vless-ws",
		"hy2-node", "tuic-node", "socks-node"} {
		if _, ok := lines[name]; !ok {
			t.Errorf("节点 %q 被丢弃:\n%s", name, out)
		}
	}
	if v := lines["vless-ws"]; !strings.Contains(v, "ws=true") || !strings.Contains(v, "ws-path=/vlpath") {
		t.Errorf("vless ws 参数缺失: %s", v)
	}
	if v := lines["vmess-ws"]; !strings.Contains(v, "tls=true") {
		t.Errorf("vmess 应带 tls=true: %s", v)
	}
	// Surge 不支持 reality，带 reality 的 vless 必须跳过而非输出连不上的条目
	if _, ok := lines["vless-reality"]; ok {
		t.Error("Surge 不支持 reality，vless-reality 应跳过")
	}
}

func TestEgernCoversModernProtocols(t *testing.T) {
	out, err := NodesToEgern(sampleNodes())
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v", err)
	}
	seen := map[string]map[string]interface{}{}
	for _, p := range cfg.Proxies {
		for kind, v := range p {
			inner, _ := v.(map[string]interface{})
			if inner != nil {
				seen[valueString(inner["name"])] = map[string]interface{}{"kind": kind, "inner": inner}
			}
		}
	}
	for _, name := range []string{"ss-node", "trojan-node", "vmess-ws", "vless-reality",
		"vless-ws", "hy2-node", "tuic-node", "socks-node"} {
		if _, ok := seen[name]; !ok {
			t.Errorf("节点 %q 被丢弃:\n%s", name, out)
		}
	}
	if e := seen["vless-reality"]; e != nil {
		if e["kind"] != "vless" {
			t.Errorf("vless 类型键错误: %v", e["kind"])
		}
		inner := e["inner"].(map[string]interface{})
		if _, ok := inner["reality"]; !ok {
			t.Errorf("reality 段缺失: %#v", inner)
		}
	}
	if e := seen["vless-ws"]; e != nil {
		inner := e["inner"].(map[string]interface{})
		if _, ok := inner["websocket"]; !ok {
			t.Errorf("websocket 段缺失: %#v", inner)
		}
	}
}

// TestAllTargetsNonEmptyForModernNodes 端到端锁定回归：
// 一份全是 vless/vmess/hysteria2 的订阅（真实机场的常见形态），
// 在各输出格式下都不应得到空响应。
func TestAllTargetsNonEmptyForModernNodes(t *testing.T) {
	nodes := []Node{
		{Name: "v-ws", Type: "vless", Server: "a.com", Port: 443,
			Extra: map[string]interface{}{"uuid": "u1", "tls": true, "network": "ws",
				"ws-opts": map[string]interface{}{"path": "/p"}}},
		{Name: "vm", Type: "vmess", Server: "b.com", Port: 443,
			Extra: map[string]interface{}{"uuid": "u2", "cipher": "auto"}},
		{Name: "hy2", Type: "hysteria2", Server: "c.com", Port: 443,
			Extra: map[string]interface{}{"password": "p"}},
	}
	// 这些格式对上述节点都应有实质输出
	for _, target := range []string{
		"mihomo-yaml", "base64-links", "share-links", "sing-box",
		"v2ray", "json", "stash", "shadowrocket", "egern", "loon", "quantumultx",
	} {
		_, body, err := RenderTemplate(target, "", nodes)
		if err != nil {
			t.Errorf("target=%s 渲染失败: %v", target, err)
			continue
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("target=%s 输出为空", target)
		}
	}
	// Surge 系不支持 hysteria2 之外的部分协议，但本例含 vless(ws)/hysteria2，
	// 应当有输出
	for _, target := range []string{"surge", "surgemac", "surfboard"} {
		_, body, err := RenderTemplate(target, "", nodes)
		if err != nil {
			t.Errorf("target=%s 渲染失败: %v", target, err)
			continue
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("target=%s 输出为空", target)
		}
	}
}

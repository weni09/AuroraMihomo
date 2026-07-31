package substore

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

// realityNode 是一个典型的机场下发的 reality 节点：有 public-key，
// 但没有 client-fingerprint——这正是本组测试要覆盖的失效场景
// （mihomo 会报 "REALITY is based on uTLS, please set a client-fingerprint"）。
func realityNode() Node {
	return Node{
		Name: "Dmit-vless-reality-vision", Type: "vless",
		Server: "cdsa.650998.xyz", Port: 42345,
		Extra: map[string]interface{}{
			"uuid": "1ff35761", "tls": true, "network": "tcp",
			"flow": "xtls-rprx-vision", "servername": "www.icloud.com",
			"reality-opts": map[string]interface{}{
				"public-key": "PUB", "short-id": "93c8",
			},
		},
	}
}

func TestResolveClientFingerprint(t *testing.T) {
	withFP := realityNode()
	withFP.Extra["client-fingerprint"] = "firefox"

	emptyReality := realityNode()
	// reality-opts 存在但 public-key 为空：内核视为 reality 未启用
	emptyReality.Extra["reality-opts"] = map[string]interface{}{"short-id": "93c8"}

	noneFP := realityNode()
	noneFP.Extra["client-fingerprint"] = "none"

	cases := []struct {
		name string
		node Node
		want string
	}{
		{"reality 缺指纹时补默认值", realityNode(), "chrome"},
		{"已有指纹不被覆盖", withFP, "firefox"},
		{"显式 none 是用户意图，不覆盖", noneFP, "none"},
		{"reality-opts 无 public-key 视为未启用", emptyReality, ""},
		{
			"普通 TLS 节点不补",
			Node{Name: "ws", Type: "vless", Server: "a.com", Port: 443,
				Extra: map[string]interface{}{"uuid": "u", "tls": true}},
			"",
		},
		{
			"Extra 为 nil 不 panic",
			Node{Name: "bare", Type: "ss", Server: "a.com", Port: 443},
			"",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveClientFingerprint(c.node); got != c.want {
				t.Errorf("期望 %q，实际 %q", c.want, got)
			}
		})
	}
}

// TestRealityFingerprintInMihomoOutput 是本次故障的直接回归：
// reality 节点渲染成 mihomo 配置后必须带 client-fingerprint，否则内核拒绝连接。
func TestRealityFingerprintInMihomoOutput(t *testing.T) {
	out, err := NodesToMihomoYAML([]Node{realityNode()})
	if err != nil {
		t.Fatal(err)
	}
	proxy := firstProxyMap(t, out)
	if proxy["client-fingerprint"] != "chrome" {
		t.Errorf("mihomo 输出未补 client-fingerprint: %#v", proxy["client-fingerprint"])
	}
	// 补字段不能碰坏 reality-opts 本身
	if _, ok := proxy["reality-opts"].(map[string]interface{}); !ok {
		t.Errorf("reality-opts 结构被破坏: %#v", proxy["reality-opts"])
	}
}

func TestRealityFingerprintInStashOutput(t *testing.T) {
	out, err := NodesToStash([]Node{realityNode()})
	if err != nil {
		t.Fatal(err)
	}
	proxy := firstProxyMap(t, out)
	if proxy["client-fingerprint"] != "chrome" {
		t.Errorf("Stash 输出未补 client-fingerprint: %#v", proxy["client-fingerprint"])
	}
}

// TestRealityFingerprintInSingBoxOutput 覆盖 sing-box：它同样要求 reality
// 必须启用 uTLS（"uTLS is required by reality client"）。
func TestRealityFingerprintInSingBoxOutput(t *testing.T) {
	out, err := NodesToSingBox([]Node{realityNode()})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			TLS struct {
				UTLS struct {
					Enabled     bool   `json:"enabled"`
					Fingerprint string `json:"fingerprint"`
				} `json:"utls"`
			} `json:"tls"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 JSON: %v", err)
	}
	if len(cfg.Outbounds) == 0 {
		t.Fatal("没有出站")
	}
	utls := cfg.Outbounds[0].TLS.UTLS
	if !utls.Enabled || utls.Fingerprint != "chrome" {
		t.Errorf("sing-box utls 未补全: enabled=%v fingerprint=%q", utls.Enabled, utls.Fingerprint)
	}
}

func TestRealityFingerprintInV2RayOutput(t *testing.T) {
	out, err := NodesToV2RayJSON([]Node{realityNode()})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			StreamSettings struct {
				RealitySettings struct {
					Fingerprint string `json:"fingerprint"`
				} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 JSON: %v", err)
	}
	if len(cfg.Outbounds) == 0 {
		t.Fatal("没有出站")
	}
	if fp := cfg.Outbounds[0].StreamSettings.RealitySettings.Fingerprint; fp != "chrome" {
		t.Errorf("v2ray realitySettings.fingerprint 未补全: %q", fp)
	}
}

// TestNonRealityNodeGetsNoFingerprint 确认补全不外溢：
// 普通节点凭空多出 client-fingerprint 会让用户比对配置时无从解释。
func TestNonRealityNodeGetsNoFingerprint(t *testing.T) {
	plain := Node{Name: "ss-1", Type: "ss", Server: "a.example.com", Port: 8388,
		Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}}
	out, err := NodesToMihomoYAML([]Node{plain})
	if err != nil {
		t.Fatal(err)
	}
	proxy := firstProxyMap(t, out)
	if _, ok := proxy["client-fingerprint"]; ok {
		t.Errorf("非 reality 节点不应被写入 client-fingerprint: %#v", proxy)
	}
}

// firstProxyMap 从一份 Clash 系 YAML 里取出第一个 proxies 项。
func firstProxyMap(t *testing.T, y string) map[string]interface{} {
	t.Helper()
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(y), &root); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, y)
	}
	arr, _ := root["proxies"].([]interface{})
	if len(arr) == 0 {
		t.Fatalf("产物里没有 proxies:\n%s", y)
	}
	m, ok := arr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("proxies[0] 不是映射: %#v", arr[0])
	}
	return m
}

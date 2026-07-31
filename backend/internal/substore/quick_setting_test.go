package substore

import "testing"

// quickSettingNodes 覆盖会被「常用配置」不同字段区分对待的协议：
// vmess（aead）、snell（reuse）、tuic（ecn）、ss（三者都不适用，用作对照）。
func quickSettingNodes() []Node {
	return []Node{
		{Name: "vmess-1", Type: "vmess", Server: "v.example.com", Port: 443, Extra: map[string]interface{}{"uuid": "u"}},
		{Name: "snell-1", Type: "snell", Server: "s.example.com", Port: 443, Extra: map[string]interface{}{"psk": "p"}},
		{Name: "tuic-1", Type: "tuic", Server: "t.example.com", Port: 443, Extra: map[string]interface{}{}},
		{Name: "ss-1", Type: "ss", Server: "ss.example.com", Port: 8388, Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}},
	}
}

func TestQuickSetting_TriStateFieldsApplyToAllProtocols(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{
		"udp":   "ENABLED",
		"scert": "ENABLED",
		"tfo":   "ENABLED",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if !n.UDP {
			t.Errorf("%s: UDP 应被启用", n.Name)
		}
		if n.Extra["udp"] != true {
			t.Errorf("%s: Extra[udp] 应为 true", n.Name)
		}
		if n.Extra["skip-cert-verify"] != true {
			t.Errorf("%s: skip-cert-verify 应为 true", n.Name)
		}
		if n.Extra["tfo"] != true || n.Extra["fast-open"] != true {
			t.Errorf("%s: tfo/fast-open 应同时为 true，实际 tfo=%v fast-open=%v", n.Name, n.Extra["tfo"], n.Extra["fast-open"])
		}
	}
}

func TestQuickSetting_DefaultLeavesFieldsUntouched(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{
		"udp":   "DEFAULT",
		"scert": "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if n.UDP {
			t.Errorf("%s: DEFAULT 不应改变 UDP", n.Name)
		}
		if _, ok := n.Extra["udp"]; ok {
			t.Errorf("%s: DEFAULT 不应写入 Extra[udp]", n.Name)
		}
		if _, ok := n.Extra["skip-cert-verify"]; ok {
			t.Errorf("%s: 空值不应写入 skip-cert-verify", n.Name)
		}
	}
}

func TestQuickSetting_AeadOnlyAffectsVmess(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{"aead": "ENABLED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if n.Type == "vmess" {
			if n.Extra["alterId"] != 0 {
				t.Errorf("vmess 节点 aead 开启后 alterId 应为 0，实际 %v", n.Extra["alterId"])
			}
			if _, ok := n.Extra["aead"]; ok {
				t.Errorf("aead 中间字段不应保留在最终结果中")
			}
		} else if _, ok := n.Extra["alterId"]; ok {
			t.Errorf("%s: 非 vmess 节点不应被写入 alterId", n.Name)
		}
	}
}

func TestQuickSetting_ReuseOnlyAffectsSnellAnytlsTrusttunnel(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{"reuse": "ENABLED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if n.Type == "snell" {
			if n.Extra["reuse"] != true {
				t.Errorf("snell 节点应写入 reuse=true")
			}
		} else if _, ok := n.Extra["reuse"]; ok {
			t.Errorf("%s: 该协议不应被写入 reuse", n.Name)
		}
	}
}

func TestQuickSetting_ECNOnlyAffectsTuicHysteria2(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{"ecn": "ENABLED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if n.Type == "tuic" {
			if n.Extra["ecn"] != true {
				t.Errorf("tuic 节点应写入 ecn=true")
			}
		} else if _, ok := n.Extra["ecn"]; ok {
			t.Errorf("%s: 该协议不应被写入 ecn", n.Name)
		}
	}
}

func TestQuickSetting_BlockQuicAcceptsOnlyKnownValues(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{"block_quic": "on"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if n.Extra["block-quic"] != "on" {
			t.Errorf("%s: block-quic 应为 on，实际 %v", n.Name, n.Extra["block-quic"])
		}
	}

	// 未知取值（含缺省 nil）不应写入字段
	nodes2 := quickSettingNodes()
	out2, err := applyQuickSetting(nodes2, map[string]interface{}{"block_quic": "DEFAULT"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out2 {
		if _, ok := n.Extra["block-quic"]; ok {
			t.Errorf("%s: DEFAULT 不应写入 block-quic", n.Name)
		}
	}
}

func TestQuickSetting_IPVersionMapsToMihomoEnum(t *testing.T) {
	cases := map[string]string{
		"dual":      "dual",
		"v4-only":   "ipv4",
		"v6-only":   "ipv6",
		"prefer-v4": "ipv4-prefer",
		"prefer-v6": "ipv6-prefer",
	}
	for in, want := range cases {
		nodes := quickSettingNodes()
		out, err := applyQuickSetting(nodes, map[string]interface{}{"ip_version": in})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, n := range out {
			if n.Extra["ip-version"] != want {
				t.Errorf("ip_version=%s: %s 期望 %s，实际 %v", in, n.Name, want, n.Extra["ip-version"])
			}
		}
	}
}

func TestQuickSetting_UselessDropsInfoNodesAndKeepsOthers(t *testing.T) {
	nodes := []Node{
		{Name: "过期勿使用", Type: "ss", Server: "expire.example.com", Port: 8388, Extra: map[string]interface{}{}},
		{Name: "正常节点", Type: "ss", Server: "ok.example.com", Port: 8388, Extra: map[string]interface{}{}},
		{Name: "端口非法", Type: "ss", Server: "bad.example.com", Port: 99999, Extra: map[string]interface{}{}},
	}
	out, err := applyQuickSetting(nodes, map[string]interface{}{"useless": "ENABLED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].Name != "正常节点" {
		t.Errorf("useless 过滤后应只剩「正常节点」，实际 %v", out)
	}

	// DISABLED（默认）不应剔除任何节点
	out2, err := applyQuickSetting(nodes, map[string]interface{}{"useless": "DISABLED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out2) != len(nodes) {
		t.Errorf("useless=DISABLED 不应剔除节点，实际剩 %d/%d", len(out2), len(nodes))
	}
}

func TestQuickSetting_ClientFingerprintProtocolGating(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := applyQuickSetting(nodes, map[string]interface{}{"client_fingerprint": "firefox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		// quickSettingNodes 里 vmess / snell / ss 都接受该字段，tuic 不接受
		if n.Type == "tuic" {
			if _, ok := n.Extra["client-fingerprint"]; ok {
				t.Errorf("tuic 不支持 client-fingerprint，不应写入")
			}
			continue
		}
		if n.Extra["client-fingerprint"] != "firefox" {
			t.Errorf("%s: 期望 firefox，实际 %v", n.Name, n.Extra["client-fingerprint"])
		}
	}
}

func TestQuickSetting_ClientFingerprintRejectsUnknownValues(t *testing.T) {
	// DEFAULT、空值与拼错的指纹都不应写入：写入一个内核不认识的值会让
	// reality 节点连不上，还会绕过 resolveClientFingerprint 的兜底
	for _, v := range []string{"DEFAULT", "", "none", "chrome_typo"} {
		nodes := quickSettingNodes()
		out, err := applyQuickSetting(nodes, map[string]interface{}{"client_fingerprint": v})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, n := range out {
			if got, ok := n.Extra["client-fingerprint"]; ok {
				t.Errorf("client_fingerprint=%q: %s 不应被写入，实际 %v", v, n.Name, got)
			}
		}
	}
}

func TestQuickSetting_ViaApplyPipeline(t *testing.T) {
	nodes := quickSettingNodes()
	out, err := ApplyPipeline(nodes, []PipelineOperator{
		op(OpQuickSetting, map[string]interface{}{"udp": "ENABLED"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range out {
		if !n.UDP {
			t.Errorf("%s: 经 ApplyPipeline 后 UDP 应被启用", n.Name)
		}
	}
}

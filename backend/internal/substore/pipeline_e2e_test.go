package substore

import (
	"context"
	"strings"
	"testing"
)

func TestFullSubStoreE2E(t *testing.T) {
	// 1. Simulate VLESS/Reality link dropping from user subscription
	vlessLink := "vless://b831381d-6324-4d53-ad4f-8cda48b30811@104.21.34.45:443?encryption=none&security=reality&sni=yahoo.com&pbk=123456789&sid=abcdef&type=tcp&headerType=none#Test_VLESS_Node\n"

	e := NewEngine()

	ops := []PipelineOperator{
		// Test Regex Rename
		{
			Type:    OpRename,
			Enabled: true,
			Payload: map[string]interface{}{"pattern": "Test_VLESS_", "replace": "Premium "},
		},
		// Test Filter
		{
			Type:    OpFilter,
			Enabled: true,
			Payload: map[string]interface{}{"action": "keep", "pattern": "Premium"},
		},
		// Test Flag
		{
			Type:    OpFlag,
			Enabled: true,
			Payload: map[string]interface{}{},
		},
		// Test Set Property
		{
			Type:    OpSetProperty,
			Enabled: true,
			Payload: map[string]interface{}{"udp": true, "skip-cert-verify": true},
		},
	}

	res, err := e.Convert(context.Background(), ConvertRequest{Content: vlessLink}, nil, ops, "mihomo-yaml", "")
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if len(res.Nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(res.Nodes))
	}

	n := res.Nodes[0]
	if n.Name != "Premium Node" {
		t.Errorf("Rename failed: %s", n.Name)
	}
	if !n.UDP {
		t.Errorf("UDP SetProperty failed")
	}
	if n.Extra["skip-cert-verify"] != true {
		t.Errorf("skip-cert-verify SetProperty failed")
	}
	if n.Extra["servername"] != "yahoo.com" {
		t.Errorf("SNI reality mapping failed: %v", n.Extra["servername"])
	}

	yamlStr := res.YAML
	if !strings.Contains(yamlStr, "reality-opts") || !strings.Contains(yamlStr, "public-key: \"123456789\"") {
		t.Errorf("Reality opts not exported to YAML properly: %s", yamlStr)
	}
}

// TestRegionPayloadContract 锁定 region/regex_sort 的 payload 字段名，
// 前端曾误传 regionsText/patternsText 导致算子静默失效。
func TestRegionPayloadContract(t *testing.T) {
	nodes := []Node{
		{Name: "香港-01", Type: "ss"},
		{Name: "JP-Tokyo-02", Type: "ss"},
		{Name: "US-LA-03", Type: "ss"},
	}

	kept, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegion,
		Enabled: true,
		Payload: map[string]interface{}{
			"action":  "keep",
			"regions": []interface{}{"HK", "JP"},
		},
	}})
	if err != nil {
		t.Fatalf("region 算子执行失败: %v", err)
	}
	if len(kept) != 2 {
		t.Fatalf("期望保留 2 个节点，实际 %d: %+v", len(kept), kept)
	}
	for _, n := range kept {
		if strings.Contains(n.Name, "US") {
			t.Fatalf("US 节点未被剔除: %s", n.Name)
		}
	}

	sorted, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegexSort,
		Enabled: true,
		Payload: map[string]interface{}{"patterns": []interface{}{"US", "JP", "香港"}},
	}})
	if err != nil {
		t.Fatalf("regex_sort 算子执行失败: %v", err)
	}
	if !strings.Contains(sorted[0].Name, "US") {
		t.Fatalf("regex_sort 未生效，首位应为 US，实际 %s", sorted[0].Name)
	}
}

// TestSurgeAndSurgeMacTargets 验证 Surge 新增 snell 支持，
// 以及 SurgeMac 对 Surge 原生不支持协议的 external 落地。
func TestSurgeAndSurgeMacTargets(t *testing.T) {
	nodes := []Node{
		{Name: "SS节点", Type: "ss", Server: "1.1.1.1", Port: 8388,
			Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "pw"}},
		{Name: "Snell节点", Type: "snell", Server: "2.2.2.2", Port: 44046,
			Extra: map[string]interface{}{"psk": "mypsk", "version": 4,
				"obfs-opts": map[string]interface{}{"mode": "http", "host": "bing.com"}}},
		{Name: "SSH节点", Type: "ssh", Server: "3.3.3.3", Port: 2222,
			Extra: map[string]interface{}{"username": "root"}},
	}

	surge := NodesToSurge(nodes)
	if !strings.Contains(surge, "Snell节点 = snell, 2.2.2.2, 44046") {
		t.Fatalf("Surge 未输出 snell: \n%s", surge)
	}
	if !strings.Contains(surge, "psk=mypsk") || !strings.Contains(surge, "obfs=http") {
		t.Fatalf("Surge snell 参数缺失: \n%s", surge)
	}
	if strings.Contains(surge, "SSH节点") {
		t.Fatalf("Surge (iOS) 不应输出 ssh: \n%s", surge)
	}

	mac := NodesToSurgeMac(nodes)
	if !strings.Contains(mac, "SS节点") || !strings.Contains(mac, "Snell节点") {
		t.Fatalf("SurgeMac 丢失原生协议: \n%s", mac)
	}
	if !strings.Contains(mac, "SSH节点 = external") {
		t.Fatalf("SurgeMac 未以 external 落地 ssh: \n%s", mac)
	}
	if !strings.Contains(mac, `exec="/usr/bin/ssh"`) {
		t.Fatalf("SurgeMac external exec 错误: \n%s", mac)
	}
}

// TestRenderTemplateSurgeMac 确认 surgemac 已在渲染入口注册
func TestRenderTemplateSurgeMac(t *testing.T) {
	nodes := []Node{{Name: "n1", Type: "ss", Server: "1.1.1.1", Port: 80,
		Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}}}
	for _, name := range []string{"surgemac", "surge-mac"} {
		kind, out, err := RenderTemplate(name, "", nodes)
		if err != nil {
			t.Fatalf("%s 渲染失败: %v", name, err)
		}
		if kind != "surgemac" || !strings.Contains(out, "n1 = ss") {
			t.Fatalf("%s 渲染结果异常: kind=%s out=%s", name, kind, out)
		}
	}
}

// TestTwoStagePipeline 验证「单订阅管道 → 组合管道」两级流水线：
// 订阅 A 自己剔除 US 节点，订阅 B 自己给节点改名，
// 最后组合级统一加国旗。
func TestTwoStagePipeline(t *testing.T) {
	subA := ConvertRequest{
		Source: "A",
		Content: "ss://YWVzLTI1Ni1nY206cHcx@1.1.1.1:8388#HK-A01\n" +
			"ss://YWVzLTI1Ni1nY206cHcx@2.2.2.2:8388#US-A02\n",
		Operators: []PipelineOperator{{
			Type:    OpRegion,
			Enabled: true,
			Payload: map[string]interface{}{"action": "drop", "regions": []interface{}{"US"}},
		}},
	}
	subB := ConvertRequest{
		Source:  "B",
		Content: "ss://YWVzLTI1Ni1nY206cHcy@3.3.3.3:8388#tokyo-B01\n",
		Operators: []PipelineOperator{{
			Type:    OpRename,
			Enabled: true,
			Payload: map[string]interface{}{"pattern": "^tokyo", "replace": "JP"},
		}},
	}

	res, err := NewEngine().ConvertMany(context.Background(),
		[]ConvertRequest{subA, subB}, nil,
		[]PipelineOperator{{Type: OpFlag, Enabled: true}},
		"share-links", "")
	if err != nil {
		t.Fatalf("两级流水线执行失败: %v", err)
	}

	out := res.Links
	if strings.Contains(out, "US-A02") {
		t.Fatalf("订阅 A 的独立管道未生效，US 节点仍存在:\n%s", out)
	}
	if !strings.Contains(out, "JP-B01") {
		t.Fatalf("订阅 B 的独立管道未生效，改名未发生:\n%s", out)
	}
	// 组合级 flag 应作用于两条订阅的节点
	if !strings.Contains(out, "🇭🇰") || !strings.Contains(out, "🇯🇵") {
		t.Fatalf("组合级管道未作用于全部节点:\n%s", out)
	}
}

// 手动粘贴节点（无 URL）应可直接解析
func TestContentOnlySubscription(t *testing.T) {
	res, err := NewEngine().ConvertMany(context.Background(), []ConvertRequest{{
		Source:  "本地",
		Content: "ss://YWVzLTI1Ni1nY206cHc=@9.9.9.9:8388#Local-01\n",
	}}, nil, nil, "share-links", "")
	if err != nil {
		t.Fatalf("手动节点解析失败: %v", err)
	}
	if !strings.Contains(res.Links, "Local-01") {
		t.Fatalf("手动节点未出现在结果中: %s", res.Links)
	}
}

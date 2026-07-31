package substore

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"auroramihomo/backend/internal/model"
	"gopkg.in/yaml.v3"
)

// jsOverrideNodes 是三种模板语言对比时共用的节点样本，
// 覆盖嵌套结构（ws-opts / reality-opts）与含 emoji、冒号的节点名。
func jsOverrideNodes() []Node {
	return []Node{
		{Name: "Dmit-vless-ws-fake", Type: "vless", Server: "cdsi.650998.xyz", Port: 443,
			Extra: map[string]interface{}{
				"uuid": "f2d3af2d", "tls": true, "network": "ws", "servername": "",
				"ws-opts": map[string]interface{}{"path": "/3ijfs99wqeq"},
			}},
		{Name: "Dmit-vless-reality-vision", Type: "vless", Server: "cdsa.650998.xyz", Port: 42345,
			Extra: map[string]interface{}{
				"uuid": "1ff35761", "tls": true, "network": "tcp", "flow": "xtls-rprx-vision",
				"servername":   "www.icloud.com",
				"reality-opts": map[string]interface{}{"public-key": "PUB", "short-id": "93c8"},
			}},
		{Name: "剩余流量：53.7 GB", Type: "vmess", Server: "256.256.256.256", Port: 80,
			Extra: map[string]interface{}{"uuid": "303188d3", "alterId": 0, "cipher": "auto", "network": "tcp", "tls": false}},
	}
}

// TestVerifyNewJSOverride 检查 JS 脚本覆写版模板本身的产物形态：
// 键数、策略组/规则/规则集条数，以及嵌套结构是否真的序列化成了 YAML 映射。
func TestVerifyNewJSOverride(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_js_override.js")
	if err != nil {
		t.Fatal(err)
	}
	nodes := jsOverrideNodes()

	out, err := RenderMihomoOverride(model.TemplateLangJS, string(b), nodes)
	if err != nil {
		t.Fatalf("JS 脚本渲染失败: %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("产物不是合法 YAML: %v", err)
	}

	px, _ := got["proxies"].([]interface{})
	if len(px) != 3 {
		t.Errorf("proxies 应为 3，实际 %d", len(px))
	}
	// JS 侧的 proxies 由基础配置带入、经 JSON 往返，嵌套结构必须仍是映射
	for _, p := range px {
		m, ok := p.(map[string]interface{})
		if !ok {
			t.Fatalf("proxies 项不是映射: %#v", p)
		}
		if wo, ok := m["ws-opts"]; ok {
			if _, isMap := wo.(map[string]interface{}); !isMap {
				t.Errorf("ws-opts 未序列化为 YAML 映射: %#v", wo)
			}
		}
		if ro, ok := m["reality-opts"]; ok {
			if _, isMap := ro.(map[string]interface{}); !isMap {
				t.Errorf("reality-opts 未序列化为 YAML 映射: %#v", ro)
			}
		}
	}

	pg, _ := got["proxy-groups"].([]interface{})
	if len(pg) != 34 {
		t.Errorf("proxy-groups 应为 34，实际 %d", len(pg))
	}
	// 基础配置自带的占位组必须被整体替换，不能残留
	for _, g := range pg {
		if m, ok := g.(map[string]interface{}); ok && m["name"] == "Proxy" {
			t.Error("占位策略组 Proxy 未被替换")
		}
	}
	if len(pg) > 0 {
		first, _ := pg[0].(map[string]interface{})
		if first["type"] != "fallback" {
			t.Errorf("首个策略组类型应为 fallback，实际 %v", first["type"])
		}
	}

	rules, _ := got["rules"].([]interface{})
	if len(rules) != 35 {
		t.Errorf("rules 应为 35，实际 %d", len(rules))
	}
	if len(rules) > 0 && rules[len(rules)-1] == "MATCH,Proxy" {
		t.Error("基础配置的占位规则 MATCH,Proxy 未被替换")
	}

	rp, _ := got["rule-providers"].(map[string]interface{})
	if len(rp) != 34 {
		t.Errorf("rule-providers 应为 34，实际 %d", len(rp))
	}
	if a1, ok := rp["a1"].(map[string]interface{}); ok {
		for _, k := range []string{"type", "interval", "behavior", "format", "url"} {
			if _, ok := a1[k]; !ok {
				t.Errorf("a1 缺字段 %q", k)
			}
		}
	} else {
		t.Errorf("a1 类型异常: %T", rp["a1"])
	}
	// 锚点是 YAML 语法，JS 版本不该出现这类容器键
	for _, k := range []string{"pr", "pr1", "rule-anchor"} {
		if _, ok := got[k]; ok {
			t.Errorf("JS 版不应出现锚点定义键 %q", k)
		}
	}
}

// TestJSOverrideEquivalentToYAMLOverride 验证 JS 脚本覆写版与既有 YAML 覆写版
// 产出的配置在语义上一致（除 JS 版不含锚点定义键这一预期差异）。
func TestJSOverrideEquivalentToYAMLOverride(t *testing.T) {
	jsTpl, err := os.ReadFile("testdata/mihomo_js_override.js")
	if err != nil {
		t.Fatal(err)
	}
	yamlTpl, err := os.ReadFile("testdata/mihomo_yaml_override.yaml")
	if err != nil {
		t.Skip("缺少既有 YAML 模板副本，跳过对比")
	}
	nodes := jsOverrideNodes()

	outJS, err := RenderMihomoOverride(model.TemplateLangJS, string(jsTpl), nodes)
	if err != nil {
		t.Fatalf("JS 脚本渲染失败: %v", err)
	}
	outYaml, err := RenderMihomoOverride(model.TemplateLangYAML, string(yamlTpl), nodes)
	if err != nil {
		t.Fatalf("YAML 覆写渲染失败: %v", err)
	}

	var mJS, mYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(outJS), &mJS); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(outYaml), &mYaml); err != nil {
		t.Fatal(err)
	}

	// YAML 覆写版保留锚点定义键，JS 版没有——预期差异，比较前剔除
	for _, k := range []string{"pr", "pr1", "rule-anchor"} {
		delete(mYaml, k)
	}
	// 与 gotpl_equiv_test 同一处既有笔误：YAML 版 a23(Netflix) 的 url 多一个斜杠
	if rp, ok := mYaml["rule-providers"].(map[string]interface{}); ok {
		if a23, ok := rp["a23"].(map[string]interface{}); ok {
			if u, ok := a23["url"].(string); ok {
				a23["url"] = strings.Replace(u, "/list//", "/list/", 1)
			}
		}
	}

	if len(mJS) != len(mYaml) {
		t.Errorf("顶层键数不一致: js=%d yaml=%d", len(mJS), len(mYaml))
		for k := range mJS {
			if _, ok := mYaml[k]; !ok {
				t.Errorf("  仅 JS 版有: %s", k)
			}
		}
		for k := range mYaml {
			if _, ok := mJS[k]; !ok {
				t.Errorf("  仅 YAML 版有: %s", k)
			}
		}
	}

	for _, key := range []string{"proxies", "proxy-groups", "rules", "rule-providers",
		"global-ua", "ipv6", "allow-lan", "unified-delay", "tcp-concurrent",
		"geodata-mode", "geodata-loader", "geo-auto-update", "geo-update-interval",
		"geox-url", "profile"} {
		if !reflect.DeepEqual(mJS[key], mYaml[key]) {
			t.Errorf("字段 %q 不一致\n  js  = %#v\n  yaml= %#v", key, mJS[key], mYaml[key])
		}
	}
}

// TestValidateJSOverrideTemplate 确认这份模板能通过保存前的语法校验，
// 即用户把它粘进「模板文件」表单时不会被拒。
func TestValidateJSOverrideTemplate(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_js_override.js")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTemplateLang(model.TemplateLangJS, string(b)); err != nil {
		t.Fatalf("JS 模板未通过语法校验: %v", err)
	}
}

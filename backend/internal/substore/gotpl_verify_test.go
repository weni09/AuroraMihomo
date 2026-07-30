package substore

import (
	"os"
	"testing"

	"auroramihomo/backend/internal/model"
	"gopkg.in/yaml.v3"
)

func TestVerifyNewGoTemplate(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_gotemplate.tpl")
	if err != nil {
		t.Fatal(err)
	}
	nodes := []Node{
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

	out, err := RenderMihomoOverride(model.TemplateLangGo, string(b), nodes)
	if err != nil {
		t.Fatalf("Go 模板渲染失败: %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("产物不是合法 YAML: %v", err)
	}
	t.Logf("顶层键数=%d", len(got))

	px, _ := got["proxies"].([]interface{})
	t.Logf("proxies=%d", len(px))
	if len(px) != 3 {
		t.Errorf("proxies 应为 3，实际 %d", len(px))
	}
	// 嵌套结构必须是真正的 map，而不是被打印成 map[...] 字符串
	for _, p := range px {
		m := p.(map[string]interface{})
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
	t.Logf("proxy-groups=%d", len(pg))
	if len(pg) != 34 {
		t.Errorf("proxy-groups 应为 34，实际 %d", len(pg))
	}
	if len(pg) > 0 {
		first := pg[0].(map[string]interface{})
		t.Logf("第一个策略组: %#v", first)
		if first["type"] != "fallback" {
			t.Errorf("$pr 变量未正确展开，type=%v", first["type"])
		}
	}

	rules, _ := got["rules"].([]interface{})
	t.Logf("rules=%d", len(rules))
	if len(rules) != 35 {
		t.Errorf("rules 应为 35，实际 %d", len(rules))
	}

	rp, _ := got["rule-providers"].(map[string]interface{})
	t.Logf("rule-providers=%d", len(rp))
	if len(rp) != 34 {
		t.Errorf("rule-providers 应为 34，实际 %d", len(rp))
	}
	if a1, ok := rp["a1"].(map[string]interface{}); ok {
		t.Logf("a1=%#v", a1)
		for _, k := range []string{"type", "interval", "behavior", "format", "url"} {
			if _, ok := a1[k]; !ok {
				t.Errorf("a1 缺字段 %q", k)
			}
		}
	} else {
		t.Errorf("a1 类型异常: %T", rp["a1"])
	}
	// 不该残留锚点定义键
	for _, k := range []string{"pr", "pr1", "rule-anchor"} {
		if _, ok := got[k]; ok {
			t.Errorf("Go 模板版不应出现锚点定义键 %q", k)
		}
	}
}

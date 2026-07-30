package substore

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"auroramihomo/backend/internal/model"
	"gopkg.in/yaml.v3"
)

// TestGoTemplateEquivalentToYAMLOverride 验证新写的 Go 模板版与既有 YAML 覆写版
// 产出的配置在语义上一致（除 Go 模板版不含锚点定义键这一预期差异）。
func TestGoTemplateEquivalentToYAMLOverride(t *testing.T) {
	goTpl, err := os.ReadFile("testdata/mihomo_gotemplate.tpl")
	if err != nil {
		t.Fatal(err)
	}
	yamlTpl, err := os.ReadFile("testdata/mihomo_yaml_override.yaml")
	if err != nil {
		t.Skip("缺少既有 YAML 模板副本，跳过对比")
	}
	nodes := []Node{
		{Name: "A", Type: "vless", Server: "a.com", Port: 443,
			Extra: map[string]interface{}{"uuid": "u1", "tls": true,
				"ws-opts": map[string]interface{}{"path": "/p"}}},
		{Name: "B", Type: "vmess", Server: "b.com", Port: 80,
			Extra: map[string]interface{}{"uuid": "u2", "cipher": "auto"}},
	}

	outGo, err := RenderMihomoOverride(model.TemplateLangGo, string(goTpl), nodes)
	if err != nil {
		t.Fatalf("Go 模板渲染失败: %v", err)
	}
	outYaml, err := RenderMihomoOverride(model.TemplateLangYAML, string(yamlTpl), nodes)
	if err != nil {
		t.Fatalf("YAML 覆写渲染失败: %v", err)
	}

	var mGo, mYaml map[string]interface{}
	if err := yaml.Unmarshal([]byte(outGo), &mGo); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(outYaml), &mYaml); err != nil {
		t.Fatal(err)
	}

	// YAML 覆写版会保留锚点定义键，Go 模板版没有——这是预期差异，比较前剔除
	for _, k := range []string{"pr", "pr1", "rule-anchor"} {
		delete(mYaml, k)
	}

	// 已知且有意的差异：原 YAML 版 a23(Netflix) 的 url 多写了一个斜杠
	// （".../list//Netflix.list"），实测该地址返回 307 重定向，单斜杠返回 200。
	// Go 模板版写的是正确的单斜杠。这里把 YAML 侧对齐后再比，
	// 避免这个既有笔误把整体等价性检查带偏。
	if rp, ok := mYaml["rule-providers"].(map[string]interface{}); ok {
		if a23, ok := rp["a23"].(map[string]interface{}); ok {
			if u, ok := a23["url"].(string); ok {
				a23["url"] = strings.Replace(u, "/list//", "/list/", 1)
			}
		}
	}

	if len(mGo) != len(mYaml) {
		t.Errorf("顶层键数不一致: go=%d yaml=%d", len(mGo), len(mYaml))
		for k := range mGo {
			if _, ok := mYaml[k]; !ok {
				t.Errorf("  仅 Go 版有: %s", k)
			}
		}
		for k := range mYaml {
			if _, ok := mGo[k]; !ok {
				t.Errorf("  仅 YAML 版有: %s", k)
			}
		}
	}

	for _, key := range []string{"proxies", "proxy-groups", "rules", "rule-providers",
		"global-ua", "ipv6", "allow-lan", "unified-delay", "tcp-concurrent",
		"geodata-mode", "geodata-loader", "geo-auto-update", "geo-update-interval",
		"geox-url", "profile"} {
		if !reflect.DeepEqual(mGo[key], mYaml[key]) {
			t.Errorf("字段 %q 不一致\n  go  = %#v\n  yaml= %#v", key, mGo[key], mYaml[key])
		} else {
			t.Logf("字段 %q 一致", key)
		}
	}
}

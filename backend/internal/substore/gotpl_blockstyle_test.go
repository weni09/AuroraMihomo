package substore

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 产物必须是块状（block）YAML，不能出现流式（flow）的花括号写法。
//
// 官方 Sub-Store 的转换产物是块状展开：
//
//	rule-providers:
//	  a1:
//	    type: http
//	    behavior: classical
//
// 而不是 `a1: {type: http, behavior: classical}`。
// 本项目的示例 Go 模板一度用手写花括号来复用字段（因为 text/template
// 没有构造列表的内置函数），产出与官方形态完全对不上，用户拿两边
// 做对照时无法解释。补了 list / fields 辅助函数后改为 range + 块状输出。
func TestGoTemplateProducesBlockStyle(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_gotemplate.tpl")
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	out, err := RenderMihomoOverride("gotemplate", string(b), blockStyleNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}

	// 流式映射：形如 `key: {a: 1, b: 2}`。
	// 只查"值以 { 开头"的行，避免误伤 Go 模板语法或 URL 里的花括号。
	flowRe := regexp.MustCompile(`(?m)^\s*[\w.-]+:\s*\{`)
	if m := flowRe.FindAllString(out, -1); len(m) > 0 {
		t.Errorf("产物出现流式花括号写法（官方为块状展开）: %q", m)
	}
	// 列表项的流式写法：`- {name: X, ...}`
	flowItemRe := regexp.MustCompile(`(?m)^\s*-\s*\{`)
	if m := flowItemRe.FindAllString(out, -1); len(m) > 0 {
		t.Errorf("列表项出现流式花括号写法: %q", m)
	}

	if !yamlIsValid(out) {
		t.Fatalf("产物不是合法 YAML:\n%s", firstLines(out, 40))
	}
}

// 缩进需与官方一致的 2 空格：4 空格会让两边 diff 满屏都是缩进变化。
func TestYAMLOutputUsesTwoSpaceIndent(t *testing.T) {
	out, err := NodesToMihomoYAML(blockStyleNodes())
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	// proxies 下的列表项应缩进 2 空格。
	// proxies 是产物首行，前面没有换行，故不能用 "\nproxies:" 匹配。
	if !strings.Contains(out, "proxies:\n  - ") {
		t.Errorf("proxies 列表项缩进不是 2 空格:\n%s", firstLines(out, 12))
	}
	if strings.Contains(out, "proxies:\n    - ") {
		t.Errorf("proxies 列表项仍是 4 空格缩进:\n%s", firstLines(out, 12))
	}
}

// 两条模板路径的 rule-providers 都应展开成块状，且字段齐全。
func TestGoTemplateRuleProvidersExpanded(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_gotemplate.tpl")
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	out, err := RenderMihomoOverride("gotemplate", string(b), blockStyleNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v", err)
	}
	providers, ok := cfg["rule-providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("rule-providers 结构异常: %#v", cfg["rule-providers"])
	}
	if len(providers) != 34 {
		t.Errorf("provider 数量应为 34，实际 %d", len(providers))
	}
	a1, ok := providers["a1"].(map[string]interface{})
	if !ok {
		t.Fatalf("a1 结构异常: %#v", providers["a1"])
	}
	for k, want := range map[string]interface{}{
		"type": "http", "interval": 86400, "behavior": "classical", "format": "text",
	} {
		if a1[k] != want {
			t.Errorf("a1[%s] 期望 %v，实际 %v", k, want, a1[k])
		}
	}
	if !strings.HasSuffix(a1["url"].(string), "/AI.list") {
		t.Errorf("a1.url 异常: %v", a1["url"])
	}
}

// list / fields 是让模板能输出块状结构的前提，缺任一个模板都会解析失败。
func TestGoTemplateListAndFieldsHelpers(t *testing.T) {
	out, err := execGoTemplate(`{{- range $p := list "a1 AI.list" "a2 CN.list" }}
{{- $f := fields $p }}
{{ index $f 0 }}: {{ index $f 1 }}
{{- end }}`, nil)
	if err != nil {
		t.Fatalf("list/fields 渲染失败: %v", err)
	}
	for _, want := range []string{"a1: AI.list", "a2: CN.list"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q，产出 %q", want, out)
		}
	}
}

func blockStyleNodes() []Node {
	return []Node{
		{Name: "👵 大妈节点", Type: "vless", Server: "a.com", Port: 443, UDP: true,
			Extra: map[string]interface{}{"uuid": "u1", "tls": true}},
		{Name: "🦁 香港A", Type: "vmess", Server: "b.com", Port: 80, UDP: true,
			Extra: map[string]interface{}{"uuid": "u2"}},
	}
}

func yamlIsValid(s string) bool {
	var v interface{}
	return yaml.Unmarshal([]byte(s), &v) == nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

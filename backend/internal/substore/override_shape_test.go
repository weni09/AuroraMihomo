package substore

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 产物形态必须对齐官方 Sub-Store：保序、块状、锚点已展开、2 空格缩进。
//
// 这几条此前都不成立，且都是"功能正常但人工无法核对"的问题：
//   - map 合并导致键按字母重排，模板里 proxies→…→rule-providers 的
//     布局被打散，锚点宿主键（pr / pr1 / rule-anchor）被冲到中间，
//     看起来像没被处理的残留
//   - Go 模板是纯文本替换，模板里写 `a1: {type: http, ...}` 就原样输出
//     流式花括号，而官方一律块状展开
//
// 用户拿本项目产物与官方产物对照时，上面任一条不成立都会得出
// "转换功能有问题"的结论，因此必须钉住。

// 一份贴近真实模板的覆写内容：流式花括号 + 显式 !!merge + 锚点宿主键，
// 且键的书写顺序刻意不是字母序。
const shapeTemplate = `global-ua: clash
ipv6: true
profile: {store-selected: true, store-fake-ip: true}
pr: &pr {type: fallback, proxies: [👵 大妈节点,🐂 所有-手动]}
pr1: &pr1 {type: select, proxies: [DIRECT,REJECT]}
proxy-groups:
  - {name: 🤖⚡ AI,!!merge <<: *pr}
  - {name: ⏱ 检测,!!merge <<: *pr1}
rules:
  - RULE-SET,a1,🤖⚡ AI
  - MATCH,🐼 国内
rule-anchor:
  class: &class {type: http, interval: 86400, behavior: classical, format: text}
rule-providers:
  a1: {!!merge <<: *class,url: "https://example.com/AI.list"}
  a2: {!!merge <<: *class,url: "https://example.com/CN.list"}
`

func shapeNodes() []Node {
	return []Node{
		{Name: "👵 大妈节点", Type: "vless", Server: "a.com", Port: 443, UDP: true,
			Extra: map[string]interface{}{"uuid": "u1", "tls": true}},
	}
}

var (
	flowMapRe  = regexp.MustCompile(`(?m)^\s*[\w.-]+:\s*\{`)
	flowItemRe = regexp.MustCompile(`(?m)^\s*-\s*\{`)
)

// 顶层键顺序必须跟随模板书写顺序，proxies（系统生成）排在最前。
func TestYAMLOverridePreservesKeyOrder(t *testing.T) {
	out, err := RenderMihomoOverride("yaml", shapeTemplate, shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// 锚点容器键（pr / pr1 / rule-anchor）不在其中：锚点已在各引用处
	// 展开，容器键随之丢弃，理由见 anchorScaffoldKeys 的注释。
	// 其余键保持模板书写顺序不变。
	want := []string{
		"proxies", // 系统生成的节点列表在最前
		"global-ua", "ipv6", "profile",
		"proxy-groups", "rules", "rule-providers",
	}
	got := topLevelKeys(t, out)
	if len(got) != len(want) {
		t.Fatalf("顶层键数量不符\n期望 %v\n实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个顶层键应为 %q，实际 %q\n完整顺序 %v", i+1, want[i], got[i], got)
		}
	}
}

// 锚点语法展开，且承载锚点的容器键一并丢弃。
func TestYAMLOverrideExpandsAnchorsDropsScaffoldKeys(t *testing.T) {
	out, err := RenderMihomoOverride("yaml", shapeTemplate, shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	for _, tok := range []string{"&pr", "*pr", "&class", "*class", "<<:", "!!merge"} {
		if strings.Contains(out, tok) {
			t.Errorf("锚点语法 %q 未展开:\n%s", tok, out)
		}
	}
	// 光去掉 & 与 * 符号不够：容器键本身留下来仍是 mihomo 不认识的顶层键，
	// 用户在预览里看到 pr / rule-anchor 原样列着，会认为锚点没被处理。
	keys := topLevelKeys(t, out)
	for _, k := range []string{"pr", "pr1", "rule-anchor"} {
		if contains(keys, k) {
			t.Errorf("锚点容器键 %q 应被丢弃，实际仍在顶层:\n%s", k, out)
		}
	}
	// 但展开后的内容必须完好——容器键消失不等于引用处的字段也跟着少了
	if !strings.Contains(out, "fallback") {
		t.Errorf("来自 *pr 的 type: fallback 丢失:\n%s", out)
	}
	if !strings.Contains(out, "classical") {
		t.Errorf("来自 *class 的 behavior: classical 丢失:\n%s", out)
	}
}

// 两条模板路径的产物都不得出现流式花括号。
func TestOverrideOutputsBlockStyle(t *testing.T) {
	yamlOut, err := RenderMihomoOverride("yaml", shapeTemplate, shapeNodes())
	if err != nil {
		t.Fatalf("YAML 覆写失败: %v", err)
	}
	// Go 模板：模板里就写着花括号，靠渲染后的规范化消除
	goOut, err := RenderMihomoOverride("gotemplate",
		"proxies:\n{{ proxiesYaml .Nodes | indent 2 }}\n"+
			"rule-providers:\n  a1: {type: http, behavior: classical, url: \"u1\"}\n"+
			"proxy-groups:\n  - {name: G, type: select, proxies: [DIRECT]}\n"+
			"rules:\n  - MATCH,G\n", shapeNodes())
	if err != nil {
		t.Fatalf("Go 模板失败: %v", err)
	}

	for _, c := range []struct{ label, out string }{
		{"YAML 覆写", yamlOut},
		{"Go 模板", goOut},
	} {
		if m := flowMapRe.FindAllString(c.out, -1); len(m) > 0 {
			t.Errorf("%s 产物出现流式映射 %q:\n%s", c.label, m, c.out)
		}
		if m := flowItemRe.FindAllString(c.out, -1); len(m) > 0 {
			t.Errorf("%s 产物出现流式列表项 %q:\n%s", c.label, m, c.out)
		}
	}
}

// Go 模板的规范化不得改变任何键值。
func TestGoTemplateNormalizationPreservesValues(t *testing.T) {
	out, err := RenderMihomoOverride("gotemplate",
		"rule-providers:\n  a1: {type: http, interval: 86400, behavior: classical, format: text, url: \"https://example.com/AI.list\"}\n", shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
	}
	a1 := cfg["rule-providers"].(map[string]interface{})["a1"].(map[string]interface{})
	for k, want := range map[string]interface{}{
		"type": "http", "interval": 86400, "behavior": "classical",
		"format": "text", "url": "https://example.com/AI.list",
	} {
		if a1[k] != want {
			t.Errorf("a1[%s] 期望 %v，实际 %v", k, want, a1[k])
		}
	}
}

// 非 YAML 产物（Go 模板也可用于生成别的格式）不得被规范化破坏。
func TestGoTemplateNonYAMLPassthrough(t *testing.T) {
	// 分享链接风格的产物：不是 YAML 映射，应原样返回
	tpl := "vless://uuid@a.com:443?type=tcp#节点A\nvless://uuid@b.com:443?type=tcp#节点B\n"
	out, err := RenderMihomoOverride("gotemplate", tpl, shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != tpl {
		t.Errorf("非 YAML 映射的产物被改写了\n期望 %q\n实际 %q", tpl, out)
	}
}

// 缩进为 2 空格递进：嵌套层的列表项相对父键多 2 空格。
func TestOverrideUsesTwoSpaceIndent(t *testing.T) {
	out, err := RenderMihomoOverride("yaml", shapeTemplate, shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// 用 profile 而非 pr 作锚点：后者是锚点容器键，已被丢弃。
	// profile.store-selected 在第二层，应是 2 空格。
	if !strings.Contains(out, "\nprofile:\n  store-selected: true\n") {
		t.Errorf("二层键缩进不是 2 空格:\n%s", out)
	}
	// proxy-groups 列表项的 proxies 在第三层，其列表项应是 6 空格
	// （- name 占 2，proxies 与之对齐为 4，项再递进 2）
	if !strings.Contains(out, "    proxies:\n      - 👵 大妈节点") {
		t.Errorf("嵌套列表项缩进不符合 2 空格递进:\n%s", out)
	}
}

// "+key" / "key+" / "key!" 合并修饰符在节点树实现下必须仍然生效
func TestNodeMergeModifiersStillWork(t *testing.T) {
	cases := []struct {
		name     string
		tpl      string
		wantHead string
		wantTail string
	}{
		{
			name:     "追加",
			tpl:      "rules+:\n  - MATCH,Extra\n",
			wantHead: "MATCH,Proxy",
			wantTail: "MATCH,Extra",
		},
		{
			name:     "前插",
			tpl:      "+rules:\n  - DOMAIN,x.com,DIRECT\n",
			wantHead: "DOMAIN,x.com,DIRECT",
			wantTail: "MATCH,Proxy",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RenderMihomoOverride("yaml", c.tpl, shapeNodes())
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			var cfg map[string]interface{}
			if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
				t.Fatalf("产物不是合法 YAML: %v", err)
			}
			rules, ok := cfg["rules"].([]interface{})
			if !ok || len(rules) < 2 {
				t.Fatalf("rules 结构异常: %#v", cfg["rules"])
			}
			if rules[0] != c.wantHead {
				t.Errorf("首条规则应为 %q，实际 %q", c.wantHead, rules[0])
			}
			if rules[len(rules)-1] != c.wantTail {
				t.Errorf("末条规则应为 %q，实际 %q", c.wantTail, rules[len(rules)-1])
			}
		})
	}

	// "key!" 强制覆盖：不与 base 合并
	out, err := RenderMihomoOverride("yaml", "proxy-groups!:\n  - name: OnlyOne\n    type: select\n    proxies: [DIRECT]\n", shapeNodes())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	groups := cfg["proxy-groups"].([]interface{})
	if len(groups) != 1 {
		t.Errorf("key! 应整体覆盖，期望 1 个组，实际 %d", len(groups))
	}
}

func topLevelKeys(t *testing.T, s string) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, s)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("产物顶层不是映射:\n%s", s)
	}
	m := doc.Content[0]
	out := make([]string, 0, len(m.Content)/2)
	for i := 0; i < len(m.Content); i += 2 {
		out = append(out, m.Content[i].Value)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

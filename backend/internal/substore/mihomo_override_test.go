package substore

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/model"

	"gopkg.in/yaml.v3"
)

func TestDeepMergeIntoReplace(t *testing.T) {
	base := map[string]interface{}{
		"a": "old",
		"nested": map[string]interface{}{
			"x": 1,
			"y": 2,
		},
	}
	override := map[string]interface{}{
		"a": "new",
		"nested": map[string]interface{}{
			"y": 20,
			"z": 30,
		},
	}
	deepMergeInto(base, override)

	if base["a"] != "new" {
		t.Errorf("scalar not replaced: got %v", base["a"])
	}
	nested, ok := base["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested not a map: %T", base["nested"])
	}
	if nested["x"] != 1 {
		t.Errorf("nested.x should stay from base, got %v", nested["x"])
	}
	if nested["y"] != 20 {
		t.Errorf("nested.y should be overridden, got %v", nested["y"])
	}
	if nested["z"] != 30 {
		t.Errorf("nested.z should be added, got %v", nested["z"])
	}
}

func TestDeepMergeIntoPrepend(t *testing.T) {
	base := map[string]interface{}{
		"rules": []interface{}{"MATCH,Proxy"},
	}
	override := map[string]interface{}{
		"+rules": []interface{}{"DOMAIN,google.com,DIRECT"},
	}
	deepMergeInto(base, override)

	rules, ok := base["rules"].([]interface{})
	if !ok {
		t.Fatalf("rules not a slice: %T", base["rules"])
	}
	want := []string{"DOMAIN,google.com,DIRECT", "MATCH,Proxy"}
	if len(rules) != len(want) {
		t.Fatalf("rules length = %d, want %d (%v)", len(rules), len(want), rules)
	}
	for i, w := range want {
		if rules[i] != w {
			t.Errorf("rules[%d] = %v, want %v", i, rules[i], w)
		}
	}
}

func TestDeepMergeIntoAppend(t *testing.T) {
	base := map[string]interface{}{
		"rules": []interface{}{"MATCH,Proxy"},
	}
	override := map[string]interface{}{
		"rules+": []interface{}{"DOMAIN,google.com,DIRECT"},
	}
	deepMergeInto(base, override)

	rules, ok := base["rules"].([]interface{})
	if !ok {
		t.Fatalf("rules not a slice: %T", base["rules"])
	}
	want := []string{"MATCH,Proxy", "DOMAIN,google.com,DIRECT"}
	if len(rules) != len(want) {
		t.Fatalf("rules length = %d, want %d (%v)", len(rules), len(want), rules)
	}
	for i, w := range want {
		if rules[i] != w {
			t.Errorf("rules[%d] = %v, want %v", i, rules[i], w)
		}
	}
}

func TestDeepMergeIntoForceOverwrite(t *testing.T) {
	base := map[string]interface{}{
		"proxy-groups": map[string]interface{}{
			"name": "Proxy",
			"type": "select",
		},
	}
	override := map[string]interface{}{
		// 目标 key 本身在 base 里是 map，若不加 ! 会被当对象递归合并；
		// 加 ! 后应整体覆盖，不保留 base 侧任何字段
		"proxy-groups!": map[string]interface{}{
			"type": "url-test",
		},
	}
	deepMergeInto(base, override)

	pg, ok := base["proxy-groups"].(map[string]interface{})
	if !ok {
		t.Fatalf("proxy-groups not a map: %T", base["proxy-groups"])
	}
	if _, exists := pg["name"]; exists {
		t.Errorf("force overwrite should drop base's other fields, but 'name' still present: %v", pg)
	}
	if pg["type"] != "url-test" {
		t.Errorf("proxy-groups.type = %v, want url-test", pg["type"])
	}
}

// TestDeepMergeIntoAppendTypedBaseSlice 复现浏览器验证时发现的真实 bug：
// buildBaseMihomoConfig 生成的 rules 是具体类型 []string（不是 []interface{}），
// 而 yaml.Unmarshal 出来的 override 侧永远是 []interface{}。
// 修复前 toSlice 只认 []interface{}，会把整个 []string 当标量包成一个元素，
// 追加后变成 [][]string 嵌套结构（YAML 里表现为 "- - MATCH,Proxy"），而不是
// 期望的两条平级规则。
func TestDeepMergeIntoAppendTypedBaseSlice(t *testing.T) {
	base := map[string]interface{}{
		"rules": []string{"MATCH,Proxy"}, // 具体类型，模拟 buildBaseMihomoConfig 的真实产出
	}
	override := map[string]interface{}{
		"rules+": []interface{}{"DOMAIN,example.com,DIRECT"}, // yaml.Unmarshal 的真实产出类型
	}
	deepMergeInto(base, override)

	rules, ok := base["rules"].([]interface{})
	if !ok {
		t.Fatalf("rules not a slice: %T", base["rules"])
	}
	if len(rules) != 2 {
		t.Fatalf("rules length = %d, want 2 (got %v)", len(rules), rules)
	}
	for _, r := range rules {
		if _, isSlice := r.([]string); isSlice {
			t.Fatalf("rules element is still a nested slice (bug not fixed): %v", rules)
		}
	}
	if rules[0] != "MATCH,Proxy" || rules[1] != "DOMAIN,example.com,DIRECT" {
		t.Errorf("unexpected rules order/content: %v", rules)
	}
}

func TestDeepMergeIntoNewKey(t *testing.T) {
	base := map[string]interface{}{}
	override := map[string]interface{}{
		"dns": map[string]interface{}{"enable": true},
	}
	deepMergeInto(base, override)

	dns, ok := base["dns"].(map[string]interface{})
	if !ok {
		t.Fatalf("dns not a map: %T", base["dns"])
	}
	if dns["enable"] != true {
		t.Errorf("dns.enable = %v, want true", dns["enable"])
	}
}

func TestApplyConfigScriptNormal(t *testing.T) {
	config := map[string]interface{}{
		"rules": []interface{}{"MATCH,Proxy"},
	}
	script := `
		function main(config) {
			config.rules.unshift("DOMAIN,google.com,DIRECT");
			config.injected = true;
			return config;
		}
	`
	result, err := applyConfigScript(config, script)
	if err != nil {
		t.Fatalf("applyConfigScript failed: %v", err)
	}
	if result["injected"] != true {
		t.Errorf("expected injected=true, got %v", result["injected"])
	}
	rules, ok := result["rules"].([]interface{})
	if !ok {
		t.Fatalf("rules not a slice: %T", result["rules"])
	}
	if len(rules) != 2 || rules[0] != "DOMAIN,google.com,DIRECT" {
		t.Errorf("unexpected rules: %v", rules)
	}
}

func TestApplyConfigScriptTimeout(t *testing.T) {
	config := map[string]interface{}{}
	script := `
		function main(config) {
			while (true) {}
			return config;
		}
	`
	_, err := applyConfigScript(config, script)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "已中断") {
		t.Errorf("expected timeout-related error message, got: %v", err)
	}
}

func TestApplyConfigScriptInvalidReturnType(t *testing.T) {
	config := map[string]interface{}{}
	script := `
		function main(config) {
			return "not an object";
		}
	`
	_, err := applyConfigScript(config, script)
	if err == nil {
		t.Fatal("expected error for invalid return type, got nil")
	}
}

func TestApplyConfigScriptNoMainFunction(t *testing.T) {
	config := map[string]interface{}{"a": 1}
	// 未定义 main 函数，应原样返回 config，不视为错误
	result, err := applyConfigScript(config, "var x = 1;")
	if err != nil {
		t.Fatalf("expected no error when main is undefined, got: %v", err)
	}
	// goja ExportTo 对整数值的 JS number 导出为 int64
	if result["a"] != int64(1) {
		t.Errorf("expected config echoed back unchanged, got: %v (%T)", result["a"], result["a"])
	}
}

func TestRenderMihomoOverrideYAML(t *testing.T) {
	nodes := []Node{{Name: "n1", Type: "ss", Server: "1.2.3.4", Port: 8443}}
	content := "rules+:\n  - DOMAIN,google.com,DIRECT\n"
	out, err := RenderMihomoOverride(model.TemplateLangYAML, content, nodes)
	if err != nil {
		t.Fatalf("RenderMihomoOverride failed: %v", err)
	}
	if !strings.Contains(out, "DOMAIN,google.com,DIRECT") {
		t.Errorf("expected appended rule in output, got:\n%s", out)
	}
	if !strings.Contains(out, "n1") {
		t.Errorf("expected node name in output, got:\n%s", out)
	}
}

func TestRenderMihomoOverrideJS(t *testing.T) {
	nodes := []Node{{Name: "n1", Type: "ss", Server: "1.2.3.4", Port: 8443}}
	content := `
		function main(config) {
			config.rules.unshift("DOMAIN,google.com,DIRECT");
			return config;
		}
	`
	out, err := RenderMihomoOverride(model.TemplateLangJS, content, nodes)
	if err != nil {
		t.Fatalf("RenderMihomoOverride failed: %v", err)
	}
	if !strings.Contains(out, "DOMAIN,google.com,DIRECT") {
		t.Errorf("expected injected rule in output, got:\n%s", out)
	}
}

func TestValidateTemplateLang(t *testing.T) {
	cases := []struct {
		name    string
		lang    string
		content string
		wantErr bool
	}{
		{"go template valid", model.TemplateLangGo, "{{ range .Nodes }}{{ .Name }}{{ end }}", false},
		{"go template invalid", model.TemplateLangGo, "{{ range .Nodes }", true},
		{"yaml valid", model.TemplateLangYAML, "rules:\n  - MATCH,Proxy\n", false},
		{"yaml invalid", model.TemplateLangYAML, "rules:\n  - a\n  b\n c", true},
		{"yaml compact flow colon", model.TemplateLangYAML, "pr1: &pr1 {type: select, proxies:[DIRECT,REJECT]}\n", false},
		{"js valid", model.TemplateLangJS, "function main(config){return config;}", false},
		{"js invalid", model.TemplateLangJS, "function main(config) {", true},
		{"empty content always valid", model.TemplateLangYAML, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTemplateLang(c.lang, c.content)
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// TestNormalizeYAMLFlowColons 锁定扫描器的行为边界：只在 flow 集合内补空格，
// 块上下文、引号、注释里的 `:[` 都不能动——动了会改变 YAML 语义。
func TestNormalizeYAMLFlowColons(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// flow 映射键后紧跟 flow 指示符：补空格
		{"x: {a:[1]}", "x: {a: [1]}"},
		{"x: {a:{b:1}}", "x: {a: {b:1}}"},
		{"pr2: &pr2 {type: url-test, proxies:[A, B]}", "pr2: &pr2 {type: url-test, proxies: [A, B]}"},
		// 嵌套 flow 序列里的键同样要补
		{"- {name: AI, proxies:[a]}", "- {name: AI, proxies: [a]}"},
		// 块上下文 `a:[1]` 是普通标量（键名含冒号），不能加空格
		{"a:[1]", "a:[1]"},
		{"key: value\nsub:[x]", "key: value\nsub:[x]"},
		// 引号内的 `:[` 是字符串内容，不动
		{`x: {a: "proxies:[1]"}`, `x: {a: "proxies:[1]"}`},
		{`x: {a: 'proxies:[1]'}`, `x: {a: 'proxies:[1]'}`},
		// 注释里的 `:[` 不动，后面的真实 flow 正常补
		{"# proxies:[a,b]\nx: {a:[1]}", "# proxies:[a,b]\nx: {a: [1]}"},
		// 无空格模式的文本原样返回
		{"a: b", "a: b"},
	}
	for _, c := range cases {
		if got := normalizeYAMLFlowColons(c.in); got != c.want {
			t.Errorf("normalizeYAMLFlowColons(%q)=\n  %q\nwant %q", c.in, got, c.want)
		}
	}
}

// compactFlowOverride 模拟真实 mihomo 配置的紧凑写法：flow 映射键后紧跟
// `[`（无空格）+ emoji 节点名 + `!!merge` 显式合并标签。这是从用户报障的
// 远程模板文件提炼出的最小可复现子集。
const compactFlowOverride = `pr: &pr {type: fallback, proxies: [👵 大妈节点,🐂 所有-手动]}
pr1: &pr1 {type: select, proxies:[DIRECT,REJECT,👵 大妈节点]}
proxy-groups:
  - {name: 🤖⚡ AI,!!merge <<: *pr}
  - {name: ⏱ 检测,!!merge <<: *pr1}
rules:
  - RULE-SET,a1,🤖⚡ AI
`

// TestRenderYAMLOverrideCompactFlow 验证带 `proxies:[` 紧凑写法的模板
// 能正常渲染：锚点展开、`!!merge` 生效、产物里不再出现无空格冒号。
func TestRenderYAMLOverrideCompactFlow(t *testing.T) {
	nodes := []Node{{Name: "HK-01", Type: "ss", Server: "1.2.3.4", Port: 8443}}
	out, err := RenderMihomoOverride(model.TemplateLangYAML, compactFlowOverride, nodes)
	if err != nil {
		t.Fatalf("紧凑 flow 模板渲染失败: %v", err)
	}
	if strings.Contains(out, "proxies:[") {
		t.Errorf("产物里不应残留无空格冒号:\n%s", out)
	}
	for _, token := range []string{"&pr", "&pr1", "<<:"} {
		if strings.Contains(out, token) {
			t.Errorf("锚点语法 %q 未被展开:\n%s", token, out)
		}
	}
	var got map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("渲染产物不是合法 YAML: %v\n%s", err, out)
	}
	groups, ok := got["proxy-groups"].([]interface{})
	if !ok || len(groups) != 2 {
		t.Fatalf("proxy-groups 应有 2 个，实际: %#v", got["proxy-groups"])
	}
	first, ok := groups[0].(map[string]interface{})
	if !ok {
		t.Fatalf("第一个策略组类型异常: %T", groups[0])
	}
	// *pr 合并来的 type 必须展开
	if first["type"] != "fallback" {
		t.Errorf("锚点未展开：AI 组 type 应为 fallback，实际 %v（完整: %#v）", first["type"], first)
	}
}

// anchorMergeConfig 是一份带 YAML 锚点合并的真实 mihomo 配置骨架。
//
// 用户从官方 Sub-Store 导出的配置普遍这么写：用 &pr 定义策略组模板，
// 再用 `!!merge <<: *pr` 显式标签复用到几十个策略组上；规则集同理。
// 这里保留了 `!!merge` 的显式写法——正是它暴露了前后端 YAML 宽严不一致的问题：
// 后端 yaml.v3 能解析，而前端 js-yaml 默认的 YAML 1.2 schema 会报
// "unknown scalar tag !<tag:yaml.org,2002:merge>" 把配置误判为非法。
const anchorMergeConfig = `
pr: &pr {type: fallback, proxies: [A, B]}
pr1: &pr1 {type: select, proxies: [DIRECT, REJECT]}
proxy-groups:
  - {name: AI, !!merge <<: *pr}
  - {name: Google, !!merge <<: *pr}
  - {name: Domestic, !!merge <<: *pr1}
  - {name: All-Auto, type: url-test, include-all: true, interval: 180}
rules:
  - RULE-SET,a1,AI
  - MATCH,Domestic
rule-anchor:
  class: &class {type: http, interval: 86400, behavior: classical, format: text}
rule-providers:
  a1: {!!merge <<: *class, url: "https://example.com/AI.list"}
  a2: {!!merge <<: *class, url: "https://example.com/Proxy.list"}
`

// TestValidateTemplateLangAcceptsAnchorMerge 锁定「带 !!merge 锚点的配置必须能保存」。
// 这是一份后端能正确渲染的配置，保存前校验不能把它拦下。
func TestValidateTemplateLangAcceptsAnchorMerge(t *testing.T) {
	if err := ValidateTemplateLang(model.TemplateLangYAML, anchorMergeConfig); err != nil {
		t.Errorf("带 !!merge 锚点的配置应通过 YAML 覆写校验，却报错: %v", err)
	}
	// Go 模板分支只做模板语法检查，锚点对它是普通文本，同样应放行
	if err := ValidateTemplateLang(model.TemplateLangGo, anchorMergeConfig); err != nil {
		t.Errorf("带 !!merge 锚点的配置应通过 Go 模板校验，却报错: %v", err)
	}
}

// TestRenderYAMLOverrideAnchorMerge 验证锚点配置经 YAML 覆写渲染后：
// 锚点被正确展开、用户的策略组/规则集全部保留、节点被注入 proxies。
func TestRenderYAMLOverrideAnchorMerge(t *testing.T) {
	nodes := []Node{
		{Name: "HK-01", Type: "ss", Server: "1.2.3.4", Port: 8443,
			Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}},
		{Name: "US-01", Type: "trojan", Server: "5.6.7.8", Port: 443,
			Extra: map[string]interface{}{"password": "q"}},
	}
	out, err := RenderMihomoOverride(model.TemplateLangYAML, anchorMergeConfig, nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}

	// 产物必须仍是合法 YAML，否则客户端拿到的是坏配置
	var got map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("渲染产物不是合法 YAML: %v\n产物:\n%s", err, out)
	}

	// 节点注入 proxies
	proxies, ok := got["proxies"].([]interface{})
	if !ok || len(proxies) != 2 {
		t.Fatalf("proxies 应含 2 个节点，实际: %#v", got["proxies"])
	}

	// 用户的 4 个策略组必须全部保留（不能被基础配置的单个 Proxy 组覆盖掉）
	groups, ok := got["proxy-groups"].([]interface{})
	if !ok || len(groups) != 4 {
		t.Fatalf("proxy-groups 应保留 4 个，实际: %d (%#v)", len(groups), got["proxy-groups"])
	}

	// 锚点必须已展开：AI 组应从 *pr 拿到 type=fallback 与 proxies=[A,B]
	first, ok := groups[0].(map[string]interface{})
	if !ok {
		t.Fatalf("第一个策略组类型异常: %T", groups[0])
	}
	if first["type"] != "fallback" {
		t.Errorf("锚点未展开：AI 组 type 应为 fallback，实际 %v（完整: %#v）", first["type"], first)
	}
	if pl, ok := first["proxies"].([]interface{}); !ok || len(pl) != 2 {
		t.Errorf("锚点未展开：AI 组 proxies 应为 [A B]，实际 %#v", first["proxies"])
	}

	// rule-providers 的锚点同样要展开：a1 应带上 *class 的 4 个字段 + 自己的 url
	rp, ok := got["rule-providers"].(map[string]interface{})
	if !ok || len(rp) != 2 {
		t.Fatalf("rule-providers 应有 2 项，实际: %#v", got["rule-providers"])
	}
	a1, ok := rp["a1"].(map[string]interface{})
	if !ok {
		t.Fatalf("a1 类型异常: %T", rp["a1"])
	}
	for _, k := range []string{"type", "interval", "behavior", "format", "url"} {
		if _, exists := a1[k]; !exists {
			t.Errorf("锚点未展开：a1 缺少字段 %q（完整: %#v）", k, a1)
		}
	}
}

// TestValidateTemplateLangAcceptsHelperFuncs 锁定「保存前校验必须认识 Go 模板
// 的辅助函数」。ValidateTemplateLang 若忘记注册 goTemplateFuncs，
// 用了 proxiesYaml 等函数的模板会在保存时被误判为语法错误，
// 而其实际渲染是正常的——这类前后不一致最难排查。
func TestValidateTemplateLangAcceptsHelperFuncs(t *testing.T) {
	// toYaml/proxyYaml/proxiesYaml 产出的都是多行块，必须换行后再 indent 对齐，
	// 不能写成 `key: {{ ... }}` 内联形式（那样会拼出非法 YAML）。
	tpl := `proxies:
{{ proxiesYaml .Nodes | indent 2 }}
proxy-groups:
  - name: Proxy
    type: select
    proxies:
{{ names .Nodes | toYaml | indent 6 }}
first: {{ with index .Nodes 0 }}{{ .Name | quote }}{{ end }}
one:
{{ with index .Nodes 0 }}{{ proxyYaml . | indent 2 }}{{ end }}
`
	if err := ValidateTemplateLang(model.TemplateLangGo, tpl); err != nil {
		t.Fatalf("使用辅助函数的 Go 模板应通过保存校验，却报错: %v", err)
	}
	// 校验通过的模板必须真的能渲染出合法 YAML，两者不能脱节
	nodes := []Node{
		{Name: "节点 A", Type: "vless", Server: "a.com", Port: 443,
			Extra: map[string]interface{}{"ws-opts": map[string]interface{}{"path": "/p"}}},
	}
	out, err := RenderMihomoOverride(model.TemplateLangGo, tpl, nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var v map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
	}
	px, ok := v["proxies"].([]interface{})
	if !ok || len(px) != 1 {
		t.Fatalf("proxies 异常: %#v", v["proxies"])
	}
	m := px[0].(map[string]interface{})
	if _, isMap := m["ws-opts"].(map[string]interface{}); !isMap {
		t.Errorf("ws-opts 应为 YAML 映射而非 Go map 字符串: %#v", m["ws-opts"])
	}
}

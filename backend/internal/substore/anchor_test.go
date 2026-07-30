package substore

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// YAML 覆写对锚点的处理分两步，两步都要做到：
//
//  1. 锚点定义（&x）、别名（*x）与合并键（<<: / !!merge <<:）展开成
//     实际内容，产物里不留这些语法符号。
//  2. 承载锚点的容器键（pr / pr1 / rule-anchor 这类本身不是 mihomo
//     配置项、只为写下锚点而存在的顶层键）一并丢弃。
//
// 关于第 2 步：官方 Sub-Store 会把容器键原样留着，mihomo 也确实忽略未知
// 顶层键，所以早期实现选择了对齐官方、保留容器键。但实际使用中这些键在
// 预览里原样列出（pr / pr1 / rule-anchor 后面跟着完整内容），用户看到的
// 就是"锚点根本没被处理"，反复被当成 bug 报上来。
//
// 现按产品决定改为丢弃：产物是给 mihomo 用的最终配置，不该带模板的
// 书写脚手架。代价是与官方产物存在这一处差异，属已知取舍。
// 判据是结构性的而非按名字硬编码，见 anchorScaffoldKeys。
const anchorTemplate = `pr: &pr
  type: select
  proxies:
    - 👵 大妈节点
    - 🐂 所有-手动
    - DIRECT
rule-anchor:
  domain: &domain
    type: http
    interval: 86400
    behavior: domain
    format: mrs
  class: &class
    type: http
    interval: 86400
    behavior: classical
    format: text
proxy-groups:
  - name: 🤖⚡ AI
    <<: *pr
  - name: ⏱ 检测
    <<: *pr
rule-providers:
  a1:
    <<: *class
    url: https://example.com/AI.list
  a2:
    <<: *domain
    url: https://example.com/CN.list
rules:
  - RULE-SET,a1,🤖⚡ AI
  - MATCH,🐼 国内
`

func TestYAMLOverrideExpandsAnchors(t *testing.T) {
	nodes := []Node{{Name: "👵 大妈节点", Type: "vmess", Server: "s.com", Port: 80}}
	out, err := RenderMihomoOverride("yaml", anchorTemplate, nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}

	// 锚点语法不得出现在产物里
	for _, token := range []string{"&pr", "*pr", "&domain", "*domain", "&class", "*class", "<<:"} {
		if strings.Contains(out, token) {
			t.Errorf("锚点语法 %q 未被展开:\n%s", token, out)
		}
	}

	// 承载锚点的容器键一并丢弃
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
	}
	for _, key := range []string{"pr", "rule-anchor"} {
		if _, ok := cfg[key]; ok {
			t.Errorf("锚点容器键 %q 应被丢弃，实际仍在产物里:\n%s", key, out)
		}
	}
}

// 引用处必须真的拿到锚点的内容，而不是空壳
func TestYAMLOverrideAnchorContentExpanded(t *testing.T) {
	nodes := []Node{{Name: "👵 大妈节点", Type: "vmess", Server: "s.com", Port: 80}}
	out, err := RenderMihomoOverride("yaml", anchorTemplate, nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v", err)
	}

	groups, ok := cfg["proxy-groups"].([]interface{})
	if !ok || len(groups) != 2 {
		t.Fatalf("proxy-groups 结构异常: %#v", cfg["proxy-groups"])
	}
	for _, g := range groups {
		m := g.(map[string]interface{})
		if m["type"] != "select" {
			t.Errorf("组 %v 未从锚点继承 type: select，实际 %v", m["name"], m["type"])
		}
		members, _ := m["proxies"].([]interface{})
		if len(members) != 3 {
			t.Errorf("组 %v 未从锚点继承 proxies 列表，实际 %#v", m["name"], m["proxies"])
		}
	}

	// 两个 provider 各自继承不同锚点，不能串味
	providers, ok := cfg["rule-providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("rule-providers 结构异常: %#v", cfg["rule-providers"])
	}
	a1 := providers["a1"].(map[string]interface{})
	if a1["behavior"] != "classical" || a1["format"] != "text" {
		t.Errorf("a1 应继承 class 锚点，实际 %#v", a1)
	}
	if a1["url"] != "https://example.com/AI.list" {
		t.Errorf("a1 自身的 url 被锚点覆盖了: %#v", a1["url"])
	}
	a2 := providers["a2"].(map[string]interface{})
	if a2["behavior"] != "domain" || a2["format"] != "mrs" {
		t.Errorf("a2 应继承 domain 锚点，实际 %#v", a2)
	}
}

// 显式 !!merge 标签写法（官方 Sub-Store 导出的配置里常见）同样要支持。
// 前端 utils/yaml.ts 为此显式切到 YAML 1.1 schema，后端这条路径也必须一致，
// 否则会出现"后端能存、前端校验不过"或反之的分歧。
func TestYAMLOverrideSupportsExplicitMergeTag(t *testing.T) {
	tpl := `base: &base
  type: select
  proxies: [DIRECT]
proxy-groups:
  - name: G
    !!merge <<: *base
rules:
  - MATCH,G
`
	nodes := []Node{{Name: "A", Type: "vmess", Server: "s.com", Port: 80}}
	out, err := RenderMihomoOverride("yaml", tpl, nodes)
	if err != nil {
		t.Fatalf("显式 !!merge 标签渲染失败: %v", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
	}
	groups := cfg["proxy-groups"].([]interface{})
	g := groups[0].(map[string]interface{})
	if g["type"] != "select" {
		t.Errorf("!!merge 未展开，组内容 %#v", g)
	}
}

// 容器键的判定必须是结构性的，不能按名字硬编码：
// 用户给锚点容器起什么名字都可能，而真实配置键即使长得像也不能删。
func TestAnchorScaffoldDetectionIsStructural(t *testing.T) {
	nodes := []Node{{Name: "N1", Type: "vmess", Server: "s.com", Port: 80}}

	cases := []struct {
		name     string
		template string
		dropped  []string // 应被丢弃的顶层键
		kept     []string // 必须保留的顶层键
	}{
		{
			name: "任意命名的容器键同样被丢弃",
			template: "我的模板: &tpl {type: select, proxies: [DIRECT]}\n" +
				"zzz-whatever:\n  x: &x {type: http}\n" +
				"proxy-groups:\n  - {name: G, <<: *tpl}\n",
			dropped: []string{"我的模板", "zzz-whatever"},
			kept:    []string{"proxy-groups"},
		},
		{
			name: "mihomo 已知键即使带锚点也必须保留",
			// dns 是真实配置项，作者顺手给它挂了锚点复用到别处，
			// 按名字判断会误删整段 DNS 配置
			template: "dns: &dns {enable: true, nameserver: [1.1.1.1]}\n" +
				"proxy-groups:\n  - {name: G, type: select, proxies: [DIRECT]}\n",
			kept: []string{"dns", "proxy-groups"},
		},
		{
			name: "混写了真实内容的容器键不整体丢弃",
			// 子项里有一个没锚点，说明这个键并非纯脚手架
			template: "mixed:\n  a: &a {type: http}\n  b: {type: http}\n" +
				"proxy-groups:\n  - {name: G, type: select, proxies: [DIRECT]}\n",
			kept: []string{"mixed", "proxy-groups"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RenderMihomoOverride("yaml", c.template, nodes)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			var cfg map[string]interface{}
			if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
				t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
			}
			for _, k := range c.dropped {
				if _, ok := cfg[k]; ok {
					t.Errorf("键 %q 应被丢弃，实际仍在:\n%s", k, out)
				}
			}
			for _, k := range c.kept {
				if _, ok := cfg[k]; !ok {
					t.Errorf("键 %q 必须保留，实际已丢失:\n%s", k, out)
				}
			}
		})
	}
}

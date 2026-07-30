package substore

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// marshalYAML 的核心约定：产出必须与 yaml.Marshal 语义完全等价，
// 只在可读性上更好（emoji 不再被转义成 \U0001F475）。
// 因此每个用例都同时断言"明文可读"与"反解回来一模一样"。

func TestMarshalYAMLKeepsEmojiReadable(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"四字节 emoji", "👵 大妈节点"},
		{"emoji 带变体选择符", "🏝️ 台湾-自动"},
		{"ZWJ 组合", "👨‍👩‍👦 家庭"},
		{"emoji 后紧跟冒号", "🤖: AI"},
		{"多个 emoji", "🐂 所有-手动 🐳 所有-自动"},
		{"emoji 与 CJK 混排", "🦁 香港-自动"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := marshalYAML(map[string]string{"name": c.in})
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if !strings.Contains(out, c.in) {
				t.Errorf("emoji 未以明文出现\n输入 %q\n产出 %s", c.in, out)
			}
			if strings.Contains(out, `\U`) || strings.Contains(out, `\u`) {
				t.Errorf("仍存在 unicode 转义: %s", out)
			}
			assertRoundTrip(t, out, map[string]string{"name": c.in})
		})
	}
}

// 不可打印的控制字符必须保持转义：还原它们会破坏 YAML 结构或丢失语义。
func TestMarshalYAMLKeepsControlEscapes(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		expect string
	}{
		{"制表符", "a\tb", `\t`},
		{"回车", "a\rb", `\r`},
		{"空字符", "a\x00b", `\0`},
		{"退格", "a\bb", `\b`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := marshalYAML(map[string]string{"name": c.in})
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			if !strings.Contains(out, c.expect) {
				t.Errorf("控制字符的转义被错误还原，应保留 %s\n产出 %s", c.expect, out)
			}
			assertRoundTrip(t, out, map[string]string{"name": c.in})
		})
	}
}

// 歧义值必须仍带引号，否则会被解析成布尔/数字/null 而改变类型。
func TestMarshalYAMLKeepsAmbiguousQuoting(t *testing.T) {
	for _, in := range []string{"12345", "true", "false", "no", "yes", "null", "~", "1.5", "007", "0x1f", "0123456789", "节点: 冒号", "# 井号"} {
		t.Run(in, func(t *testing.T) {
			out, err := marshalYAML(map[string]string{"name": in})
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			assertRoundTrip(t, out, map[string]string{"name": in})
		})
	}
}

// 还原转义后多余的双引号会被去掉，但去引号不安全的必须保留。
// 判定一律靠"反解后与原值相同"，不靠逐个枚举危险字符。
func TestMarshalYAMLUnquotesOnlyWhenSafe(t *testing.T) {
	cases := []struct {
		in          string
		wantQuoting bool // 是否必须保留双引号
		why         string
	}{
		{"👵 大妈节点", false, "普通 emoji 名，无需引号"},
		{"🐂 所有-手动", false, "含连字符也安全"},
		{"🕊️ Twitter(X)", false, "括号在标量内合法"},
		{"👵", false, "纯 emoji"},
		{"🤖⚡ AI", false, "连续 emoji"},
		{"🤖: AI", true, "冒号+空格去引号会被当成嵌套 mapping"},
		{"🤖 #井号", true, "空格+井号去引号会被当成注释"},
		{"🤖 AI: X: Y", true, "多个冒号"},
		{"🤖\tTab", true, "含制表符，必须保留 \\t 转义"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			out, err := marshalYAML(map[string]string{"name": c.in})
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			quoted := strings.Contains(out, `name: "`)
			if quoted != c.wantQuoting {
				t.Errorf("引号处理与预期不符（%s）\n期望保留引号=%v 实际=%v\n产出 %s",
					c.why, c.wantQuoting, quoted, out)
			}
			// 无论加不加引号，语义必须不变——这是最关键的断言
			assertRoundTrip(t, out, map[string]string{"name": c.in})
		})
	}
}

// 歧义值的引号不能因为"顺手去引号"而丢失：它们不含 BMP 外字符，
// 本就不该进入去引号逻辑。
func TestMarshalYAMLNeverUnquotesAmbiguous(t *testing.T) {
	for _, in := range []string{"true", "12345", "007", "null", "1.5"} {
		out, err := marshalYAML(map[string]string{"name": in})
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		if !strings.Contains(out, `"`+in+`"`) {
			t.Errorf("歧义值 %q 的引号被去掉了: %s", in, out)
		}
		assertRoundTrip(t, out, map[string]string{"name": in})
	}
}

// emoji 与需要转义的字符同时出现时，只还原 emoji，不碰其余转义。
func TestMarshalYAMLMixedEscapes(t *testing.T) {
	in := "👵 大妈\t节点"
	out, err := marshalYAML(map[string]string{"name": in})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(out, "👵") {
		t.Errorf("emoji 未还原: %s", out)
	}
	if !strings.Contains(out, `\t`) {
		t.Errorf("制表符转义被误还原: %s", out)
	}
	assertRoundTrip(t, out, map[string]string{"name": in})
}

// 真实场景：整份配置里 proxies / proxy-groups / rules 的 emoji 都应明文。
func TestRenderYAMLOverrideEmojiReadable(t *testing.T) {
	nodes := []Node{
		{Name: "👵 大妈节点", Type: "vless", Server: "a.com", Port: 443, UDP: true,
			Extra: map[string]interface{}{"uuid": "u1"}},
	}
	tpl := `proxy-groups:
  - name: 🤖⚡ AI
    type: select
    proxies:
      - 👵 大妈节点
      - DIRECT
rules:
  - MATCH,🐼 国内
`
	out, err := RenderMihomoOverride("yaml", tpl, nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	for _, want := range []string{"👵 大妈节点", "🤖⚡ AI", "MATCH,🐼 国内"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少明文 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `\U`) {
		t.Errorf("产物仍含 unicode 转义:\n%s", out)
	}
	// 必须仍是合法 YAML 且能解析出预期结构
	var back map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("产物不是合法 YAML: %v\n%s", err, out)
	}
	groups, ok := back["proxy-groups"].([]interface{})
	if !ok || len(groups) != 1 {
		t.Fatalf("proxy-groups 结构异常: %#v", back["proxy-groups"])
	}
	g := groups[0].(map[string]interface{})
	if g["name"] != "🤖⚡ AI" {
		t.Errorf("组名反解不一致: %q", g["name"])
	}
}

// NodesToMihomoYAML 是分享直链的默认出口，同样不应出现转义。
func TestNodesToMihomoYAMLEmojiReadable(t *testing.T) {
	nodes := []Node{{Name: "🦁 香港A", Type: "vmess", Server: "s.com", Port: 80}}
	out, err := NodesToMihomoYAML(nodes)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !strings.Contains(out, "🦁 香港A") || strings.Contains(out, `\U`) {
		t.Errorf("emoji 未明文输出:\n%s", out)
	}
	assertRoundTripYAML(t, out)
}

// Go 模板的辅助函数也要保持一致，否则两条路径产出不同
func TestGoTemplateHelpersEmojiReadable(t *testing.T) {
	nodes := []Node{{Name: "🎌 日本B", Type: "vmess", Server: "s.com", Port: 80}}
	out, err := execGoTemplate(`{{ proxiesYaml .Nodes }}`, nodes)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !strings.Contains(out, "🎌 日本B") || strings.Contains(out, `\U`) {
		t.Errorf("proxiesYaml 未明文输出 emoji:\n%s", out)
	}

	out2, err := execGoTemplate(`{{ quote "👵 大妈节点" }}`, nodes)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !strings.Contains(out2, "👵 大妈节点") || strings.Contains(out2, `\U`) {
		t.Errorf("quote 未明文输出 emoji: %s", out2)
	}
}

// isSafeToUnescape 是安全闸门的第一道，直接测它而不只测端到端：
// 控制字符等在正常流程里根本走不到改写路径（yaml.v3 不会把它们和
// emoji 一起放进同一个可还原的转义里），端到端测试因此覆盖不到这个判断。
// 但它一旦被改错，后续任何新增调用方都会立刻踩坑。
func TestIsSafeToUnescapeRejectsUnsafeRunes(t *testing.T) {
	unsafe := []struct {
		r   rune
		why string
	}{
		{'\t', "制表符：还原会破坏缩进结构"},
		{'\r', "回车"},
		{'\n', "换行：还原会拆行"},
		{0x00, "空字符"},
		{0x08, "退格"},
		{0x1B, "ESC"},
		{0x7F, "DEL"},
		{0x9F, "C1 控制区上界"},
		{'"', "双引号：会提前结束标量"},
		{'\\', "反斜杠：会引入新转义"},
		{0x85, "NEL：YAML 视为换行"},
		{0x2028, "行分隔符"},
		{0x2029, "段分隔符"},
		{0xFEFF, "BOM：不可见"},
		{0xD800, "UTF-16 高代理"},
		{0xDFFF, "UTF-16 低代理"},
	}
	for _, c := range unsafe {
		if isSafeToUnescape(c.r) {
			t.Errorf("U+%04X 应判为不可还原（%s）", c.r, c.why)
		}
	}

	safe := []struct {
		r   rune
		why string
	}{
		{'👵', "四字节 emoji"},
		{'⚡', "BMP 符号"},
		{'香', "CJK"},
		{' ', "空格：双引号标量内合法，且 emoji 名常含空格"},
		{'A', "ASCII 字母"},
		{0x1F1F9, "区域指示符（旗帜）"},
	}
	for _, c := range safe {
		if !isSafeToUnescape(c.r) {
			t.Errorf("U+%04X 应判为可还原（%s）", c.r, c.why)
		}
	}
}

// unquoteSafeScalars 只应对含 BMP 外字符的标量下手。
// 同理：歧义值本身不带转义、进不了改写流程，端到端测不到这个限定，
// 但去掉它会让 "true" / "12345" 这类引号被误删而改变类型。
func TestUnquoteSafeScalarsOnlyTouchesNonBMP(t *testing.T) {
	cases := []struct {
		in   string
		want string
		why  string
	}{
		{`name: "👵 大妈节点"`, `name: 👵 大妈节点`, "含 emoji，去引号"},
		{`name: "true"`, `name: "true"`, "歧义布尔值，必须保留引号"},
		{`name: "12345"`, `name: "12345"`, "歧义数字"},
		{`name: "007"`, `name: "007"`, "前导零"},
		{`  - "🐂 所有-手动"`, `  - 🐂 所有-手动`, "列表项含 emoji"},
		{`  - "plain"`, `  - "plain"`, "无 emoji 的列表项不动"},
		{`name: ""`, `name: ""`, "空串不动"},
	}
	for _, c := range cases {
		got := unquoteSafeScalars(c.in)
		if got != c.want {
			t.Errorf("%s\n输入 %q\n期望 %q\n实际 %q", c.why, c.in, c.want, got)
		}
	}
}

// 转义还原时遇到会破坏标量的码点必须原样保留。
// yaml.v3 目前不会产出 \u0022（双引号）这类转义，所以正常流程走不到，
// 但 rewriteLineEscapes 是纯文本处理，任何来源的输入都应安全。
func TestRewriteLineEscapesRefusesStructuralRunes(t *testing.T) {
	cases := []struct {
		in   string
		want string
		why  string
	}{
		{`name: "a\u0022b"`, `name: "a\u0022b"`, "双引号会提前结束标量"},
		{`name: "a\u005Cb"`, `name: "a\u005Cb"`, "反斜杠会引入新转义"},
		{`name: "a\u000Ab"`, `name: "a\u000Ab"`, "换行会拆行"},
		{`name: "a\u0009b"`, `name: "a\u0009b"`, "制表符"},
		{`name: "\U0001F475 x"`, `name: "👵 x"`, "emoji 应被还原"},
	}
	for _, c := range cases {
		got := rewriteLineEscapes(c.in)
		if got != c.want {
			t.Errorf("%s\n输入 %q\n期望 %q\n实际 %q", c.why, c.in, c.want, got)
		}
	}
}

// sameSemantics 是最后一道闸门，必须能识别出语义已变的候选文本。
func TestSameSemanticsGate(t *testing.T) {
	var before interface{}
	if err := yaml.Unmarshal([]byte(`name: "🤖: AI"`), &before); err != nil {
		t.Fatalf("基准解析失败: %v", err)
	}
	// 去掉引号后 YAML 解析会报错，闸门必须判为不等价
	if sameSemantics(before, `name: 🤖: AI`) {
		t.Error("去引号后已不是合法 YAML，闸门却判为等价")
	}
	// 同样内容应判为等价
	if !sameSemantics(before, `name: "🤖: AI"`) {
		t.Error("相同内容应判为等价")
	}
	// 类型变化必须识别
	var b2 interface{}
	_ = yaml.Unmarshal([]byte(`name: "12345"`), &b2)
	if sameSemantics(b2, `name: 12345`) {
		t.Error("string 变 int 应判为不等价")
	}
}

// assertRoundTrip 断言 YAML 文本反解后与期望值完全一致——
// 这是"只改可读性不改语义"的保证
func assertRoundTrip(t *testing.T, out string, want map[string]string) {
	t.Helper()
	var back map[string]string
	if err := yaml.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("产出不是合法 YAML: %v\n%s", err, out)
	}
	for k, v := range want {
		if back[k] != v {
			t.Errorf("反解不一致\nkey  %s\n期望 %q\n实际 %q\n产出 %s", k, v, back[k], out)
		}
	}
}

func assertRoundTripYAML(t *testing.T, out string) {
	t.Helper()
	var back map[string]interface{}
	if err := yaml.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("产出不是合法 YAML: %v\n%s", err, out)
	}
}

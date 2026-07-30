package substore

import (
	"bytes"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// marshalYAML 是本包所有 mihomo 配置输出的统一序列化入口。
//
// 存在的理由：yaml.v3 把 BMP 之外的字符（emoji，码点 > 0xFFFF）判为不可打印，
// 强制写成双引号转义形式，于是策略组名会变成
//
//   - "\U0001F475 大妈节点"
//
// 语义上没错（反解回来与原串相等），但一份含几十个 emoji 策略组的配置里
// 满屏都是 \U0001F...，人工核对和 diff 都无法进行，与官方 Sub-Store 及
// 各类现成模板的产物也对不上。yaml.v3 没有开关能关掉这个行为，
// 只能在 Marshal 之后把这类转义还原成明文。
//
// 安全性由两道保证：
//  1. 只还原 \Uxxxxxxxx / \uxxxx 中确实是"可打印"的码点（emoji、符号等），
//     控制字符（\t \r \0 \b …）一律保留转义——还原它们会破坏 YAML 结构。
//  2. 还原后重新解析，与还原前的解析结果做深度比对，不一致就丢弃改写、
//     回退到 yaml.v3 的原始产出。宁可可读性差，不可语义出错。
//
// yamlIndent 与官方 Sub-Store 产物保持一致的缩进宽度。
// yaml.Marshal 默认 4 空格，而官方及各类现成 mihomo 模板都用 2 空格，
// 差异会让用户拿两边产物做 diff 时满屏都是缩进变化。
const yamlIndent = 2

func marshalYAML(v interface{}) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(v); err != nil {
		// Encode 失败时 Close 的错误已无意义，优先返回根因
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return humanizeYAMLEscapes(buf.String()), nil
}

// marshalYAMLNode 序列化节点树，与 marshalYAML 共用缩进与 emoji 还原规则。
// 单独成函数是因为 *yaml.Node 必须按指针交给 Encode——走 interface{}
// 那条路会被当成普通结构体，序列化出一堆 Kind/Tag/Value 字段。
func marshalYAMLNode(n *yaml.Node) (string, error) {
	if n == nil {
		return "", nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(n); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return humanizeYAMLEscapes(buf.String()), nil
}

// 双引号标量内的 \Uxxxxxxxx（8 位）与 \uxxxx（4 位）转义。
// 前面的 (^|[^\\]) 用于排除 \\U 这种"转义了的反斜杠 + U"，
// 那种情况下 U 是普通字面量，不构成 unicode 转义。
var unicodeEscapeRe = regexp.MustCompile(`\\U[0-9a-fA-F]{8}|\\u[0-9a-fA-F]{4}`)

// humanizeYAMLEscapes 把 YAML 文本中可安全还原的 unicode 转义改回明文。
// 若还原后语义发生任何变化，返回原文。
func humanizeYAMLEscapes(s string) string {
	if !strings.Contains(s, `\U`) && !strings.Contains(s, `\u`) {
		return s
	}

	// 先记录改写前的语义，作为比对基准。原文解析不了（理论上不会发生，
	// 因为它刚由 yaml.Marshal 产出）时不做任何改写。
	var before interface{}
	if err := yaml.Unmarshal([]byte(s), &before); err != nil {
		return s
	}

	rewritten := rewriteEscapes(s)
	if rewritten == s {
		return s
	}
	if !sameSemantics(before, rewritten) {
		return s
	}

	// 转义还原后，双引号往往已无必要——它们本来只是为了容纳转义序列
	// （对照：BMP 内的 Ⓜ️ / ⏱ 这类符号 yaml.v3 从不加引号）。
	// 去掉后更贴近手写模板与官方 Sub-Store 的产物形态。
	// 但并非总能去：`🤖: AI` 去引号会被解析成嵌套 mapping 而报错，
	// 因此同样靠语义比对判定，不安全就保留引号。
	unquoted := unquoteSafeScalars(rewritten)
	if unquoted != rewritten && sameSemantics(before, unquoted) {
		return unquoted
	}
	return rewritten
}

// sameSemantics 判断 candidate 解析后与 before 是否完全一致。
// 这是所有文本改写的安全闸门：可读性提升不得改变任何语义。
func sameSemantics(before interface{}, candidate string) bool {
	var after interface{}
	if err := yaml.Unmarshal([]byte(candidate), &after); err != nil {
		return false
	}
	return reflect.DeepEqual(before, after)
}

// 匹配"整个值就是一个双引号标量"的行（行尾无注释、无尾随内容）。
// 分组 1 是缩进/列表符号/键前缀，分组 2 是引号内的内容。
var quotedScalarRe = regexp.MustCompile(`^(\s*(?:-\s+)?(?:[\w.+-][^:]*:\s+)?)"([^"\\]*)"$`)

// unquoteSafeScalars 逐行尝试去掉不再需要的双引号。
// 只对"含 BMP 外字符"的标量下手：其余引号可能是歧义值所必需
// （如 "true" / "12345" / "007"），去掉会改变类型。
// 生成的只是候选文本，是否采用由调用方的语义比对决定。
func unquoteSafeScalars(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		m := quotedScalarRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		inner := m[2]
		if inner == "" || !hasNonBMP(inner) {
			continue
		}
		lines[i] = m[1] + inner
	}
	return strings.Join(lines, "\n")
}

// hasNonBMP 判断字符串是否含 BMP 之外的字符（即被 yaml.v3 转义的那类）。
func hasNonBMP(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}

// rewriteEscapes 逐行处理：只改双引号标量，且只替换可打印码点的转义。
func rewriteEscapes(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if !strings.Contains(line, `\U`) && !strings.Contains(line, `\u`) {
			continue
		}
		lines[i] = rewriteLineEscapes(line)
	}
	return strings.Join(lines, "\n")
}

func rewriteLineEscapes(line string) string {
	out := unicodeEscapeRe.ReplaceAllStringFunc(line, func(esc string) string {
		// 前一个字符是反斜杠时说明这里是 \\U，U 不是转义引导符，原样保留。
		// ReplaceAllStringFunc 不提供位置信息，故在 replaced 后统一校验：
		// 由 humanizeYAMLEscapes 的反解比对兜底。
		code, err := strconv.ParseUint(esc[2:], 16, 32)
		if err != nil {
			return esc
		}
		r := rune(code)
		if !isSafeToUnescape(r) {
			return esc
		}
		return string(r)
	})
	return out
}

// isSafeToUnescape 判断某码点能否以明文出现在双引号 YAML 标量里。
//
// 放行的是"看得见、且不影响双引号标量解析"的字符：emoji、各类符号、
// CJK 扩展区等。明确排除：
//   - 控制字符（含 \t \r \n \0 \b \f \e 等）：还原会破坏行结构
//   - 双引号与反斜杠：还原会提前结束标量或引入新的转义
//   - 不成对的代理区码点：无法编码成合法 UTF-8
//   - 非字符与无效码点
//
// 注意不排除空格：emoji 名称里常见 "👵 大妈节点" 这种带空格的形式，
// 而空格在双引号标量内部是合法的。
func isSafeToUnescape(r rune) bool {
	if r == '"' || r == '\\' {
		return false
	}
	// C0/C1 控制字符与 DEL
	if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
		return false
	}
	// UTF-16 代理区：单独出现时不是合法字符
	if r >= 0xD800 && r <= 0xDFFF {
		return false
	}
	// NEL / LS / PS：YAML 视为换行符，还原会改变结构
	if r == 0x85 || r == 0x2028 || r == 0x2029 {
		return false
	}
	// BOM / 零宽不换行空格：不可见且可能影响解析
	if r == 0xFEFF {
		return false
	}
	if !utf8.ValidRune(r) {
		return false
	}
	return true
}

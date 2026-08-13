package substore

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 生产模板的「其他-自动」组用单字「泰」匹配泰国，误伤节点名里含「泰」
// 的非泰国节点（如「泰逢「美国|A」」是机场品牌名，节点实际在美国）。
// 这里锁定：模板 filter 必须用精确泰国词，且修复后「泰逢」不再进「其他」组、
// 真泰国节点仍被匹配。
func TestTemplateAutoGroupFilterNoFalseThai(t *testing.T) {
	for _, f := range []string{"testdata/mihomo_yaml_override.yaml", "testdata/mihomo_gotemplate.tpl", "testdata/mihomo_js_override.js"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)

		// 1. 「其他-自动」filter 不得再含裸单字「泰」（应为泰国精确词）
		other := extractFilter(s, "其他-自动")
		if other == "" {
			t.Fatalf("%s 找不到「其他-自动」filter", f)
		}
		if hasBareThai(other) {
			t.Errorf("%s 「其他-自动」filter 含裸单字「泰」，会误伤「泰逢」类节点: %q", f, other)
		}

		// 2. 修复后的 filter 必须可被 Go regexp 编译（mihomo filter 是 RE2，
		//    含 (?!) 等 lookahead 的模板在 mihomo 端可能被拒或静默失效）
		if _, err := regexp.Compile(other); err != nil {
			t.Errorf("%s 「其他-自动」filter 无法编译: %v", f, err)
		}
		us := extractFilter(s, "美国-自动")
		if us == "" {
			t.Fatalf("%s 找不到「美国-自动」filter", f)
		}
		if _, err := regexp.Compile(us); err != nil {
			t.Errorf("%s 「美国-自动」filter 无法编译: %v", f, err)
		}
	}
}

// 复现用户现象：修复后「泰逢「美国|A」」应只被美国组匹配，不被其它组匹配。
func TestTemplateAutoGroupNoFalseThaiSemantics(t *testing.T) {
	b, err := os.ReadFile("testdata/mihomo_yaml_override.yaml")
	if err != nil {
		t.Fatal(err)
	}
	other := extractFilter(string(b), "其他-自动")
	us := extractFilter(string(b), "美国-自动")

	reOther := regexp.MustCompile(other)
	reUS := regexp.MustCompile(us)

	if !reUS.MatchString("泰逢「美国|A」") {
		t.Errorf("「泰逢「美国|A」」应命中美国组")
	}
	if reOther.MatchString("泰逢「美国|A」") {
		t.Errorf("「泰逢「美国|A」」的「泰」是品牌名不是泰国，不应命中其他组")
	}
	if !reOther.MatchString("泰国-01") {
		t.Errorf("真泰国节点应命中其他组")
	}
	if !reOther.MatchString("Bangkok-01") {
		t.Errorf("曼谷节点应命中其他组")
	}
}

// extractFilter 从模板文本里取出指定策略组的 filter 表达式。
// 组名会同时出现在定义处与锚点/成员引用处，必须精确匹配定义行：
//   - YAML：`name: 🌀 其他-自动` 定义（内联或块状）
//   - JS：`['🌀 其他-自动', 180, '正则']` 数组定义
func extractFilter(tpl, groupName string) string {
	lines := strings.Split(tpl, "\n")
	for i, ln := range lines {
		// 定义行特征：含 name: 组名（YAML）或 以 [' 开头的 JS 数组且含组名
		isYAMLDef := strings.Contains(ln, "name:") && strings.Contains(ln, groupName)
		isJSDef := strings.HasPrefix(strings.TrimSpace(ln), "['") && strings.Contains(ln, groupName)
		if !isYAMLDef && !isJSDef {
			continue
		}
		// YAML 单行内联：filter 与 name 同行
		if inline := extractInlineFilter(ln); inline != "" {
			return inline
		}
		// YAML 块状：定义行之后的行
		for j := i; j < len(lines) && j < i+12; j++ {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "filter:") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "filter:"))
				return strings.Trim(val, `"'`)
			}
		}
		// JS 数组：['名称', 180, '正则']
		if inline := extractJSArrayFilter(ln); inline != "" {
			return inline
		}
	}
	return ""
}

// extractInlineFilter 从单行里取 filter: "..." 或 "filter": "..." 的值。
func extractInlineFilter(line string) string {
	for _, marker := range []string{`filter: "`, `filter: '`, `"filter": "`, `'filter': '`} {
		if idx := strings.Index(line, marker); idx >= 0 {
			rest := line[idx+len(marker):]
			var val string
			for _, c := range rest {
				if c == '"' || c == '\'' {
					break
				}
				val += string(c)
			}
			return val
		}
	}
	return ""
}

// extractJSArrayFilter 从 JS 数组行 ['名称', 180, '正则'] 取第三个元素。
func extractJSArrayFilter(line string) string {
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return ""
	}
	// 从第二个逗号后取字符串
	third := parts[2]
	third = strings.TrimSpace(third)
	third = strings.Trim(third, `'"[]`)
	// 去掉尾部可能的 ] 与后续
	if idx := strings.Index(third, "]"); idx >= 0 {
		third = third[:idx]
	}
	return strings.TrimSpace(third)
}

// hasBareThai 判断 filter 里是否存在独立的单字「泰」（非「泰国」「泰國」等精确词）。
func hasBareThai(filter string) bool {
	// 去掉「泰国」「泰國」后若仍含「泰」，说明存在裸单字
	rest := strings.ReplaceAll(filter, "泰国", "")
	rest = strings.ReplaceAll(rest, "泰國", "")
	return strings.Contains(rest, "泰")
}

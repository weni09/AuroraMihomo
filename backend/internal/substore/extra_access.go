package substore

import (
	"fmt"
	"strconv"
	"strings"
)

// Node.Extra 是 map[string]interface{}，值的实际类型取决于来源：
// 分享链接解析出来的多是 string / bool，Clash YAML 反序列化出来的
// 数字可能是 int 或 float64，字符串数组可能是 []interface{}。
// 各导出器若各自写类型断言，很容易漏掉某种形态而静默丢字段，
// 因此统一收敛到这几个读取器。

// extraString 读字符串字段；数字/布尔也按其字面量返回，
// 因为上游偶有把 port、alterId 之类写成字符串或反之的情况。
func extraString(n Node, key string) string {
	return valueString(n.Extra[key])
}

// firstExtraString 按顺序返回第一个非空字段，
// 用于同一语义在不同上游用不同键名的场景（如 sni / servername）。
func firstExtraString(n Node, keys ...string) string {
	for _, k := range keys {
		if v := valueString(n.Extra[k]); v != "" {
			return v
		}
	}
	return ""
}

func extraBool(n Node, key string) bool {
	switch v := n.Extra[key].(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		return err == nil && b
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

func extraInt(n Node, key string) int {
	switch v := n.Extra[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return i
		}
	}
	return 0
}

// extraStringSlice 读字符串数组，兼容 []string、[]interface{}
// 与 "a,b" 这种逗号分隔的单串写法。
func extraStringSlice(n Node, key string) []string {
	return stringSlice(n.Extra[key])
}

func stringSlice(v interface{}) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []interface{}:
		out := make([]string, 0, len(s))
		for _, it := range s {
			if str := valueString(it); str != "" {
				out = append(out, str)
			}
		}
		return out
	case string:
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

// mapString 从嵌套的 xxx-opts 里读字符串字段；opts 为 nil 时返回空串，
// 免得每个调用点都先判空。
func mapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	return valueString(m[key])
}

func mapValue(m map[string]interface{}, key string) interface{} {
	if m == nil {
		return nil
	}
	return m[key]
}

func valueString(v interface{}) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case bool:
		return strconv.FormatBool(s)
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case float64:
		// 整数值不要输出 1.000000 这种形式
		if s == float64(int64(s)) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return fmt.Sprint(s)
	}
}

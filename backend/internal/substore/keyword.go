package substore

import "strings"

// 节点名里的国家/地区标识常被分隔符包围（如 "香港-01"、"US_LA"、"[JP] Tokyo"），
// 因此 "us"、"in"、"ca" 这类两字母短码必须按词边界匹配，
// 否则 "Russia" 会被判成美国、"Singapore" 会被判成印度。
func isNameBoundary(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return false
	case b >= 'A' && b <= 'Z':
		return false
	case b >= '0' && b <= '9':
		return false
	default:
		return true
	}
}

// matchKeyword 判断节点名是否包含指定关键词。
// 纯 ASCII 短关键词（<= 3 字符）要求前后为分隔符或串首尾；
// 中文、emoji 及较长英文单词直接子串匹配。
func matchKeyword(lowerName, keyword string) bool {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return false
	}
	if !isShortASCII(kw) {
		return strings.Contains(lowerName, kw)
	}

	from := 0
	for {
		idx := strings.Index(lowerName[from:], kw)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(kw)
		leftOK := start == 0 || isNameBoundary(lowerName[start-1])
		rightOK := end == len(lowerName) || isNameBoundary(lowerName[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
		if from >= len(lowerName) {
			return false
		}
	}
}

func isShortASCII(s string) bool {
	if len(s) > 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

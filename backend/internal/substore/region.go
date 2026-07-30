package substore

import "strings"

// regionKeywords 各区域的常见命名关键词（含中文/英文/缩写/emoji 旗帜）
var regionKeywords = map[string][]string{
	"HK": {"hk", "hong kong", "hongkong", "香港", "港", "🇭🇰"},
	"TW": {"tw", "taiwan", "台湾", "台", "🇹🇼"},
	"JP": {"jp", "japan", "日本", "东京", "大阪", "🇯🇵"},
	"SG": {"sg", "singapore", "新加坡", "狮城", "🇸🇬"},
	"US": {"us", "usa", "united states", "america", "美国", "🇺🇸"},
	"KR": {"kr", "korea", "韩国", "首尔", "🇰🇷"},
	"UK": {"uk", "gb", "united kingdom", "britain", "英国", "🇬🇧"},
	"DE": {"de", "germany", "德国", "🇩🇪"},
	"FR": {"fr", "france", "法国", "🇫🇷"},
	"CA": {"ca", "canada", "加拿大", "🇨🇦"},
	"AU": {"au", "australia", "澳大利亚", "澳洲", "🇦🇺"},
	"RU": {"ru", "russia", "俄罗斯", "🇷🇺"},
	"IN": {"in", "india", "印度", "🇮🇳"},
	"TR": {"tr", "turkey", "土耳其", "🇹🇷"},
	"NL": {"nl", "netherlands", "荷兰", "🇳🇱"},
}

// regionOrder 固定地区匹配顺序。
// map 遍历顺序随机，若直接遍历 regionKeywords，
// 同一份订阅每次执行可能得到不同的国旗结果。
var regionOrder = []string{
	"HK", "TW", "JP", "SG", "KR", "US", "UK",
	"DE", "FR", "CA", "AU", "RU", "IN", "TR", "NL",
}

// regionFlags 各地区对应的国旗 emoji
var regionFlags = map[string]string{
	"HK": "🇭🇰", "TW": "🇹🇼", "JP": "🇯🇵", "SG": "🇸🇬",
	"US": "🇺🇸", "KR": "🇰🇷", "UK": "🇬🇧", "DE": "🇩🇪",
	"FR": "🇫🇷", "CA": "🇨🇦", "AU": "🇦🇺", "RU": "🇷🇺",
	"IN": "🇮🇳", "TR": "🇹🇷", "NL": "🇳🇱",
}

// SupportedRegions 返回所有可用区域代码
func SupportedRegions() []string {
	out := make([]string, 0, len(regionOrder))
	return append(out, regionOrder...)
}

// matchRegion 判断节点名是否属于指定区域
func matchRegion(name, region string) bool {
	kws, ok := regionKeywords[strings.ToUpper(strings.TrimSpace(region))]
	if !ok {
		return false
	}
	lower := strings.ToLower(name)
	for _, kw := range kws {
		if matchKeyword(lower, kw) {
			return true
		}
	}
	return false
}

// applyRegionFilter 按区域保留/剔除节点（Sub-Store: Region Filter）
// payload.action: keep | drop
// payload.regions: []string，如 ["HK","JP"]
func applyRegionFilter(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	action, _ := payload["action"].(string)
	if action == "" {
		action = "keep"
	}

	raw, _ := payload["regions"].([]interface{})
	regions := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok && strings.TrimSpace(s) != "" {
			regions = append(regions, s)
		}
	}
	if len(regions) == 0 {
		return nodes, nil
	}

	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		matched := false
		for _, region := range regions {
			if matchRegion(n.Name, region) {
				matched = true
				break
			}
		}
		if action == "drop" && matched {
			continue
		}
		if action == "keep" && !matched {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

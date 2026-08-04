package substore

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

// 从节点名解析流量信息的兜底逻辑（Sub-Store 的 parseTraffic/parseExpire 等价物）。
//
// 部分机场（V2Board 等面板）不下发标准的 subscription-userinfo 响应头，
// 只在部分节点的名字里写「剩余流量：1000 GB」「套餐到期：长期有效」，
// 原 Sub-Store 项目正是从节点名解析这些信息并在订阅信息中展示。
// 面板抓取时若响应头无 userinfo（UserInfo.IsZero），就用节点名兜底。
//
// 匹配语义对齐 Sub-Store：
//   - 「已用流量/使用流量：x」→ 已用量（存入 Download，Used() = Upload+Download）；
//   - 「剩余流量/剩余：x」→ 总量配额（Total）；
//   - 「流量：x」→ 总量配额（兜底，已用模式已在前面拦截）；
//   - 「到期时间/过期时间/套餐到期：日期」→ 到期时间（Expire，本地时区零点）；
//   - 「长期有效」「不限时」等非日期文本不匹配，保持 0（不限期）。

var (
	// 节点名里最常见的写法是「剩余流量：1000 GB」，数字与单位间可能有空格，
	// 单位大小写不一（gb/GB/GiB 少见，只认 B/KB/MB/GB/TB）。
	reTrafficSize = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(B|KB|MB|GB|TB)\b`)

	// 前缀词按交替顺序匹配，捕获组 1 即实际命中的词：
	// 「已用/使用」开头 → 已用量；「剩余」→ 总量配额；裸「流量」→ 总量兜底。
	// 用同一个正则+捕获组判断而非三个正则分别试，是为了避免「已用流量」
	// 先命中 used 分支后，裸「流量」又把它当 total 二次命中。
	// （Go 的 RE2 不支持负向前瞻，靠交替顺序 + 捕获组区分。）
	reTraffic = regexp.MustCompile(`(已用流量|使用流量|已用|剩余流量|剩余|流量)\s*[:：]?\s*`)

	// 日期写法覆盖 2026-12-31 / 2026/12/31 / 2026.12.31，可带可选时分秒。
	reExpire = regexp.MustCompile(`(?:到期时间|过期时间|套餐到期|到期)\s*[:：]?\s*(\d{4}[-/.]\d{1,2}[-/.]\d{1,2}(?:\s+\d{1,2}:\d{1,2}(?::\d{1,2})?)?)`)
)

// parseUserInfoFromNames 从节点名列表解析流量信息，找不到任何字段时返回零值。
func parseUserInfoFromNames(names []string) fetcher.UserInfo {
	var info fetcher.UserInfo
	for _, name := range names {
		used, total := parseTrafficFields(name)
		if info.Download == 0 && used > 0 {
			info.Download = used
		}
		if info.Total == 0 && total > 0 {
			info.Total = total
		}
		if info.Expire == 0 {
			if v, ok := parseExpireField(name); ok {
				info.Expire = v
			}
		}
		if info.Total != 0 && info.Expire != 0 && info.Download != 0 {
			break
		}
	}
	return info
}

// parseTrafficFields 在节点名里找「已用/剩余/流量」前缀并解析其后的
// 「数字+单位」，返回 (已用量, 总量)；未命中返回 (0, 0)。
func parseTrafficFields(name string) (used, total int64) {
	loc := reTraffic.FindStringSubmatchIndex(name)
	if loc == nil {
		return 0, 0
	}
	kind := name[loc[2]:loc[3]] // 捕获组 1：实际命中的前缀词
	rest := name[loc[1]:]
	m := reTrafficSize.FindStringSubmatch(rest)
	if m == nil {
		return 0, 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, 0
	}
	sz := sizeToBytes(v, strings.ToUpper(m[2]))
	if strings.HasPrefix(kind, "已用") || strings.HasPrefix(kind, "使用") {
		return sz, 0
	}
	return 0, sz
}

func sizeToBytes(v float64, unit string) int64 {
	mult := map[string]float64{
		"B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40,
	}[unit]
	return int64(v * mult)
}

// parseExpireField 解析节点名里的到期日期。非日期文本（「长期有效」等）
// 不匹配正则，返回 false 保持不限期。
func parseExpireField(name string) (int64, bool) {
	m := reExpire.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	dateStr := strings.TrimSpace(m[1])
	// 归一化分隔符，统一按 2006-1-2 布局解析；时区取本地（与 Sub-Store 一致，
	// 面板部署者与机场通常同处一国，用 UTC 会把「今天到期」误判成昨天/明天）。
	normalized := strings.NewReplacer("/", "-", ".", "-").Replace(dateStr)
	for _, layout := range []string{"2006-1-2 15:4:5", "2006-1-2 15:4", "2006-1-2"} {
		if t, err := time.ParseInLocation(layout, normalized, time.Local); err == nil {
			return t.Unix(), true
		}
	}
	return 0, false
}

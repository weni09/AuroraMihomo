package substore

import "strings"

// applyPacketEncoding 在输出 mihomo/Stash 代理项时补齐 packet-encoding。
//
// 对齐官方 Sub-Store（proxy-utils/index.js + clash meta producer）：
//   - 仅 vmess / vless
//   - 已有 packet-encoding（含显式空串）则不改
//   - 否则若 xudp / packet-addr 标志为真，分别映射为 xudp / packetaddr
//   - vless 仍缺省时写 xudp（与官方 VLESS URI 解析缺省一致，避免分享链导入后
//     与官方产物差这个键，UDP 中继行为不一致）
//
// 解析阶段：Clash YAML 原文已有的 packet-encoding 会进 Extra 并原样带出；
// VLESS 分享链在 parser 里按 query 写入。本函数兜底「有 xudp 无 encoding」
// 以及「vless 整条链路都没带上」的情况。
func applyPacketEncoding(n Node, item map[string]interface{}) {
	if item == nil {
		return
	}
	typ := strings.ToLower(n.Type)
	if typ != "vless" && typ != "vmess" {
		return
	}
	if _, exists := item["packet-encoding"]; exists {
		return
	}
	// Extra 里也可能已有（拷贝前被跳过的边界情况）
	if n.Extra != nil {
		if _, exists := n.Extra["packet-encoding"]; exists {
			item["packet-encoding"] = n.Extra["packet-encoding"]
			return
		}
	}

	if extraTruthy(n, item, "xudp") {
		item["packet-encoding"] = "xudp"
		return
	}
	if extraTruthy(n, item, "packet-addr") {
		item["packet-encoding"] = "packetaddr"
		return
	}
	if typ == "vless" {
		item["packet-encoding"] = "xudp"
	}
}

func extraTruthy(n Node, item map[string]interface{}, key string) bool {
	if item != nil {
		if v, ok := item[key]; ok && isTruthy(v) {
			return true
		}
	}
	if n.Extra != nil {
		if v, ok := n.Extra[key]; ok && isTruthy(v) {
			return true
		}
	}
	return false
}

func isTruthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true" || s == "yes"
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}

package engine

import (
	"strings"

	"auroramihomo/backend/internal/domain"

	"golang.org/x/text/unicode/norm"
)

// NormalizeName 按设计文档 §6 对节点/规则名称做标准化，
// 避免 "HK01" / " HK01" / "hk01" 被误判为三个不同对象。
//
// 处理顺序：
//  1. Unicode NFC 规范化（消除组合字符差异）
//  2. 去除首尾空白
//  3. 折叠内部连续空白为单个空格
func NormalizeName(s string) string {
	s = norm.NFC.String(s)
	s = strings.TrimSpace(s)
	return collapseSpaces(s)
}

// normalizeKey 生成用于「同一性判断」的比较键。
// 在 NormalizeName 基础上再做大小写折叠，使 HK01 与 hk01 视为同一节点。
func normalizeKey(s string) string {
	return strings.ToLower(NormalizeName(s))
}

func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\u00a0' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeConfig 在合并前统一清洗配置内的名称字段。
// 显示值保留 NormalizeName 的结果（保留原始大小写），
// 同一性比较由 normalizeKey 负责。
func NormalizeConfig(cfg *domain.Config) {
	if cfg == nil {
		return
	}

	for i := range cfg.Proxies {
		cfg.Proxies[i].Name = NormalizeName(cfg.Proxies[i].Name)
		cfg.Proxies[i].Server = strings.TrimSpace(cfg.Proxies[i].Server)
	}

	for i := range cfg.ProxyGroups {
		cfg.ProxyGroups[i].Name = NormalizeName(cfg.ProxyGroups[i].Name)
		for j := range cfg.ProxyGroups[i].Proxies {
			cfg.ProxyGroups[i].Proxies[j] = NormalizeName(cfg.ProxyGroups[i].Proxies[j])
		}
		for j := range cfg.ProxyGroups[i].Use {
			cfg.ProxyGroups[i].Use[j] = NormalizeName(cfg.ProxyGroups[i].Use[j])
		}
	}

	for i := range cfg.Rules {
		cfg.Rules[i] = normalizeRule(cfg.Rules[i])
	}
}

// normalizeRule 清洗规则行：去掉每个逗号分段的多余空白。
// 例如 "DOMAIN-SUFFIX , google.com ,  Proxy" -> "DOMAIN-SUFFIX,google.com,Proxy"
func normalizeRule(rule string) string {
	rule = norm.NFC.String(strings.TrimSpace(rule))
	if rule == "" {
		return ""
	}
	parts := strings.Split(rule, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ",")
}

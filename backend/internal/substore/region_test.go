package substore

import (
	"strings"
	"testing"
)

// 自动国旗词典覆盖回归：用户报告「蚯蚓」订阅中马来西亚/印尼/阿根廷/
// 新西兰/迪拜/比利时/意大利的节点漏加国旗。此处用真实节点名形态验证
// 中文关键词命中，并确认每个国家得到正确的国旗前缀。
func TestFlagCoversReportedCountries(t *testing.T) {
	cases := []struct {
		original string
		wantFlag string
	}{
		{"神农「马来西亚|B」", "🇲🇾"},
		{"常先「印尼|B」", "🇮🇩"},
		{"瑶姬「阿根廷|B」", "🇦🇷"},
		{"射手「新西兰|B」", "🇳🇿"},
		{"侍女「迪拜|B」", "🇦🇪"},
		{"侍女「迪拜D」", "🇦🇪"},
		{"水瓶「比利时|B」", "🇧🇪"},
		{"白羊「意大利|B」", "🇮🇹"},
	}

	nodes := make([]Node, 0, len(cases))
	for _, c := range cases {
		nodes = append(nodes, Node{Name: c.original})
	}
	out, err := applyFlag(nodes)
	if err != nil {
		t.Fatalf("applyFlag 失败: %v", err)
	}
	if len(out) != len(nodes) {
		t.Fatalf("applyFlag 不应增删节点，got %d want %d", len(out), len(nodes))
	}

	for i, c := range cases {
		got := out[i].Name
		if !strings.HasPrefix(got, c.wantFlag) {
			t.Errorf("%q 应加国旗 %s，实际 %q", c.original, c.wantFlag, got)
		}
		if !strings.Contains(got, c.original) {
			t.Errorf("%q 处理后应保留原名，实际 %q", c.original, got)
		}
	}
}

// 新增国家的英文/缩写关键词同样生效（词边界匹配，不误伤单词内部）。
func TestFlagMatchesNewCountriesEnglishAndAbbrev(t *testing.T) {
	cases := []struct {
		name     string
		wantFlag string
	}{
		{"Malaysia-01", "🇲🇾"},
		{"MY-01", "🇲🇾"},
		{"Indonesia-01", "🇮🇩"},
		{"ID-01", "🇮🇩"},
		{"Argentina-01", "🇦🇷"},
		{"New Zealand-01", "🇳🇿"},
		{"Dubai-01", "🇦🇪"},
		{"Belgium-01", "🇧🇪"},
		{"Italy-01", "🇮🇹"},
		{"IT-01", "🇮🇹"},
	}

	nodes := make([]Node, 0, len(cases))
	for _, c := range cases {
		nodes = append(nodes, Node{Name: c.name})
	}
	out, err := applyFlag(nodes)
	if err != nil {
		t.Fatalf("applyFlag 失败: %v", err)
	}
	for i, c := range cases {
		if got := out[i].Name; !strings.HasPrefix(got, c.wantFlag) {
			t.Errorf("%q 应加国旗 %s，实际 %q", c.name, c.wantFlag, got)
		}
	}
}

// 两字母缩写按词边界匹配：不应命中单词内部的片段
// （如 "India" 里的 "in"、"Belgium" 里的 "be"、"Italy" 里的 "it"）。
func TestFlagAbbrevRespectsWordBoundary(t *testing.T) {
	nodes := []Node{
		{Name: "India-01"},   // 应命中 IN（印度），不是 ID
		{Name: "Beijing-01"}, // "be" 在单词内部，不应命中比利时 BE
		{Name: "Capital-01"}, // "it" 在单词内部，不应命中意大利 IT
	}
	out, err := applyFlag(nodes)
	if err != nil {
		t.Fatalf("applyFlag 失败: %v", err)
	}

	// India → 🇮🇳（IN 先于 ID 匹配，且 "in" 词边界命中）
	if got := out[0].Name; !strings.HasPrefix(got, "🇮🇳") {
		t.Errorf("India 应加印度国旗，实际 %q", got)
	}
	// Beijing 含 "be" 但不是独立词 → 不加比利时旗
	if got := out[1].Name; strings.HasPrefix(got, "🇧🇪") {
		t.Errorf("Beijing 的 be 是单词内部片段，不应加比利时旗，实际 %q", got)
	}
	// Capital 含 "it" 但不是独立词 → 不加意大利旗
	if got := out[2].Name; strings.HasPrefix(got, "🇮🇹") {
		t.Errorf("Capital 的 it 是单词内部片段，不应加意大利旗，实际 %q", got)
	}
}

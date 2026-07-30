package engine

import (
	"testing"

	"auroramihomo/backend/internal/domain"
)

func findDiff(items []domain.DiffItem, kind, name string) *domain.DiffItem {
	for i := range items {
		if items[i].Kind == kind && items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

// 设计 §14：Diff Engine 必须覆盖 proxy / proxy-group / rule / provider 四类，
// 此前 BuildDiff 只处理了 proxy 与 rule，proxy-group 和 rule-provider 完全遗漏。
func TestBuildDiffCoversProxyGroupAndProvider(t *testing.T) {
	prev := &domain.Config{
		ProxyGroups: []domain.ProxyGroup{
			{Name: "Proxy", Type: "select", Proxies: []string{"HK01"}},
			{Name: "OldGroup", Type: "select", Proxies: []string{"JP01"}},
		},
		RuleProviders: map[string]domain.RuleProvider{
			"reject": {Type: "http", Behavior: "domain", URL: "http://a.com/reject.txt"},
			"stale":  {Type: "http", Behavior: "domain", URL: "http://a.com/stale.txt"},
		},
	}
	next := &domain.Config{
		ProxyGroups: []domain.ProxyGroup{
			{Name: "Proxy", Type: "select", Proxies: []string{"HK01", "JP01"}}, // 修改
			{Name: "NewGroup", Type: "select", Proxies: []string{"US01"}},      // 新增
			// OldGroup 被删除
		},
		RuleProviders: map[string]domain.RuleProvider{
			"reject": {Type: "http", Behavior: "domain", URL: "http://a.com/reject-v2.txt"}, // 修改
			"direct": {Type: "http", Behavior: "domain", URL: "http://a.com/direct.txt"},    // 新增
			// stale 被删除
		},
	}

	diff := BuildDiff(prev, next)

	if findDiff(diff.Changed, "proxy-group", "Proxy") == nil {
		t.Fatalf("Proxy 分组的 proxies 变化应体现为 changed，实际 changed=%+v", diff.Changed)
	}
	if findDiff(diff.Added, "proxy-group", "NewGroup") == nil {
		t.Fatalf("NewGroup 应体现为 added，实际 added=%+v", diff.Added)
	}
	if findDiff(diff.Removed, "proxy-group", "OldGroup") == nil {
		t.Fatalf("OldGroup 应体现为 removed，实际 removed=%+v", diff.Removed)
	}

	if findDiff(diff.Changed, "provider", "reject") == nil {
		t.Fatalf("reject provider 的 URL 变化应体现为 changed，实际 changed=%+v", diff.Changed)
	}
	if findDiff(diff.Added, "provider", "direct") == nil {
		t.Fatalf("direct provider 应体现为 added，实际 added=%+v", diff.Added)
	}
	if findDiff(diff.Removed, "provider", "stale") == nil {
		t.Fatalf("stale provider 应体现为 removed，实际 removed=%+v", diff.Removed)
	}
}

// 设计 §14 举例 `~ Rule Proxy -> DIRECT`：同一 matcher 只是 target 变了，
// 应体现为一条 changed，而不是"删除旧的一条 + 新增新的一条"两条互不相关的记录。
func TestBuildDiffRuleChangedSameMatcher(t *testing.T) {
	prev := &domain.Config{
		Rules: []string{
			"DOMAIN-SUFFIX,google.com,DIRECT",
			"DOMAIN-SUFFIX,removed.com,DIRECT",
			"MATCH,DIRECT",
		},
	}
	next := &domain.Config{
		Rules: []string{
			"DOMAIN-SUFFIX,google.com,PROXY", // target 变了：DIRECT -> PROXY
			"DOMAIN-SUFFIX,added.com,PROXY",  // 新增
			"MATCH,DIRECT",                   // 未变
		},
	}

	diff := BuildDiff(prev, next)

	changed := findDiff(diff.Changed, "rule", "DOMAIN-SUFFIX,google.com")
	if changed == nil {
		t.Fatalf("同 matcher 换 target 应体现为 changed，实际 changed=%+v", diff.Changed)
	}
	if changed.From != "DOMAIN-SUFFIX,google.com,DIRECT" || changed.To != "DOMAIN-SUFFIX,google.com,PROXY" {
		t.Fatalf("changed 记录的 From/To 应为完整规则行，实际 From=%v To=%v", changed.From, changed.To)
	}

	if findDiff(diff.Removed, "rule", "DOMAIN-SUFFIX,removed.com,DIRECT") == nil {
		t.Fatalf("removed.com 规则应体现为 removed，实际 removed=%+v", diff.Removed)
	}
	if findDiff(diff.Added, "rule", "DOMAIN-SUFFIX,added.com,PROXY") == nil {
		t.Fatalf("added.com 规则应体现为 added，实际 added=%+v", diff.Added)
	}

	// MATCH,DIRECT 完全未变，不应出现在任何一类里
	for _, items := range [][]domain.DiffItem{diff.Added, diff.Removed, diff.Changed} {
		if findDiff(items, "rule", "MATCH,DIRECT") != nil {
			t.Fatalf("未变化的规则不应出现在 diff 结果中: %+v", items)
		}
	}
}

// 同一 matcher 出现多条重复规则时，diff 不应崩溃或产生越界，
// 且数量对齐的重复项应被视为未变化。
func TestBuildDiffRuleDuplicateMatcher(t *testing.T) {
	prev := &domain.Config{
		Rules: []string{
			"DOMAIN,dup.com,DIRECT",
			"DOMAIN,dup.com,DIRECT",
		},
	}
	next := &domain.Config{
		Rules: []string{
			"DOMAIN,dup.com,DIRECT",
			"DOMAIN,dup.com,DIRECT",
		},
	}
	diff := BuildDiff(prev, next)
	if len(diff.Added)+len(diff.Removed)+len(diff.Changed) != 0 {
		t.Fatalf("完全相同的重复规则不应产生任何 diff 记录，实际 %+v", diff)
	}
}

// prev/next 为 nil 时不应 panic
func TestBuildDiffNilConfigsDoNotPanic(t *testing.T) {
	diff := BuildDiff(nil, nil)
	if len(diff.Added)+len(diff.Removed)+len(diff.Changed) != 0 {
		t.Fatalf("两侧均为空时 diff 应为空，实际 %+v", diff)
	}
}

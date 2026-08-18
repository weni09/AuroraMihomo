package engine

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

const rA1Base = `
proxies:
  - {name: L, type: ss, server: a.com, port: 1}
proxy-groups:
  - {name: Proxy, type: select, proxies: [L]}
rules:
  - MATCH,DIRECT
`
const rA1Remote = `
proxies:
  - {name: N1, type: ss, server: b.com, port: 2}
proxy-groups:
  - {name: Proxy, type: select, proxies: [N1]}
rules:
  - DOMAIN,google.com,Proxy
  - MATCH,Proxy
`

// A1: 订阅模板（含 MATCH,Proxy）+ 开箱 base（MATCH,DIRECT）默认 local：
// 订阅的 DOMAIN 规则正常保留且排在 MATCH 之前（可达），MATCH 沉底保留本地 DIRECT，冲突正确上报。
func TestVerifyADefaultLocalSubTemplate(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(rA1Base))
	remote, _ := e.LoadAndParse([]byte(rA1Remote))
	res := e.MergeDetailed(base, remote, nil, nil)
	out := res.Config.Rules
	t.Logf("rules: %v", out)
	if out[len(out)-1] != "MATCH,DIRECT" {
		t.Errorf("MATCH 应沉底保留本地，实际末尾 %q", out[len(out)-1])
	}
	mi := indexOf(out, "MATCH,DIRECT")
	gi := indexOf(out, "DOMAIN,google.com,Proxy")
	if gi < 0 || gi > mi {
		t.Errorf("订阅规则应位于 MATCH 之前（可达），got MATCH@%d DOMAIN@%d", mi, gi)
	}
	foundMatch := false
	for _, c := range res.Conflicts {
		if c.Type == "rule" && c.Path == "rules.MATCH" {
			foundMatch = true
			t.Logf("MATCH 冲突: %v -> %v res=%q", c.Local, c.Remote, c.Resolution)
		}
	}
	if !foundMatch {
		t.Error("MATCH 目标变化应产生冲突")
	}
}

// A2: MATCH 目标组缺失时（例如远程优先选了不存在的组），规则保留并产生告警，绝不破坏兜底。
func TestVerifyA2MatchTargetGroupMissingKeepsRuleWithWarning(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte("rules:\n  - MATCH,Blast\n"))
	remote, _ := e.LoadAndParse([]byte(""))
	res := e.MergeDetailed(base, remote, nil, nil)
	out := res.Config.Rules
	if len(out) == 0 || out[len(out)-1] != "MATCH,Blast" {
		t.Errorf("MATCH 缺失目标时不得删兜底，实际 %v", out)
	}
	warned := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "MATCH") && strings.Contains(w, "Blast") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("应产生 MATCH 缺失目标告警，实际 warnings=%v", res.Warnings)
	}
}

// A3: no-resolve 修饰词不得被误当成 target 策略名删除。
func TestVerifyA3NoResolveKept(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte("rules:\n  - MATCH,DIRECT,no-resolve\n"))
	remote, _ := e.LoadAndParse([]byte("rules:\n  - DOMAIN,a.com,DIRECT,no-resolve\n"))
	res := e.MergeDetailed(base, remote, nil, nil)
	all := strings.Join(res.Config.Rules, "|")
	for _, want := range []string{"MATCH,DIRECT,no-resolve", "DOMAIN,a.com,DIRECT,no-resolve"} {
		if !strings.Contains(all, want) {
			t.Errorf("no-resolve 规则不应被误删，缺少 %q", want)
		}
	}
	if len(res.Warnings) != 0 {
		t.Errorf("合法 no-resolve 不应产生误删告警，实际 %v", res.Warnings)
	}
}

// A4: 远程优先策略下规则替换生效且 MATCH 沉底为远程 MATCH。
func TestVerifyA4RemoteFirstDedupAndMatch(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte("rules:\n  - DOMAIN,a.com,DIRECT\n  - MATCH,DIRECT\n"))
	remote, _ := e.LoadAndParse([]byte("rules:\n  - DOMAIN,a.com,REJECT\n  - MATCH,REJECT\n"))
	policy := domain.DefaultMergePolicy()
	policy.RulePriority = "remote"
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, policy)
	out := res.Config.Rules
	t.Logf("rules: %v", out)
	if indexOf(out, "DOMAIN,a.com,REJECT") < 0 {
		t.Errorf("远程优先下 DOMAIN,a.com,REJECT 应替换本地，实际 %v", out)
	}
	if indexOf(out, "MATCH,REJECT") < 0 {
		t.Errorf("远程优先下 MATCH,REJECT 应替换本地，实际 %v", out)
	}
	if out[len(out)-1] != "MATCH,REJECT" {
		t.Errorf("MATCH,REJECT 应沉底，实际末尾 %q", out[len(out)-1])
	}
}

// A5: 多订阅聚合时保留双方普通规则且 MATCH 位于末尾。
func TestVerifyA5MultiSubAggregation(t *testing.T) {
	e := NewMergeEngine()
	sub1, _ := e.LoadAndParse([]byte("rules:\n  - DOMAIN,a.com,DIRECT\n  - MATCH,DIRECT\n"))
	sub2, _ := e.LoadAndParse([]byte("rules:\n  - DOMAIN,b.com,DIRECT\n  - MATCH,DIRECT\n"))
	agg := e.Merge(sub1, sub2)
	t.Logf("聚合: %v", agg.Rules)
	if indexOf(agg.Rules, "DOMAIN,a.com,DIRECT") < 0 || indexOf(agg.Rules, "DOMAIN,b.com,DIRECT") < 0 {
		t.Errorf("多订阅聚合应保留双方规则，实际 %v", agg.Rules)
	}
	if agg.Rules[len(agg.Rules)-1] != "MATCH,DIRECT" {
		t.Errorf("聚合后 MATCH 应沉底，实际末尾 %q", agg.Rules[len(agg.Rules)-1])
	}
}

func indexOf(rules []string, want string) int {
	for i, r := range rules {
		if r == want {
			return i
		}
	}
	return -1
}

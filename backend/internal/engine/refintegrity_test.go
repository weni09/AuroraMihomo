package engine

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

// 悬空引用会让 mihomo 拒绝加载整份配置，表现为每次合并都校验失败并回滚，
// 用户还看不出根因。合并阶段应主动摘掉并给出告警。
func TestPruneDanglingProxyGroupMember(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: MyGroup
    type: select
    proxies: [GoneNode, AliveNode, DIRECT]
rules:
  - MATCH,MyGroup
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - {name: AliveNode, type: ss, server: a.com, port: 1, cipher: aes-256-gcm, password: p}
`))

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())

	var group *domain.ProxyGroup
	for i := range res.Config.ProxyGroups {
		if res.Config.ProxyGroups[i].Name == "MyGroup" {
			group = &res.Config.ProxyGroups[i]
		}
	}
	if group == nil {
		t.Fatal("仍有可用成员的策略组不应被删除")
	}
	for _, ref := range group.Proxies {
		if ref == "GoneNode" {
			t.Error("已下线节点的引用应被移除")
		}
	}
	// 合法引用必须保留，不能过度删除
	if !contains(group.Proxies, "AliveNode") {
		t.Error("存在的节点引用应保留")
	}
	if !contains(group.Proxies, "DIRECT") {
		t.Error("内置策略 DIRECT 应保留")
	}
	if len(res.Warnings) == 0 {
		t.Error("自动移除失效引用后应产出告警，让用户知情")
	}
}

// 成员全部失效的策略组无法工作，应连同引用它的规则一起清理
func TestPruneEmptyGroupAndItsRules(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: DeadGroup
    type: select
    proxies: [Gone1, Gone2]
rules:
  - DOMAIN,x.com,DeadGroup
  - MATCH,DIRECT
`))
	remote, _ := e.LoadAndParse([]byte("proxies: []\n"))

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())

	for _, g := range res.Config.ProxyGroups {
		if g.Name == "DeadGroup" {
			t.Error("无可用成员的策略组应被移除")
		}
	}
	for _, r := range res.Config.Rules {
		if strings.Contains(r, "DeadGroup") {
			t.Errorf("指向已移除策略组的规则应被清理，实际残留 %q", r)
		}
	}
	// 合法规则不能被误删
	if !contains(res.Config.Rules, "MATCH,DIRECT") {
		t.Error("指向内置策略的规则应保留")
	}
}

// 策略组之间的相互引用、以及规则直接指向节点，都是合法的，不能误删
func TestPruneKeepsValidReferences(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: Inner
    type: select
    proxies: [N1]
  - name: Outer
    type: select
    proxies: [Inner, REJECT]
rules:
  - DOMAIN,a.com,N1
  - DOMAIN,b.com,Outer
  - MATCH,REJECT
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - {name: N1, type: ss, server: a.com, port: 1, cipher: aes-256-gcm, password: p}
`))

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())

	if len(res.Config.ProxyGroups) != 2 {
		t.Fatalf("两个策略组都合法，不该被删，实际剩 %d 个", len(res.Config.ProxyGroups))
	}
	if len(res.Config.Rules) != 3 {
		t.Errorf("三条规则都合法，不该被删，实际剩 %d 条: %v", len(res.Config.Rules), res.Config.Rules)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("没有失效引用时不该产生告警，实际 %v", res.Warnings)
	}
}

// 使用 use（proxy-provider）的策略组即使 proxies 为空也是合法的
func TestPruneKeepsGroupWithProviderOnly(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: FromProvider
    type: url-test
    use: [my-provider]
rules:
  - MATCH,FromProvider
`))
	remote, _ := e.LoadAndParse([]byte("proxies: []\n"))

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())
	found := false
	for _, g := range res.Config.ProxyGroups {
		if g.Name == "FromProvider" {
			found = true
		}
	}
	if !found {
		t.Fatal("依赖 proxy-provider 的策略组不应因 proxies 为空而被删除")
	}
	if !contains(res.Config.Rules, "MATCH,FromProvider") {
		t.Error("指向该组的规则应保留")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// include-all 型策略组没有 proxies / use，靠内核自动纳入节点。
// 悬空引用清理不能把它当空组删掉，否则订阅最常见的写法直接失效。
func TestPruneKeepsIncludeAllGroup(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: AutoAll
    type: url-test
    include-all: true
    filter: "HK|香港"
rules:
  - MATCH,AutoAll
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - {name: HK-1, type: ss, server: a.com, port: 1, cipher: aes-256-gcm, password: p}
`))
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())

	found := false
	for _, g := range res.Config.ProxyGroups {
		if g.Name == "AutoAll" {
			found = true
			if g.Extra["include-all"] != true {
				t.Errorf("include-all 字段应被保留，实际 %v", g.Extra["include-all"])
			}
			if g.Extra["filter"] != "HK|香港" {
				t.Errorf("filter 字段应被保留，实际 %v", g.Extra["filter"])
			}
		}
	}
	if !found {
		t.Fatal("include-all 型策略组不该被当作空组删除")
	}
	if !contains(res.Config.Rules, "MATCH,AutoAll") {
		t.Error("指向该组的规则应保留")
	}
}

// 策略组的官方参数按 Local First 合并：本地声明的不动，远程独有的补齐
func TestProxyGroupExtraMergesLocalFirst(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxy-groups:
  - name: G
    type: url-test
    proxies: [N1]
    filter: "本地过滤"
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - {name: N1, type: ss, server: a.com, port: 1, cipher: aes-256-gcm, password: p}
proxy-groups:
  - name: G
    type: url-test
    proxies: [N1]
    filter: "远程过滤"
    icon: "http://example.com/i.png"
    expected-status: "204"
`))
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())

	var g *domain.ProxyGroup
	for i := range res.Config.ProxyGroups {
		if res.Config.ProxyGroups[i].Name == "G" {
			g = &res.Config.ProxyGroups[i]
		}
	}
	if g == nil {
		t.Fatal("策略组 G 应存在")
	}
	if g.Extra["filter"] != "本地过滤" {
		t.Errorf("本地已声明的 filter 不应被远程覆盖，实际 %v", g.Extra["filter"])
	}
	if g.Extra["icon"] != "http://example.com/i.png" {
		t.Errorf("远程独有的 icon 应被补齐，实际 %v", g.Extra["icon"])
	}
	if g.Extra["expected-status"] != "204" {
		t.Errorf("远程独有的 expected-status 应被补齐，实际 %v", g.Extra["expected-status"])
	}
}

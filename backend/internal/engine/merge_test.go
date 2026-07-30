package engine

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

func TestMergeConflictAndDiff(t *testing.T) {
	baseYAML := `
proxies:
  - name: "HK01"
    type: ss
    server: a.com
    port: 443
rules:
  - "DOMAIN-SUFFIX,google.com,DIRECT"
`
	remoteYAML := `
proxies:
  - name: "HK01"
    type: ss
    server: b.com
    port: 443
  - name: "JP01"
    type: ss
    server: c.com
    port: 443
rules:
  - "DOMAIN-SUFFIX,google.com,Proxy"
`
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(baseYAML))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(remoteYAML))
	if err != nil {
		t.Fatal(err)
	}
	prev := &domain.Config{}
	res := e.MergeDetailed(base, remote, prev, nil)
	if len(res.Conflicts) < 2 {
		t.Fatalf("expected >=2 conflicts, got %d", len(res.Conflicts))
	}
	if len(res.Config.Proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(res.Config.Proxies))
	}
	// local proxy kept by default
	if res.Config.Proxies[0].Server != "a.com" && res.Config.Proxies[1].Server != "a.com" {
		t.Fatalf("local proxy not kept")
	}
	foundJP := false
	for _, p := range res.Config.Proxies {
		if p.Name == "JP01" {
			foundJP = true
		}
	}
	if !foundJP {
		t.Fatal("remote unique proxy missing")
	}
	if len(res.Diff.Added) == 0 {
		t.Fatal("expected diff added")
	}
}

func TestMergeResolvedRemote(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxies:
  - name: "HK01"
    type: ss
    server: a.com
    port: 1
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - name: "HK01"
    type: ss
    server: b.com
    port: 1
`))
	res1 := e.MergeDetailed(base, remote, nil, nil)
	var c domain.Conflict
	for _, x := range res1.Conflicts {
		if x.Type == "proxy" {
			c = x
			c.Resolution = "remote"
			break
		}
	}
	res2 := e.MergeDetailed(base, remote, nil, []domain.Conflict{c})
	ok := false
	for _, p := range res2.Config.Proxies {
		if p.Name == "HK01" && p.Server == "b.com" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("remote resolution not applied")
	}
	_ = strings.Contains
}

// 设计 §11/§16：DNS/TUN 默认 Local First，用户显式选择 remote 策略时才采用远程值。
// 此前 DNSPriority 字段存在但引擎从不读取，是一个死配置。
func TestMergeDNSTUNPolicy(t *testing.T) {
	baseYAML := `
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - "114.114.114.114"
tun:
  enable: false
  stack: system
`
	remoteYAML := `
dns:
  enable: true
  enhanced-mode: redir-host
  nameserver:
    - "8.8.8.8"
tun:
  enable: true
  stack: gvisor
`
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(baseYAML))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(remoteYAML))
	if err != nil {
		t.Fatal(err)
	}

	// 默认策略（local）：DNS/TUN 必须保持本地值
	def := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())
	if def.Config.DNS.EnhancedMode != "fake-ip" {
		t.Fatalf("默认策略下 DNS 应保持本地值，实际 %q", def.Config.DNS.EnhancedMode)
	}
	if def.Config.TUN.Stack != "system" {
		t.Fatalf("默认策略下 TUN 应保持本地值，实际 %q", def.Config.TUN.Stack)
	}

	// 显式选择 remote 策略：DNS/TUN 应采用远程值
	remotePolicy := domain.MergePolicy{
		ProxyPriority: "local", RulePriority: "local",
		DNSPriority: "remote", TUNPriority: "remote",
	}
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, remotePolicy)
	if res.Config.DNS.EnhancedMode != "redir-host" {
		t.Fatalf("remote 策略下 DNS 应采用远程值，实际 %q", res.Config.DNS.EnhancedMode)
	}
	if res.Config.TUN.Stack != "gvisor" {
		t.Fatalf("remote 策略下 TUN 应采用远程值，实际 %q", res.Config.TUN.Stack)
	}
}

// remote 未声明 DNS/TUN 段时（零值），即使策略选 remote 也不能用空配置抹掉本地设置
func TestMergeDNSTUNPolicyRemoteEmpty(t *testing.T) {
	baseYAML := `
dns:
  enable: true
  enhanced-mode: fake-ip
tun:
  enable: false
  stack: system
`
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(baseYAML))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(`proxies: []`))
	if err != nil {
		t.Fatal(err)
	}

	remotePolicy := domain.MergePolicy{DNSPriority: "remote", TUNPriority: "remote"}
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, remotePolicy)
	if res.Config.DNS.EnhancedMode != "fake-ip" {
		t.Fatalf("远程未声明 DNS 时应保留本地值，实际 %q", res.Config.DNS.EnhancedMode)
	}
	if res.Config.TUN.Stack != "system" {
		t.Fatalf("远程未声明 TUN 时应保留本地值，实际 %q", res.Config.TUN.Stack)
	}
}

// 需求：Mihomo 配置支持官方所有参数——本地表单未显式建模的字段落入 General 兜底 map。
// 此前合并时只拷贝了 base.General，完全丢弃 remote.General，导致订阅里任何
// 未被强类型建模的顶层字段（如 tls / experimental / profile 等）合并后静默消失。
// 现要求：本地已有的键保持本地值（Local First），本地缺失的键由远程补齐。
func TestMergeGeneralFieldsRemoteFillsMissing(t *testing.T) {
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(`
mode: rule
profile:
  store-selected: true
`))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(`
profile:
  store-selected: false
experimental:
  quic-go-disable-gso: true
`))
	if err != nil {
		t.Fatal(err)
	}

	res := e.MergeDetailed(base, remote, nil, nil)

	// 远程独有的顶层字段必须被保留，而不是被丢弃
	exp, ok := res.Config.General["experimental"]
	if !ok {
		t.Fatalf("远程独有的 General 字段应被合并补齐，实际 General=%+v", res.Config.General)
	}
	expMap, ok := exp.(map[string]interface{})
	if !ok {
		t.Fatalf("experimental 字段类型异常: %#v", exp)
	}
	if expMap["quic-go-disable-gso"] != true {
		t.Fatalf("experimental.quic-go-disable-gso 应为 true，实际 %+v", expMap)
	}

	// profile 是显式建模字段，属于 Local First，应保留本地值而非远程值
	if !res.Config.Profile.StoreSelected {
		t.Fatalf("profile.store-selected 应遵循 Local First 保留本地 true，实际 %+v", res.Config.Profile)
	}
}

// 需求核心场景：远程配置为空时，最终生成的配置应直接等价于本地配置
// （标量字段、DNS/TUN 等系统字段、General 兜底字段全部原样保留）。
func TestMergeRemoteEmptyFallsBackToLocal(t *testing.T) {
	e := NewMergeEngine()
	baseYAML := `
mode: rule
port: 7890
external-controller: 127.0.0.1:9090
find-process-mode: strict
dns:
  enable: true
  enhanced-mode: fake-ip
tun:
  enable: true
  stack: system
experimental:
  quic-go-disable-gso: true
proxies:
  - name: HK01
    type: ss
    server: a.com
    port: 443
rules:
  - "DOMAIN-SUFFIX,google.com,DIRECT"
`
	base, err := e.LoadAndParse([]byte(baseYAML))
	if err != nil {
		t.Fatal(err)
	}
	// 空远程配置，等价于「远程配置为空时使用本地配置」的场景
	remote, err := e.LoadAndParse([]byte(``))
	if err != nil {
		t.Fatal(err)
	}

	res := e.MergeDetailed(base, remote, nil, nil)

	if len(res.Conflicts) != 0 {
		t.Fatalf("远程为空时不应产生任何冲突，实际 %+v", res.Conflicts)
	}
	if res.Config.Mode != "rule" || res.Config.Port != 7890 || res.Config.ExternalController != "127.0.0.1:9090" {
		t.Fatalf("远程为空时标量字段应与本地一致，实际 %+v", res.Config)
	}
	if res.Config.FindProcessMode != "strict" {
		t.Fatalf("远程为空时新增标量字段(find-process-mode)也应遵循 Local First，实际 %q", res.Config.FindProcessMode)
	}
	if !res.Config.DNS.Enable || res.Config.DNS.EnhancedMode != "fake-ip" {
		t.Fatalf("远程为空时 DNS 应与本地一致，实际 %+v", res.Config.DNS)
	}
	if !res.Config.TUN.Enable || res.Config.TUN.Stack != "system" {
		t.Fatalf("远程为空时 TUN 应与本地一致，实际 %+v", res.Config.TUN)
	}
	if len(res.Config.Proxies) != 1 || res.Config.Proxies[0].Name != "HK01" {
		t.Fatalf("远程为空时代理列表应与本地一致，实际 %+v", res.Config.Proxies)
	}
	if len(res.Config.Rules) != 1 || res.Config.Rules[0] != "DOMAIN-SUFFIX,google.com,DIRECT" {
		t.Fatalf("远程为空时规则应与本地一致，实际 %+v", res.Config.Rules)
	}
	exp, ok := res.Config.General["experimental"]
	if !ok {
		t.Fatalf("远程为空时 General 兜底字段应与本地一致，实际 General=%+v", res.Config.General)
	}
	if expMap, ok := exp.(map[string]interface{}); !ok || expMap["quic-go-disable-gso"] != true {
		t.Fatalf("远程为空时 experimental 字段应与本地一致，实际 %+v", exp)
	}
}

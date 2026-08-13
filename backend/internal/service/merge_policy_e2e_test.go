package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/engine"
	"auroramihomo/backend/internal/model"

	"gopkg.in/yaml.v3"
)

// 本地与远程对同名节点/同 matcher 规则给出不同取值，用于触发冲突。
const conflictingLocalYAML = `
mode: rule
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - 223.5.5.5
proxies:
  - name: HK01
    type: ss
    server: local.example.com
    port: 1111
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
`

// 注意：订阅内容会经 Sub-Store 转换为「节点 + 固定模板」，
// 源 YAML 里的 dns/rules 段不会进入远程层。因此服务层这条 e2e 只断言
// 节点级冲突的策略走向；dns/tun/rules 的策略语义由 engine 包的单测覆盖
// （见 engine.TestMergeDNSTUNPolicy / TestMergeConflictAndDiff）。
const conflictingRemoteYAML = `
proxies:
  - name: HK01
    type: ss
    server: remote.example.com
    port: 2222
    cipher: aes-256-gcm
    password: pw
`

// applyWithPolicy 用指定策略跑一次完整的合并落盘，返回解析后的最终配置。
//
// 远程内容通过一个真实订阅记录提供：MergeAndApplyDetailed 内部会先执行
// buildRemoteConfig 重建 remote-merged 行，直接预置该行会被覆盖掉。
// 订阅用 Content 字段承载 YAML，从而避免测试发起网络请求。
func applyWithPolicy(t *testing.T, proxy, rule, dns string) (*domain.Config, int) {
	t.Helper()
	svc, db, _ := newTestConfigService(t)

	if err := svc.UpdateBaseConfig(conflictingLocalYAML); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}
	if err := db.CreateSubscription(&model.Subscription{
		Name:    "conflicting",
		Content: conflictingRemoteYAML,
		Enabled: 1,
	}); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	// 远程来源默认为 none（不使用远程配置），那样就不存在远程层、
	// 也无从产生冲突。本测试要验证策略在冲突下的走向，故显式选 all。
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	svc.SetPolicyProvider(func() domain.MergePolicy {
		return domain.MergePolicy{
			ProxyPriority: proxy,
			RulePriority:  rule,
			DNSPriority:   dns,
			TUNPriority:   "local",
		}
	})

	res, err := svc.MergeAndApplyDetailed(context.Background(), MergeWithRefresh(0))
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	raw, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg domain.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("生成配置非法: %v", err)
	}
	return &cfg, res.ConflictCount
}

// 本地优先：冲突项全部取本地值，但仍应上报冲突供用户知情。
func TestMergePolicyLocalFirstWins(t *testing.T) {
	cfg, conflicts := applyWithPolicy(t, "local", "local", "local")

	if conflicts == 0 {
		t.Error("本地与远程存在差异时应上报冲突")
	}

	var hk *domain.Proxy
	for i := range cfg.Proxies {
		if cfg.Proxies[i].Name == "HK01" {
			hk = &cfg.Proxies[i]
		}
	}
	if hk == nil {
		t.Fatal("最终配置应包含 HK01")
	}
	if hk.Server != "local.example.com" || hk.Port != 1111 {
		t.Errorf("本地优先时应取本地节点值，实际 server=%s port=%d", hk.Server, hk.Port)
	}

	// 本地规则与本地 DNS 属于 Local First，远程模板不得覆盖
	if !containsRule(cfg.Rules, "DOMAIN-SUFFIX,example.com,DIRECT") {
		t.Errorf("本地优先时应保留本地规则，实际 %v", cfg.Rules)
	}
	if cfg.DNS.EnhancedMode != "fake-ip" {
		t.Errorf("本地优先时 DNS 应取本地值，实际 %q", cfg.DNS.EnhancedMode)
	}
}

// 远程优先：同名冲突项改取远程值。
func TestMergePolicyRemoteFirstWins(t *testing.T) {
	cfg, conflicts := applyWithPolicy(t, "remote", "remote", "remote")

	if conflicts == 0 {
		t.Error("本地与远程存在差异时应上报冲突")
	}

	var hk *domain.Proxy
	for i := range cfg.Proxies {
		if cfg.Proxies[i].Name == "HK01" {
			hk = &cfg.Proxies[i]
		}
	}
	if hk == nil {
		t.Fatal("最终配置应包含 HK01")
	}
	if hk.Server != "remote.example.com" || hk.Port != 2222 {
		t.Errorf("远程优先时应取远程节点值，实际 server=%s port=%d", hk.Server, hk.Port)
	}

	// 远程层不含 dns 段，即使策略选 remote 也不能用空值抹掉本地 DNS
	if cfg.DNS.EnhancedMode != "fake-ip" {
		t.Errorf("远程未声明 DNS 时应保留本地值，实际 %q", cfg.DNS.EnhancedMode)
	}

	// 同一 matcher 不应同时留下两条规则，否则后一条永远不会生效
	count := 0
	for _, r := range cfg.Rules {
		if strings.HasPrefix(r, "DOMAIN-SUFFIX,example.com,") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("同一 matcher 应只保留一条规则，实际 %d 条: %v", count, cfg.Rules)
	}
}

func containsRule(rules []string, want string) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

// 冲突应按合并策略自动解决，不留给用户手动处理：
// local/remote/merge 策略下，被策略解决的冲突必须标记 resolved=1，
// 控制台 unresolvedCount 不应再显示它们。
func TestMergePolicyAutoResolvesConflicts(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	if err := svc.UpdateBaseConfig(conflictingLocalYAML); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}
	if err := db.CreateSubscription(&model.Subscription{
		Name:    "policy-test",
		Content: conflictingRemoteYAML,
	}); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})
	// 默认策略（全 local）：所有冲突都按 local 自动解决
	svc.SetPolicyProvider(func() domain.MergePolicy { return domain.DefaultMergePolicy() })

	if _, err := svc.MergeAndApplyDetailed(context.Background(), MergeWithRefresh(0)); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	// 合并后：自动解决的冲突不应留在 unresolved 列表里
	rows, err := db.ListConflicts(true) // onlyUnresolved
	if err != nil {
		t.Fatalf("查询冲突失败: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("local/remote 策略下冲突应被自动解决，仍有 %d 条 unresolved: %+v", len(rows), rows)
	}
}

// 引擎应给每条冲突按策略填 Resolution，供持久层判断是否自动解决。
func TestEngineConflictsCarryResolution(t *testing.T) {
	e := engine.NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxies:
  - name: A
    type: ss
    server: local.example.com
    port: 1111
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - name: A
    type: ss
    server: remote.example.com
    port: 2222
`))
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())
	if len(res.Conflicts) == 0 {
		t.Fatal("应产生冲突")
	}
	found := false
	for _, c := range res.Conflicts {
		if c.Type == "proxy" {
			found = true
			if c.Resolution != "local" {
				t.Fatalf("默认 local 策略下 proxy 冲突 Resolution 应为 local，实际 %q", c.Resolution)
			}
		}
	}
	if !found {
		t.Fatal("应有 proxy 冲突")
	}
}

// 自动解决的冲突不得持久化为 resolved 记录：否则用户改策略后，旧的
// 自动解决记录会被 loadResolvedConflicts 重新应用，覆盖新策略。
// 先 local 合并（自动解决），再改 remote 合并，最终必须取远程值。
func TestAutoResolvedConflictDoesNotOverrideLaterPolicy(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	if err := svc.UpdateBaseConfig(conflictingLocalYAML); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}
	if err := db.CreateSubscription(&model.Subscription{
		Name:    "policy-test",
		Content: conflictingRemoteYAML,
	}); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	// 第一次：local 策略合并（冲突自动解决为本地）
	svc.SetPolicyProvider(func() domain.MergePolicy { return domain.DefaultMergePolicy() })
	if _, err := svc.MergeAndApplyDetailed(context.Background(), MergeWithRefresh(0)); err != nil {
		t.Fatalf("第一次合并失败: %v", err)
	}

	// 第二次：remote 策略合并。若旧的自动解决记录被重新应用，HK01 会
	// 被改回本地值——这是回归。正确行为：跟随新策略取远程值。
	svc.SetPolicyProvider(func() domain.MergePolicy {
		p := domain.DefaultMergePolicy()
		p.ProxyPriority = "remote"
		return p
	})
	if _, err := svc.MergeAndApplyDetailed(context.Background(), MergeLocalOnly()); err != nil {
		t.Fatalf("第二次合并失败: %v", err)
	}

	raw, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg domain.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("生成配置非法: %v", err)
	}
	var hk *domain.Proxy
	for i := range cfg.Proxies {
		if cfg.Proxies[i].Name == "HK01" {
			hk = &cfg.Proxies[i]
		}
	}
	if hk == nil {
		t.Fatal("最终配置应包含 HK01")
	}
	if hk.Server != "remote.example.com" || hk.Port != 2222 {
		t.Fatalf("改 remote 策略后应取远程值，实际 server=%s port=%d（旧自动解决记录覆盖了新策略）", hk.Server, hk.Port)
	}
}

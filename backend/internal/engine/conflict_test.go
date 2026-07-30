package engine

import (
	"testing"

	"auroramihomo/backend/internal/domain"
)

// 设计 §12：冲突类型需覆盖 dns / tun / provider
func TestDetectDNSConflict(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
dns:
  enable: true
  enhanced-mode: fake-ip
`))
	remote, _ := e.LoadAndParse([]byte(`
dns:
  enable: false
  enhanced-mode: redir-host
`))
	res := e.MergeDetailed(base, remote, nil, nil)

	found := false
	for _, c := range res.Conflicts {
		if c.Type == "dns" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应检测到 dns 冲突，实际冲突: %+v", res.Conflicts)
	}

	// Local First：合并结果必须保留本地 DNS
	if !res.Config.DNS.Enable {
		t.Error("DNS 应遵循 Local First，保留本地 enable=true")
	}
}

func TestDetectTUNConflict(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
tun:
  enable: true
  stack: system
`))
	remote, _ := e.LoadAndParse([]byte(`
tun:
  enable: false
  stack: gvisor
`))
	res := e.MergeDetailed(base, remote, nil, nil)

	found := false
	for _, c := range res.Conflicts {
		if c.Type == "tun" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应检测到 tun 冲突，实际冲突: %+v", res.Conflicts)
	}
	if !res.Config.TUN.Enable {
		t.Error("TUN 应遵循 Local First")
	}
}

func TestDetectProviderConflict(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
rule-providers:
  apple:
    type: http
    behavior: domain
    url: https://local.example/apple.yaml
    path: ./apple.yaml
`))
	remote, _ := e.LoadAndParse([]byte(`
rule-providers:
  apple:
    type: http
    behavior: domain
    url: https://remote.example/apple.yaml
    path: ./apple.yaml
`))
	res := e.MergeDetailed(base, remote, nil, nil)

	found := false
	for _, c := range res.Conflicts {
		if c.Type == "provider" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应检测到 provider 冲突，实际冲突: %+v", res.Conflicts)
	}

	// Base 优先
	if res.Config.RuleProviders["apple"].URL != "https://local.example/apple.yaml" {
		t.Error("provider 应 Base 优先")
	}
}

// remote 未声明系统配置时不应误报冲突
func TestNoSystemConflictWhenRemoteEmpty(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
dns:
  enable: true
tun:
  enable: true
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - name: HK01
    type: ss
    server: a.com
    port: 443
`))
	res := e.MergeDetailed(base, remote, nil, nil)

	for _, c := range res.Conflicts {
		if c.Type == "dns" || c.Type == "tun" || c.Type == "sniffer" {
			t.Fatalf("远程未声明系统配置时不应报冲突: %+v", c)
		}
	}
}

// 设计 §13：merge 策略 —— 本地为基准，远程补齐缺失字段
func TestMergeResolutionStrategy(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxies:
  - name: HK01
    type: ss
    server: a.com
    port: 443
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - name: HK01
    type: ss
    server: b.com
    port: 443
    cipher: aes-256-gcm
    password: secret
`))

	first := e.MergeDetailed(base, remote, nil, nil)
	var target domain.Conflict
	for _, c := range first.Conflicts {
		if c.Type == "proxy" {
			target = c
			target.Resolution = "merge"
			break
		}
	}
	if target.Type == "" {
		t.Fatal("未产生 proxy 冲突，无法验证 merge 策略")
	}

	res := e.MergeDetailed(base, remote, nil, []domain.Conflict{target})

	var got *domain.Proxy
	for i := range res.Config.Proxies {
		if normalizeKey(res.Config.Proxies[i].Name) == normalizeKey("HK01") {
			got = &res.Config.Proxies[i]
		}
	}
	if got == nil {
		t.Fatal("未找到合并后的 HK01")
	}

	// 本地已有的 server 必须保留
	if got.Server != "a.com" {
		t.Errorf("merge 策略应以本地为基准，server 期望 a.com，实际 %q", got.Server)
	}
	// 本地缺失的字段应由远程补齐
	if got.Extra["cipher"] != "aes-256-gcm" {
		t.Errorf("merge 策略应用远程补齐 cipher，实际 %v", got.Extra["cipher"])
	}
	if got.Extra["password"] != "secret" {
		t.Errorf("merge 策略应用远程补齐 password，实际 %v", got.Extra["password"])
	}
}

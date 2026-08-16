package diagnostics

import "testing"

// presetTargets 是 DefaultTargets 的固定预设清单（不含代理目标），
// 供测试核对包含关系与目标数量。
func presetTargets() []DiagnosticTarget {
	return []DiagnosticTarget{
		{Type: TypeDNS, Target: "api.github.com"},
		{Type: TypeTCP, Target: "api.github.com", Port: 443},
		{Type: TypeHTTP, Target: "https://api.github.com/"},
		{Type: TypeTCP, Target: "raw.githubusercontent.com", Port: 443},
		{Type: TypeHTTP, Target: "https://raw.githubusercontent.com/"},
		{Type: TypeDNS, Target: "1.1.1.1"},
		{Type: TypeTCP, Target: "1.1.1.1", Port: 53},
		{Type: TypeDNS, Target: "8.8.8.8"},
		{Type: TypeTCP, Target: "8.8.8.8", Port: 53},
		{Type: TypeDNS, Target: "223.5.5.5"},
		{Type: TypeTCP, Target: "223.5.5.5", Port: 53},
	}
}

// containsTarget 报告 targets 中是否存在与 want 完全相同的目标。
func containsTarget(targets []DiagnosticTarget, want DiagnosticTarget) bool {
	for _, t := range targets {
		if t == want {
			return true
		}
	}
	return false
}

// hasProxyTarget 报告 targets 中是否含有默认代理端口上的 TCP 目标。
func hasProxyTarget(targets []DiagnosticTarget) bool {
	for _, t := range targets {
		if t.Type == TypeTCP && t.Port == DefaultProxyPort {
			return true
		}
	}
	return false
}

func TestDefaultTargets(t *testing.T) {
	// 注入可用的代理地址 → 结果应同时包含全部预设目标与代理 TCP 目标
	targets := DefaultTargets(func() string { return "http://127.0.0.1:7890" })

	for _, want := range presetTargets() {
		if !containsTarget(targets, want) {
			t.Fatalf("预设目标应包含 %+v, 实际 %+v", want, targets)
		}
	}

	// 代理地址解析出主机+端口 → 追加 TCP <主机>:<端口>
	proxy := DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: 7890}
	if !containsTarget(targets, proxy) {
		t.Fatalf("应包含代理目标 %+v, 实际 %+v", proxy, targets)
	}

	// 代理 URL 未声明端口 → 使用默认端口 7890
	withDefault := DefaultTargets(func() string { return "http://127.0.0.1" })
	if !containsTarget(withDefault, proxy) {
		t.Fatalf("代理 URL 缺省端口应使用 %d, 实际 %+v", DefaultProxyPort, withDefault)
	}
}

func TestDefaultTargetsProxyUnavailable(t *testing.T) {
	cases := []struct {
		name string
		fn   func() string
	}{
		{"空地址", func() string { return "" }},
		{"非法 URL", func() string { return "://bad" }},
		{"缺少主机名", func() string { return "http://" }},
		{"nil 回调", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			targets := DefaultTargets(c.fn)
			if len(targets) != len(presetTargets()) {
				t.Fatalf("无代理时目标数应为 %d, 实际 %d (%+v)", len(presetTargets()), len(targets), targets)
			}
			if hasProxyTarget(targets) {
				t.Fatalf("不应包含代理目标, 实际 %+v", targets)
			}
		})
	}
}

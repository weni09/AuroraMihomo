package service

import (
	"regexp"
	"strings"
	"testing"
)

// 开箱默认 base 必须可被当作完整 mihomo 配置骨架：含 DNS/TUN/规则，
// 且不得夹带个人痕迹（内网 DNS、特定设备 SRC-IP、订阅节点等）。
func TestDefaultBaseYAMLSanitizedAndUseful(t *testing.T) {
	s := &ConfigService{}
	raw := s.defaultBaseYAML()
	if strings.TrimSpace(raw) == "" {
		t.Fatal("default base 不应为空")
	}
	// 个人/内网痕迹
	for _, bad := range []string{
		"192.168.1.251",
		"192.168.1.136",
		"SRC-IP-CIDR",
		"password",
		"uuid:",
		"ss://",
		"vmess://",
	} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(bad)) {
			t.Errorf("默认 base 不应含个人/敏感痕迹 %q", bad)
		}
	}
	// 开箱能力与约定默认值
	for _, need := range []string{
		"mode: rule",
		"mixed-port:",
		"dns:",
		"tun:",
		"IP-CIDR,192.168.0.0/16,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT",
		"IP-CIDR,172.16.0.0/12,DIRECT",
		"IP-CIDR,127.0.0.0/8,DIRECT",
		"MATCH,DIRECT",
		"223.5.5.5",
		"119.29.29.29",
	} {
		if !strings.Contains(raw, need) {
			t.Errorf("默认 base 缺少开箱项 %q", need)
		}
	}
	// 默认 base 不得出现 GeoData 配置、DNS 分流模板与 geosite/geoip 引用：
	// nameserver-policy / fallback-filter 开箱留空；geo 键会让 mihomo 校验
	// 时联网下载规则库。用行首正则排除注释行。
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*"?geosite:`),
		regexp.MustCompile(`(?m)^\s*geoip:\s*true`),
		regexp.MustCompile(`(?m)^\s*geox-url:`),
		regexp.MustCompile(`(?m)^\s*geodata-mode:`),
		regexp.MustCompile(`(?m)^\s*geodata-loader:`),
		regexp.MustCompile(`(?m)^\s*geosite-matcher:`),
		regexp.MustCompile(`(?m)^\s*geo-auto-update:`),
		regexp.MustCompile(`(?m)^\s*geo-update-interval:`),
		regexp.MustCompile(`(?m)^\s*nameserver-policy:`),
		regexp.MustCompile(`(?m)^\s*fallback-filter:`),
		regexp.MustCompile(`(?m)^\s*fallback:`),
		regexp.MustCompile(`(?m)^\s*- IP-CIDR,1\.1\.1\.1/32,DIRECT`),
		regexp.MustCompile(`(?m)^\s*- IP-CIDR,8\.8\.8\.8/32,DIRECT`),
	} {
		if re.MatchString(raw) {
			t.Errorf("默认 base 不应包含 geo/DNS 分流模板或额外公网直连规则: %v", re)
		}
	}
	// 三项「交由用户开启」：通用 ipv6、dns.enable、dns.ipv6
	// 顶层 ipv6 在 dns: 块之前；dns 内 enable/ipv6 在 listen 附近。
	beforeDNS, afterDNS, ok := strings.Cut(raw, "\ndns:")
	if !ok {
		t.Fatal("默认 base 应含 dns: 段")
	}
	if !strings.Contains(beforeDNS, "\nipv6: false\n") && !strings.HasPrefix(strings.TrimSpace(beforeDNS), "ipv6: false") {
		// 允许文件开头注释后出现；用独立行匹配
		if !strings.Contains(beforeDNS, "\nipv6: false") {
			t.Error("顶层 ipv6 默认应为 false")
		}
	}
	if !strings.Contains(afterDNS, "enable: false") {
		t.Error("dns.enable 默认应为 false")
	}
	if !strings.Contains(afterDNS, "ipv6: false") {
		t.Error("dns.ipv6 默认应为 false")
	}
	// 透明代理端口不预写，避免半启用
	if strings.Contains(raw, "tproxy-port:") {
		t.Error("默认 base 不应预写 tproxy-port，应由透明代理开关写入")
	}
}

func TestEnsureDefaultBaseOnlyWhenEmpty(t *testing.T) {
	svc, _, _ := newTestConfigService(t)

	if err := svc.EnsureDefaultBase(); err != nil {
		t.Fatalf("首次 Ensure 失败: %v", err)
	}
	first, err := svc.GetBaseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "MATCH,DIRECT") {
		t.Fatalf("应已写入开箱默认（含 MATCH 兜底），实际:\n%s", first)
	}
	if !strings.Contains(first, "IP-CIDR,192.168.0.0/16,DIRECT") {
		t.Fatalf("应含私网直连规则，实际:\n%s", first)
	}
	if strings.Contains(first, "nameserver-policy:") || strings.Contains(first, "fallback-filter:") {
		t.Fatalf("开箱 base 不应预写 nameserver-policy / fallback-filter，实际:\n%s", first)
	}

	// 用户改过之后再 Ensure 不得覆盖
	custom := "mode: global\nallow-lan: false\n"
	if err := svc.UpdateBaseConfig(custom); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureDefaultBase(); err != nil {
		t.Fatalf("再次 Ensure 失败: %v", err)
	}
	got, err := svc.GetBaseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(custom) {
		t.Errorf("已有 base 时 Ensure 不得覆盖\n期望:\n%s\n实际:\n%s", custom, got)
	}
}

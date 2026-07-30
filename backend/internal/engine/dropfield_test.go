package engine

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

// 需求「支持官方所有参数」要求任何官方字段都不能在
// 「解析 -> 合并 -> 生成」的往返中丢失。
// dns/tun/sniffer 是强类型结构体，只建模了常用子集，
// 因此必须依赖内联的 Extra map 兜住其余官方子字段。
func TestUnmodeledSubFieldsSurviveRoundTrip(t *testing.T) {
	e := NewMergeEngine()
	src := `
dns:
  enable: true
  nameserver-policy:
    "geosite:cn": 223.5.5.5
  proxy-server-nameserver:
    - https://dns.alidns.com/dns-query
  respect-rules: true
tun:
  enable: true
  device: utun0
  mtu: 9000
  strict-route: true
sniffer:
  enable: true
  skip-domain:
    - "+.apple.com"
  override-destination: false
`
	cfg, err := e.LoadAndParse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, key := range []string{
		"nameserver-policy", "proxy-server-nameserver", "respect-rules",
		"device", "mtu", "strict-route",
		"skip-domain", "override-destination",
	} {
		if !strings.Contains(got, key) {
			t.Errorf("官方字段 %q 在往返后丢失，实际输出:\n%s", key, got)
		}
	}
}

// 未设置的枚举型字符串字段不能写出空值：
// mihomo 遇到 `enhanced-mode: ""` / `stack: ""` 会因非法枚举值拒绝加载配置。
func TestEmptyEnumFieldsAreOmitted(t *testing.T) {
	e := NewMergeEngine()
	cfg, err := e.LoadAndParse([]byte("dns:\n  enable: true\ntun:\n  enable: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if strings.Contains(got, `enhanced-mode: ""`) {
		t.Errorf("未设置的 enhanced-mode 不应写出空值:\n%s", got)
	}
	if strings.Contains(got, `stack: ""`) {
		t.Errorf("未设置的 tun.stack 不应写出空值:\n%s", got)
	}
}

// 合并时 Extra 里的字段同样要遵循既有的 Local First 语义：
// 远程声明 dns 段、策略为 local 时，本地的 Extra 字段不能被远程顶掉。
func TestExtraFieldsFollowLocalFirst(t *testing.T) {
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte("dns:\n  enable: true\n  respect-rules: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte("dns:\n  enable: true\n  respect-rules: false\n"))
	if err != nil {
		t.Fatal(err)
	}

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())
	if got := res.Config.DNS.Extra["respect-rules"]; got != true {
		t.Errorf("Local First 下应保留本地 respect-rules=true，实际 %v", got)
	}

	// 远程优先时应改取远程值
	remotePolicy := domain.MergePolicy{DNSPriority: "remote", TUNPriority: "local"}
	res2 := e.MergeDetailedWithPolicy(base, remote, nil, nil, remotePolicy)
	if got := res2.Config.DNS.Extra["respect-rules"]; got != false {
		t.Errorf("远程优先时应取远程 respect-rules=false，实际 %v", got)
	}
}

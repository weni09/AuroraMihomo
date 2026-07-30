package engine

import (
	"testing"

	"auroramihomo/backend/internal/domain"
)

// 设计 §6：HK01 / hk01 / " HK01 " 必须被视为同一节点
func TestNormalizeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  HK01  ", "HK01"},
		{"HK01", "HK01"},
		{"HK  01", "HK 01"},
		{"\tJP01\t", "JP01"},
	}
	for _, c := range cases {
		if got := NormalizeName(c.in); got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeKeyCaseFold(t *testing.T) {
	if normalizeKey(" HK01 ") != normalizeKey("hk01") {
		t.Fatal("大小写/空白差异应折叠为同一 key")
	}
}

func TestNormalizeRule(t *testing.T) {
	got := normalizeRule("DOMAIN-SUFFIX , google.com ,  Proxy")
	want := "DOMAIN-SUFFIX,google.com,Proxy"
	if got != want {
		t.Fatalf("normalizeRule = %q, want %q", got, want)
	}
}

// 关键回归：标准化后不应再因大小写/空白产生「假冲突」和「重复节点」
func TestMergeNoFalseConflictAfterNormalize(t *testing.T) {
	e := NewMergeEngine()

	base, err := e.LoadAndParse([]byte(`
proxies:
  - name: "  HK01  "
    type: ss
    server: a.com
    port: 443
rules:
  - "DOMAIN-SUFFIX , google.com , DIRECT"
`))
	if err != nil {
		t.Fatal(err)
	}

	// 远程用小写 + 不同空白，但内容完全相同
	remote, err := e.LoadAndParse([]byte(`
proxies:
  - name: "hk01"
    type: ss
    server: a.com
    port: 443
rules:
  - "DOMAIN-SUFFIX,google.com,DIRECT"
`))
	if err != nil {
		t.Fatal(err)
	}

	res := e.MergeDetailed(base, remote, nil, nil)

	// 名称标准化后应识别为同一节点，不应重复
	if len(res.Config.Proxies) != 1 {
		t.Fatalf("标准化后应只保留 1 个节点，实际 %d 个: %+v", len(res.Config.Proxies), res.Config.Proxies)
	}
	if res.Config.Proxies[0].Name != "HK01" {
		t.Fatalf("节点名应标准化为 HK01，实际 %q", res.Config.Proxies[0].Name)
	}

	// 规则标准化后内容一致，不应产生冲突
	for _, c := range res.Conflicts {
		if c.Type == "rule" {
			t.Fatalf("规则内容相同不应产生冲突: %+v", c)
		}
	}
	if len(res.Config.Rules) != 1 {
		t.Fatalf("标准化后应只保留 1 条规则，实际 %d 条: %v", len(res.Config.Rules), res.Config.Rules)
	}
}

// 内容确实不同的情况仍必须正常报冲突
func TestMergeStillDetectsRealConflict(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte(`
proxies:
  - name: "HK01"
    type: ss
    server: a.com
    port: 443
`))
	remote, _ := e.LoadAndParse([]byte(`
proxies:
  - name: "hk01"
    type: ss
    server: b.com
    port: 443
`))
	res := e.MergeDetailed(base, remote, nil, nil)

	found := false
	for _, c := range res.Conflicts {
		if c.Type == "proxy" {
			found = true
		}
	}
	if !found {
		t.Fatal("server 不同时应产生 proxy 冲突")
	}
	_ = domain.Config{}
}

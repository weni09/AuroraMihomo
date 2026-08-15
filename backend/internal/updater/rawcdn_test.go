package updater

import "testing"

func TestNormalizeRawCDNListEnsuresGithub(t *testing.T) {
	out := normalizeRawCDNList([]string{"ghproxy.com", "ghproxy.com"})
	if len(out) < 2 {
		t.Fatalf("unexpected: %#v", out)
	}
	hasGithub := false
	for _, v := range out {
		if v == "github" {
			hasGithub = true
		}
	}
	if !hasGithub {
		t.Fatal("github fallback missing")
	}
}

func TestNormalizeRawCDNListEmptyFallsBack(t *testing.T) {
	out := normalizeRawCDNList(nil)
	if len(out) == 0 {
		t.Fatal("empty list should fall back to defaults")
	}
	// 默认列表首元素是官方源（与 DefaultCDNProviders 一致），
	// normalize 语义是「官方源必须出现」，位置由调用方优先级决定。
	if out[0] != "github" {
		t.Fatalf("defaults should start with github official, got %#v", out)
	}
}

func TestNormalizeRawCDNListDedupesCaseInsensitive(t *testing.T) {
	out := normalizeRawCDNList([]string{"GhProxy.COM", "ghproxy.com", "github"})
	if len(out) != 2 {
		t.Fatalf("应大小写不敏感去重, got %#v", out)
	}
	if out[0] != "GhProxy.COM" {
		t.Fatalf("应保留原写法, got %#v", out)
	}
}

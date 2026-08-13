package updater

import "testing"

func TestBuildCDNURLs(t *testing.T) {
	official := "https://github.com/MetaCubeX/mihomo/releases/download/v1.0.0/a.zip"
	urls := buildCDNURLs(official, []string{"ghproxy.com", "github", "gitdl.cn"})
	if len(urls) < 3 {
		t.Fatalf("expected >=3 urls, got %d", len(urls))
	}
	if urls[0] != "https://ghproxy.com/"+official {
		t.Fatalf("unexpected first url: %s", urls[0])
	}
	foundOfficial := false
	for _, u := range urls {
		if u == official {
			foundOfficial = true
		}
	}
	if !foundOfficial {
		t.Fatal("official github url missing")
	}
}

func TestNormalizeCDNListEnsuresGithub(t *testing.T) {
	out := normalizeCDNList([]string{"ghproxy.com", "ghproxy.com"})
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

func TestPrioritizeCDNProvidersMovesLastFirst(t *testing.T) {
	in := []string{"github", "ghproxy.com", "gitdl.cn"}
	got := prioritizeCDNProviders(in, "ghproxy.com")
	want := []string{"ghproxy.com", "github", "gitdl.cn"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %#v want %#v", got, want)
	}
	// 原切片不得被改
	if in[0] != "github" {
		t.Fatal("prioritize 不得改入参")
	}
}

func TestPrioritizeCDNProvidersEmptyOrMissingKeepsOrder(t *testing.T) {
	in := []string{"github", "ghproxy.com"}
	if got := prioritizeCDNProviders(in, ""); got[0] != "github" {
		t.Fatalf("空 last 应保持原序, got %#v", got)
	}
	if got := prioritizeCDNProviders(in, "mirror.ghproxy.com"); got[0] != "github" {
		t.Fatalf("不在列表里的 last 应忽略, got %#v", got)
	}
}

func TestPrioritizeCDNProvidersCaseInsensitive(t *testing.T) {
	in := []string{"github", "ghproxy.com"}
	got := prioritizeCDNProviders(in, "GhProxy.COM")
	if got[0] != "ghproxy.com" {
		t.Fatalf("应命中列表里的原写法, got %#v", got)
	}
}

func TestRememberLastCDNCallsPersisterOnce(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	var n int
	var last string
	m.SetLastCDNPersister(func(p string) error {
		n++
		last = p
		return nil
	})
	m.rememberLastCDN("ghproxy.com")
	m.rememberLastCDN("ghproxy.com")
	if n != 1 || last != "ghproxy.com" {
		t.Fatalf("相同源只应落库一次, n=%d last=%q", n, last)
	}
	m.rememberLastCDN("gitdl.cn")
	if n != 2 || last != "gitdl.cn" {
		t.Fatalf("换源应再落库, n=%d last=%q", n, last)
	}
}

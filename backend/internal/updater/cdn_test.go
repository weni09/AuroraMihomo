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

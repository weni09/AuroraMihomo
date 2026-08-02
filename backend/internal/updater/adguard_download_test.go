package updater

import (
	"runtime"
	"strings"
	"testing"
)

func TestExpandAdGuardURLTemplate(t *testing.T) {
	got := expandAdGuardURLTemplate(
		"https://static.adguard.com/adguardhome/beta/AdGuardHome_${GOOS}_${Arch}.tar.gz",
		"v0.107.78",
	)
	wantSub := "AdGuardHome_" + runtime.GOOS + "_" + adGuardArch() + ".tar.gz"
	if !strings.Contains(got, wantSub) {
		t.Fatalf("got %s want contain %s", got, wantSub)
	}
	got2 := expandAdGuardURLTemplate(
		"https://github.com/AdguardTeam/AdGuardHome/releases/download/${latest_ver}/AdGuardHome_linux_${Arch}.tar.gz",
		"v0.107.78",
	)
	if !strings.Contains(got2, "/download/v0.107.78/") {
		t.Fatalf("latest_ver not expanded: %s", got2)
	}
	if !strings.Contains(got2, "AdGuardHome_linux_"+adGuardArch()) {
		t.Fatalf("Arch not expanded: %s", got2)
	}
}

func TestBuildAdGuardDownloadURLs_Defaults(t *testing.T) {
	urls := buildAdGuardDownloadURLs(nil, "v0.107.78")
	if len(urls) < 3 {
		t.Fatalf("want >=3 default urls, got %d %v", len(urls), urls)
	}
	for _, u := range urls {
		if strings.Contains(u, "${") {
			t.Fatalf("unexpanded var in %s", u)
		}
		if !strings.HasPrefix(u, "https://") {
			t.Fatalf("not https: %s", u)
		}
	}
}

func TestBuildAdGuardDownloadURLs_CustomOrder(t *testing.T) {
	tmpls := []string{
		"https://static.adguard.com/adguardhome/beta/AdGuardHome_linux_${Arch}.tar.gz",
		"https://github.com/AdguardTeam/AdGuardHome/releases/download/${latest_ver}/AdGuardHome_linux_${Arch}.tar.gz",
		"https://static.adguard.com/adguardhome/release/AdGuardHome_linux_${Arch}.tar.gz",
	}
	urls := buildAdGuardDownloadURLs(tmpls, "v1.2.3")
	if len(urls) != 3 {
		t.Fatalf("got %d %v", len(urls), urls)
	}
	if !strings.Contains(urls[0], "/beta/") || !strings.Contains(urls[1], "/download/v1.2.3/") || !strings.Contains(urls[2], "/release/") {
		t.Fatalf("order/content wrong: %v", urls)
	}
}

func TestArchiveNameFromURL(t *testing.T) {
	n := archiveNameFromURL("https://example.com/path/AdGuardHome_linux_amd64.tar.gz?x=1")
	if n != "AdGuardHome_linux_amd64.tar.gz" {
		t.Fatalf("got %s", n)
	}
}

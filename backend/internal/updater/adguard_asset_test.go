package updater

import (
	"runtime"
	"strings"
	"testing"
)

func TestPickAdGuardAsset_CurrentPlatform(t *testing.T) {
	rel := &githubRelease{
		TagName: "v0.107.61",
		Assets: []githubAsset{
			{Name: "AdGuardHome_windows_amd64.zip", BrowserDownloadURL: "https://example/win-amd64.zip", Size: 11},
			{Name: "AdGuardHome_windows_arm64.zip", BrowserDownloadURL: "https://example/win-arm64.zip", Size: 12},
			{Name: "AdGuardHome_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/linux-amd64.tgz", Size: 10},
			{Name: "AdGuardHome_linux_arm64.tar.gz", BrowserDownloadURL: "https://example/linux-arm64.tgz", Size: 13},
			{Name: "AdGuardHome_linux_armv7.tar.gz", BrowserDownloadURL: "https://example/linux-armv7.tgz", Size: 14},
			{Name: "AdGuardHome_darwin_amd64.tar.gz", BrowserDownloadURL: "https://example/mac-amd64.tgz", Size: 15},
			{Name: "AdGuardHome_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example/mac-arm64.tgz", Size: 16},
			{Name: "AdGuardHome_linux_amd64.deb", BrowserDownloadURL: "https://example/deb", Size: 9},
		},
	}
	url, name, _, err := pickAdGuardAsset(rel)
	if err != nil {
		t.Fatalf("当前平台应匹配到资产: %v", err)
	}
	if strings.HasSuffix(strings.ToLower(name), ".deb") {
		t.Fatalf("不应选中 deb: %s", name)
	}
	_ = url
	// 文件名应含 adguardhome 与当前 goos 片段
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "adguardhome") {
		t.Fatalf("name=%s", name)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(lower, ".zip") {
			t.Fatalf("windows 期望 zip, got %s", name)
		}
	} else {
		if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
			t.Fatalf("非 windows 期望 .tar.gz/.tgz, got %s", name)
		}
	}
}

func TestPickAdGuardAsset_NoMatch(t *testing.T) {
	rel := &githubRelease{TagName: "v1", Assets: nil}
	// 无资产时 pick 应返回 error
	_, _, _, err := pickAdGuardAsset(rel)
	if err == nil {
		t.Fatal("无资产应 error")
	}
}

func TestAdGuardFileName(t *testing.T) {
	n := adguardFileName()
	if runtime.GOOS == "windows" {
		if n != "AdGuardHome.exe" {
			t.Fatalf("got %s", n)
		}
	} else if n != "AdGuardHome" {
		t.Fatalf("got %s", n)
	}
}

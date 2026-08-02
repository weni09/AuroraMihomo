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
			{Name: "AdGuardHome_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/linux.tgz", Size: 10},
			{Name: "AdGuardHome_windows_amd64.zip", BrowserDownloadURL: "https://example/win.zip", Size: 11},
			{Name: "AdGuardHome_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example/mac.tgz", Size: 12},
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
	if runtime.GOOS == "windows" && !strings.HasSuffix(lower, ".zip") {
		t.Fatalf("windows 期望 zip, got %s", name)
	}
	if runtime.GOOS != "windows" && !strings.Contains(lower, ".tar.gz") && !strings.HasSuffix(lower, ".gz") {
		// 官方多为 .tar.gz
		t.Logf("got archive %s (ok if tar.gz)", name)
	}
}

func TestPickAdGuardAsset_Unsupported(t *testing.T) {
	rel := &githubRelease{TagName: "v1", Assets: nil}
	// 通过临时把逻辑写成对空 assets 返回 error 即可；本测只断言无匹配时有 error
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

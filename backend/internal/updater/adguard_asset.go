package updater

import (
	"fmt"
	"runtime"
	"strings"
)

func adguardFileName() string {
	if runtime.GOOS == "windows" {
		return "AdGuardHome.exe"
	}
	return "AdGuardHome"
}

// pickAdGuardAsset 从 AdGuardHome release 中选当前平台压缩包。
// 官方命名：AdGuardHome_<os>_<arch>.tar.gz | .zip；排除 .deb/.rpm 等。
func pickAdGuardAsset(rel *githubRelease) (url, name string, size int64, err error) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	// 官方 arch 用 amd64/arm64/386 等
	var needle string
	switch {
	case goos == "windows" && goarch == "amd64":
		needle = "windows_amd64"
	case goos == "windows" && goarch == "arm64":
		needle = "windows_arm64"
	case goos == "linux" && goarch == "amd64":
		needle = "linux_amd64"
	case goos == "linux" && goarch == "arm64":
		needle = "linux_arm64"
	case goos == "linux" && (goarch == "arm" || goarch == "armv7"):
		needle = "linux_armv7"
	case goos == "darwin" && goarch == "amd64":
		needle = "darwin_amd64"
	case goos == "darwin" && goarch == "arm64":
		needle = "darwin_arm64"
	default:
		return "", "", 0, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
	wantZip := goos == "windows"
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if isDistroPackage(lower) {
			continue
		}
		if !strings.Contains(lower, "adguardhome") {
			continue
		}
		if !strings.Contains(lower, needle) {
			continue
		}
		if wantZip {
			if !strings.HasSuffix(lower, ".zip") {
				continue
			}
		} else {
			if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
				continue
			}
		}
		return a.BrowserDownloadURL, a.Name, a.Size, nil
	}
	return "", "", 0, fmt.Errorf("no AdGuardHome asset matched for %s/%s in %s", goos, goarch, rel.TagName)
}

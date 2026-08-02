package updater

import (
	"fmt"
	"runtime"
	"strings"
)

// DefaultAdGuardDownloadTemplates 是 AdGuard Home 二进制的默认下载地址模板（按序回落）。
// 支持变量：
//
//	${Arch} / ${arch}       — 架构：amd64、arm64、armv7、386…
//	${latest_ver} / ${tag}  — GitHub 最新 release tag（如 v0.107.78）
//	${GOOS} / ${os}         — 运行时 GOOS（linux/windows/darwin）
//	${GOARCH}               — 运行时 GOARCH 原样
//
// 用户在设置里填写的「升级链接」同样走此展开逻辑。
var DefaultAdGuardDownloadTemplates = []string{
	"https://static.adguard.com/adguardhome/beta/AdGuardHome_${GOOS}_${Arch}.tar.gz",
	"https://github.com/AdguardTeam/AdGuardHome/releases/download/${latest_ver}/AdGuardHome_${GOOS}_${Arch}.tar.gz",
	"https://static.adguard.com/adguardhome/release/AdGuardHome_${GOOS}_${Arch}.tar.gz",
}

// 兼容用户只写 linux 的模板（文档示例）；展开时 GOOS 仍可替换。
// 若模板已写死 linux 而当前是 windows，该源会失败并回落到下一条。

// adGuardArch 返回 AdGuard 官方资产名中的 arch 段。
func adGuardArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm", "armv7":
		return "armv7"
	case "386":
		return "386"
	default:
		return runtime.GOARCH
	}
}

// expandAdGuardURLTemplate 替换模板中的变量；未知 ${...} 原样保留。
func expandAdGuardURLTemplate(tmpl, latestVer string) string {
	arch := adGuardArch()
	goos := runtime.GOOS
	// Windows 官方资产多为 .zip；若模板写死 .tar.gz 会在该源失败后换源
	replacer := strings.NewReplacer(
		"${Arch}", arch,
		"${arch}", arch,
		"${ARCH}", arch,
		"${latest_ver}", latestVer,
		"${Latest_ver}", latestVer,
		"${LATEST_VER}", latestVer,
		"${tag}", latestVer,
		"${Tag}", latestVer,
		"${version}", latestVer,
		"${Version}", latestVer,
		"${GOOS}", goos,
		"${goos}", goos,
		"${os}", goos,
		"${OS}", goos,
		"${GOARCH}", runtime.GOARCH,
		"${goarch}", runtime.GOARCH,
	)
	return replacer.Replace(tmpl)
}

// normalizeAdGuardURLTemplates 清洗用户填写的升级链接列表（完整 URL 模板）。
// 与 GitHub CDN token 列表不同：不会强行追加 "github"，空则回落默认模板。
func normalizeAdGuardURLTemplates(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		// 跳过纯注释行
		if strings.HasPrefix(v, "#") {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return append([]string{}, DefaultAdGuardDownloadTemplates...)
	}
	return out
}

// buildAdGuardDownloadURLs 将模板列表展开为可请求的 URL（去重保序）。
func buildAdGuardDownloadURLs(templates []string, latestVer string) []string {
	templates = normalizeAdGuardURLTemplates(templates)
	out := make([]string, 0, len(templates))
	seen := map[string]bool{}
	for _, tmpl := range templates {
		u := strings.TrimSpace(expandAdGuardURLTemplate(tmpl, latestVer))
		if u == "" || seen[u] {
			continue
		}
		// 必须是 http(s) 才当下载地址；否则忽略（避免把 ghproxy 这类 token 当 URL）
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// archiveNameFromURL 从 URL 路径取文件名，供临时落盘。
func archiveNameFromURL(rawURL string) string {
	u := rawURL
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	if u == "" {
		return fmt.Sprintf("AdGuardHome_%s_%s.bin", runtime.GOOS, adGuardArch())
	}
	return u
}

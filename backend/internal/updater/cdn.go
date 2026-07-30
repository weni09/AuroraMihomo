package updater

import (
	"fmt"
	"strings"
)

// DefaultCDNProviders 是 GitHub Release 资产的下载源，按优先级排列。
//
// 只服务于 Release 资产（github.com/owner/repo/releases/download/...），
// 这是本项目唯一需要加速的下载类型：内核与面板都以 Release 形式分发。
//
// GitHub API（查询最新版本）不走这里：ghproxy 系镜像只代理 github.com 的
// 下载路径，套在 api.github.com 上只会得到 404。API 一律直连官方，
// 网络不通时由 mihomo 代理兜底。
var DefaultCDNProviders = []string{
	"github",
	"ghproxy.com",
	"mirror.ghproxy.com",
	"gh.ddlc.top",
	"ghproxy.net",
	"gitdl.cn",
	"gh.llkk.cc",
	"ghp.ci",
}

// jsdelivrHosts 列出已知的 jsdelivr 镜像域名。
// jsdelivr 只镜像仓库内文件，代理不了 Release 资产，因此填进来会被跳过——
// 拼出的地址必然 404，与其产生无效请求不如直接忽略。
var jsdelivrHosts = []string{
	"jsdelivr.net",
	"jsdelivr.com",
}

// normalizeCDNList 去空白、按大小写不敏感去重，并保证官方源始终兜底。
// 列表为空时回落到默认值。
func normalizeCDNList(list []string) []string {
	if len(list) == 0 {
		return append([]string{}, DefaultCDNProviders...)
	}
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		v := strings.TrimSpace(item)
		if v == "" {
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
		return append([]string{}, DefaultCDNProviders...)
	}
	// 镜像全挂时仍要能直连，官方源必须留作最后兜底
	if !seen["github"] {
		out = append(out, "github")
	}
	return out
}

// buildCDNURLs converts an official GitHub download URL into provider-specific URLs.
// official example:
//
//	https://github.com/MetaCubeX/mihomo/releases/download/v1.19.0/xxx.zip
func buildCDNURLs(official string, providers []string) []string {
	providers = normalizeCDNList(providers)
	out := make([]string, 0, len(providers))
	seen := map[string]bool{}

	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}

	for _, p := range providers {
		switch strings.ToLower(p) {
		case "github", "official":
			add(official)
		case "ghproxy.com":
			add("https://ghproxy.com/" + official)
		case "mirror.ghproxy.com":
			add("https://mirror.ghproxy.com/" + official)
		case "gh.ddlc.top":
			add("https://gh.ddlc.top/" + official)
		case "ghproxy.net":
			add("https://ghproxy.net/" + official)
		case "gitdl.cn":
			add("https://gitdl.cn/" + official)
		case "gh.llkk.cc":
			add("https://gh.llkk.cc/" + official)
		case "ghp.ci":
			add("https://ghp.ci/" + official)
		default:
			// custom prefix or full template
			if strings.Contains(p, "%s") {
				add(fmt.Sprintf(p, official))
			} else if isJsdelivr(p) {
				// jsdelivr 代理不了 Release 资产，拼出来必然 404，直接跳过
				continue
			} else if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				// treat as prefix
				if strings.HasSuffix(p, "/") {
					add(p + official)
				} else {
					add(p + "/" + official)
				}
			}
			// 其余无法识别的 token（如裸域名）直接忽略
		}
	}

	// 这里不再追加 URL 编码形式：候选列表只由 providers 决定，保持可预测。
	return out
}

// isJsdelivr 判断一个源是否为 jsdelivr 镜像。
// 只看域名部分，避免把路径里恰好含该字样的自定义源误判。
func isJsdelivr(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	p = strings.TrimPrefix(strings.TrimPrefix(p, "https://"), "http://")
	if i := strings.IndexAny(p, "/?#"); i >= 0 {
		p = p[:i]
	}
	for _, h := range jsdelivrHosts {
		if p == h || strings.HasSuffix(p, "."+h) {
			return true
		}
	}
	return false
}

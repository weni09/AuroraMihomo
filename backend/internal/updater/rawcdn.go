package updater

import (
	"strings"
)

// DefaultRawCDNProviders 是 raw.githubusercontent.com 类内容（模板转换远程地址、
// 订阅远程源、外部配置来源等）的加速下载源，按优先级排列。
//
// 与 DefaultCDNProviders 同族：ghproxy 系镜像同时代理 github.com 的
// Release 下载路径与 raw.githubusercontent.com 的文件路径。
// 官方源恒为最后兜底——镜像全挂时仍要能直连。
var DefaultRawCDNProviders = []string{
	"github",
	"ghproxy.com",
	"mirror.ghproxy.com",
	"gh.llkk.cc",
	"ghproxy.net",
	"gh.ddlc.top",
	"gitdl.cn",
	"ghp.ci",
}

// normalizeRawCDNList 去空白、按大小写不敏感去重，并保证官方源始终兜底。
// 列表为空时回落到默认值。与 normalizeCDNList 同构。
func normalizeRawCDNList(list []string) []string {
	if len(list) == 0 {
		return append([]string{}, DefaultRawCDNProviders...)
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
		return append([]string{}, DefaultRawCDNProviders...)
	}
	// 镜像全挂时仍要能直连，官方源必须留作最后兜底
	if !seen["github"] {
		out = append(out, "github")
	}
	return out
}

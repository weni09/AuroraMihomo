//go:build !linux && !darwin

package netcheck

import "runtime"

// 其余平台（主要是 Windows，开发环境常用）不提供透明代理。
//
// Windows 上 mihomo 确实支持 TUN，但接管方式、权限模型（需管理员而非
// capability）、以及 WinTun 驱动依赖都与 Unix 差别很大；本功能的目标环境是
// Alpine / Debian / Ubuntu 服务器，Windows 只需明确报不可用，避免前端把开关
// 显示成可操作。
func detect() *Report {
	r := &Report{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	reason := "透明代理目前仅支持 Linux（TUN / TProxy）与 macOS（TUN）"
	r.Modes = []ModeStatus{
		{Mode: ModeTUN, Reason: reason},
		{Mode: ModeTProxy, Reason: reason},
	}
	return r
}

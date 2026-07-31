package netcheck

import (
	"context"
	"net"
	"sync"
	"syscall"
	"time"
)

// markWarnOnce 保证"打标失败"只提醒一次。
//
// 失败原因通常是缺 CAP_NET_ADMIN，属于持续状态而非偶发——每次拨号都记一条
// 日志会在拉订阅、查更新的高频路径上把日志刷满，反而埋掉真正的问题。
var markWarnOnce sync.Once

// MarkedDialer 返回一个会给连接打上 PanelMark 的拨号器。
//
// 为什么面板需要这个：见 firewall.go 里 PanelMark 的注释。简言之，
// 本机流量被 TProxy 接管后面板自身的出站也会被捕获，既绕过了用户对出网
// 方式的显式选择，也让 mihomo 故障时失去恢复手段。
//
// warn 在打标失败时被调用一次，可为 nil。刻意不把失败当错误往上抛：
// 打标只是"尽量别被自己的规则抓走"，拿不到权限时仍然要能拨号出去——
// 让缺一个 capability 演变成"面板完全无法联网"是不可接受的取舍。
func MarkedDialer(timeout time.Duration, warn func(format string, args ...interface{})) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, c syscall.RawConn) error {
			if err := markSocket(network, address, c); err != nil && warn != nil {
				markWarnOnce.Do(func() {
					warn("面板出站流量打标失败（%v）。透明代理 TProxy 模式下，"+
						"面板自身的订阅拉取与下载可能被 mihomo 接管；"+
						"如需避免请确保进程持有 CAP_NET_ADMIN", err)
				})
			}
			// 一律返回 nil：打标失败不该阻断拨号
			return nil
		},
	}
}

// MarkedDialContext 返回可直接塞进 http.Transport.DialContext 的函数。
//
// 单独提供是因为两个调用方形态不同：fetcher 用默认 Transport（需要整个
// Dialer），updater 已经在构造自定义 Transport（只需要 DialContext）。
func MarkedDialContext(timeout time.Duration,
	warn func(format string, args ...interface{})) func(context.Context, string, string) (net.Conn, error) {
	return MarkedDialer(timeout, warn).DialContext
}

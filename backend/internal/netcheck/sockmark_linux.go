//go:build linux

package netcheck

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// markSocket 给即将建立的连接打上 PanelMark。
//
// 打标的意义见 firewall.go 里 PanelMark 的注释：本机流量被 TProxy 接管后，
// 面板自己拉订阅、查版本、下载内核的请求也会被捕获，从而绕过用户对
// 「优先经由本地 Mihomo 代理出网」的显式选择，并且在 mihomo 挂掉时连带
// 让面板失去出网能力（连重新下载内核都做不到）。
//
// 用 Dialer.Control 而不是自定义 Dial：Control 在 socket 已创建、connect
// 之前被调用，正是设置 SO_MARK 的唯一时机，且不必自己重写拨号逻辑
// （DNS 解析、happy eyeballs、超时都还由标准库处理）。
func markSocket(_, _ string, c syscall.RawConn) error {
	var setErr error
	// Control 的回调里拿到的是原始 fd。err 是 Control 自身的错误
	// （fd 不可用），setErr 才是 setsockopt 的结果，两者要分开看。
	if err := c.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, PanelMark)
	}); err != nil {
		return err
	}
	return setErr
}

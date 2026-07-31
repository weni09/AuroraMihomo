//go:build !linux

package netcheck

import "syscall"

// markSocket 在非 Linux 平台是空操作。
//
// SO_MARK 是 Linux 专有的 socket 选项，而 TProxy 本身也只在 Linux 可用
// （macOS 只有 TUN，Windows 完全不支持），所以其它平台上没有需要放行的
// 东西。返回 nil 而不是报错：调用方是通用的拨号路径，在 Windows 上开发
// 时不该因为"打不了标"而拿不到 HTTP 客户端。
func markSocket(_, _ string, _ syscall.RawConn) error { return nil }

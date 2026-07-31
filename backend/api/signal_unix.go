//go:build !windows

package main

import (
	"os"
	"syscall"
)

// reloadSignal 返回触发热重载的信号。
// SIGHUP 是 Unix 下重载配置的惯例（nginx / sshd 等都用它）。
func reloadSignal() os.Signal { return syscall.SIGHUP }

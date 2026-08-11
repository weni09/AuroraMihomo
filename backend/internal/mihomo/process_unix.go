//go:build !windows

package mihomo

import (
	"os"
	"syscall"
)

// processExists 判断给定 PID 是否仍在运行。
// Unix 用 Signal(0) 探测：进程存在返回 nil，不存在返回 ESRCH。
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

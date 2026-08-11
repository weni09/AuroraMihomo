//go:build windows

package mihomo

import (
	"golang.org/x/sys/windows"
)

// processExists 判断给定 PID 是否仍在运行。
// Windows 上 os.FindProcess 对任意 PID 都成功，必须用 OpenProcess 真探。
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

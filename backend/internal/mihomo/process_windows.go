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

// findRunningMihomoPid 见 process_unix.go 说明。Windows 没有 /proc 且
// 枚举进程表（tasklist/psapi）过重，直接返回不存在，启动路径退回 Start——
// 自升级主路径本就有库 PID 记录走 AttachExternal，这里只是兜底边缘场景。
func findRunningMihomoPid(binaryName string) (int, error) {
	return 0, nil
}

// PIDMatchesBinary 见 process_unix.go 说明。Windows 不做 cmdline 校验
// （OpenProcess 拿命令行需要额外权限与开销），一律视为通过，由
// AttachExternal 的进程存在性检查兜底。
func PIDMatchesBinary(pid int, binaryName string) bool {
	return true
}

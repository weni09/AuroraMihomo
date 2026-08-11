//go:build !windows

package mihomo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// findRunningMihomoPid 在进程表里查找正在运行、cmdline 首参为 binaryName
// 的进程 PID，找不到返回 0。
//
// Linux 直接扫 /proc/<pid>/cmdline：无外部命令依赖（busybox 无 pgrep 也
// 可靠），且能精确匹配可执行名避免误抓面板自身。其它 Unix（如 darwin）
// 没有 /proc，一律返回 0，调用方退回原路径。
func findRunningMihomoPid(binaryName string) (int, error) {
	if binaryName == "" {
		return 0, nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, nil // 无 /proc 视为不存在
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue // 进程已退出或权限不足，跳过
		}
		// cmdline 以 NUL 分隔参数，首个是启动时的二进制路径
		parts := strings.Split(string(cmdline), "\x00")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		if filepath.Base(strings.TrimSpace(parts[0])) == binaryName {
			return pid, nil
		}
	}
	return 0, nil
}

// PIDMatchesBinary 校验给定 PID 的进程 cmdline 首参是否为 binaryName。
//
// 用途：自升级接管前排除 PID 复用误伤。数据库记录的旧内核 PID 若在
// 「关停保存 → 新进程接管」的窗口内被系统回收并复用给无关进程，按名比对
// 会失败，从而跳过 Attach，避免后续 Stop 对无辜进程发信号。
//
// 无法确认时（非 Linux 无 /proc、进程刚退出/权限不足读不到）返回 true，
// 把决定权交回 AttachExternal 自身的进程存在性检查与调用方的 Start 兜底。
func PIDMatchesBinary(pid int, binaryName string) bool {
	if pid <= 0 || binaryName == "" {
		return true
	}
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return true
	}
	parts := strings.Split(string(cmdline), "\x00")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return true
	}
	return filepath.Base(strings.TrimSpace(parts[0])) == binaryName
}

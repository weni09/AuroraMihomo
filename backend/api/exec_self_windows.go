//go:build windows

package main

import "fmt"

// execSelfIfPossible Windows 无可靠的同 PID 热替换（CreateProcess 会起新 PID，
// 且运行中的 exe 已在 SwapSelfBinary 里用 rename 技巧换好）。
// 这里直接返回错误，让调用方走「进程退出 + NSSM/服务管理器拉起」路径。
func execSelfIfPossible(bin string) error {
	return fmt.Errorf("windows: exec self not supported, rely on process manager restart")
}

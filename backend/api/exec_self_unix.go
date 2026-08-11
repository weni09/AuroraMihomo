//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// execSelfIfPossible 用新二进制在同 PID 上热替换当前进程。
//
// 自升级场景：mihomo 是本进程的子进程。若走「退出 + supervisor 再起」，
// systemd 默认 KillMode=control-group 会在主进程退出时把 cgroup 里的
// mihomo 一并杀掉——这正是「面板起来了、内核没了、TProxy 全面断网」的根因。
// Exec 成功后 PID 不变、子进程关系不变，内核继续跑。
//
// 失败时返回错误，由调用方退回普通退出路径。
func execSelfIfPossible(bin string) error {
	if bin == "" {
		return fmt.Errorf("empty binary path")
	}
	if _, err := os.Stat(bin); err != nil {
		return err
	}
	argv := append([]string{bin}, os.Args[1:]...)
	return syscall.Exec(bin, argv, os.Environ())
}

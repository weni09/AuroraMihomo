//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// execSelfIfPossible 用新二进制在同 PID 上热替换当前进程。
//
// 自升级场景：mihomo 是本进程的子进程。Exec 成功后 PID 与父子关系不变，
// 内核继续跑，旁路由/TProxy 不会出现「规则在、内核死」的断网窗口。
//
// 若走「退出 + supervisor 再起」：
//   - Alpine/OpenRC 的 supervise-daemon 只监管主进程，子进程可能变成
//     孤儿继续跑，新主进程需 AttachExternal 接管；
//   - systemd 默认 KillMode=control-group 会在主进程退出时把 cgroup 里的
//     mihomo 一并杀掉（Debian/Ubuntu 单元须 KillMode=process）。
//
// 两种路径都不如同 PID Exec 干净，故优先 Exec，失败再退回退出+拉起。
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

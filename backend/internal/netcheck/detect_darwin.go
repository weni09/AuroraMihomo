//go:build darwin

package netcheck

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// macOS 上只有 TUN 一条可行路径：
//
//   - TProxy 是 Linux 专有（依赖 TPROXY netfilter target），macOS 没有等价物。
//   - redir-port 在 macOS 理论可用，但需要自己写 pfctl 的 rdr 规则，而 pf
//     既没有 UDP 重定向也没有 TPROXY 等价能力，只能代理 TCP，覆盖面太窄，
//     不值得为它引入一套 pfctl 规则管理与回滚。
//
// macOS 没有 Linux 的 capability 机制，一切都要 root。
// TUN 设备走系统内建 utun（AF_SYSTEM + com.apple.net.utun_control），
// 无需额外驱动，但设备名必须是 utun<N>，且路由通过 PF_ROUTE 添加。
func detect() *Report {
	r := &Report{
		OS:     "darwin",
		Arch:   runtime.GOARCH,
		Kernel: darwinKernel(),
		Root:   os.Geteuid() == 0,
	}
	// macOS 无 capability，用 root 与否直接映射
	r.CapNetAdmin = r.Root
	r.CapNetRaw = r.Root

	tun := ModeStatus{Mode: ModeTUN}
	if !r.Root {
		tun.Missing = append(tun.Missing, "root 权限")
		tun.Reason = "macOS 没有 capability 机制，创建 utun 设备与修改路由都必须以 root 运行"
	} else {
		tun.Available = true
		tun.Reason = "可用。设备名会自动取 utun<N>；" +
			"macOS 上 auto-redirect 不生效，也无法自动劫持局域网 DNS，" +
			"局域网设备需手动指向本机 DNS"
	}

	tproxy := ModeStatus{
		Mode:   ModeTProxy,
		Reason: "TProxy 依赖 Linux 的 TPROXY netfilter 目标，macOS 不支持。请使用 TUN 模式",
	}

	r.Modes = []ModeStatus{tun, tproxy}
	if !r.Root {
		r.Warnings = append(r.Warnings, "当前非 root 运行，透明代理不可用")
	}
	return r
}

func darwinKernel() string {
	// 带超时的理由同 detect_linux.go 的 defaultCommandProbe：
	// Detect() 在每次读取透明代理状态时被调用，探测不该有无限等待的可能。
	// CI 只跑 Linux，所以 noctx 不会在这里报错，但问题是同一个。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

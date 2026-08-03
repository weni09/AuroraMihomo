package adguard

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// PortAvailability 描述某端口对 AdGuard 是否可用。
type PortAvailability int

const (
	// PortFree 端口空闲，可以使用。
	PortFree PortAvailability = iota
	// PortOwnedByAdGuard 已被本机 AdGuardHome 进程占用（可视为成功）。
	PortOwnedByAdGuard
	// PortOwnedByOther 被其它进程占用（失败）。
	PortOwnedByOther
)

// CheckDNSPortAvailability 判断 port 是否可供 AdGuard 绑定。
//
// 规则：
//   - 无占用 → PortFree（成功）
//   - AdGuard 自身占用 → PortOwnedByAdGuard（成功）
//   - 其它进程占用 → PortOwnedByOther（失败，error 非 nil）
func CheckDNSPortAvailability(port int, aghRunning bool, currentAGHPort int) (PortAvailability, string, error) {
	if port < 1 || port > 65535 {
		return PortOwnedByOther, "", fmt.Errorf("DNS 端口无效: %d（须为 1–65535）", port)
	}

	udpBusy := !canBindUDP(port)
	tcpBusy := !canBindTCP(port)
	if !udpBusy && !tcpBusy {
		return PortFree, "", nil
	}

	if aghRunning && currentAGHPort == port && currentAGHPort > 0 {
		return PortOwnedByAdGuard, "AdGuardHome", nil
	}

	owner := lookupListenerProcess(port)
	if owner != "" {
		if isAdGuardProcessName(owner) {
			return PortOwnedByAdGuard, owner, nil
		}
		return PortOwnedByOther, owner, fmt.Errorf("端口 %d 已被其它进程占用（%s）", port, owner)
	}

	return PortOwnedByOther, "", fmt.Errorf("端口 %d 已被占用（无法确认是否为 AdGuard，请更换端口或先停止占用进程）", port)
}

func isAdGuardProcessName(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "adguardhome") ||
		(strings.Contains(n, "adguard") && !strings.Contains(n, "aurora"))
}

func canBindUDP(port int) bool {
	lc := net.ListenConfig{}
	for _, host := range []string{"0.0.0.0", "127.0.0.1"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pc, err := lc.ListenPacket(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(port)))
		cancel()
		if err != nil {
			return false
		}
		_ = pc.Close()
	}
	return true
}

func canBindTCP(port int) bool {
	lc := net.ListenConfig{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, host := range []string{"0.0.0.0", "127.0.0.1"} {
		ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = ln.Close()
	}
	return true
}

// lookupListenerProcess 尽力解析占用 port 的进程名（Linux ss；其它平台可能返回空）。
func lookupListenerProcess(port int) string {
	if runtime.GOOS != "linux" {
		return lookupListenerProcessGeneric(port)
	}
	// ss -lntp / -lnup：users:(("name",pid=...,fd=...))
	for _, args := range [][]string{
		{"ss", "-lntp"},
		{"ss", "-lnup"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		if name := parseSSUsersForPort(string(out), port); name != "" {
			return name
		}
	}
	return ""
}

func lookupListenerProcessGeneric(port int) string {
	// Windows：尝试 netstat -ano + tasklist 过于沉重；交由 bind + currentAGHPort 回退。
	_ = port
	return ""
}

func parseSSUsersForPort(ssOut string, port int) string {
	portToken := ":" + strconv.Itoa(port)
	sc := bufio.NewScanner(strings.NewReader(ssOut))
	for sc.Scan() {
		line := sc.Text()
		// 匹配本地端口：*:53、0.0.0.0:53、127.0.0.1:53、[::]:53
		if !ssLineHasLocalPort(line, portToken) {
			continue
		}
		// users:(("AdGuardHome",pid=123,fd=6))
		if i := strings.Index(line, `users:(("`); i >= 0 {
			rest := line[i+len(`users:(("`):]
			if j := strings.Index(rest, `"`); j > 0 {
				return rest[:j]
			}
		}
	}
	return ""
}

func ssLineHasLocalPort(line, portToken string) bool {
	// ss 列：Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return strings.Contains(line, portToken)
	}
	local := fields[4]
	if strings.HasSuffix(local, portToken) {
		return true
	}
	// [::]:5353
	if strings.Contains(local, "]"+portToken) {
		return true
	}
	return false
}

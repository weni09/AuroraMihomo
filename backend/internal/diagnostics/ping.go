package diagnostics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"
)

// errNoICMP 表示当前环境无 ICMP 权限（ping 命令不可用），
// 探测据此降级为 TCP 建连探测。
var errNoICMP = errors.New("无 ICMP 权限")

// PingProbe ICMP ping 探测；无 ICMP 权限时降级为 TCP 建连。
//
// 无全局可变状态：Timeout 在 Run 内按值读取并解析默认值，不写回结构体字段，
// 同一实例可安全并发执行。
type PingProbe struct {
	Timeout time.Duration
	// PingCmd 是注入的 ping 命令执行函数（输出已丢弃，仅取成败）；
	// nil 表示不具备 ICMP 能力，直接走 TCP 降级路径。
	// 返回 errNoICMP 时同样降级为 TCP。
	PingCmd func(ctx context.Context, host string) ([]byte, error)
}

func (p *PingProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.PingCmd != nil {
		start := time.Now()
		_, err := p.PingCmd(ctx, target.Target)
		latency := time.Since(start)
		if err == nil {
			return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds()}
		}
		if !errors.Is(err, errNoICMP) {
			status := StatusFail
			if ctx.Err() != nil {
				status = StatusTimeout
			}
			return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
		}
		// errNoICMP：降级到 TCP 建连探测。
	}
	return p.tcpFallback(ctx, target, path)
}

// tcpFallback 以 TCP 建连代替 ICMP：默认连 443，target.Port 指定时用之。
func (p *PingProbe) tcpFallback(ctx context.Context, target DiagnosticTarget, path string) ProbeResult {
	port := target.Port
	if port <= 0 {
		port = 443
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	addr := net.JoinHostPort(target.Target, strconv.Itoa(port))
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	latency := time.Since(start)
	detail := map[string]interface{}{
		"degraded": true,
		"reason":   "无 ICMP 权限已用 TCP ping",
	}
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Detail: detail, Error: err.Error()}
	}
	conn.Close()
	return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds(), Detail: detail}
}

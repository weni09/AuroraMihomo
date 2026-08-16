package diagnostics

import (
	"context"
	"net"
	"strconv"
	"time"
)

// TCPProbe 对 host:port 建连测延迟。
//
// 无全局可变状态：DialTimeout 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type TCPProbe struct {
	DialTimeout time.Duration
}

func (p *TCPProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if target.Port <= 0 {
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusError, Error: "端口必填: tcp 探测需要指定有效的 target.port"}
	}
	addr := net.JoinHostPort(target.Target, strconv.Itoa(target.Port))
	timeout := p.DialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	conn.Close()
	return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds()}
}

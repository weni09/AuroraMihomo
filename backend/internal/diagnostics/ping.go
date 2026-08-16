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
// direct 路径优先 ICMP（不可用时降级 TCP 建连）；proxy 路径 ICMP 无法经
// HTTP 代理（层三协议不经代理），改为经出网选择器指定的代理 CONNECT target
// 测隧道建立延迟（代理不可用时回落直连并标注）。
// 无全局可变状态：Timeout/PingCmd/Selector 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type PingProbe struct {
	Timeout time.Duration
	// PingCmd 是注入的 ping 命令执行函数（输出已丢弃，仅取成败）；
	// nil 表示不具备 ICMP 能力，直接走 TCP 降级路径。
	// 返回 errNoICMP 时同样降级为 TCP。
	PingCmd func(ctx context.Context, host string) ([]byte, error)
	// Selector 按出网路径返回 http.Client：proxy 路径从这里取代理地址发
	// CONNECT；nil 时 proxy 路径回落直连。
	Selector ClientSelector
}

func (p *PingProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	// proxy 路径：ICMP 无法经 HTTP 代理（层三协议不经过代理），改为经代理
	// CONNECT target 测隧道建立延迟，与 TCPProbe 的 proxy 路径同语义。
	if path == PathProxy {
		return p.proxyConnect(ctx, target, path)
	}
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

// proxyConnect 经代理 CONNECT target 测延迟；未注入 selector 或代理不可用时
// 回落直连 TCP 建连并标注。
func (p *PingProbe) proxyConnect(ctx context.Context, target DiagnosticTarget, path string) ProbeResult {
	port := target.Port
	if port <= 0 {
		port = 443
	}
	if p.Selector == nil {
		return p.tcpDial(ctx, target, path, fallbackDetail("未配置出网选择器，proxy 路径回落直连"))
	}
	client := p.Selector(path)
	if client == nil || proxyAddrOf(client) == "" {
		return p.tcpDial(ctx, target, path, fallbackDetail("代理不可用，proxy 路径回落直连"))
	}
	latency, err := proxyConnect(ctx, client, target.Target, port)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	detail := map[string]interface{}{
		"note": "ICMP 无法经 HTTP 代理，proxy 路径经代理 CONNECT 测延迟",
	}
	return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds(), Detail: detail}
}

// fallbackDetail 构造 proxy 路径回落直连的结果 Detail：如实标注并非代理路径。
func fallbackDetail(note string) map[string]interface{} {
	return map[string]interface{}{
		"degraded":      true,
		"reason":        "无 ICMP 权限已用 TCP ping",
		"proxyFallback": true,
		"note":          note,
	}
}

// tcpFallback 以 TCP 建连代替 ICMP：默认连 443，target.Port 指定时用之。
func (p *PingProbe) tcpFallback(ctx context.Context, target DiagnosticTarget, path string) ProbeResult {
	return p.tcpDial(ctx, target, path, map[string]interface{}{
		"degraded": true,
		"reason":   "无 ICMP 权限已用 TCP ping",
	})
}

// tcpDial 直连 target.Port（默认 443）建连测延迟；detail 透传给结果。
func (p *PingProbe) tcpDial(ctx context.Context, target DiagnosticTarget, path string, detail map[string]interface{}) ProbeResult {
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
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Detail: detail, Error: err.Error()}
	}
	_ = conn.Close()
	return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds(), Detail: detail}
}

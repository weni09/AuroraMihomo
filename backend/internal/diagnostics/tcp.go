package diagnostics

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
)

// TCPProbe 对 host:port 建连测延迟。
//
// direct 路径直连目标；proxy 路径经出网选择器指定的 HTTP 代理向目标发
// CONNECT 建立隧道测延迟（代理不可用时回落直连并标注）。
// 无全局可变状态：DialTimeout/Selector 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type TCPProbe struct {
	DialTimeout time.Duration
	// Selector 按出网路径返回 http.Client：proxy 路径从这里取代理地址发
	// CONNECT；nil 时 proxy 路径回落直连。
	Selector ClientSelector
}

func (p *TCPProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if target.Port <= 0 {
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusError, Error: "端口必填: tcp 探测需要指定有效的 target.port"}
	}
	addr := net.JoinHostPort(target.Target, strconv.Itoa(target.Port))
	if path == PathProxy && p.Selector != nil {
		return p.proxyRun(ctx, target, path, p.Selector(path))
	}
	if path == PathProxy {
		// 未注入出网选择器：无法取代理地址，如实回落直连并标注，
		// 避免把直连结果冒充代理路径。
		return p.dial(ctx, target, path, addr, "未配置出网选择器，proxy 路径回落直连")
	}
	return p.dial(ctx, target, path, addr, "")
}

// dial 直连 host:port 建连测延迟。fallbackNote 非空时（proxy 路径回落直连）
// 写入 Detail，如实标注该结果并非代理路径。
func (p *TCPProbe) dial(ctx context.Context, target DiagnosticTarget, path, addr, fallbackNote string) ProbeResult {
	timeout := p.DialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var detail map[string]interface{}
	if fallbackNote != "" {
		detail = map[string]interface{}{"proxyFallback": true, "note": fallbackNote}
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
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Detail: detail, Error: err.Error()}
	}
	conn.Close()
	return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds(), Detail: detail}
}

// proxyRun 经代理 CONNECT target 测隧道建立延迟；client 未提供代理
// （选择器回落直连）时回落直连并标注。
func (p *TCPProbe) proxyRun(ctx context.Context, target DiagnosticTarget, path string, client *http.Client) ProbeResult {
	if client == nil || proxyAddrOf(client) == "" {
		addr := net.JoinHostPort(target.Target, strconv.Itoa(target.Port))
		return p.dial(ctx, target, path, addr, "代理不可用，proxy 路径回落直连")
	}
	latency, err := proxyConnect(ctx, client, target.Target, target.Port)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds()}
}

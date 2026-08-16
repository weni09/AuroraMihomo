package diagnostics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPProbe GET 目标，报状态码/耗时/重定向链。
//
// 无全局可变状态：Timeout/Client/Selector 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type HTTPProbe struct {
	Timeout time.Duration
	Client  *http.Client // 可注入（代理/直连）；nil 用默认
	// Selector 按出网路径返回 http.Client：proxy 路径经 mihomo 代理、
	// direct 路径直连（两条路径都带 SSRF 防线）。非 nil 时优先于 Client。
	Selector ClientSelector
}

func (p *HTTPProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := p.Client
	if p.Selector != nil {
		// proxy 路径用出网选择器提供的 client（经 mihomo 代理），direct 路径
		// 也用它的直连 client——两条路径的 SSRF 防线由此保持一致。
		client = p.Selector(path)
	}
	if client == nil {
		// 默认直连 client 带 fetcher 同款 SSRF 防线（DNS 复验 + 逐跳校验），
		// 与订阅拉取一致：探测目标虽经 ValidateTarget 预检，但重定向目标与
		// DNS 重绑定仍需在 client 层兜底。
		client = directHTTPClient(timeout)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Target, nil)
	if err != nil {
		return ProbeResult{Target: target.Target, Type: TypeHTTP, Path: path, Status: StatusError, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "AuroraMihomo-Diagnostics/0.1")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeHTTP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return ProbeResult{
			Target:    target.Target,
			Type:      TypeHTTP,
			Path:      path,
			Status:    StatusSuccess,
			LatencyMs: latency.Milliseconds(),
			Detail:    map[string]interface{}{"statusCode": resp.StatusCode, "finalURL": resp.Request.URL.String()},
		}
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeHTTP,
		Path:      path,
		Status:    StatusFail,
		LatencyMs: latency.Milliseconds(),
		Detail:    map[string]interface{}{"statusCode": resp.StatusCode, "finalURL": resp.Request.URL.String()},
		Error:     fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}

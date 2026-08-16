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

// ProbeTimeout 返回该探测期望的独立超时：显式设置时用显式值，否则 10s。
// 实现 TimeoutProbe，避免被服务级 5s 超时压垮（下载慢速目标常需更久）。
func (p *HTTPProbe) ProbeTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 10 * time.Second
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
	// proxy 路径回落直连检测：选择器在代理不可用时返回直连 client（无 Proxy，
	// 与 TCP/Ping 的 proxyAddrOf 检测一致）。如实标注，避免 both 模式下把
	// 直连成功冒充代理路径。
	var fallbackNote string
	if path == PathProxy && proxyAddrOf(client) == "" {
		fallbackNote = "代理不可用，已直连"
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
		return ProbeResult{Target: target.Target, Type: TypeHTTP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Detail: withProxyFallback(nil, fallbackNote), Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	detail := withProxyFallback(map[string]interface{}{"statusCode": resp.StatusCode, "finalURL": resp.Request.URL.String()}, fallbackNote)
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return ProbeResult{
			Target:    target.Target,
			Type:      TypeHTTP,
			Path:      path,
			Status:    StatusSuccess,
			LatencyMs: latency.Milliseconds(),
			Detail:    detail,
		}
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeHTTP,
		Path:      path,
		Status:    StatusFail,
		LatencyMs: latency.Milliseconds(),
		Detail:    detail,
		Error:     fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}

// withProxyFallback 把 proxy 路径回落直连的标注并入 Detail；note 为空时原样
// 返回（不标注）。与 TCP/Ping 的 proxyFallback 标注同语义：直连/代理对比
// 输出真实路径的结果。
func withProxyFallback(detail map[string]interface{}, note string) map[string]interface{} {
	if note == "" {
		return detail
	}
	if detail == nil {
		detail = map[string]interface{}{}
	}
	detail["proxyFallback"] = true
	detail["note"] = note
	return detail
}

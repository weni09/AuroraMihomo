package diagnostics

import (
	"context"
	"net"
	"time"
)

// DNSProbe 查 A/AAAA 记录与耗时。
//
// DNS 查询（UDP/53 直出）不经 HTTP 代理：proxy 路径执行与 direct 相同的查询，
// 并在成功结果中如实标注「结果与 direct 路径一致」，不伪造代理路径数据。
// 无全局可变状态：Timeout/Resolver 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type DNSProbe struct {
	Timeout  time.Duration
	Resolver *net.Resolver // 可注入 mock；nil 用系统默认
}

func (p *DNSProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	ips, err := resolver.LookupIPAddr(pctx, target.Target)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if pctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeDNS, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	if len(ips) == 0 {
		return ProbeResult{Target: target.Target, Type: TypeDNS, Path: path, Status: StatusFail, LatencyMs: latency.Milliseconds(), Error: "无解析结果"}
	}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, ip.IP.String())
	}
	detail := map[string]interface{}{"records": addrs}
	// 标注本次查询使用的解析器：net.DefaultResolver 无法直接取服务器列表，
	// 简化标注——用系统默认 resolver 记 "system"，注入的自定义 resolver
	// （测试/调用方）记 "custom"。前端据此展示「DNS 服务器: 系统默认 / 自定义」。
	if p.Resolver == nil {
		detail["resolver"] = "system"
	} else {
		detail["resolver"] = "custom"
	}
	if path == PathProxy {
		// DNS 查询不经 HTTP 代理（UDP/53 直出），结果与 direct 路径一致——
		// 如实标注，避免前端把直出结果误读为代理路径数据。
		detail["note"] = "DNS 查询不经 HTTP 代理，结果与 direct 路径一致"
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeDNS,
		Path:      path,
		Status:    StatusSuccess,
		LatencyMs: latency.Milliseconds(),
		Detail:    detail,
	}
}

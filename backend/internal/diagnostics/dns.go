package diagnostics

import (
	"context"
	"net"
	"time"
)

// DNSProbe 查 A/AAAA 记录与耗时。
type DNSProbe struct {
	Timeout  time.Duration
	Resolver *net.Resolver // 可注入 mock；nil 用系统默认
}

func (p *DNSProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.Timeout <= 0 {
		p.Timeout = 5 * time.Second
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	pctx, cancel := context.WithTimeout(ctx, p.Timeout)
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
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeDNS,
		Path:      path,
		Status:    StatusSuccess,
		LatencyMs: latency.Milliseconds(),
		Detail:    map[string]interface{}{"records": addrs},
	}
}

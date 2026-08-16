package diagnostics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc 把普通函数适配为 http.RoundTripper，便于测试注入自定义传输。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunProbeFramework(t *testing.T) {
	// 验证探测框架：探测执行一次、结果回填、完成后置 Done
	calls := 0
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		calls++
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypeTCP, Target: "x"}}, PathDirect, 5*time.Second, map[string]Probe{TypeTCP: probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if calls != 1 {
		t.Fatalf("探测应执行一次, got %d", calls)
	}
	snap := run.Snapshot()
	if len(snap.Results) != 1 || snap.Results[0].Status != StatusSuccess {
		t.Fatalf("结果应回填, got %+v", snap.Results)
	}
	if !snap.Done {
		t.Fatal("执行完成后应置 Done")
	}
}

func TestRunProbeTimeout(t *testing.T) {
	// 验证每探测独立超时：探测等 ctx.Done，超时后结果标记 timeout
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		<-ctx.Done() // 等框架的超时
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypePing, Target: "x"}}, PathDirect, 100*time.Millisecond, map[string]Probe{TypePing: probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	snap := run.Snapshot()
	if len(snap.Results) != 1 {
		t.Fatalf("应有 1 条结果, got %d", len(snap.Results))
	}
	if snap.Results[0].Status != StatusTimeout {
		t.Fatalf("超时应标记 timeout, got %q", snap.Results[0].Status)
	}
	if !snap.Done {
		t.Fatal("超时探测结束后应置 Done")
	}
}

func TestRunProbeProgressSingleOwner(t *testing.T) {
	// 验证进度回调单一所有者：即使探测按旧文档「完成时也调一次 cb」，
	// onProgress 也只收到一次最终事件——最终结果由框架在 Run 返回后统一回调，
	// 不会因探测自身再调 cb 而重复触发。
	events := 0
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		res := ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
		if cb != nil {
			cb(res) // 旧文档行为：完成时上报一次；当前 cb 为 nil，须判空
		}
		return res
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypePing, Target: "x"}}, PathDirect, 5*time.Second, map[string]Probe{TypePing: probe})
	run.Execute(context.Background(), func(ProbeResult) { events++ })
	if events != 1 {
		t.Fatalf("onProgress 应只收到一次最终事件, got %d", events)
	}
	snap := run.Snapshot()
	if len(snap.Results) != 1 || snap.Results[0].Status != StatusSuccess {
		t.Fatalf("结果应回填, got %+v", snap.Results)
	}
}

func TestExecuteDispatchesByType(t *testing.T) {
	// 验证按类型分派：多个探测器注册不同类型，每个目标只执行匹配类型的探测器，
	// 未注册类型回填 StatusError 且不 panic
	tcpCalls, dnsCalls := 0, 0
	tcpProbe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		tcpCalls++
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	dnsProbe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		dnsCalls++
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	targets := []DiagnosticTarget{
		{Type: TypeTCP, Target: "t1"},
		{Type: TypeDNS, Target: "d1"},
		{Type: TypeTCP, Target: "t2"},
		{Type: "ftp", Target: "u1"}, // 未注册类型
	}
	run := NewRun("req-1", targets, PathDirect, 5*time.Second, map[string]Probe{TypeTCP: tcpProbe, TypeDNS: dnsProbe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if tcpCalls != 2 {
		t.Fatalf("tcp 探测应执行 2 次, got %d", tcpCalls)
	}
	if dnsCalls != 1 {
		t.Fatalf("dns 探测应执行 1 次, got %d", dnsCalls)
	}
	snap := run.Snapshot()
	if len(snap.Results) != len(targets) {
		t.Fatalf("应有 %d 条结果, got %d", len(targets), len(snap.Results))
	}
	if snap.Results[3].Status != StatusError || snap.Results[3].Error == "" {
		t.Fatalf("未注册类型应回填 StatusError, got %+v", snap.Results[3])
	}
}

func TestRunProbeTimeoutOverride(t *testing.T) {
	// TracerouteProbe 实现 TimeoutProbe：Execute 应使用其 30s 覆盖值而非
	// 服务级 5s——注入 RunCmd 断言 ctx 截止时间在 30s 量级。
	probe := &TracerouteProbe{
		RunCmd: func(ctx context.Context, host string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("ctx 应有截止时间")
			}
			if remaining := time.Until(deadline); remaining < 25*time.Second {
				t.Fatalf("Traceroute 探测超时应为 30s 覆盖值, 剩余 %v", remaining)
			}
			return []byte(tracerouteSample), nil
		},
	}
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypeTraceroute, Target: "8.8.8.8"}}, PathDirect, 5*time.Second, map[string]Probe{TypeTraceroute: probe})
	run.Execute(context.Background(), nil)
	snap := run.Snapshot()
	if len(snap.Results) != 1 || snap.Results[0].Status != StatusSuccess {
		t.Fatalf("应成功完成, got %+v", snap.Results)
	}
}

func TestRunHTTPProbeTimeoutOverride(t *testing.T) {
	// HTTPProbe 实现 TimeoutProbe：Execute 应使用其 10s 覆盖值而非服务级
	// 5s——注入自定义 RoundTripper 断言请求 ctx 截止时间在 10s 量级。
	var got time.Duration
	probe := &HTTPProbe{
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if dl, ok := req.Context().Deadline(); ok {
					got = time.Until(dl)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    req,
				}, nil
			}),
		},
	}
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypeHTTP, Target: "http://target.invalid/"}}, PathDirect, 5*time.Second, map[string]Probe{TypeHTTP: probe})
	run.Execute(context.Background(), nil)
	if got < 9*time.Second {
		t.Fatalf("HTTP 探测超时应为 10s 覆盖值, got 剩余 %v", got)
	}
}

func TestProbeTimeoutDefaults(t *testing.T) {
	// HTTP/Traceroute 实现 TimeoutProbe：默认值与显式覆盖都正确。
	if got := (&HTTPProbe{}).ProbeTimeout(); got != 10*time.Second {
		t.Fatalf("HTTPProbe 默认应为 10s, got %v", got)
	}
	if got := (&HTTPProbe{Timeout: 3 * time.Second}).ProbeTimeout(); got != 3*time.Second {
		t.Fatalf("HTTPProbe 显式 Timeout 应生效, got %v", got)
	}
	if got := (&TracerouteProbe{}).ProbeTimeout(); got != 30*time.Second {
		t.Fatalf("TracerouteProbe 默认应为 30s, got %v", got)
	}
	if got := (&TracerouteProbe{Timeout: 7 * time.Second}).ProbeTimeout(); got != 7*time.Second {
		t.Fatalf("TracerouteProbe 显式 Timeout 应生效, got %v", got)
	}
}

func TestSnapshotConcurrencySafe(t *testing.T) {
	// 验证并发安全快照：Execute 在 goroutine 中运行，主协程循环 Snapshot()
	// 读取不产生数据竞争（配合 go test -race 验证），且快照是深拷贝——
	// 修改快照内容不影响后续快照。
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		time.Sleep(1 * time.Millisecond)
		return ProbeResult{
			Target: target.Target,
			Type:   target.Type,
			Path:   path,
			Status: StatusSuccess,
			Detail: map[string]interface{}{"seq": 1, "msg": "original"},
		}
	})
	const n = 50
	targets := make([]DiagnosticTarget, n)
	for i := range targets {
		targets[i] = DiagnosticTarget{Type: TypeTCP, Target: "x"}
	}
	run := NewRun("req-1", targets, PathDirect, 5*time.Second, map[string]Probe{TypeTCP: probe})

	done := make(chan struct{})
	go func() {
		defer close(done)
		run.Execute(context.Background(), func(ProbeResult) {})
	}()

	for {
		select {
		case <-done:
			goto finished
		default:
		}
		snap := run.Snapshot()
		if len(snap.Results) > n {
			t.Fatalf("结果数不应超过目标数, got %d", len(snap.Results))
		}
		if len(snap.Results) > 0 {
			// 修改拷贝的 Detail map，不得影响内部状态
			if d, ok := snap.Results[0].Detail.(map[string]interface{}); ok {
				d["seq"] = 999
				d["msg"] = "mutated"
			}
			snap.Results[0] = ProbeResult{} // 修改拷贝，不得影响内部状态
		}
	}
finished:
	snap := run.Snapshot()
	if !snap.Done || len(snap.Results) != n {
		t.Fatalf("执行完成后快照应含全部结果, got done=%v len=%d", snap.Done, len(snap.Results))
	}
	for i, res := range snap.Results {
		if res.Status != StatusSuccess {
			t.Fatalf("结果 %d 被并发快照污染: %+v", i, res)
		}
		// Detail 也必须是深拷贝：并发期间被改过的 map 不得残留
		d, ok := res.Detail.(map[string]interface{})
		if !ok {
			t.Fatalf("结果 %d Detail 应为 map[string]interface{}, got %T", i, res.Detail)
		}
		if d["seq"] != 1 || d["msg"] != "original" {
			t.Fatalf("结果 %d 的 Detail 被并发快照污染: %+v", i, d)
		}
	}
}

func TestSnapshotDetailIsolation(t *testing.T) {
	// 验证 Detail 深拷贝契约：快照返回的 Detail map 与内部状态不共享引用，
	// 修改快照的 Detail 后再次 Snapshot 应看到原始值。覆盖 map[string]interface{}、
	// map[string]string 与标量（原样保留）三种形态。
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		var detail any
		switch target.Type {
		case TypeTCP:
			detail = map[string]interface{}{"code": 0, "msg": "ok"}
		case TypeDNS:
			detail = map[string]string{"server": "1.1.1.1", "rcode": "NOERROR"}
		default:
			detail = "plain-value"
		}
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess, Detail: detail}
	})
	targets := []DiagnosticTarget{
		{Type: TypeTCP, Target: "t1"},
		{Type: TypeDNS, Target: "d1"},
		{Type: TypePing, Target: "p1"},
	}
	run := NewRun("req-1", targets, PathDirect, 5*time.Second, map[string]Probe{TypeTCP: probe, TypeDNS: probe, TypePing: probe})
	run.Execute(context.Background(), func(ProbeResult) {})

	// 修改返回快照的 Detail map，再取快照应恢复为原始值
	first := run.Snapshot()
	m, ok := first.Results[0].Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map[string]interface{}, got %T", first.Results[0].Detail)
	}
	m["code"] = 999
	m["msg"] = "mutated"
	if s, ok := first.Results[1].Detail.(map[string]string); ok {
		s["rcode"] = "SERVFAIL"
	} else {
		t.Fatalf("Detail 应为 map[string]string, got %T", first.Results[1].Detail)
	}

	second := run.Snapshot()
	if got := second.Results[0].Detail.(map[string]interface{})["code"]; got != 0 {
		t.Fatalf("快照间共享 Detail 引用: 期望 code=0, got %v", got)
	}
	if got := second.Results[1].Detail.(map[string]string)["rcode"]; got != "NOERROR" {
		t.Fatalf("快照间共享 Detail 引用: 期望 rcode=NOERROR, got %v", got)
	}
	if got := second.Results[2].Detail; got != "plain-value" {
		t.Fatalf("非 map Detail 应原样保留, got %v", got)
	}
}

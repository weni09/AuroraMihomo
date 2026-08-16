package diagnostics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestServiceRunAndGetResult(t *testing.T) {
	// TCP 探测 127.0.0.1:1（失败但完成），轮询 GetResult 直到 Done
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		res := ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusFail, Error: "connection refused"}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(target.Target, strconv.Itoa(target.Port)), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			res.Status = StatusSuccess
		}
		return res
	})
	svc := New(Config{
		MaxConcurrent: 3,
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "127.0.0.1", Port: 1}},
		Path:    PathDirect,
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}
	if len(id) < 5 || id[:5] != "diag-" {
		t.Fatalf("requestId 应以 diag- 开头, got %q", id)
	}

	var snap RunSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for {
		s, ok := svc.GetResult(id)
		if !ok {
			t.Fatalf("GetResult 应能找到请求 %q", id)
		}
		snap = s
		if snap.Done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 Done 超时, snap=%+v", snap)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snap.Results) != 1 {
		t.Fatalf("应有 1 条结果, got %d", len(snap.Results))
	}
	if snap.Results[0].Status != StatusFail {
		t.Fatalf("127.0.0.1:1 应探测失败, got %+v", snap.Results[0])
	}
	if snap.Results[0].Path != PathDirect {
		t.Fatalf("结果路径应为 %q, got %q", PathDirect, snap.Results[0].Path)
	}
}

func TestServiceConcurrencyLimit(t *testing.T) {
	// 3 个慢探测占满信号量，第 4 个 Run 返回 ErrBusy
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	svc := New(Config{
		MaxConcurrent: 3,
		ProbeTimeout:  time.Hour, // 慢探测不因超时而让出信号量
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id, err := svc.Run(context.Background(), DiagnosticRequest{
			Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "x"}},
			Path:    PathDirect,
		})
		if err != nil {
			t.Fatalf("第 %d 个 Run 应成功, got %v", i+1, err)
		}
		ids = append(ids, id)
	}
	// 等 3 个探测都开始执行，确保信号量被占满
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("探测 %d 未开始执行", i+1)
		}
	}

	if _, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "y"}},
		Path:    PathDirect,
	}); !errors.Is(err, ErrBusy) {
		t.Fatalf("第 4 个 Run 应返回 ErrBusy, got %v", err)
	}

	// 放行全部探测，等待请求完成，避免 goroutine 泄漏
	close(release)
	for _, id := range ids {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if s, ok := svc.GetResult(id); ok && s.Done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("请求 %q 未在放行后完成", id)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestServiceBothPathExpands(t *testing.T) {
	// Path=both 展开为 direct+proxy 两次执行：探测与进度事件都应包含两类路径
	var probeMu sync.Mutex
	seenPaths := map[string]int{}
	var eventMu sync.Mutex
	events := []DiagnosticEvent{}
	eventCh := make(chan struct{}, 2)

	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		probeMu.Lock()
		seenPaths[path]++
		probeMu.Unlock()
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	publish := func(eventType string, data interface{}) {
		if eventType != EventTypeProgress {
			t.Errorf("事件类型应为 %q, got %q", EventTypeProgress, eventType)
		}
		ev, ok := data.(DiagnosticEvent)
		if !ok {
			t.Errorf("事件数据应为 DiagnosticEvent, got %T", data)
			return
		}
		eventMu.Lock()
		events = append(events, ev)
		eventMu.Unlock()
		eventCh <- struct{}{}
	}
	svc := New(Config{
		MaxConcurrent: 3,
		Publish:       publish,
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "example.com"}},
		Path:    "both",
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}

	// 等两个阶段的进度事件都发布（事件在结果 append 之后发布，等事件而非
	// 等结果，避免结果已就绪但第 2 条事件尚未发布的竞态）
	for i := range 2 {
		select {
		case <-eventCh:
		case <-time.After(3 * time.Second):
			t.Fatalf("等待进度事件超时, 收到 %d/2", i)
		}
	}

	probeMu.Lock()
	if seenPaths[PathDirect] != 1 || seenPaths[PathProxy] != 1 {
		probeMu.Unlock()
		t.Fatalf("探测应各执行一次 direct/proxy, got %v", seenPaths)
	}
	probeMu.Unlock()

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) != 2 {
		t.Fatalf("应有 2 条进度事件, got %d", len(events))
	}
	got := map[string]bool{}
	for _, ev := range events {
		got[ev.Path] = true
		if ev.RequestID != id {
			t.Errorf("事件 requestId 应为 %q, got %q", id, ev.RequestID)
		}
		if ev.Status != StatusSuccess {
			t.Errorf("事件状态应为 %q, got %q", StatusSuccess, ev.Status)
		}
	}
	if !got[PathDirect] || !got[PathProxy] {
		t.Fatalf("事件应包含 direct 与 proxy 两类路径, got %v", got)
	}
}

func TestGetResultDoneIncludesAllResults(t *testing.T) {
	// both 路径：等 GetResult.Done=true 后，快照必须已含 direct+proxy 两个阶段
	// 的全部结果。旧实现先快照后观察 done，可能快照缺最后阶段（proxy）却返回
	// Done=true，轮询方停止轮询丢结果；-race -count 多次跑可稳定暴露该竞态。
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	svc := New(Config{
		MaxConcurrent: 3,
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "example.com"}},
		Path:    "both",
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		s, ok := svc.GetResult(id)
		if !ok {
			t.Fatalf("GetResult 应能找到请求 %q", id)
		}
		if !s.Done {
			if time.Now().After(deadline) {
				t.Fatalf("等待 Done 超时, snap=%+v", s)
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		// Done=true 的快照必须含 both 两阶段结果，缺任一阶段即为丢结果
		if len(s.Results) != 2 {
			t.Fatalf("Done=true 时结果应含 direct+proxy 两条, got %d: %+v", len(s.Results), s.Results)
		}
		paths := map[string]bool{}
		for _, r := range s.Results {
			paths[r.Path] = true
		}
		if !paths[PathDirect] || !paths[PathProxy] {
			t.Fatalf("Done=true 时结果应含 direct 与 proxy, got %+v", s.Results)
		}
		break
	}
}

func TestServiceCancel(t *testing.T) {
	// 慢探测：Cancel 后执行中断，Done 置位且结果标记 timeout
	started := make(chan struct{})
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		close(started)
		<-ctx.Done()
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	svc := New(Config{
		MaxConcurrent: 3,
		ProbeTimeout:  time.Hour,
		Probes:        map[string]Probe{TypePing: probe},
	})
	defer svc.Close()

	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypePing, Target: "x"}},
		Path:    PathDirect,
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("探测未开始执行")
	}

	svc.Cancel(id)

	deadline := time.Now().Add(2 * time.Second)
	for {
		s, ok := svc.GetResult(id)
		if ok && s.Done {
			if len(s.Results) != 1 || s.Results[0].Status != StatusTimeout {
				t.Fatalf("Cancel 后结果应为 1 条 timeout, got %+v", s.Results)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Cancel 后等待 Done 超时, ok=%v", ok)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServiceCancelSkipsRemainingStages(t *testing.T) {
	// Path=both：direct 阶段慢探测，Cancel 后 direct 返回 timeout，
	// 剩余 proxy 阶段必须被跳过——结果只含 direct 一条，无 proxy 结果/事件
	started := make(chan struct{})
	var stageMu sync.Mutex
	stageCalls := []string{}
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		stageMu.Lock()
		stageCalls = append(stageCalls, path)
		stageMu.Unlock()
		if path == PathDirect {
			close(started)
			<-ctx.Done()
		}
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	var eventMu sync.Mutex
	events := []DiagnosticEvent{}
	publish := func(eventType string, data interface{}) {
		if eventType != EventTypeProgress {
			t.Errorf("事件类型应为 %q, got %q", EventTypeProgress, eventType)
		}
		ev, ok := data.(DiagnosticEvent)
		if !ok {
			t.Errorf("事件数据应为 DiagnosticEvent, got %T", data)
			return
		}
		eventMu.Lock()
		events = append(events, ev)
		eventMu.Unlock()
	}
	svc := New(Config{
		MaxConcurrent: 3,
		ProbeTimeout:  time.Hour,
		Publish:       publish,
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "x"}},
		Path:    "both",
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("direct 阶段探测未开始执行")
	}

	svc.Cancel(id)

	deadline := time.Now().Add(2 * time.Second)
	for {
		s, ok := svc.GetResult(id)
		if ok && s.Done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Cancel 后等待 Done 超时, ok=%v", ok)
		}
		time.Sleep(5 * time.Millisecond)
	}

	stageMu.Lock()
	if len(stageCalls) != 1 || stageCalls[0] != PathDirect {
		t.Fatalf("Cancel 后应只执行 direct 一个阶段, got %v", stageCalls)
	}
	stageMu.Unlock()

	s, ok := svc.GetResult(id)
	if !ok {
		t.Fatalf("GetResult 应能找到请求 %q", id)
	}
	if len(s.Results) != 1 {
		t.Fatalf("结果应只含 1 条（direct timeout）, got %+v", s.Results)
	}
	if s.Results[0].Path != PathDirect || s.Results[0].Status != StatusTimeout {
		t.Fatalf("唯一结果应为 direct/timeout, got %+v", s.Results[0])
	}

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) != 1 {
		t.Fatalf("应只有 1 条进度事件（direct）, got %d", len(events))
	}
	if events[0].Path != PathDirect {
		t.Fatalf("事件路径应为 direct, got %+v", events[0])
	}
}

func TestServiceTotalTimeoutEmitsSyntheticForSkippedStage(t *testing.T) {
	// 总时限（父 ctx 短截止早于 totalRunTimeout 生效）+ 慢 direct：
	// direct 耗尽预算后 proxy 阶段被跳过，必须补发 synthetic error 结果，
	// 保证 both 运行每个阶段都有结果（前端能看到代理侧对比占位）。
	started := make(chan struct{})
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		if path == PathDirect {
			close(started)
			<-ctx.Done() // 慢 direct：挂到总时限耗尽
		}
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	svc := New(Config{
		MaxConcurrent: 3,
		ProbeTimeout:  time.Hour, // 不因单探测超时结束 direct，让总时限收口
		Probes:        map[string]Probe{TypeTCP: probe},
	})
	defer svc.Close()

	// 父 ctx 短截止：rctx 取更早截止（totalRunTimeout 为兜底上限），
	// 200ms 后总时限生效——direct 返回 timeout、proxy 阶段被跳过。
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	id, err := svc.Run(ctx, DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: TypeTCP, Target: "x"}},
		Path:    "both",
	})
	if err != nil {
		t.Fatalf("Run 应成功, got %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("direct 阶段探测未开始执行")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		s, ok := svc.GetResult(id)
		if ok && s.Done {
			// direct timeout + proxy synthetic error 各一条
			if len(s.Results) != 2 {
				t.Fatalf("结果应含 direct + proxy 两条, got %+v", s.Results)
			}
			var proxyRes *ProbeResult
			for i := range s.Results {
				if s.Results[i].Path == PathProxy {
					proxyRes = &s.Results[i]
				}
			}
			if proxyRes == nil {
				t.Fatalf("缺少 proxy 阶段结果, got %+v", s.Results)
			}
			if proxyRes.Status != StatusError {
				t.Fatalf("proxy 结果应为 synthetic error, got %+v", *proxyRes)
			}
			if proxyRes.Error != "总时限已到，跳过该路径阶段" {
				t.Fatalf("synthetic error 应说明总时限跳过, got %q", proxyRes.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 Done 超时, ok=%v snap=%+v", ok, s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestValidateTargetBasic(t *testing.T) {
	// http/https 允许，ftp 及畸形输入拒绝
	allowed := []string{
		"http://example.com",
		"https://example.com/path?q=1",
		"HTTP://EXAMPLE.COM",
	}
	for _, raw := range allowed {
		if err := ValidateTarget(raw); err != nil {
			t.Errorf("ValidateTarget(%q) 应通过, got %v", raw, err)
		}
	}
	rejected := []string{
		"ftp://example.com",
		"file:///etc/passwd",
		"",
		"   ",
		"http://",
		"not-a-url",
	}
	for _, raw := range rejected {
		if err := ValidateTarget(raw); err == nil {
			t.Errorf("ValidateTarget(%q) 应拒绝", raw)
		}
	}
}

func TestValidateTargetRejectsEmptyHost(t *testing.T) {
	// http://:80 的 u.Host==":80" 非空但 Hostname() 为空，旧校验只查 u.Host 漏检
	rejected := []string{
		"http://:80",
		"https://:443",
		"http://:80/path",
	}
	for _, raw := range rejected {
		if err := ValidateTarget(raw); err == nil {
			t.Errorf("ValidateTarget(%q) 应拒绝空主机名", raw)
		}
	}
	// 带端口的主机名仍应通过，避免误伤
	allowed := []string{
		"http://example.com:8080",
		"http://127.0.0.1:80",
	}
	for _, raw := range allowed {
		if err := ValidateTarget(raw); err != nil {
			t.Errorf("ValidateTarget(%q) 应通过, got %v", raw, err)
		}
	}
}

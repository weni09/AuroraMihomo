package diagnostics

import (
	"context"
	"testing"
	"time"
)

func TestRunProbeFramework(t *testing.T) {
	// 验证探测框架：探测执行一次、结果回填、完成后置 Done
	calls := 0
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		calls++
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusSuccess}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypePing, Target: "x"}}, PathDirect, 5*time.Second, []Probe{probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if calls != 1 {
		t.Fatalf("探测应执行一次, got %d", calls)
	}
	if len(run.Results) != 1 || run.Results[0].Status != StatusSuccess {
		t.Fatalf("结果应回填, got %+v", run.Results)
	}
	if !run.Done {
		t.Fatal("执行完成后应置 Done")
	}
}

func TestRunProbeTimeout(t *testing.T) {
	// 验证每探测独立超时：探测等 ctx.Done，超时后结果标记 timeout
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		<-ctx.Done() // 等框架的超时
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: StatusTimeout}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: TypePing, Target: "x"}}, PathDirect, 100*time.Millisecond, []Probe{probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if len(run.Results) != 1 {
		t.Fatalf("应有 1 条结果, got %d", len(run.Results))
	}
	if run.Results[0].Status != StatusTimeout {
		t.Fatalf("超时应标记 timeout, got %q", run.Results[0].Status)
	}
	if !run.Done {
		t.Fatal("超时探测结束后应置 Done")
	}
}

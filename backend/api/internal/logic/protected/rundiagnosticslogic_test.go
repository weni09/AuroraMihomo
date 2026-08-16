package protected

import (
	"context"
	"strings"
	"testing"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/diagnostics"
)

// newDiagSvcCtx 构造只含 Diag 的 ServiceContext：logic 层只访问 svcCtx.Diag，
// 无需拉起完整 ServiceContext（数据库/内核等）。探测全部用假实现即时返回，
// 避免单元测试真实出网。
func newDiagSvcCtx(t *testing.T, proxyURL func() string) *svc.ServiceContext {
	t.Helper()
	probe := diagnostics.ProbeFunc(func(ctx context.Context, target diagnostics.DiagnosticTarget, path string, cb diagnostics.ProgressFunc) diagnostics.ProbeResult {
		return diagnostics.ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: diagnostics.StatusSuccess}
	})
	diag := diagnostics.New(diagnostics.Config{
		MaxConcurrent: 3,
		Probes: map[string]diagnostics.Probe{
			diagnostics.TypeHTTP: probe,
			diagnostics.TypeTCP:  probe,
			diagnostics.TypeDNS:  probe,
		},
		ProxyURL: proxyURL,
	})
	t.Cleanup(func() { diag.Close() })
	return &svc.ServiceContext{Diag: diag}
}

// 单个非法 HTTP 目标（SSRF）不应阻塞整体：合法目标照常执行并返回
// requestId，非法目标进 Invalid 列表。
func TestRunDiagnosticsInvalidTargetDoesNotBlockOthers(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, nil)
	l := NewRunDiagnosticsLogic(context.Background(), svcCtx)

	resp, err := l.RunDiagnostics(&types.DiagnosticRunReq{
		Targets: []types.DiagnosticTarget{
			{Type: "http", Target: "http://169.254.169.254/latest/meta-data/"},
			{Type: "tcp", Target: "api.github.com", Port: 443},
			{Type: "dns", Target: "1.1.1.1"},
		},
		Path: "direct",
	})
	if err != nil {
		t.Fatalf("单个非法目标不应整体失败, got %v", err)
	}
	if resp.RequestId == "" {
		t.Fatal("有合法目标时应返回 requestId")
	}
	if len(resp.Invalid) != 1 {
		t.Fatalf("应有 1 条非法目标, got %+v", resp.Invalid)
	}
	inv := resp.Invalid[0]
	if inv.Target != "http://169.254.169.254/latest/meta-data/" {
		t.Fatalf("非法目标应保留原值, got %+v", inv)
	}
	if !strings.Contains(inv.Reason, "已拒绝") {
		t.Fatalf("非法目标应带拒绝原因, got %q", inv.Reason)
	}
}

// 空目标（非 HTTP 类型也会被拦）同样进 Invalid 而不整体失败。
func TestRunDiagnosticsInvalidEmptyTarget(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, nil)
	l := NewRunDiagnosticsLogic(context.Background(), svcCtx)

	resp, err := l.RunDiagnostics(&types.DiagnosticRunReq{
		Targets: []types.DiagnosticTarget{
			{Type: "tcp", Target: "   "},
			{Type: "tcp", Target: "example.com", Port: 443},
		},
		Path: "direct",
	})
	if err != nil {
		t.Fatalf("空目标不应整体失败, got %v", err)
	}
	if resp.RequestId == "" {
		t.Fatal("有合法目标时应返回 requestId")
	}
	if len(resp.Invalid) != 1 || resp.Invalid[0].Target != "   " {
		t.Fatalf("应有 1 条空目标非法记录, got %+v", resp.Invalid)
	}
	if resp.Invalid[0].Reason == "" {
		t.Fatalf("空目标应带原因, got %+v", resp.Invalid[0])
	}
}

// 全部非法：不启动诊断，返回空 requestId + 完整非法清单。
func TestRunDiagnosticsAllInvalid(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, nil)
	l := NewRunDiagnosticsLogic(context.Background(), svcCtx)

	resp, err := l.RunDiagnostics(&types.DiagnosticRunReq{
		Targets: []types.DiagnosticTarget{
			{Type: "http", Target: "http://169.254.169.254/"},
			{Type: "http", Target: "ftp://example.com"},
		},
	})
	if err != nil {
		t.Fatalf("全部非法不应返回错误, got %v", err)
	}
	if resp.RequestId != "" {
		t.Fatalf("全部非法时 requestId 应为空, got %q", resp.RequestId)
	}
	if len(resp.Invalid) != 2 {
		t.Fatalf("应有 2 条非法目标, got %+v", resp.Invalid)
	}
}

// 完全没有目标：维持原语义报错（调用方失误，不吞掉）。
func TestRunDiagnosticsNoTargets(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, nil)
	l := NewRunDiagnosticsLogic(context.Background(), svcCtx)

	if _, err := l.RunDiagnostics(&types.DiagnosticRunReq{}); err == nil {
		t.Fatal("无任何目标应返回错误")
	}
}

// 预设目标端点：代理可用时清单含代理端口 TCP 探测目标（修复前生产路径
// 缺失该目标，只在测试里出现过）。
func TestGetDiagnosticsTargetsIncludesProxyPort(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, func() string { return "http://127.0.0.1:7890" })
	l := NewGetDiagnosticsTargetsLogic(context.Background(), svcCtx)

	resp, err := l.GetDiagnosticsTargets()
	if err != nil {
		t.Fatalf("获取预设目标应成功, got %v", err)
	}
	var hasProxy bool
	for _, tg := range resp.Targets {
		if tg.Type == "tcp" && tg.Target == "127.0.0.1" && tg.Port == 7890 {
			hasProxy = true
		}
	}
	if !hasProxy {
		t.Fatalf("预设目标应含代理端口 TCP 探测, got %+v", resp.Targets)
	}
}

// 预设目标端点：代理不可用（空地址）时清单不含代理目标，且基础目标仍在。
func TestGetDiagnosticsTargetsSkipsUnavailableProxy(t *testing.T) {
	svcCtx := newDiagSvcCtx(t, func() string { return "" })
	l := NewGetDiagnosticsTargetsLogic(context.Background(), svcCtx)

	resp, err := l.GetDiagnosticsTargets()
	if err != nil {
		t.Fatalf("获取预设目标应成功, got %v", err)
	}
	for _, tg := range resp.Targets {
		if tg.Type == "tcp" && tg.Target == "127.0.0.1" {
			t.Fatalf("代理不可用时不应急于含代理目标, got %+v", resp.Targets)
		}
	}
	// 基础清单仍在（GitHub DNS + 公共 DNS 共 11 条）
	if len(resp.Targets) != 11 {
		t.Fatalf("无代理时应有 11 条基础目标, got %d: %+v", len(resp.Targets), resp.Targets)
	}
}

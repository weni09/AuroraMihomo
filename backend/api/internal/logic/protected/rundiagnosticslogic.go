package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/diagnostics"

	"github.com/zeromicro/go-zero/core/logx"
)

type RunDiagnosticsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRunDiagnosticsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RunDiagnosticsLogic {
	return &RunDiagnosticsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RunDiagnosticsLogic) RunDiagnostics(req *types.DiagnosticRunReq) (resp *types.DiagnosticRunResp, err error) {
	targets := make([]diagnostics.DiagnosticTarget, 0, len(req.Targets))
	var invalid []types.InvalidTarget
	for _, t := range req.Targets {
		// HTTP/URL 类目标做 SSRF 校验；其余类型只校验非空。
		// 单个非法目标不阻塞整体：收集进 invalid，随响应返回给前端渲染
		// 为 error 结果，其余合法目标照常执行。
		if t.Type == diagnostics.TypeHTTP {
			if verr := diagnostics.ValidateTarget(t.Target); verr != nil {
				invalid = append(invalid, types.InvalidTarget{Target: t.Target, Reason: verr.Error()})
				continue
			}
		}
		if strings.TrimSpace(t.Target) == "" {
			invalid = append(invalid, types.InvalidTarget{Target: t.Target, Reason: "诊断目标为空"})
			continue
		}
		targets = append(targets, diagnostics.DiagnosticTarget{Type: t.Type, Target: t.Target, Port: t.Port})
	}
	if len(targets) == 0 {
		// 全部目标非法：没有可执行的目标，返回空 requestId + 非法清单
		// （req.Targets 完全为空时仍按原语义报错，避免吞掉调用方失误）。
		if len(invalid) > 0 {
			return &types.DiagnosticRunResp{Invalid: invalid}, nil
		}
		return nil, fmt.Errorf("未提供诊断目标")
	}
	id, err := l.svcCtx.Diag.Run(context.Background(), diagnostics.DiagnosticRequest{Targets: targets, Path: req.Path})
	if err != nil {
		return nil, err
	}
	return &types.DiagnosticRunResp{RequestId: id, Invalid: invalid}, nil
}

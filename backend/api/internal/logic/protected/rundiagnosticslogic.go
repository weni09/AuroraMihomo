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
	for _, t := range req.Targets {
		// HTTP/URL 类目标做 SSRF 校验；其余类型只校验非空
		if t.Type == diagnostics.TypeHTTP {
			if verr := diagnostics.ValidateTarget(t.Target); verr != nil {
				return nil, verr
			}
		}
		if strings.TrimSpace(t.Target) == "" {
			return nil, fmt.Errorf("诊断目标为空")
		}
		targets = append(targets, diagnostics.DiagnosticTarget{Type: t.Type, Target: t.Target, Port: t.Port})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("未提供诊断目标")
	}
	id, err := l.svcCtx.Diag.Run(context.Background(), diagnostics.DiagnosticRequest{Targets: targets, Path: req.Path})
	if err != nil {
		return nil, err
	}
	return &types.DiagnosticRunResp{RequestId: id}, nil
}

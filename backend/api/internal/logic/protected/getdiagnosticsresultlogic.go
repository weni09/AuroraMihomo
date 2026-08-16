package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDiagnosticsResultLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetDiagnosticsResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDiagnosticsResultLogic {
	return &GetDiagnosticsResultLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetDiagnosticsResultLogic) GetDiagnosticsResult(req *types.DiagnosticResultReq) (resp *types.DiagnosticResultResp, err error) {
	run, ok := l.svcCtx.Diag.GetResult(req.RequestId)
	if !ok {
		return &types.DiagnosticResultResp{RequestId: req.RequestId, Done: false}, nil
	}
	results := make([]types.ProbeResult, 0, len(run.Results))
	for _, r := range run.Results {
		results = append(results, types.ProbeResult{
			Target: r.Target, Type: r.Type, Path: r.Path, Status: r.Status,
			LatencyMs: r.LatencyMs, Detail: r.Detail, Error: r.Error,
		})
	}
	return &types.DiagnosticResultResp{RequestId: req.RequestId, Done: run.Done, Results: results}, nil
}

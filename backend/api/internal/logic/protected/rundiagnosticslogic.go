package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

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
	// todo: add your logic here and delete this line

	return
}

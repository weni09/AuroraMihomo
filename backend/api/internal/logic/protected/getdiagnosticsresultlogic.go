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
	// todo: add your logic here and delete this line

	return
}

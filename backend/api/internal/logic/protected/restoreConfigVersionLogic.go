package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RestoreConfigVersionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRestoreConfigVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestoreConfigVersionLogic {
	return &RestoreConfigVersionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RestoreConfigVersionLogic) RestoreConfigVersion(req *types.IdPathReq) (resp *types.Result, err error) {
	if err := l.svcCtx.ConfigService.RestoreVersion(l.ctx, req.Id); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "已回滚到该历史版本"}, nil
}

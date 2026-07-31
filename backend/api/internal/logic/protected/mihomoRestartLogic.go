package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MihomoRestartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMihomoRestartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MihomoRestartLogic {
	return &MihomoRestartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MihomoRestartLogic) MihomoRestart() (resp *types.Result, err error) {
	if err := l.svcCtx.MihomoManager.Restart(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "内核已重启"}, nil
}

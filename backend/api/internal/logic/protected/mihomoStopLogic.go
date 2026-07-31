package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MihomoStopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMihomoStopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MihomoStopLogic {
	return &MihomoStopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MihomoStopLogic) MihomoStop() (resp *types.Result, err error) {
	if err := l.svcCtx.MihomoManager.Stop(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "内核已停止"}, nil
}

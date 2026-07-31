package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateMihomoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateMihomoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateMihomoLogic {
	return &UpdateMihomoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateMihomoLogic) UpdateMihomo() (resp *types.Result, err error) {
	if err := l.svcCtx.Updater.UpdateMihomo(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	_ = l.svcCtx.MihomoManager.Restart(l.ctx)
	return &types.Result{Success: true, Message: "Mihomo 内核已更新"}, nil
}

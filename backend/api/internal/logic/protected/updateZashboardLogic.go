package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateZashboardLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateZashboardLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateZashboardLogic {
	return &UpdateZashboardLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateZashboardLogic) UpdateZashboard() (resp *types.Result, err error) {
	if err := l.svcCtx.Updater.UpdateZashboard(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "Zashboard 面板已更新"}, nil
}

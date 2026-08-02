package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardStopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardStopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardStopLogic {
	return &AdGuardStopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardStop 停止 AdGuard Home 子进程。
func (l *AdGuardStopLogic) AdGuardStop() (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.Stop(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "AdGuard Home 已停止"}, nil
}

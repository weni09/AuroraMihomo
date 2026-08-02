package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardWiringRollbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardWiringRollbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardWiringRollbackLogic {
	return &AdGuardWiringRollbackLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardWiringRollback 按快照解除 DNS 对接并恢复原文。
func (l *AdGuardWiringRollbackLogic) AdGuardWiringRollback() (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.WiringRollback(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "DNS 对接已回滚"}, nil
}

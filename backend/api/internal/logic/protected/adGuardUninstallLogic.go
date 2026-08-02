package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardUninstallLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardUninstallLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardUninstallLogic {
	return &AdGuardUninstallLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardUninstall 彻底卸载 AdGuard Home（需 confirm=true）。
func (l *AdGuardUninstallLogic) AdGuardUninstall(req *types.AdGuardUninstallReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.Uninstall(l.ctx, req.Confirm); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "AdGuard Home 已卸载"}, nil
}

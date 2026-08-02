package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetComponentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetComponentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetComponentLogic {
	return &AdGuardSetComponentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetComponent 设置 AdGuard Home 产品化组件总开关。
func (l *AdGuardSetComponentLogic) AdGuardSetComponent(req *types.AdGuardComponentReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.SetComponentEnabled(l.ctx, req.Enabled); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	msg := "AdGuard Home 组件已关闭"
	if req.Enabled {
		msg = "AdGuard Home 组件已启用"
	}
	return &types.Result{Success: true, Message: msg}, nil
}

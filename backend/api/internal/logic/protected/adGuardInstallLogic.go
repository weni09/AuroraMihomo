package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardInstallLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardInstallLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardInstallLogic {
	return &AdGuardInstallLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardInstall 下载并安装最新 AdGuard Home 二进制。
func (l *AdGuardInstallLogic) AdGuardInstall() (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.Install(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "AdGuard Home 已安装"}, nil
}

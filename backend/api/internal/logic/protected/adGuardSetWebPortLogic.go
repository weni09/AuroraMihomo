package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetWebPortLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetWebPortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetWebPortLogic {
	return &AdGuardSetWebPortLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetWebPort 设置 AdGuard Web 管理端口（强制 127.0.0.1）；运行中会重启。
func (l *AdGuardSetWebPortLogic) AdGuardSetWebPort(req *types.AdGuardWebPortReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	if err := l.svcCtx.AdGuardService.SetWebPort(l.ctx, req.Port); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{
		Success: true,
		Message: fmt.Sprintf("Web 端口已设置为 127.0.0.1:%d", req.Port),
	}, nil
}

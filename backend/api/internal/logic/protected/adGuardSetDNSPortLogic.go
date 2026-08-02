package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetDNSPortLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetDNSPortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetDNSPortLogic {
	return &AdGuardSetDNSPortLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetDNSPort 设置 AdGuard DNS 监听端口（空闲或自身占用成功；其它进程占用失败）。
func (l *AdGuardSetDNSPortLogic) AdGuardSetDNSPort(req *types.AdGuardDNSPortReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	if err := l.svcCtx.AdGuardService.SetDNSListenPort(l.ctx, req.Port); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{
		Success: true,
		Message: fmt.Sprintf("AdGuard DNS 端口已设置为 %d", req.Port),
	}, nil
}

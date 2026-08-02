package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetDNSModeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetDNSModeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetDNSModeLogic {
	return &AdGuardSetDNSModeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetDNSMode 切换 AdGuard DNS 服务模式 0/1/2。
func (l *AdGuardSetDNSModeLogic) AdGuardSetDNSMode(req *types.AdGuardDNSModeReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	mode := service.DNSMode(req.Mode)
	if err := l.svcCtx.AdGuardService.SetDNSMode(l.ctx, mode); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	labels := map[int]string{
		0: "未托管",
		1: "使用 53 端口",
		2: "重定向 53→AdGuard",
	}
	label := labels[req.Mode]
	if label == "" {
		label = fmt.Sprintf("%d", req.Mode)
	}
	return &types.Result{
		Success: true,
		Message: fmt.Sprintf("DNS 服务模式已切换为 %s", label),
	}, nil
}

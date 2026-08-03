package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardApplyEntryDNSPresetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardApplyEntryDNSPresetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardApplyEntryDNSPresetLogic {
	return &AdGuardApplyEntryDNSPresetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardApplyEntryDNSPreset 一键写入 TUN/TProxy 通用入口 DNS 方案。
func (l *AdGuardApplyEntryDNSPresetLogic) AdGuardApplyEntryDNSPreset() (*types.Result, error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.ApplyEntryDNSPreset(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{
		Success: true,
		Message: "已应用入口 DNS 方案：" + service.EntryDNSPresetSummary(),
	}, nil
}

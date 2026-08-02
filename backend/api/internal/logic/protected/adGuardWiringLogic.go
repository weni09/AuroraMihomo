package protected

import (
	"context"
	"errors"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardWiringLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardWiringLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardWiringLogic {
	return &AdGuardWiringLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardWiring 预检 DNS 一键对接计划（不落库、不改配置）。
func (l *AdGuardWiringLogic) AdGuardWiring() (resp *types.AdGuardWiringResp, err error) {
	if l.svcCtx.AdGuardService == nil {
		return nil, errors.New("AdGuard 服务未初始化")
	}
	// 当前对接状态来自 Status；计划动作来自 WiringPreview
	wiring := "off"
	if st, stErr := l.svcCtx.AdGuardService.Status(l.ctx); stErr == nil && st != nil && st.Wiring != "" {
		wiring = st.Wiring
	}
	preview, err := l.svcCtx.AdGuardService.WiringPreview(l.ctx)
	if err != nil {
		// 预检失败仍尽量返回当前 wiring，便于前端展示「已对接/未对接」
		return &types.AdGuardWiringResp{
			Wiring:   wiring,
			Actions:  nil,
			Warnings: []string{err.Error()},
		}, nil
	}
	return &types.AdGuardWiringResp{
		Wiring:     wiring,
		Actions:    preview.Actions,
		Warnings:   preview.Warnings,
		AGHDNSPort: preview.AGHDNSPort,
	}, nil
}

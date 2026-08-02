package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardWiringApplyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardWiringApplyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardWiringApplyLogic {
	return &AdGuardWiringApplyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardWiringApply 按向导勾选项执行 DNS 一键对接。
func (l *AdGuardWiringApplyLogic) AdGuardWiringApply(req *types.AdGuardWiringApplyReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		req = &types.AdGuardWiringApplyReq{}
	}
	opts := service.WiringOptions{
		RedirectTProxy:  req.RedirectTProxy,
		ResolveConflict: req.ResolveConflict,
		PatchUpstream:   req.PatchUpstream,
		WeakenTUNHijack: req.WeakenTUNHijack,
	}
	if _, err := l.svcCtx.AdGuardService.WiringApply(l.ctx, opts); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "DNS 对接已应用"}, nil
}

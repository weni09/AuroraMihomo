package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardCheckUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardCheckUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardCheckUpdateLogic {
	return &AdGuardCheckUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardCheckUpdate 仅检查 AdGuard Home 版本，不附带 mihomo/zashboard。
func (l *AdGuardCheckUpdateLogic) AdGuardCheckUpdate() (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	c, err := l.svcCtx.AdGuardService.CheckUpdateOnly(l.ctx)
	if err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	msg := describeComponentCheck("AdGuard Home", c)
	if c.LocalVersion != "" && c.Error == "" && !c.UpdateNeeded {
		// describe 已含「已是最新」；附本地版本更清晰
		msg = fmt.Sprintf("AdGuard Home 已是最新（本地 %s）", c.LocalVersion)
	} else if c.UpdateNeeded && c.LocalVersion != "" {
		msg = fmt.Sprintf("AdGuard Home 有新版本可用（本地 %s → %s）", c.LocalVersion, c.LatestVersion)
	}
	success := c.Error == ""
	return &types.Result{Success: success, Message: msg}, nil
}

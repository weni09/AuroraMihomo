package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetAutoUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetAutoUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetAutoUpdateLogic {
	return &AdGuardSetAutoUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetAutoUpdate 保存 AdGuard 独立自动更新开关与 cron。
func (l *AdGuardSetAutoUpdateLogic) AdGuardSetAutoUpdate(req *types.AdGuardAutoUpdateReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	if req.Enabled == nil && strings.TrimSpace(req.Cron) == "" {
		return &types.Result{Success: false, Message: "请提供 enabled 或 cron"}, nil
	}
	if err := l.svcCtx.AdGuardService.SetAutoUpdateSettings(req.Enabled, req.Cron); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	on := l.svcCtx.AdGuardService.AutoUpdateEnabled()
	cron := l.svcCtx.AdGuardService.AutoUpdateCron()
	msg := fmt.Sprintf("AdGuard 自动更新已%s，cron=%s", map[bool]string{true: "开启", false: "关闭"}[on], cron)
	if !l.svcCtx.AdGuardService.ComponentEnabled() && on {
		msg += "（组件未启用，调度暂不执行）"
	}
	return &types.Result{Success: true, Message: msg}, nil
}

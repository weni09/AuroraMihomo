package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MihomoReloadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMihomoReloadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MihomoReloadLogic {
	return &MihomoReloadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MihomoReloadLogic) MihomoReload() (resp *types.Result, err error) {
	// 走 ConfigService.ReloadKernel 而非直接 Manager.Reload：
	// 前者优先用 external-controller 热重载，不会断开代理连接
	if err := l.svcCtx.ConfigService.ReloadKernel(l.ctx); err != nil {
		l.Errorf("reload failed: %v", err)
		_ = l.svcCtx.Database.MarkTaskRun("mihomo_reload", "error", err.Error(), time.Time{})
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	_ = l.svcCtx.Database.MarkTaskRun("mihomo_reload", "ok", "", time.Time{})
	return &types.Result{Success: true, Message: "配置已重载"}, nil
}

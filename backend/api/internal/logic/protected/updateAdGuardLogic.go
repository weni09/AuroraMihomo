package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateAdGuardLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateAdGuardLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAdGuardLogic {
	return &UpdateAdGuardLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UpdateAdGuard 更新 AdGuard Home 二进制；若原先在跑或期望运行则停机更新后拉起。
// 复用 Install：下载最新包、落盘并记录 version 设置。
func (l *UpdateAdGuardLogic) UpdateAdGuard() (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}

	wantRunning := l.svcCtx.AdGuardService.DesiredRunning()
	wasRunning := false
	if st, stErr := l.svcCtx.AdGuardService.Status(l.ctx); stErr == nil && st != nil {
		wasRunning = st.Running
	}
	if wasRunning {
		// 临时停机，勿清 enabled_at_boot
		if err := l.svcCtx.AdGuardService.StopProcess(l.ctx); err != nil {
			return &types.Result{Success: false, Message: "更新前停止失败: " + err.Error()}, nil
		}
	}

	if err := l.svcCtx.AdGuardService.Install(l.ctx); err != nil {
		if wantRunning || wasRunning {
			_ = l.svcCtx.AdGuardService.Start(l.ctx)
		}
		return &types.Result{Success: false, Message: err.Error()}, nil
	}

	if wantRunning || wasRunning {
		if err := l.svcCtx.AdGuardService.Start(l.ctx); err != nil {
			return &types.Result{Success: false, Message: "已更新，但重新启动失败: " + err.Error()}, nil
		}
	}
	return &types.Result{Success: true, Message: "AdGuard Home 已更新"}, nil
}

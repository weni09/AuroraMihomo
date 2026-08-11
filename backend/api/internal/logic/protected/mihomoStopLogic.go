package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MihomoStopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMihomoStopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MihomoStopLogic {
	return &MihomoStopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MihomoStopLogic) MihomoStop() (resp *types.Result, err error) {
	if err := l.svcCtx.MihomoManager.Stop(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	// 尊重手动停止：清掉期望运行，守护与面板重启都不再自动拉起，
	// 直到用户再次手动启动或重新开启守护。
	if l.svcCtx.MihomoGuard != nil {
		if err := l.svcCtx.MihomoGuard.SetDesiredRunning(false); err != nil {
			l.Errorf("记录内核停止期望失败: %v", err)
		}
	}
	return &types.Result{Success: true, Message: "内核已停止"}, nil
}

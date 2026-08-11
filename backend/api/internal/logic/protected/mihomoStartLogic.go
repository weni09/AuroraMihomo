package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MihomoStartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMihomoStartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MihomoStartLogic {
	return &MihomoStartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MihomoStartLogic) MihomoStart() (resp *types.Result, err error) {
	if err := l.svcCtx.MihomoManager.Start(l.ctx); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	// 手动启动 = 期望运行：守护恢复武装，同时清掉旧的失败尝试计数。
	if l.svcCtx.MihomoGuard != nil {
		if err := l.svcCtx.MihomoGuard.SetDesiredRunning(true); err != nil {
			l.Errorf("记录内核启动期望失败: %v", err)
		}
		l.svcCtx.MihomoGuard.ResetAttempts()
	}
	return &types.Result{Success: true, Message: "内核已启动"}, nil
}

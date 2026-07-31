package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SystemStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSystemStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatusLogic {
	return &SystemStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SystemStatusLogic) SystemStatus() (resp *types.Status, err error) {
	_, _ = l.svcCtx.MihomoManager.Version(l.ctx)
	st := l.svcCtx.MihomoManager.Status()
	state := "stopped"
	if st.IsRunning {
		state = "running"
	}
	return &types.Status{Status: state, Version: st.Version, Pid: st.PID}, nil
}

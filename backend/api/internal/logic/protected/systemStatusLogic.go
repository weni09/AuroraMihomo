package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/version"

	"github.com/zeromicro/go-zero/core/logx"
)

// SystemStatusLogic 处理系统状态获取逻辑
type SystemStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSystemStatusLogic 创建系统状态逻辑实例
func NewSystemStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatusLogic {
	return &SystemStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SystemStatus 返回当前系统及 Mihomo 服务状态
func (l *SystemStatusLogic) SystemStatus() (resp *types.Status, err error) {
	_, _ = l.svcCtx.MihomoManager.Version(l.ctx)
	st := l.svcCtx.MihomoManager.Status()
	state := "stopped"
	if st.IsRunning {
		state = "running"
	}
	return &types.Status{
		Status:     state,
		Version:    st.Version,
		AppVersion: version.Get(),
		Pid:        st.PID,
	}, nil
}

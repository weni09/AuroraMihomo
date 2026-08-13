package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// SystemStatsLogic 处理宿主服务器资源快照获取逻辑
type SystemStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSystemStatsLogic 创建系统资源统计逻辑实例
func NewSystemStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatsLogic {
	return &SystemStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SystemStats 返回宿主 CPU/内存/网络/磁盘/运行时长快照。
//
// 网络速率是「自上次查询以来的平均速率」，由 MonitorService 内部
// 保留的采样基线差分得出；首次查询（面板刚启动）没有基线，速率为 0，
// 前端按 0 展示即可，下一次轮询即恢复正常。
func (l *SystemStatsLogic) SystemStats() (resp *types.SystemStats, err error) {
	st, err := l.svcCtx.MonitorService.Stats(l.ctx)
	if err != nil {
		return nil, err
	}
	vols := make([]types.DiskVolume, 0, len(st.DiskVolumes))
	for _, v := range st.DiskVolumes {
		vols = append(vols, types.DiskVolume{
			Path:    v.Path,
			Total:   v.Total,
			Used:    v.Used,
			Percent: v.Percent,
			Fstype:  v.Fstype,
		})
	}
	return &types.SystemStats{
		CPUPercent:    st.CPUPercent,
		MemTotal:      st.MemTotal,
		MemUsed:       st.MemUsed,
		MemPercent:    st.MemPercent,
		NetUpRate:     st.NetUpRate,
		NetDownRate:   st.NetDownRate,
		NetUpTotal:    st.NetUpTotal,
		NetDownTotal:  st.NetDownTotal,
		DiskTotal:     st.DiskTotal,
		DiskUsed:      st.DiskUsed,
		DiskPercent:   st.DiskPercent,
		DiskPath:      st.DiskPath,
		DiskVolumes:   vols,
		UptimeSeconds: st.UptimeSeconds,
	}, nil
}

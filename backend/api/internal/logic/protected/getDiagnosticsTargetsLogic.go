package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/diagnostics"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetDiagnosticsTargetsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetDiagnosticsTargetsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDiagnosticsTargetsLogic {
	return &GetDiagnosticsTargetsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// GetDiagnosticsTargets 返回预设诊断目标清单（含代理端口 TCP 探测目标）。
//
// 走 DefaultTargets 而非前端硬编码：代理目标依赖当前生效的本地代理地址
// （ProxyURLFunc），前端无法自行推导，必须由后端下发。
func (l *GetDiagnosticsTargetsLogic) GetDiagnosticsTargets() (resp *types.DiagnosticsTargetsResp, err error) {
	targets := diagnostics.DefaultTargets(l.svcCtx.Diag.ProxyURLFunc())
	respTargets := make([]types.DiagnosticTarget, 0, len(targets))
	for _, t := range targets {
		respTargets = append(respTargets, types.DiagnosticTarget{
			Type:   t.Type,
			Target: t.Target,
			Port:   t.Port,
		})
	}
	return &types.DiagnosticsTargetsResp{Targets: respTargets}, nil
}

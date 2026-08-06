package protected

import (
	"context"
	"errors"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardStatusLogic {
	return &AdGuardStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardStatus 返回 AdGuard Home 安装/运行与 DNS 对接状态。
func (l *AdGuardStatusLogic) AdGuardStatus() (resp *types.AdGuardStatusResp, err error) {
	if l.svcCtx.AdGuardService == nil {
		return nil, errors.New("AdGuard 服务未初始化")
	}
	dto, err := l.svcCtx.AdGuardService.Status(l.ctx)
	if err != nil {
		return nil, err
	}
	entryPath := dto.EntryPath
	if entryPath == "" {
		entryPath = "/adguard-ui/"
	}
	return &types.AdGuardStatusResp{
		Installed:        dto.Installed,
		Running:          dto.Running,
		PID:              dto.PID,
		Version:          dto.Version,
		WorkDir:          dto.WorkDir,
		WebAddr:          dto.WebAddr,
		DNSPort:          dto.DNSPort,
		Wiring:           dto.Wiring,
		WiringLabel:      dto.WiringLabel,
		LastError:        dto.LastError,
		EntryPath:        entryPath,
		ComponentEnabled: dto.ComponentEnabled,
		DnsMode:          dto.DnsMode,
		CdnProviders:     dto.CdnProviders,
		AutoUpdate:       dto.AutoUpdate,
		AutoUpdateCron:   dto.AutoUpdateCron,
		Username:         dto.Username,
		DesiredRunning:   dto.DesiredRunning,
		ManagedBy:        dto.ManagedBy,
	}, nil
}

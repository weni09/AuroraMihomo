package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type PutUpdateSettingsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutUpdateSettingsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutUpdateSettingsLogic {
	return &PutUpdateSettingsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PutUpdateSettingsLogic) PutUpdateSettings(req *types.UpdateSettingsReq) (resp *types.UpdateSettings, err error) {
	st, err := l.svcCtx.SettingsService.Update(service.UpdateSettingsInput{
		AutoUpdateEnabled:  req.AutoUpdateEnabled,
		AutoUpdateCron:     req.AutoUpdateCron,
		CDNProviders:       req.CDNProviders,
		RawCDNProviders:    req.RawCDNProviders,
		UseMihomoProxy:     req.UseMihomoProxy,
		SelfRepo:           req.SelfRepo,
		LogRetentionDays:   req.LogRetentionDays,
		LogCleanupCron:     req.LogCleanupCron,
		LogCleanupEnabled:  req.LogCleanupEnabled,
		MonitorEnabled:     req.MonitorEnabled,
		MonitorIntervalSec: req.MonitorIntervalSec,
	})
	if err != nil {
		return nil, err
	}
	return toUpdateSettings(l.svcCtx, st), nil
}

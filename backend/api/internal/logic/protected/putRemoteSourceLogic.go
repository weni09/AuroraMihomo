package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type PutRemoteSourceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutRemoteSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutRemoteSourceLogic {
	return &PutRemoteSourceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PutRemoteSourceLogic) PutRemoteSource(req *types.RemoteSourceReq) (resp *types.RemoteSourceResp, err error) {
	src, err := l.svcCtx.SettingsService.SetRemoteSource(service.RemoteSourceInput{
		Type:        req.SourceType,
		ID:          req.SourceId,
		URL:         req.SourceUrl,
		Cron:        req.Cron,
		CronEnabled: req.CronEnabled,
	})
	if err != nil {
		return nil, err
	}
	return toRemoteSourceResp(src, remoteSourceOptions(l.svcCtx)), nil
}

package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUpdateSettingsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUpdateSettingsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUpdateSettingsLogic {
	return &GetUpdateSettingsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUpdateSettingsLogic) GetUpdateSettings() (resp *types.UpdateSettings, err error) {
	return toUpdateSettings(l.svcCtx, l.svcCtx.SettingsService.Get()), nil
}

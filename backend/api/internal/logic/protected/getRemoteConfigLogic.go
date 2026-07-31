package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetRemoteConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRemoteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRemoteConfigLogic {
	return &GetRemoteConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRemoteConfigLogic) GetRemoteConfig() (resp *types.ConfigContent, err error) {
	content, err := l.svcCtx.ConfigService.GetRemoteConfig()
	if err != nil {
		return nil, err
	}
	return &types.ConfigContent{Content: content}, nil
}

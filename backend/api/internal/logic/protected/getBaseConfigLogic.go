package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBaseConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetBaseConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBaseConfigLogic {
	return &GetBaseConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBaseConfigLogic) GetBaseConfig() (resp *types.ConfigContent, err error) {
	content, err := l.svcCtx.ConfigService.GetBaseConfig()
	if err != nil {
		return nil, err
	}
	return &types.ConfigContent{Content: content}, nil
}

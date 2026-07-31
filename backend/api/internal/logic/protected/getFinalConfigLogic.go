package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFinalConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetFinalConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFinalConfigLogic {
	return &GetFinalConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetFinalConfigLogic) GetFinalConfig() (resp *types.ConfigContent, err error) {
	content, err := l.svcCtx.ConfigService.GetFinalConfig()
	if err != nil {
		return nil, err
	}
	return &types.ConfigContent{Content: content}, nil
}

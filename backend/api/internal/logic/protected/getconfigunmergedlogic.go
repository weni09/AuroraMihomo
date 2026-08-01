package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetConfigUnmergedLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetConfigUnmergedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConfigUnmergedLogic {
	return &GetConfigUnmergedLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetConfigUnmergedLogic) GetConfigUnmerged() (resp *types.ConfigUnmergedStatus, err error) {
	unmerged, err := l.svcCtx.ConfigService.BaseUnmerged()
	if err != nil {
		return nil, err
	}
	return &types.ConfigUnmergedStatus{Unmerged: unmerged}, nil
}

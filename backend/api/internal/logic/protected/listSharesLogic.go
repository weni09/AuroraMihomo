package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListSharesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListSharesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSharesLogic {
	return &ListSharesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListSharesLogic) ListShares() (resp *types.ShareListResp, err error) {
	return listShares(l.svcCtx)
}

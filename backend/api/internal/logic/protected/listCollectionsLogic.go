package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListCollectionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListCollectionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListCollectionsLogic {
	return &ListCollectionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListCollectionsLogic) ListCollections() (resp []types.Collection, err error) {
	rows, err := l.svcCtx.Database.ListCollections()
	if err != nil {
		return nil, err
	}
	resp = make([]types.Collection, 0, len(rows))
	for _, r := range rows {
		resp = append(resp, toCollectionType(l.svcCtx, r))
	}
	return resp, nil
}

package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListConfigVersionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListConfigVersionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListConfigVersionsLogic {
	return &ListConfigVersionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListConfigVersionsLogic) ListConfigVersions() (resp []types.ConfigVersionItem, err error) {
	rows, err := l.svcCtx.Database.ListConfigVersions(50)
	if err != nil {
		return nil, err
	}
	resp = make([]types.ConfigVersionItem, 0, len(rows))
	for _, r := range rows {
		resp = append(resp, types.ConfigVersionItem{
			Id: r.ID, Hash: r.Hash, Note: r.Note,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	return resp, nil
}

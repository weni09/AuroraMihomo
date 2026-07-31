package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetConflictsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetConflictsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConflictsLogic {
	return &GetConflictsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetConflictsLogic) GetConflicts() (resp []types.ConflictItem, err error) {
	rows, err := l.svcCtx.Database.ListConflicts(false)
	if err != nil {
		return nil, err
	}
	resp = make([]types.ConflictItem, 0, len(rows))
	for _, r := range rows {
		resp = append(resp, types.ConflictItem{
			Id: r.ID, Key: r.Key, Type: r.Type, Path: r.Path,
			LocalValue: r.LocalValue, RemoteValue: r.RemoteValue,
			ManualValue: r.ManualValue, Resolution: r.Resolution,
			Resolved: r.Resolved == 1,
		})
	}
	return resp, nil
}

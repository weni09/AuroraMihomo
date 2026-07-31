package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListFilesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListFilesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListFilesLogic {
	return &ListFilesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListFilesLogic) ListFiles() (resp []types.SubFile, err error) {
	rows, err := l.svcCtx.Database.ListFiles()
	if err != nil {
		return nil, err
	}
	resp = make([]types.SubFile, 0, len(rows))
	for _, r := range rows {
		resp = append(resp, toFileType(r))
	}
	return resp, nil
}

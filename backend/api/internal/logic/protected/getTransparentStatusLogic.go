package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTransparentStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTransparentStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTransparentStatusLogic {
	return &GetTransparentStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTransparentStatusLogic) GetTransparentStatus() (resp *types.TransparentStatusResp, err error) {
	st, env := l.svcCtx.TransparentService.Status()
	return toTransparentStatus(st, env), nil
}

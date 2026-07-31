package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateShareLogic {
	return &UpdateShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateShareLogic) UpdateShare(req *types.ShareUpdateReq) (resp *types.ShareListResp, err error) {
	return updateShare(l.svcCtx, req)
}

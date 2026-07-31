package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RevokeShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRevokeShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevokeShareLogic {
	return &RevokeShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RevokeShareLogic) RevokeShare(req *types.ShareActionReq) (resp *types.ShareListResp, err error) {
	return revokeShare(l.svcCtx, req)
}

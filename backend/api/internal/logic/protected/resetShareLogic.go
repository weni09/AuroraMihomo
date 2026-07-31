package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResetShareLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResetShareLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetShareLogic {
	return &ResetShareLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetShareLogic) ResetShare(req *types.ShareActionReq) (resp *types.ShareListResp, err error) {
	return resetShare(l.svcCtx, req)
}

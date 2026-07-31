package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteSubscriptionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteSubscriptionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteSubscriptionLogic {
	return &DeleteSubscriptionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteSubscriptionLogic) DeleteSubscription(req *types.IdPathReq) (resp *types.Result, err error) {
	if err := l.svcCtx.Database.DeleteSubscription(req.Id); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "订阅已删除"}, nil
}

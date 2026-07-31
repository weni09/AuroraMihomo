package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListSubscriptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListSubscriptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSubscriptionsLogic {
	return &ListSubscriptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListSubscriptionsLogic) ListSubscriptions() (resp []types.Subscription, err error) {
	subs, err := l.svcCtx.Database.GetSubscriptions()
	if err != nil {
		return nil, err
	}
	resp = make([]types.Subscription, 0, len(subs))
	for _, s := range subs {
		resp = append(resp, toSubscriptionType(s))
	}
	return resp, nil
}

package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RefreshAllSubscriptionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRefreshAllSubscriptionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefreshAllSubscriptionsLogic {
	return &RefreshAllSubscriptionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RefreshAllSubscriptionsLogic) RefreshAllSubscriptions() (resp *types.RefreshAllResult, err error) {
	res, err := l.svcCtx.ConfigService.RefreshAllSubscriptionCaches(l.ctx)
	if err != nil {
		// 只有读取订阅列表等顶层错误才走到这里（HTTP 非 200，由拦截器 toast）；
		// 单条订阅的失败已并入结果，不在此处报错
		l.Errorf("refresh all subscription caches failed: %v", err)
		return nil, err
	}
	return &types.RefreshAllResult{
		Total:       res.Total,
		Success:     res.Success,
		Failed:      res.Failed,
		FailedNames: res.FailedNames,
	}, nil
}

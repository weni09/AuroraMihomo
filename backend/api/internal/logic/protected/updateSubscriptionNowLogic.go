package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateSubscriptionNowLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSubscriptionNowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscriptionNowLogic {
	return &UpdateSubscriptionNowLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UpdateSubscriptionNow 对应「单个订阅」页的「刷新缓存」。
//
// 只刷新该订阅自身的节点缓存，不触碰配置中心：订阅缓存服务于 substore 的
// 分享/预览链路，与最终配置的生成是两件事。此前这里走的是完整合并流程，
// 导致远程来源失效时刷新必然失败，且远程来源指向别的订阅时本次刷新会被
// 静默跳过（详见 ConfigService.RefreshSubscriptionCache 的注释）。
func (l *UpdateSubscriptionNowLogic) UpdateSubscriptionNow(req *types.IdPathReq) (resp *types.Result, err error) {
	if err := l.svcCtx.ConfigService.RefreshSubscriptionCache(l.ctx, req.Id); err != nil {
		l.Errorf("refresh subscription cache failed: %v", err)
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "订阅缓存已刷新"}, nil
}

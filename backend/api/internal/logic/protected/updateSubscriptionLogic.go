package protected

import (
	"context"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateSubscriptionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSubscriptionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscriptionLogic {
	return &UpdateSubscriptionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscriptionLogic) UpdateSubscription(req *types.UpdateSubscriptionReq) (resp *types.Subscription, err error) {
	sub, err := l.svcCtx.Database.GetSubscription(req.Id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) != "" {
		sub.Name = strings.TrimSpace(req.Name)
	}
	oldURL, oldContent, oldOperators := sub.URL, sub.Content, sub.Operators
	// 传空串表示清空订阅地址（切换为手动粘贴节点）
	if req.Url != nil {
		sub.URL = strings.TrimSpace(*req.Url)
	}
	if req.Enabled != nil {
		if *req.Enabled {
			sub.Enabled = 1
		} else {
			sub.Enabled = 0
		}
	}
	if req.Interval > 0 {
		sub.Interval = req.Interval
	}
	// 传空串表示清除自定义 UA，回落到默认值
	if req.UserAgent != nil {
		sub.UserAgent = strings.TrimSpace(*req.UserAgent)
	}
	if req.Content != nil {
		sub.Content = strings.TrimSpace(*req.Content)
	}
	if req.Operators != nil {
		sub.Operators = encodeOperators(req.Operators)
	}
	// 影响节点解析结果的字段一旦变化，必须失效已缓存的节点。
	// 否则分享链接与后续合并会继续用旧节点，用户改了配置却看不到效果。
	if sub.URL != oldURL || sub.Content != oldContent || sub.Operators != oldOperators {
		sub.CachedNodes = ""
	}
	if strings.TrimSpace(sub.URL) == "" && strings.TrimSpace(sub.Content) == "" {
		return nil, errInvalid("订阅地址与节点内容不能同时为空")
	}
	// 历史数据可能没有分享令牌，补发一个
	if sub.ShareToken == "" {
		token, tErr := randomToken(8)
		if tErr != nil {
			return nil, tErr
		}
		sub.ShareToken = token
	}
	sub.UpdatedAt = nowTime()
	if err := l.svcCtx.Database.UpdateSubscription(sub); err != nil {
		return nil, err
	}
	out := toSubscriptionType(*sub)
	return &out, nil
}

package protected

import (
	"context"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateSubscriptionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateSubscriptionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSubscriptionLogic {
	return &CreateSubscriptionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateSubscriptionLogic) CreateSubscription(req *types.CreateSubscriptionReq) (resp *types.Subscription, err error) {
	name := strings.TrimSpace(req.Name)
	url := strings.TrimSpace(req.Url)
	content := strings.TrimSpace(req.Content)
	if name == "" {
		return nil, errInvalid("订阅名称不能为空")
	}
	// 远程订阅填 URL，手动粘贴节点填 Content，二者至少有一个
	if url == "" && content == "" {
		return nil, errInvalid("请填写订阅地址，或直接粘贴节点内容")
	}
	interval := req.Interval
	if interval <= 0 {
		interval = 3600
	}
	enabled := 0
	if req.Enabled {
		enabled = 1
	}
	token, err := randomToken(shareTokenBytes)
	if err != nil {
		return nil, err
	}
	sub := &model.Subscription{
		Name: name, URL: url, Content: content, Type: "mihomo", Enabled: enabled,
		Interval: interval, Status: "pending",
		UserAgent:  strings.TrimSpace(req.UserAgent),
		Operators:  encodeOperators(req.Operators),
		ShareToken: token,
		CreatedAt:  nowTime(), UpdatedAt: nowTime(),
	}
	if err := l.svcCtx.Database.CreateSubscription(sub); err != nil {
		return nil, err
	}
	out := toSubscriptionType(*sub)
	return &out, nil
}

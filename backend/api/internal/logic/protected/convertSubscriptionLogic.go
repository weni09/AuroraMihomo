package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/substore"

	"github.com/zeromicro/go-zero/core/logx"
)

type ConvertSubscriptionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewConvertSubscriptionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConvertSubscriptionLogic {
	return &ConvertSubscriptionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ConvertSubscriptionLogic) ConvertSubscription(req *types.ConvertReq) (resp *types.ConvertResp, err error) {
	res, err := l.svcCtx.ConfigService.SubStoreEngine().Convert(l.ctx, substore.ConvertRequest{
		URL:     req.Url,
		Content: req.Content,
	}, loadRewriteRules(l.svcCtx), nil, resolveTarget(req.Template, "mihomo-yaml"), "")
	if err != nil {
		return nil, err
	}
	return &types.ConvertResp{
		Format: res.Format, Yaml: res.YAML, Links: res.Links, Count: len(res.Nodes),
	}, nil
}

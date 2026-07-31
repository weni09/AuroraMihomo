package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type BuildCollectionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBuildCollectionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BuildCollectionLogic {
	return &BuildCollectionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BuildCollectionLogic) BuildCollection(req *types.IdPathReq) (resp *types.ConvertResp, err error) {
	c, err := l.svcCtx.Database.GetCollection(req.Id)
	if err != nil {
		return nil, err
	}
	reqs, err := collectRequests(l.svcCtx, c.ID)
	if err != nil {
		return nil, err
	}
	ops := buildOperators(c.Operators)
	res, err := l.svcCtx.ConfigService.SubStoreEngine().ConvertMany(
		l.ctx, reqs, loadRewriteRules(l.svcCtx), ops,
		resolveTarget("", "mihomo-yaml"), "")
	if err != nil {
		return nil, err
	}
	return &types.ConvertResp{
		Format: res.Format, Yaml: res.YAML, Links: res.Links, Count: len(res.Nodes),
	}, nil
}

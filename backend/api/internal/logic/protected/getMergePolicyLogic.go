package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMergePolicyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetMergePolicyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMergePolicyLogic {
	return &GetMergePolicyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *GetMergePolicyLogic) GetMergePolicy() (resp *types.MergePolicyResp, err error) {
	p := l.svcCtx.SettingsService.GetMergePolicy()
	return &types.MergePolicyResp{
		ProxyPriority:   p.ProxyPriority,
		RulePriority:    p.RulePriority,
		DNSPriority:     p.DNSPriority,
		TUNPriority:     p.TUNPriority,
		GeneralPriority: p.GeneralPriority,
	}, nil
}

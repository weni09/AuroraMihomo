package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type PutMergePolicyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutMergePolicyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutMergePolicyLogic {
	return &PutMergePolicyLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *PutMergePolicyLogic) PutMergePolicy(req *types.MergePolicyReq) (resp *types.MergePolicyResp, err error) {
	p, err := l.svcCtx.SettingsService.SetMergePolicy(req.ProxyPriority, req.RulePriority, req.DNSPriority, req.TUNPriority, req.GeneralPriority)
	if err != nil {
		return nil, err
	}
	return &types.MergePolicyResp{
		ProxyPriority:   p.ProxyPriority,
		RulePriority:    p.RulePriority,
		DNSPriority:     p.DNSPriority,
		TUNPriority:     p.TUNPriority,
		GeneralPriority: p.GeneralPriority,
	}, nil
}

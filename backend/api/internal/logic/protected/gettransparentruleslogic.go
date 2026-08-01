package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTransparentRulesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTransparentRulesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTransparentRulesLogic {
	return &GetTransparentRulesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTransparentRulesLogic) GetTransparentRules() (resp *types.TransparentRulesResp, err error) {
	// l.Info("面板操作：查询透明代理防火墙规则")
	builtin, policyRoutes, err := l.svcCtx.TransparentService.BuiltinRules()
	if err != nil {
		l.Errorf("面板操作：生成内置规则失败: %v", err)
		return nil, err
	}
	active, err := l.svcCtx.TransparentService.ActiveRules(l.ctx)
	if err != nil {
		l.Errorf("面板操作：读取实际生效规则失败: %v", err)
		return nil, err
	}
	custom := l.svcCtx.TransparentService.GetCustomRules()
	// l.Infof("面板操作：查询透明代理规则完成 customBytes=%d activeBytes=%d backend=%s",
	// 	len(custom), len(active), l.svcCtx.TransparentService.IPTablesBackend())
	return &types.TransparentRulesResp{
		CustomRules:     custom,
		IptablesBackend: l.svcCtx.TransparentService.IPTablesBackend(),
		BuiltinNFTRules: builtin,
		PolicyRoutes:    policyRoutes,
		ActiveRules:     active,
	}, nil
}

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
	// 规则查询是设置页的展示数据，不是控制面关键路径。
	// 未启用 / 不支持透明代理、nft 不可用、表不存在时，内置或实际规则
	// 读不到就留空返回——把错误抛成 HTTP 500 只会在打开设置时弹无意义失败。
	builtin, policyRoutes, berr := l.svcCtx.TransparentService.BuiltinRules()
	if berr != nil {
		l.Infof("面板操作：生成内置规则失败（展示为空）: %v", berr)
		builtin, policyRoutes = "", nil
	}
	active, aerr := l.svcCtx.TransparentService.ActiveRules(l.ctx)
	if aerr != nil {
		l.Infof("面板操作：读取实际生效规则失败（展示为空）: %v", aerr)
		active = ""
	}
	custom := l.svcCtx.TransparentService.GetCustomRules()
	exemptPorts := l.svcCtx.TransparentService.GetExemptPorts()
	return &types.TransparentRulesResp{
		CustomRules:     custom,
		ExemptPorts:     exemptPorts,
		IptablesBackend: l.svcCtx.TransparentService.IPTablesBackend(),
		BuiltinNFTRules: builtin,
		PolicyRoutes:    policyRoutes,
		ActiveRules:     active,
	}, nil
}

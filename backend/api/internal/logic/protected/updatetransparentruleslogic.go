package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateTransparentRulesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateTransparentRulesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateTransparentRulesLogic {
	return &UpdateTransparentRulesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateTransparentRulesLogic) UpdateTransparentRules(req *types.TransparentRulesReq) (resp *types.Result, err error) {
	l.Infof("面板操作：保存自定义防火墙规则与免代理端口 customBytes=%d exempt=%q",
		len(req.CustomRules), req.ExemptPorts)
	// 一次提交、一次 Resync：避免先规则后端口半成功，也避免成功路径 Apply 两次。
	if err := l.svcCtx.TransparentService.SaveTransparentRules(l.ctx, req.CustomRules, req.ExemptPorts); err != nil {
		l.Errorf("面板操作：保存透明代理规则扩展失败: %v", err)
		return nil, err
	}
	// 文案必须区分三种状态，不能在 pending 时谎报"已应用到宿主"：
	//   - 待确认窗口：Resync 故意跳过，库已写、宿主仍是启用时规则；
	//   - 已托管且已确认：Resync 已尝试下发（失败则上面已 return error）；
	//   - 未托管：仅落库。
	msg := "已保存"
	st, _ := l.svcCtx.TransparentService.Status()
	switch {
	case l.svcCtx.TransparentService.RulesHostApplyPending():
		msg += "（待确认网络期间仅写入数据库，确认后会同步到宿主）"
	case st != nil && st.Enabled && st.Mode == "tproxy" && l.svcCtx.TransparentService.TProxyManaged():
		msg += "，已立即重新应用到宿主"
	}
	l.Infof("面板操作：保存透明代理规则扩展成功 %s", msg)
	return &types.Result{Message: msg}, nil
}

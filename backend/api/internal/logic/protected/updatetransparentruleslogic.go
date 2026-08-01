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
	l.Infof("面板操作：保存自定义防火墙规则 bytes=%d", len(req.CustomRules))
	if err := l.svcCtx.TransparentService.SaveCustomRules(l.ctx, req.CustomRules); err != nil {
		l.Errorf("面板操作：保存自定义防火墙规则失败: %v", err)
		return nil, err
	}
	// 文案不写死"已立即重新应用"：未托管/未开启 TProxy 时 Resync 是空操作；
	// 重应用失败时 SaveCustomRules 已返回 error，不会走到这里。
	msg := "自定义防火墙规则已保存"
	st, _ := l.svcCtx.TransparentService.Status()
	if st != nil && st.Enabled && st.Mode == "tproxy" && l.svcCtx.TransparentService.TProxyManaged() {
		msg += "，已立即重新应用到宿主"
	}
	l.Infof("面板操作：保存自定义防火墙规则成功 %s", msg)
	return &types.Result{Message: msg}, nil
}

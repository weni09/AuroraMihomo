package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ConfirmTransparentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewConfirmTransparentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConfirmTransparentLogic {
	return &ConfirmTransparentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ConfirmTransparentLogic) ConfirmTransparent() (resp *types.TransparentStatusResp, err error) {
	// 确认即取消自动回滚。能走到这里说明请求真的到达了面板，
	// 也就间接证明网络仍通——这正是确认机制要验证的事。
	if err := l.svcCtx.TransparentService.Confirm(l.ctx); err != nil {
		return nil, err
	}
	st, env := l.svcCtx.TransparentService.Status()
	return toTransparentStatus(st, env), nil
}

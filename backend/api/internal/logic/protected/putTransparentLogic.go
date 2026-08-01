package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type PutTransparentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPutTransparentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PutTransparentLogic {
	return &PutTransparentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PutTransparentLogic) PutTransparent(req *types.TransparentUpdateReq) (resp *types.TransparentStatusResp, err error) {
	// 环境不具备条件时 Update 会带着"缺什么、怎么补"的原因失败，
	// 直接把错误透给前端展示，不在这里改写成泛泛的提示
	l.Infof("面板操作：透明代理更新请求 enabled=%v mode=%s tproxyPort=%d tunStack=%q",
		req.Enabled, req.Mode, req.TProxyPort, req.TUNStack)
	if err := l.svcCtx.TransparentService.Update(l.ctx, req.Enabled, req.Mode,
		req.TProxyPort, req.TUNStack); err != nil {
		l.Errorf("面板操作：透明代理更新失败: %v", err)
		return nil, err
	}
	st, env := l.svcCtx.TransparentService.Status()
	l.Infof("面板操作：透明代理更新成功 enabled=%v mode=%s pendingConfirm=%v",
		st.Enabled, st.Mode, st.PendingConfirm)
	return toTransparentStatus(st, env), nil
}

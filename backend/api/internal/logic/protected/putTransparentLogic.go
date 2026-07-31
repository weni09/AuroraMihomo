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
	if err := l.svcCtx.TransparentService.Update(l.ctx, req.Enabled, req.Mode,
		req.TProxyPort, req.TUNStack); err != nil {
		return nil, err
	}
	st, env := l.svcCtx.TransparentService.Status()
	return toTransparentStatus(st, env), nil
}

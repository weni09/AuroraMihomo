package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTransparentStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetTransparentStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTransparentStatusLogic {
	return &GetTransparentStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTransparentStatusLogic) GetTransparentStatus() (resp *types.TransparentStatusResp, err error) {
	// 状态接口会被倒计时/页面刷新频繁轮询，不在这里打业务日志，
	// 避免把真正的开启/关闭记录淹没。写操作日志在 Update/Confirm 等路径。
	st, env := l.svcCtx.TransparentService.Status()
	return toTransparentStatus(st, env), nil
}

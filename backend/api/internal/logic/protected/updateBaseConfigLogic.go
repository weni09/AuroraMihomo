package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateBaseConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateBaseConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBaseConfigLogic {
	return &UpdateBaseConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateBaseConfigLogic) UpdateBaseConfig(req *types.UpdateBaseConfigReq) (resp *types.Result, err error) {
	if err := l.svcCtx.ConfigService.UpdateBaseConfig(req.Content); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	return &types.Result{Success: true, Message: "基础配置已保存"}, nil
}

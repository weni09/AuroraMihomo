package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetBootLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetBootLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetBootLogic {
	return &AdGuardSetBootLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetBoot 设置开机自启。
// 服务模式下驱动 systemctl enable/disable（系统真实状态）；
// exec 模式写 settings（面板重启后据此拉起）。不启停进程。
func (l *AdGuardSetBootLogic) AdGuardSetBoot(req *types.AdGuardBootReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if err := l.svcCtx.AdGuardService.SetBootEnabled(l.ctx, req.Enabled); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	msg := "开机自启已关闭"
	if req.Enabled {
		msg = "已开启开机自启"
	}
	return &types.Result{Success: true, Message: msg}, nil
}

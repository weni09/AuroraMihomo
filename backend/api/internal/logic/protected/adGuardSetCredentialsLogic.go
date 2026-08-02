package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetCredentialsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetCredentialsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetCredentialsLogic {
	return &AdGuardSetCredentialsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetCredentials 设置 AGH 管理员用户名/密码，可选同步开关。
// 密码只写入 AdGuardHome.yaml（bcrypt），不落库明文。
func (l *AdGuardSetCredentialsLogic) AdGuardSetCredentials(req *types.AdGuardCredentialsReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	if strings.TrimSpace(req.Password) == "" {
		return &types.Result{Success: false, Message: "密码不能为空"}, nil
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "admin"
	}

	if err := l.svcCtx.AdGuardService.SetCredentials(l.ctx, username, req.Password); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}

	msg := fmt.Sprintf("AdGuard 管理员账号已更新（用户 %s）", username)
	if req.SyncWithAurora != nil {
		if err := l.svcCtx.AdGuardService.SetPasswordSync(l.ctx, *req.SyncWithAurora); err != nil {
			return &types.Result{Success: false, Message: "账号已写入，但同步开关保存失败: " + err.Error()}, nil
		}
		if *req.SyncWithAurora {
			msg += "；已开启与 Aurora 密码同步"
		} else {
			msg += "；已关闭与 Aurora 密码同步"
		}
	}
	return &types.Result{Success: true, Message: msg}, nil
}

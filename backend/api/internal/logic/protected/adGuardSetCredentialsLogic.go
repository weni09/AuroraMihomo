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

// AdGuardSetCredentials 设置 AGH 管理员用户名/密码。
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

	// 供免密永久接管：加密落库 + 换 agh_session
	if bridge := l.svcCtx.AdGuardSSO; bridge != nil {
		bridge.SetUsername(username)
		if pErr := bridge.PersistCredentials(username, req.Password); pErr != nil {
			l.Errorf("持久化 AGH 凭据失败: %v", pErr)
			// 账号已写入 yaml，凭据持久化失败仍告知用户
			return &types.Result{
				Success: true,
				Message: fmt.Sprintf("AdGuard 管理员账号已更新（用户 %s），但免密凭据落库失败: %v", username, pErr),
			}, nil
		}
		const userKey = "1"
		if l.svcCtx.AdGuardManager != nil && l.svcCtx.AdGuardManager.Status().Running {
			if eErr := bridge.Establish(l.ctx, userKey, username, req.Password); eErr != nil {
				l.Infof("AdGuard 免密会话建立失败: %v", eErr)
			}
		}
	}

	msg := fmt.Sprintf("AdGuard 管理员账号已更新（用户 %s），免密接管已持久化", username)
	return &types.Result{Success: true, Message: msg}, nil
}

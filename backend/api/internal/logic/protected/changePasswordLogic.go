package protected

import (
	"context"
	"errors"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/auth"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChangePasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChangePasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChangePasswordLogic {
	return &ChangePasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// minPasswordLen 新口令的最小长度。管理面板通常直接暴露在内网甚至公网，
// 过短的口令会让登录限流成为唯一防线。
const minPasswordLen = 8

// ChangePassword 校验旧口令后更新管理员口令。
//
// 该接口位于受保护路由下（需携带有效 JWT），但仍必须校验旧口令：
// 令牌可能来自被盗用的浏览器会话，仅凭令牌就允许改密等于把会话劫持
// 直接升级为账户接管。
func (l *ChangePasswordLogic) ChangePassword(req *types.ChangePasswordReq) (*types.Result, error) {
	oldPwd := req.OldPassword
	newPwd := req.NewPassword

	if strings.TrimSpace(newPwd) == "" {
		return nil, errors.New("新密码不能为空")
	}
	// 按字符数而非字节数校验，避免中文口令被误判为过短
	if len([]rune(newPwd)) < minPasswordLen {
		return nil, errors.New("新密码长度不得少于 8 位")
	}
	if oldPwd == newPwd {
		return nil, errors.New("新密码不能与当前密码相同")
	}

	stored, err := l.svcCtx.Database.GetSetting("admin_password")
	if err != nil || strings.TrimSpace(stored) == "" {
		l.Errorf("读取管理员密码失败: %v", err)
		return nil, errors.New("服务端密码未初始化")
	}

	if ok, _ := auth.VerifyPassword(stored, oldPwd); !ok {
		l.Infof("修改密码失败：旧密码不匹配")
		return nil, errors.New("当前密码错误")
	}

	hashed, err := auth.HashPassword(newPwd)
	if err != nil {
		return nil, err
	}
	if err := l.svcCtx.Database.SetSetting("admin_password", hashed); err != nil {
		return nil, err
	}

	l.Info("管理员密码已更新")
	return &types.Result{Success: true, Message: "密码已更新，请使用新密码重新登录"}, nil
}

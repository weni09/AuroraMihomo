package protected

import (
	"context"
	"errors"
	"strconv"
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

	// 口令版本先落库并更新内存，最后才写新密码哈希。
	// 次序取舍：若「版本 +1 已生效但密码写失败」，只是所有会话被要求
	// 重新登录（fail-closed，可接受）；反过来（密码已改但版本没写进去）
	// 会让旧令牌跨重启复活，是真正的安全漏洞。
	newVer := l.svcCtx.PasswordVer.Current() + 1
	if err := l.svcCtx.Database.SetSetting("admin_password_ver", strconv.FormatInt(newVer, 10)); err != nil {
		l.Errorf("记录口令版本失败，改密中止: %v", err)
		return nil, errors.New("记录口令版本失败，改密未生效")
	}
	l.svcCtx.PasswordVer.Set(newVer)

	if err := l.svcCtx.Database.SetSetting("admin_password", hashed); err != nil {
		return nil, err
	}

	// 改密后初始密码文件已无意义，且仍可能含旧明文——必须清掉。
	// 路径与 servicecontext 首次写入时一致。
	svc.RemoveInitialPasswordFile(l.svcCtx.Config.Mihomo.ConfigDir)

	l.Info("管理员密码已更新")
	msg := "密码已更新，请使用新密码重新登录"
	// AGH 口令与 Aurora 管理员密码独立：切勿把新 Aurora 密码写进 SSO 内存，
	// 否则会挡住 CredStore 里真正的 AGH 凭据，导致 /adguard-ui 免密失败。
	// 只丢掉旧 agh_session；下次反代用持久化 AGH 口令重新 Establish。
	if bridge := l.svcCtx.AdGuardSSO; bridge != nil {
		const userKey = "1"
		bridge.InvalidateSession(userKey)
		_ = bridge.HydrateFromStore()
		if l.svcCtx.AdGuardService != nil {
			bridge.SetUsername(l.svcCtx.AdGuardService.AdminUsername())
		}
	}
	return &types.Result{Success: true, Message: msg}, nil
}

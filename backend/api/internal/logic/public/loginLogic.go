package public

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/auth"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LoginLogic) getJwtToken(secretKey string, iat, seconds, uid int64) (string, error) {
	claims := jwt.MapClaims{
		"exp": iat + seconds,
		"iat": iat,
		"uid": uid,
	}
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secretKey))
}

// Login 校验管理员口令并签发 JWT。
// r 用于取来源 IP 做失败次数限流，可为 nil（如单元测试）。
func (l *LoginLogic) Login(req *types.LoginReq, r *http.Request) (resp *types.LoginResp, err error) {
	src := clientIP(r, l.svcCtx.Config.TrustedProxies)

	if ok, wait := l.svcCtx.LoginLimiter.Allow(src); !ok {
		return nil, fmt.Errorf("尝试次数过多，请在 %s 后重试", wait)
	}

	stored, dbErr := l.svcCtx.Database.GetSetting("admin_password")
	if dbErr != nil || strings.TrimSpace(stored) == "" {
		l.Errorf("读取管理员密码失败: %v", dbErr)
		return nil, errors.New("服务端密码未初始化")
	}

	ok, needsUpgrade := auth.VerifyPassword(stored, req.Password)
	if !ok {
		l.svcCtx.LoginLimiter.Fail(src)
		l.Infof("登录失败，来源 %s", src)
		return nil, errors.New("密码错误")
	}
	l.svcCtx.LoginLimiter.Reset(src)

	// 存量明文密码在首次成功登录后升级为哈希存储
	if needsUpgrade {
		if hashed, hErr := auth.HashPassword(req.Password); hErr == nil {
			if sErr := l.svcCtx.Database.SetSetting("admin_password", hashed); sErr != nil {
				l.Errorf("升级密码存储失败: %v", sErr)
			}
		}
	}

	// 初始密码文件按用户要求保留：登录成功后不再自动删除，
	// 方便随时查回。代价是明文会长期留在数据卷里，
	// 因此建议尽快改密码并手动删除该文件。
	now := time.Now().Unix()
	tokenStr, err := l.getJwtToken(l.svcCtx.Config.Auth.AccessSecret, now, l.svcCtx.Config.Auth.AccessExpire, 1)
	if err != nil {
		return nil, err
	}

	// AdGuard 免密：只用持久化 AGH 凭据（设置里「保存账号」写入）预热 session。
	// 不再用 Aurora 登录密码试登 AGH 并 Persist——口令独立后会污染 CredStore。
	if bridge := l.svcCtx.AdGuardSSO; bridge != nil {
		const userKey = "1"
		_ = bridge.HydrateFromStore()
		if l.svcCtx.AdGuardService != nil {
			bridge.SetUsername(l.svcCtx.AdGuardService.AdminUsername())
		}
		if l.svcCtx.AdGuardManager != nil && l.svcCtx.AdGuardManager.Status().Running {
			if c := bridge.SessionCookie(l.ctx, userKey); c == "" {
				l.Info("AdGuard 免密预热未就绪（可在 AdGuard 设置中保存账号）")
			}
		}
	}

	return &types.LoginResp{Token: tokenStr}, nil
}

// clientIP 取用于限流计数的来源标识。
//
// 只有当直连来源本身是配置中声明的可信代理时，才采信 X-Forwarded-For /
// X-Real-IP。否则任何人都能每次请求换一个伪造头部，使失败计数永远落在
// 不同的 key 上，登录限流形同虚设。
func clientIP(r *http.Request, trustedProxies []string) string {
	if r == nil {
		return "unknown"
	}
	direct := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		direct = host
	}

	if !isTrustedProxy(direct, trustedProxies) {
		return direct
	}

	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// XFF 形如 "client, proxy1, proxy2"，最左侧为原始客户端
		if i := strings.Index(v, ","); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return direct
}

// isTrustedProxy 判断直连来源是否在可信代理白名单内，支持单 IP 与 CIDR
func isTrustedProxy(ip string, trusted []string) bool {
	if len(trusted) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	for _, entry := range trusted {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil || parsed == nil {
				continue
			}
			if network.Contains(parsed) {
				return true
			}
			continue
		}
		if entry == ip {
			return true
		}
		if other := net.ParseIP(entry); other != nil && parsed != nil && other.Equal(parsed) {
			return true
		}
	}
	return false
}

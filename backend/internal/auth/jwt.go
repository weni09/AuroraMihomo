package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/golang-jwt/jwt/v4"
)

// PasswordVer 记录管理员口令版本（改密次数）。
//
// 登录签发的 JWT 携带该值；三处验签路径（API 闸门、WS、AdGuard 反代）
// 用它拒绝改密前签发的旧令牌——无状态 JWT 无法在签发后吊销，
// 加版本声明是改动最小、不用维护黑名单的方案。
// 并发安全的单一计数器：本面板单实例单用户，无需跨进程同步，
// 重启后由 settings 表（admin_password_ver）恢复，见 servicecontext。
type PasswordVer struct {
	v atomic.Int64
}

func NewPasswordVer(initial int64) *PasswordVer {
	p := &PasswordVer{}
	p.v.Store(initial)
	return p
}

// Current 返回当前口令版本。
func (p *PasswordVer) Current() int64 {
	if p == nil {
		return 0
	}
	return p.v.Load()
}

// Set 覆盖当前版本（改密成功、已落库后调用）。
func (p *PasswordVer) Set(v int64) {
	p.v.Store(v)
}

// Bump 将版本 +1 并返回新值。
func (p *PasswordVer) Bump() int64 {
	return p.v.Add(1)
}

// ParseToken 校验 JWT 签名（仅接受 HMAC 系）与 exp/iat/nbf 有效期，返回 claims。
// 校验语义与 go-zero rest.WithJwt、既有 verifyWSToken / AuthorizeRequest 一致。
func ParseToken(raw, secret string) (jwt.MapClaims, error) {
	if strings.TrimSpace(raw) == "" || secret == "" {
		return nil, errors.New("empty token or secret")
	}
	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// TokenVersionValid 检查令牌的口令版本声明是否仍有效。
// 改密（ver+1）后旧令牌的 ver 落后于当前版本 → 拒绝。
// 无 ver 声明的存量令牌按 0 处理：不改密不踢下线（平滑升级）；
// 首次改密后（版本变 1）所有存量令牌立即失效。
func TokenVersionValid(claims jwt.MapClaims, currentVer int64) bool {
	if currentVer <= 0 {
		return true
	}
	v, err := claimInt64(claims, "ver")
	if err != nil {
		return false
	}
	return v >= currentVer
}

// claimInt64 从 JWT 声明中取整数，兼容常见数值形态。
// 签发端写入 int64，解析端默认得到 float64；json.Number 与 string
// 是防御性兜底（第三方签发工具可能产生）。
func claimInt64(claims jwt.MapClaims, key string) (int64, error) {
	switch v := claims[key].(type) {
	case float64:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(v, 10, 64)
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	}
	return 0, fmt.Errorf("claim %q 缺失或类型不支持", key)
}

// ExtractBearerToken 从 Authorization 头提取 Bearer 令牌，无则返回空串。
// 宽松匹配与既有 verifyWSToken / AuthorizeRequest / syncSessionHandler 的
// 提取方式一致：Bearer 前缀可选，裸令牌同样接受（这些路径历来如此，
// 收紧会改变非浏览器客户端的既有行为）。API 路径即使提取到裸令牌，
// go-zero rest.WithJwt 仍会因其提取器要求 Bearer 前缀而 401，无安全差异。
func ExtractBearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

// SessionCookieName 为面板登录会话 Cookie 名。
// API 闸门、/ws 与两个同源反代（/adguard-ui、/mihomo-api）共用同一会话，
// 常量统一放在 auth 包，避免各层各自定义字符串。
const SessionCookieName = "aurora_session"

// AuthorizeRequest 校验同源反代请求：优先 aurora_session cookie，
// 其次 Authorization Bearer。JWT 校验方式与 verifyWSToken 一致（HMAC）；
// ver 为口令版本闸门：改密后旧令牌即使签名有效也拒绝访问。
//
// /adguard-ui 与 /mihomo-api 两个反代共用此函数：iframe 内嵌页面的请求
// 无法像 fetch 那样显式带 Authorization，但同源下浏览器会自动附带 Cookie，
// 因此两者都以 Cookie 为主要鉴权通道。
func AuthorizeRequest(r *http.Request, secret string, ver *PasswordVer) bool {
	if r == nil || secret == "" {
		return false
	}
	raw := ""
	if c, err := r.Cookie(SessionCookieName); err == nil && c != nil {
		raw = strings.TrimSpace(c.Value)
	}
	if raw == "" {
		raw = ExtractBearerToken(r)
	}
	if raw == "" {
		return false
	}
	claims, err := ParseToken(raw, secret)
	if err != nil {
		return false
	}
	return TokenVersionValid(claims, ver.Current())
}

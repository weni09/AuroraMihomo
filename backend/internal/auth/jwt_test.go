package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testJWTSecret = "test-jwt-secret-32bytes-long!!"

func TestPasswordVer(t *testing.T) {
	p := NewPasswordVer(0)
	if p.Current() != 0 {
		t.Fatalf("初始版本应为 0，实际 %d", p.Current())
	}
	if got := p.Bump(); got != 1 {
		t.Fatalf("Bump 应返回 1，实际 %d", got)
	}
	if p.Current() != 1 {
		t.Fatalf("Bump 后 Current 应为 1，实际 %d", p.Current())
	}
	p.Set(5)
	if p.Current() != 5 {
		t.Fatalf("Set(5) 后 Current 应为 5，实际 %d", p.Current())
	}
	// nil 指针兜底：不 panic、按 0 处理（如未初始化的组件）
	var nilP *PasswordVer
	if nilP.Current() != 0 {
		t.Fatal("nil PasswordVer 的 Current 应为 0")
	}
}

func signToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func TestParseToken(t *testing.T) {
	now := time.Now().Unix()
	valid := signToken(t, testJWTSecret, jwt.MapClaims{
		"exp": now + 3600, "iat": now, "uid": 1, "ver": 2,
	})

	claims, err := ParseToken(valid, testJWTSecret)
	if err != nil {
		t.Fatalf("有效令牌解析失败: %v", err)
	}
	if v, _ := claims["ver"].(float64); int64(v) != 2 {
		t.Fatalf("ver 声明应为 2，实际 %v", claims["ver"])
	}

	// 错误密钥：签名不匹配
	if _, err := ParseToken(valid, "wrong-secret"); err == nil {
		t.Fatal("错误密钥应解析失败")
	}
	// 乱串
	if _, err := ParseToken("garbage.token.value", testJWTSecret); err == nil {
		t.Fatal("乱串应解析失败")
	}
	// 空输入
	if _, err := ParseToken("", testJWTSecret); err == nil {
		t.Fatal("空令牌应解析失败")
	}
	if _, err := ParseToken(valid, ""); err == nil {
		t.Fatal("空密钥应解析失败")
	}
	// 过期令牌
	expired := signToken(t, testJWTSecret, jwt.MapClaims{
		"exp": now - 60, "iat": now - 3600, "uid": 1,
	})
	if _, err := ParseToken(expired, testJWTSecret); err == nil {
		t.Fatal("过期令牌应解析失败")
	}
}

func TestTokenVersionValid(t *testing.T) {
	now := time.Now().Unix()
	mk := func(ver interface{}) jwt.MapClaims {
		c := jwt.MapClaims{"exp": now + 3600, "iat": now, "uid": 1}
		if ver != nil {
			c["ver"] = ver
		}
		return c
	}

	// 当前版本 0（从未改密）：无 ver 声明与 ver=0 的令牌都放行
	if !TokenVersionValid(mk(nil), 0) {
		t.Fatal("未改密时无 ver 声明的存量令牌应放行")
	}
	if !TokenVersionValid(mk(float64(0)), 0) {
		t.Fatal("ver=0 令牌应放行")
	}

	// 改密后（版本 1）：无 ver 声明与落后版本必须拒绝
	if TokenVersionValid(mk(nil), 1) {
		t.Fatal("改密后无 ver 声明的存量令牌必须拒绝")
	}
	if TokenVersionValid(mk(float64(0)), 1) {
		t.Fatal("改密后 ver=0 令牌必须拒绝")
	}
	if !TokenVersionValid(mk(float64(1)), 1) {
		t.Fatal("ver=1 令牌应放行")
	}
	if !TokenVersionValid(mk(float64(2)), 1) {
		t.Fatal("ver 超前于当前版本应放行（重启回退等场景）")
	}

	// 数值形态兼容：json.Number 与 string
	if !TokenVersionValid(mk(json.Number("1")), 1) {
		t.Fatal("json.Number 形态的 ver 应可识别")
	}
	if !TokenVersionValid(mk("1"), 1) {
		t.Fatal("string 形态的 ver 应可识别")
	}
	// 类型不支持（如布尔）→ 视为无效
	if TokenVersionValid(mk(true), 1) {
		t.Fatal("非法 ver 类型必须拒绝")
	}
}

func TestExtractBearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := ExtractBearerToken(r); got != "" {
		t.Fatalf("无头时应返回空串，实际 %q", got)
	}
	r.Header.Set("Authorization", "Bearer abc.def")
	if got := ExtractBearerToken(r); got != "abc.def" {
		t.Fatalf("应提取出 abc.def，实际 %q", got)
	}
	// 无 Bearer 前缀：与既有路径一致的宽松匹配，裸令牌同样提取
	r.Header.Set("Authorization", "abc.def")
	if got := ExtractBearerToken(r); got != "abc.def" {
		t.Fatalf("裸令牌应同样提取，实际 %q", got)
	}
	// nil 请求兜底
	if got := ExtractBearerToken(nil); got != "" {
		t.Fatalf("nil 请求应返回空串，实际 %q", got)
	}
}

// AuthorizeRequest 是同源反代的统一鉴权（/adguard-ui、/mihomo-api 共用）：
// 优先 aurora_session cookie，其次 Bearer；口令版本闸门与 API/WS 一致。
func TestAuthorizeRequest(t *testing.T) {
	now := time.Now().Unix()
	valid := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": now + 3600,
		"iat": now,
		"uid": 1,
		"ver": 1,
	})
	validStr, err := valid.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	if AuthorizeRequest(httptest.NewRequest(http.MethodGet, "/", nil), "", NewPasswordVer(0)) {
		t.Fatal("空密钥必须拒绝")
	}
	rNoCred := httptest.NewRequest(http.MethodGet, "/", nil)
	if AuthorizeRequest(rNoCred, testJWTSecret, NewPasswordVer(1)) {
		t.Fatal("无凭据应拒绝")
	}

	rCookie := httptest.NewRequest(http.MethodGet, "/", nil)
	rCookie.AddCookie(&http.Cookie{Name: SessionCookieName, Value: validStr})
	if !AuthorizeRequest(rCookie, testJWTSecret, NewPasswordVer(1)) {
		t.Fatal("有效 cookie 应放行")
	}
	if AuthorizeRequest(rCookie, testJWTSecret, NewPasswordVer(2)) {
		t.Fatal("改密后旧令牌应拒绝")
	}

	rBearer := httptest.NewRequest(http.MethodGet, "/", nil)
	rBearer.Header.Set("Authorization", "Bearer "+validStr)
	if !AuthorizeRequest(rBearer, testJWTSecret, NewPasswordVer(1)) {
		t.Fatal("有效 Bearer 应放行")
	}
}

package public

import (
	"net/http"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/auth"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// SyncSessionHandler 用 Authorization Bearer 中的 JWT 重写 aurora_session cookie。
//
// 场景：API 走 localStorage token，而 /adguard-ui 反代只认 cookie。
// 部分手机浏览器（尤其 Android Edge）或仅带 token 的会话下 cookie 缺失/过期，
// iframe 会显示 unauthorized 或白屏。进入 AdGuard 页前由前端带 Bearer 调本接口对齐。
//
// 鉴权由 rest.WithJwt 保证；此处只负责落 cookie，不重新签发。
//
// 重要：只要请求带了有效 Bearer，就**始终**用其覆盖 cookie。
// 旧实现若已有非空 cookie 就直接 200 跳过——Android Edge 等会长期留着过期/损坏的
// aurora_session，POST 一直「成功」但 iframe 仍 401，只能靠多次整页刷新偶然清掉。
func SyncSessionHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := ""
		if h := r.Header.Get("Authorization"); h != "" {
			raw = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if raw == "" {
			// 无 Bearer：仅当已有 cookie 时幂等成功（兼容极端客户端）
			if c, err := r.Cookie("aurora_session"); err == nil && c != nil && strings.TrimSpace(c.Value) != "" {
				httpx.OkJsonCtx(r.Context(), w, map[string]any{"ok": true, "source": "cookie"})
				return
			}
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		cookie := &http.Cookie{
			Name:     "aurora_session",
			Value:    raw,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		if exp := svcCtx.Config.Auth.AccessExpire; exp > 0 {
			cookie.MaxAge = int(exp)
		}
		if auth.RequestIsHTTPS(r, svcCtx.Config.TrustedProxies) {
			cookie.Secure = true
		}
		http.SetCookie(w, cookie)
		// 避免中间层缓存「空 body 的 session 对齐」响应
		w.Header().Set("Cache-Control", "no-store")
		httpx.OkJsonCtx(r.Context(), w, map[string]any{"ok": true, "source": "bearer"})
	}
}

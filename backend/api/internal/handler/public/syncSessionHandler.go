package public

import (
	"net/http"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// SyncSessionHandler 用 Authorization Bearer 中的 JWT 重写 aurora_session cookie。
//
// 场景：API 走 localStorage token，而 /adguard-ui 反代只认 cookie。
// 部分手机浏览器或仅带 token 的会话下 cookie 缺失，iframe 会直接显示 unauthorized。
// 进入 AdGuard 页前由前端带 Bearer 调一次本接口即可对齐。
//
// 鉴权由 rest.WithJwt 保证；此处只负责落 cookie，不重新签发。
func SyncSessionHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := ""
		if h := r.Header.Get("Authorization"); h != "" {
			raw = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if raw == "" {
			// 兜底：已有 cookie 也算成功（幂等）
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
		if r.TLS != nil {
			cookie.Secure = true
		}
		http.SetCookie(w, cookie)
		httpx.OkJsonCtx(r.Context(), w, map[string]any{"ok": true, "source": "bearer"})
	}
}

package public

import (
	"net/http"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/adguard"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// LogoutHandler 清除 aurora_session HttpOnly cookie。
//
// cookie 为 HttpOnly，前端无法 document.cookie 删除；即使未带有效 JWT
// 也应允许调用，以便会话残留时用户能主动清掉。
func LogoutHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 登出时丢掉 AGH 免密缓存，避免下个用户（同进程）误用上一会话
		if svcCtx != nil && svcCtx.AdGuardSSO != nil {
			key := adguard.UserKeyFromRequest(r, svcCtx.Config.Auth.AccessSecret)
			if key == "" {
				key = "1"
			}
			svcCtx.AdGuardSSO.Clear(key)
		}

		cookie := &http.Cookie{
			Name:     "aurora_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		if r.TLS != nil {
			cookie.Secure = true
		}
		http.SetCookie(w, cookie)
		httpx.OkJsonCtx(r.Context(), w, map[string]any{"ok": true})
	}
}

package public

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/public"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/auth"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func LoginHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := public.NewLoginLogic(r.Context(), svcCtx)
		resp, err := l.Login(&req, r)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		// 同源 /adguard 反代依赖 HttpOnly cookie 携带会话；localStorage 中的
		// token 仍由前端保留，供 API Authorization 头使用。
		cookie := &http.Cookie{
			Name:     "aurora_session",
			Value:    resp.Token,
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

			httpx.OkJsonCtx(r.Context(), w, resp)
	}
}

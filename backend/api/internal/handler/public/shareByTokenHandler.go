package public

import (
	"errors"
	"net/http"

	"auroramihomo/backend/api/internal/logic/public"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ShareByTokenHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ShareTokenReq
		if err := httpx.Parse(r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		l := public.NewShareByTokenLogic(r.Context(), svcCtx)
		out, err := l.ShareRaw(&req)
		if err != nil {
			// 过期是可预期的正常状态，回 410 Gone 让用户能区分
			// 「链接填错了」与「链接过期了」。过期本身不算敏感信息。
			if errors.Is(err, service.ErrShareExpired) {
				http.Error(w, "分享链接已过期", http.StatusGone)
				return
			}
			// 该端点对公网开放，原始错误可能包含订阅名与上游响应体，
			// 只写日志，对外返回泛化文案
			// share token 本身就是访问凭据，只记录前缀便于定位，不落全量
			logx.Errorf("分享链接渲染失败 token=%s***: %v", maskToken(req.Token), err)
			http.Error(w, "订阅不存在或当前不可用", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", out.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline; filename=sub.yaml")
		if out.UserInfo != "" {
			w.Header().Set("Subscription-Userinfo", out.UserInfo)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(out.Body))
	}
}

// maskToken 只保留 token 前 6 位，避免凭据完整进入日志系统
func maskToken(t string) string {
	if len(t) <= 6 {
		return ""
	}
	return t[:6]
}

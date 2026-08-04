package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// ProbeSubscriptionHandler 手写 handler（非 goctl 生成）：
// 供 system.go 手挂的 POST /api/v1/subscriptions/probe 使用。
func ProbeSubscriptionHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req protected.ProbeSubscriptionReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := protected.NewProbeSubscriptionLogic(r.Context(), svcCtx)
		resp, err := l.ProbeSubscription(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		httpx.OkJsonCtx(r.Context(), w, resp)
	}
}

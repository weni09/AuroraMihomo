package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AdGuardSetDNSModeHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdGuardDNSModeReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := protected.NewAdGuardSetDNSModeLogic(r.Context(), svcCtx)
		resp, err := l.AdGuardSetDNSMode(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

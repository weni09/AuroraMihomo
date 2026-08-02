package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AdGuardSetDNSPortHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdGuardDNSPortReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := protected.NewAdGuardSetDNSPortLogic(r.Context(), svcCtx)
		resp, err := l.AdGuardSetDNSPort(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

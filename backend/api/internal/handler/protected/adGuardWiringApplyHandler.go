package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AdGuardWiringApplyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AdGuardWiringApplyReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := protected.NewAdGuardWiringApplyLogic(r.Context(), svcCtx)
		resp, err := l.AdGuardWiringApply(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AdGuardWiringRollbackHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := protected.NewAdGuardWiringRollbackLogic(r.Context(), svcCtx)
		resp, err := l.AdGuardWiringRollback()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

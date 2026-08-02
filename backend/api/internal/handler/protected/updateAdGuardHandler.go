package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func UpdateAdGuardHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := protected.NewUpdateAdGuardLogic(r.Context(), svcCtx)
		resp, err := l.UpdateAdGuard()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

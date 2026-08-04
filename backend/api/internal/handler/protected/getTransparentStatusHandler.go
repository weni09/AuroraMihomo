package protected

import (
	"net/http"

	"auroramihomo/backend/api/internal/logic/protected"
	"auroramihomo/backend/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetTransparentStatusHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 不缓存、不复用连接：该接口会被控制台/设置页频繁打到，
		// 一旦某条 keep-alive 连接在客户端半关闭或中间层卡住，
		// 后续 status 会在浏览器连接池里一直 pending（F12 可见），
		// 而服务端日志仍显示 200——表现就是「开关灰掉 / 不具备条件」。
		// Connection: close 让每次响应后拆掉 TCP，用多一次握手换确定性。
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "close")

		l := protected.NewGetTransparentStatusLogic(r.Context(), svcCtx)
		resp, err := l.GetTransparentStatus()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}

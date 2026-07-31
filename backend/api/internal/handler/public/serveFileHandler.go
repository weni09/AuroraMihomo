package public

import (
	"errors"
	"net/http"

	"auroramihomo/backend/api/internal/logic/public"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func ServeFileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 使用 go-zero 的路径参数解析，避免手动切分 r.URL.Path 时
		// 对已解码字符失配
		var req types.FileTokenReq
		if err := httpx.Parse(r, &req); err != nil {
			http.Error(w, "missing file token", http.StatusBadRequest)
			return
		}

		l := public.NewServeFileLogic(r.Context(), svcCtx)
		content, contentType, err := l.ServeFileRaw(req.Token)
		if err != nil {
			// 过期回 410，便于与「token 不存在」区分
			if errors.Is(err, service.ErrShareExpired) {
				http.Error(w, "分享链接已过期", http.StatusGone)
				return
			}
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		// 防止浏览器嗅探把纯文本当作可执行内容渲染
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}
}

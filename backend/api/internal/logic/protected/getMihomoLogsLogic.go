package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMihomoLogsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetMihomoLogsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMihomoLogsLogic {
	return &GetMihomoLogsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetMihomoLogsLogic) GetMihomoLogs() (resp []types.LogLine, err error) {
	lines := l.svcCtx.MihomoManager.Logs(200)
	resp = make([]types.LogLine, 0, len(lines))
	for _, ln := range lines {
		resp = append(resp, types.LogLine{
			Time:    ln.Time.Format(time.RFC3339),
			Stream:  ln.Stream,
			Message: ln.Message,
		})
	}
	return resp, nil
}

package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/mihomo"

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

const (
	logsDefaultLimit = 200
	// logsMaxLimit 与 ProcessManager 的内存缓冲上限一致：请求更多也没有意义
	logsMaxLimit = 1000
)

func (l *GetMihomoLogsLogic) GetMihomoLogs(req *types.GetMihomoLogsReq) (resp []types.LogLine, err error) {
	limit := req.Limit
	if limit <= 0 {
		limit = logsDefaultLimit
	}
	if limit > logsMaxLimit {
		limit = logsMaxLimit
	}

	// 无法识别的级别按"不筛"处理，不返回 400：日志查询是排查手段，
	// 参数写错时给全量比报错更有用（与 /system/logs 的取舍一致）。
	lines := l.svcCtx.MihomoManager.Logs(limit, mihomo.ParseLevel(req.Level))

	resp = make([]types.LogLine, 0, len(lines))
	for _, ln := range lines {
		resp = append(resp, types.LogLine{
			Time:    ln.Time.Format(time.RFC3339),
			Stream:  ln.Stream,
			Level:   string(ln.Level),
			Message: ln.Message,
		})
	}
	return resp, nil
}

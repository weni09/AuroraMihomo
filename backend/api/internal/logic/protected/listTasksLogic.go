package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListTasksLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTasksLogic {
	return &ListTasksLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *ListTasksLogic) ListTasks() (resp []types.TaskItem, err error) {
	rows, err := l.svcCtx.Database.ListTasks()
	if err != nil {
		return nil, err
	}
	fmtTime := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	resp = make([]types.TaskItem, 0, len(rows))
	for _, r := range rows {
		resp = append(resp, types.TaskItem{
			Id: r.ID, Name: r.Name, Cron: r.Cron,
			Enabled: r.Enabled == 1,
			LastRun: fmtTime(r.LastRun), NextRun: fmtTime(r.NextRun),
			Status: r.Status, Message: r.Message,
		})
	}
	return resp, nil
}

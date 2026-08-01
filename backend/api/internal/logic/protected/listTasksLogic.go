package protected

import (
	"context"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/scheduler"

	"github.com/zeromicro/go-zero/core/logx"
)

// 控制台任务列表里，settings 驱动的调度任务使用稳定 name（前端 TASK_LABELS 映射）。
// id 用负数与 DB 自增主键区分，避免和真实 tasks 行撞号。
const (
	taskNameLogCleanup = "applog_cleanup"
	taskNameAutoUpdate = "auto_update"
	taskNameRemotePull = "remote_config_pull"

	taskIDLogCleanup int64 = -1
	taskIDAutoUpdate int64 = -2
	taskIDRemotePull int64 = -3
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
	resp = make([]types.TaskItem, 0, len(rows)+3)
	for _, r := range rows {
		resp = append(resp, types.TaskItem{
			Id: r.ID, Name: r.Name, Cron: r.Cron,
			Enabled: r.Enabled == 1,
			LastRun: fmtTime(r.LastRun), NextRun: fmtTime(r.NextRun),
			Status: r.Status, Message: r.Message,
		})
	}

	// settings 调度不在 tasks 表：日志清理 / 自动更新 / 远程配置拉取。
	// 不并进来的话控制台「后台任务」会缺一大块实际在跑的定时器。
	resp = append(resp, l.scheduleTaskItems(fmtTime)...)
	return resp, nil
}

// scheduleTaskItems 从 Settings + Scheduler 拼出三类系统调度任务。
func (l *ListTasksLogic) scheduleTaskItems(fmtTime func(time.Time) string) []types.TaskItem {
	if l.svcCtx == nil || l.svcCtx.SettingsService == nil {
		return nil
	}
	s := l.svcCtx.SettingsService
	sch := l.svcCtx.Scheduler

	nextOf := func(jobName string) string {
		if sch == nil {
			return ""
		}
		return fmtTime(sch.NextRun(jobName))
	}
	statusOf := func(enabled bool) string {
		if enabled {
			return "scheduled"
		}
		return "disabled"
	}

	out := make([]types.TaskItem, 0, 3)

	// 1) 应用日志归档清理
	logEnabled := s.LogCleanupEnabled()
	out = append(out, types.TaskItem{
		Id:      taskIDLogCleanup,
		Name:    taskNameLogCleanup,
		Cron:    s.LogCleanupCron(),
		Enabled: logEnabled,
		NextRun: nextOf(scheduler.JobLogCleanup),
		Status:  statusOf(logEnabled),
		Message: "系统设置 · 日志保留清理",
	})

	// 2) 组件自动更新（内核 / 面板等）
	st := s.Get()
	out = append(out, types.TaskItem{
		Id:      taskIDAutoUpdate,
		Name:    taskNameAutoUpdate,
		Cron:    st.AutoUpdateCron,
		Enabled: st.AutoUpdateEnabled,
		NextRun: nextOf(scheduler.JobAutoUpdate),
		Status:  statusOf(st.AutoUpdateEnabled),
		Message: "系统设置 · 自动更新",
	})

	// 3) 远程配置定时拉取
	remote := s.GetRemoteSource()
	out = append(out, types.TaskItem{
		Id:      taskIDRemotePull,
		Name:    taskNameRemotePull,
		Cron:    remote.Cron,
		Enabled: remote.CronEnabled,
		NextRun: nextOf(scheduler.JobRemotePull),
		Status:  statusOf(remote.CronEnabled),
		Message: "系统设置 · 远程配置拉取",
	})

	return out
}

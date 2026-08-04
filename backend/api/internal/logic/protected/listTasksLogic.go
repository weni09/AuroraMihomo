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
	taskNameLogCleanup        = "applog_cleanup"
	taskNameAutoUpdate        = "auto_update"
	taskNameRemotePull        = "remote_config_pull"
	taskNameAdGuardAutoUpdate = "adguard_auto_update"

	taskIDLogCleanup        int64 = -1
	taskIDAutoUpdate        int64 = -2
	taskIDRemotePull        int64 = -3
	taskIDAdGuardAutoUpdate int64 = -4
)

type ListTasksLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListTasksLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListTasksLogic {
	return &ListTasksLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ledgerOnlyTaskNames 是 settings/调度器驱动的任务：启用态与 cron 以设置
// 为准，tasks 表只存 LastRun 账本。直接把表行列出再拼虚拟项会双份显示；
// version_check / subscription_update 是已下线旧名，存量行一并过滤。
var ledgerOnlyTaskNames = map[string]struct{}{
	taskNameLogCleanup:        {},
	taskNameAutoUpdate:        {},
	taskNameRemotePull:        {},
	taskNameAdGuardAutoUpdate: {},
	"version_check":           {},
	"subscription_update":     {},
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
	// name → 账本行（LastRun/status/message），供 schedule 项合并
	ledger := make(map[string]struct {
		lastRun, status, message string
	}, len(rows))
	resp = make([]types.TaskItem, 0, len(rows)+4)
	for _, r := range rows {
		if _, skip := ledgerOnlyTaskNames[r.Name]; skip {
			ledger[r.Name] = struct {
				lastRun, status, message string
			}{fmtTime(r.LastRun), r.Status, r.Message}
			continue
		}
		resp = append(resp, types.TaskItem{
			Id: r.ID, Name: r.Name, Cron: r.Cron,
			Enabled: r.Enabled == 1,
			LastRun: fmtTime(r.LastRun), NextRun: fmtTime(r.NextRun),
			Status: r.Status, Message: r.Message,
		})
	}

	// settings 调度不在 tasks 表：日志清理 / 自动更新 / 远程配置拉取。
	// 不并进来的话控制台「后台任务」会缺一大块实际在跑的定时器。
	resp = append(resp, l.scheduleTaskItems(fmtTime, ledger)...)
	return resp, nil
}

// scheduleTaskItems 从 Settings + Scheduler 拼出系统调度任务；
// ledger 提供最近一次执行痕迹（MarkTaskRun 写入的账本行）。
func (l *ListTasksLogic) scheduleTaskItems(fmtTime func(time.Time) string, ledger map[string]struct {
	lastRun, status, message string
}) []types.TaskItem {
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
	// 未跑过时 status 仍用 scheduled/disabled；跑过后保留 ok/error，
	// 但 enabled=false 时仍标 disabled，避免「关了却显示 ok」。
	mergeRun := func(name string, enabled bool, fallbackMsg string) (lastRun, status, message string) {
		status = "disabled"
		if enabled {
			status = "scheduled"
		}
		message = fallbackMsg
		if lg, ok := ledger[name]; ok {
			lastRun = lg.lastRun
			if lg.message != "" {
				message = lg.message
			}
			if enabled && lg.status != "" {
				status = lg.status
			}
		}
		return lastRun, status, message
	}

	out := make([]types.TaskItem, 0, 4)

	// 1) 应用日志归档清理
	logEnabled := s.LogCleanupEnabled()
	last, st, msg := mergeRun(taskNameLogCleanup, logEnabled, "系统设置 · 日志保留清理")
	out = append(out, types.TaskItem{
		Id:      taskIDLogCleanup,
		Name:    taskNameLogCleanup,
		Cron:    s.LogCleanupCron(),
		Enabled: logEnabled,
		LastRun: last,
		NextRun: nextOf(scheduler.JobLogCleanup),
		Status:  st,
		Message: msg,
	})

	// 2) 组件自动更新（内核 / 面板等）—— 唯一对外入口，不再并列 version_check
	au := s.Get()
	last, st, msg = mergeRun(taskNameAutoUpdate, au.AutoUpdateEnabled, "系统设置 · 自动更新")
	out = append(out, types.TaskItem{
		Id:      taskIDAutoUpdate,
		Name:    taskNameAutoUpdate,
		Cron:    au.AutoUpdateCron,
		Enabled: au.AutoUpdateEnabled,
		LastRun: last,
		NextRun: nextOf(scheduler.JobAutoUpdate),
		Status:  st,
		Message: msg,
	})

	// 3) 远程配置定时拉取
	remote := s.GetRemoteSource()
	last, st, msg = mergeRun(taskNameRemotePull, remote.CronEnabled, "系统设置 · 远程配置拉取")
	out = append(out, types.TaskItem{
		Id:      taskIDRemotePull,
		Name:    taskNameRemotePull,
		Cron:    remote.Cron,
		Enabled: remote.CronEnabled,
		LastRun: last,
		NextRun: nextOf(scheduler.JobRemotePull),
		Status:  st,
		Message: msg,
	})

	// 4) AdGuard 独立自动更新：仅组件启用时展示
	if l.svcCtx.AdGuardService != nil && l.svcCtx.AdGuardService.ComponentEnabled() {
		aghOn := l.svcCtx.AdGuardService.AutoUpdateEnabled()
		last, st, msg = mergeRun(taskNameAdGuardAutoUpdate, aghOn, "AdGuard 设置 · 自动更新")
		out = append(out, types.TaskItem{
			Id:      taskIDAdGuardAutoUpdate,
			Name:    taskNameAdGuardAutoUpdate,
			Cron:    l.svcCtx.AdGuardService.AutoUpdateCron(),
			Enabled: aghOn,
			LastRun: last,
			NextRun: nextOf(scheduler.JobAdGuardAutoUpdate),
			Status:  st,
			Message: msg,
		})
	}

	return out
}

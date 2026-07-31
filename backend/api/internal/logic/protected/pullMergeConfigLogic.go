package protected

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type PullMergeConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPullMergeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PullMergeConfigLogic {
	return &PullMergeConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// PullMergeConfig 立即拉取远程来源并与本地配置合并。
//
// 与定时拉取做同一件事，只是由用户即时触发：改完远程来源、
// 或知道上游更新了，不必等下一个定时周期。
// 与 /config/merge 的区别就在于它会回源。
func (l *PullMergeConfigLogic) PullMergeConfig() (resp *types.Result, err error) {
	src := l.svcCtx.SettingsService.GetRemoteSource()
	if src.Type == domain.RemoteSourceNone {
		// 没配远程来源时如实说明，否则用户会以为拉取成功了却什么都没变
		return &types.Result{
			Success: false,
			Message: "当前未配置远程来源，请先在上方选择远程订阅",
		}, nil
	}

	res, err := l.svcCtx.ConfigService.PullAndMerge(l.ctx)
	if err != nil {
		l.Errorf("pull and merge failed: %v", err)
		_ = l.svcCtx.Database.MarkTaskRun("config_merge", "error", err.Error(), time.Time{})
		return &types.Result{Success: false, Message: err.Error()}, nil
	}

	msg := "已拉取远程配置并完成合并"
	if res.Message != "" {
		msg = res.Message
	}
	if res.ConflictCount > 0 {
		msg = fmt.Sprintf("%s；检测到 %d 处冲突", msg, res.ConflictCount)
	}
	// 自动修正必须让用户看到，否则他只会发现策略组少了却查不到原因
	if n := len(res.Warnings); n > 0 {
		msg = fmt.Sprintf("%s；已自动修正 %d 处失效引用：%s",
			msg, n, strings.Join(res.Warnings, "；"))
	}
	_ = l.svcCtx.Database.MarkTaskRun("config_merge", "ok", "", time.Time{})
	l.svcCtx.Hub.Publish("config.updated", map[string]any{"ok": true, "source": "manual_pull"})
	return &types.Result{Success: true, Message: msg}, nil
}

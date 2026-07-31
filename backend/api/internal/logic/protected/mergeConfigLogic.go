package protected

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MergeConfigLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMergeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MergeConfigLogic {
	return &MergeConfigLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// MergeConfig 用本地配置与已缓存的远程层重新生成最终配置。
//
// 刻意不回源：这条路径由「保存本地配置」触发，用户改的是自己的
// mihomo 配置，不该因此去打上游机场。需要拉取远程时走
// PullMergeConfig（/config/pull-merge）。
func (l *MergeConfigLogic) MergeConfig() (resp *types.Result, err error) {
	res, err := l.svcCtx.ConfigService.ApplyLocalOnly(l.ctx)
	if err != nil {
		l.Errorf("merge failed: %v", err)
		_ = l.svcCtx.Database.MarkTaskRun("config_merge", "error", err.Error(), time.Time{})
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	msg := res.Message
	if res.ConflictCount > 0 {
		msg = fmt.Sprintf("%s；检测到 %d 处冲突", msg, res.ConflictCount)
	}
	// 自动修正（如摘掉指向已下线节点的引用）必须让用户看到，
	// 否则他只会发现策略组少了却不知道原因
	if n := len(res.Warnings); n > 0 {
		msg = fmt.Sprintf("%s；已自动修正 %d 处失效引用：%s",
			msg, n, strings.Join(res.Warnings, "；"))
	}
	_ = l.svcCtx.Database.MarkTaskRun("config_merge", "ok", "", time.Time{})
	return &types.Result{Success: true, Message: msg}, nil
}

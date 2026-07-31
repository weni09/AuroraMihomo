package protected

import (
	"context"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResolveConflictLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResolveConflictLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResolveConflictLogic {
	return &ResolveConflictLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResolveConflictLogic) ResolveConflict(req *types.ResolveConflictReq) (resp *types.Result, err error) {
	res := strings.ToLower(strings.TrimSpace(req.Resolution))
	switch res {
	case "local", "remote", "merge", "manual":
	default:
		return &types.Result{Success: false, Message: "解决策略必须是 local|remote|merge|manual"}, nil
	}
	if res == "manual" && strings.TrimSpace(req.ManualValue) == "" {
		return &types.Result{Success: false, Message: "选择手动策略时必须填写手写值"}, nil
	}
	if err := l.svcCtx.Database.ResolveConflict(req.Id, res, req.ManualValue); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	// 解决冲突只改变合并方式，用已缓存的远程层重算即可，无需回源打上游
	if err := l.svcCtx.ConfigService.MergeAndApply(l.ctx, service.MergeLocalOnly()); err != nil {
		_ = l.svcCtx.Database.MarkTaskRun("config_merge", "error", err.Error(), time.Time{})
		return &types.Result{Success: true, Message: "冲突已解决，但重新合并失败：" + err.Error()}, nil
	}
	_ = l.svcCtx.Database.MarkTaskRun("config_merge", "ok", "", time.Time{})
	l.svcCtx.Hub.Publish("config.updated", map[string]any{"ok": true, "source": "conflict_resolve"})
	return &types.Result{Success: true, Message: "冲突已解决并重新合并配置"}, nil
}

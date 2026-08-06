package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetWebPortLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetWebPortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetWebPortLogic {
	return &AdGuardSetWebPortLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetWebPort 设置 AdGuard Web 管理监听。
// Host 空：只改端口（保留原 host）；Host 有值：同时改 host+port。
// 允许 0.0.0.0 / 具体 IP（服务化后可对外）；默认仍是 127.0.0.1。
func (l *AdGuardSetWebPortLogic) AdGuardSetWebPort(req *types.AdGuardWebPortReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	if req == nil {
		return &types.Result{Success: false, Message: "请求体不能为空"}, nil
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		if err := l.svcCtx.AdGuardService.SetWebPort(l.ctx, req.Port); err != nil {
			return &types.Result{Success: false, Message: err.Error()}, nil
		}
	} else {
		if err := l.svcCtx.AdGuardService.SetWebListen(l.ctx, host, req.Port); err != nil {
			return &types.Result{Success: false, Message: err.Error()}, nil
		}
	}
	// 回读实际写入地址，避免 message 与真实 host 不一致
	addr := ""
	if st, stErr := l.svcCtx.AdGuardService.Status(l.ctx); stErr == nil && st != nil {
		addr = strings.TrimSpace(st.WebAddr)
	}
	if addr == "" {
		if host == "" {
			addr = fmt.Sprintf("port=%d", req.Port)
		} else {
			addr = fmt.Sprintf("%s:%d", host, req.Port)
		}
	}
	return &types.Result{
		Success: true,
		Message: fmt.Sprintf("Web 监听已设置为 %s", addr),
	}, nil
}

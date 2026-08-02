package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AdGuardSetCDNLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdGuardSetCDNLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdGuardSetCDNLogic {
	return &AdGuardSetCDNLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// AdGuardSetCDN 保存 AdGuard 专用升级镜像列表；空列表表示回落全局 CDN。
func (l *AdGuardSetCDNLogic) AdGuardSetCDN(req *types.AdGuardCDNReq) (resp *types.Result, err error) {
	if l.svcCtx.AdGuardService == nil {
		return &types.Result{Success: false, Message: "AdGuard 服务未初始化"}, nil
	}
	providers := []string(nil)
	if req != nil {
		providers = req.Providers
	}
	if err := l.svcCtx.AdGuardService.SetCDNProviders(providers); err != nil {
		return &types.Result{Success: false, Message: err.Error()}, nil
	}
	n := len(l.svcCtx.AdGuardService.CDNProviders())
	msg := "已清除 AdGuard 专用升级链接，将使用全局 CDN"
	if n > 0 {
		msg = fmt.Sprintf("已保存 %d 条 AdGuard 升级链接", n)
	}
	return &types.Result{Success: true, Message: msg}, nil
}

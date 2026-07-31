package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProvisionTransparentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProvisionTransparentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProvisionTransparentLogic {
	return &ProvisionTransparentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ProvisionTransparent 补齐透明代理所需的系统条件。
//
// 前置校验（非 root、非 Linux、未指定动作）由 service 层返回 error，
// 直接透传给用户——那些信息本身就是他需要的下一步动作。
// 单步失败不算 error：那种情况要连着"哪一步失败、命令原始输出是什么"
// 一起返回，否则用户只知道"失败了"却不知道去修什么。
func (l *ProvisionTransparentLogic) ProvisionTransparent(req *types.TransparentProvisionReq) (resp *types.TransparentProvisionResp, err error) {
	res, env, err := l.svcCtx.TransparentService.Provision(l.ctx, netcheck.ProvisionOptions{
		InstallPackages: req.InstallPackages,
		ApplySysctl:     req.ApplySysctl,
	})
	if err != nil {
		return nil, err
	}
	return toTransparentProvision(res, env), nil
}

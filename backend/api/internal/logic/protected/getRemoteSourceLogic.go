package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// GetRemoteSourceLogic 读取「配置中心远程来源」。
//
// 远程来源决定 config.yaml 的远程层从何而来：聚合全部订阅、
// 某一条订阅、某个组合，或某个 mihomo 类型的文件模板。
// 可选项汇总与写入端共用 remoteSourceOptions。
type GetRemoteSourceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRemoteSourceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRemoteSourceLogic {
	return &GetRemoteSourceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRemoteSourceLogic) GetRemoteSource() (resp *types.RemoteSourceResp, err error) {
	src := l.svcCtx.SettingsService.GetRemoteSource()
	return toRemoteSourceResp(src, remoteSourceOptions(l.svcCtx)), nil
}

package protected

import (
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// 远程来源读写两端共用的组装与汇总。
//
// 独立成文件而非放在某一端：读写返回同一形态，且可选项的筛选规则
// （停用仍列出、原样输出型文件排除）只应有一处定义。

func toRemoteSourceResp(src domain.RemoteSource, options []types.RemoteSourceOption) *types.RemoteSourceResp {
	return &types.RemoteSourceResp{
		SourceType:  src.Type,
		SourceId:    src.ID,
		SourceUrl:   src.URL,
		Cron:        src.Cron,
		CronEnabled: src.CronEnabled,
		Options:     options,
	}
}

// remoteSourceOptions 汇总可选来源。
//
// 停用的订阅/组合仍会列出但标记为不可选：直接隐藏会让用户困惑于
// 「我明明建了这个组合，为什么选不到」。文件只列出 mihomo 类型，
// 因为原样输出型文件渲染不出可合并的配置。
func remoteSourceOptions(svcCtx *svc.ServiceContext) []types.RemoteSourceOption {
	out := make([]types.RemoteSourceOption, 0, 16)

	if subs, err := svcCtx.Database.GetSubscriptions(); err == nil {
		for _, s := range subs {
			opt := types.RemoteSourceOption{
				Type: "subscription",
				Id:   s.ID,
				Name: s.Name,
			}
			if s.Enabled != 1 {
				opt.Disabled = true
				opt.Reason = "该订阅已停用"
			}
			out = append(out, opt)
		}
	}

	if cols, err := svcCtx.Database.ListCollections(); err == nil {
		for _, c := range cols {
			opt := types.RemoteSourceOption{
				Type: "collection",
				Id:   c.ID,
				Name: c.Name,
			}
			if c.Enabled != 1 {
				opt.Disabled = true
				opt.Reason = "该组合已停用"
			}
			out = append(out, opt)
		}
	}

	if files, err := svcCtx.Database.ListFiles(); err == nil {
		for _, f := range files {
			// 原样输出型文件不是配置模板，不能作为远程层
			if f.ConfigType != model.FileConfigTypeMihomo {
				continue
			}
			opt := types.RemoteSourceOption{
				Type: "file",
				Id:   f.ID,
				Name: f.Name,
			}
			if f.SourceID <= 0 {
				opt.Disabled = true
				opt.Reason = "该文件模板未指定节点来源"
			}
			out = append(out, opt)
		}
	}

	return out
}

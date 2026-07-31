package protected

import (
	"context"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/substore"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateFileLogic {
	return &UpdateFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateFileLogic) UpdateFile(req *types.SubFileUpdateReq) (resp *types.SubFile, err error) {
	f, err := l.svcCtx.Database.GetFile(req.Id)
	if err != nil {
		return nil, err
	}
	// 只有显式传入的字段才更新；传空串表示清空
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		f.Name = strings.TrimSpace(*req.Name)
	}
	if req.Content != nil {
		f.Content = *req.Content
	}
	if req.Type != nil && *req.Type != "" {
		f.Type = *req.Type
	}
	if req.SyncUrl != nil {
		f.SyncURL = strings.TrimSpace(*req.SyncUrl)
	}
	if req.SourceMode != nil && *req.SourceMode != "" {
		f.SourceMode = *req.SourceMode
	}
	if req.MergeSources != nil {
		f.MergeSources = *req.MergeSources
	}
	if req.IgnoreFailedRemote != nil {
		f.IgnoreFailedRemote = *req.IgnoreFailedRemote
	}
	if req.UserAgent != nil {
		f.UserAgent = strings.TrimSpace(*req.UserAgent)
	}
	if req.ConfigType != nil && *req.ConfigType != "" {
		ct := *req.ConfigType
		if ct != model.FileConfigTypeFile && ct != model.FileConfigTypeMihomo {
			return nil, errInvalid("配置类型只能是 file 或 mihomo")
		}
		f.ConfigType = ct
	}
	if req.SourceType != nil {
		f.SourceType = *req.SourceType
	}
	if req.SourceId != nil {
		f.SourceID = *req.SourceId
	}
	if req.TemplateLang != nil && *req.TemplateLang != "" {
		f.TemplateLang = *req.TemplateLang
	}
	if req.TrafficUrl != nil {
		f.TrafficURL = strings.TrimSpace(*req.TrafficUrl)
	}
	// 校验放在全部字段落定之后：配置类型、正文来源与远程地址可能在
	// 同一次请求里一起改，逐字段校验会误判中间状态
	if f.ConfigType == model.FileConfigTypeMihomo && f.SourceID <= 0 {
		return nil, errInvalid("Mihomo 配置类型必须指定节点来源（单条订阅或组合）")
	}
	if err := validateFileSources(f); err != nil {
		return nil, err
	}
	// 模板语法错误在保存时就暴露，而不是等预览或分享直链才发现
	if f.ConfigType == model.FileConfigTypeMihomo {
		if err := substore.ValidateTemplateLang(f.TemplateLang, f.Content); err != nil {
			return nil, errInvalid(err.Error())
		}
	}
	if err := l.svcCtx.Database.SaveFile(f); err != nil {
		return nil, err
	}
	// 正文或来源变了，直链的渲染缓存必须失效，否则访问分享链接仍是旧内容
	l.svcCtx.RenderService.InvalidateRenderCache()
	out := toFileType(*f)
	return &out, nil
}

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

type CreateFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateFileLogic {
	return &CreateFileLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateFileLogic) CreateFile(req *types.SubFileReq) (resp *types.SubFile, err error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errInvalid("文件名不能为空")
	}
	token, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	configType := firstNonEmpty(req.ConfigType, model.FileConfigTypeFile)
	if configType != model.FileConfigTypeFile && configType != model.FileConfigTypeMihomo {
		return nil, errInvalid("配置类型只能是 file 或 mihomo")
	}
	// mihomo 模板必须有节点来源，否则渲染时才失败、且分享链接已经发出去了
	if configType == model.FileConfigTypeMihomo && req.SourceId <= 0 {
		return nil, errInvalid("Mihomo 配置类型必须指定节点来源（单条订阅或组合）")
	}
	templateLang := firstNonEmpty(req.TemplateLang, model.TemplateLangGo)
	f := &model.SubFile{
		Name: name, Content: req.Content,
		Type: firstNonEmpty(req.Type, "raw"), SyncURL: req.SyncUrl,
		SourceMode:         firstNonEmpty(req.SourceMode, model.FileSourceLocal),
		MergeSources:       req.MergeSources,
		IgnoreFailedRemote: req.IgnoreFailedRemote,
		UserAgent:          strings.TrimSpace(req.UserAgent),
		// 直链凭据，公开端点按此 token 而非文件名寻址
		ShareToken:   token,
		ConfigType:   configType,
		SourceType:   firstNonEmpty(req.SourceType, model.SourceTypeSubscription),
		SourceID:     req.SourceId,
		TemplateLang: templateLang,
		TrafficURL:   strings.TrimSpace(req.TrafficUrl),
	}
	if err := validateFileSources(f); err != nil {
		return nil, err
	}
	// 模板语法错误在保存时就暴露，而不是等预览或分享直链才发现
	if configType == model.FileConfigTypeMihomo {
		if err := substore.ValidateTemplateLang(templateLang, f.Content); err != nil {
			return nil, errInvalid(err.Error())
		}
	}
	if err := l.svcCtx.Database.SaveFile(f); err != nil {
		return nil, err
	}
	out := toFileType(*f)
	return &out, nil
}

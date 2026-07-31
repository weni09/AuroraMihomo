package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/service"
	"auroramihomo/backend/internal/substore"

	"github.com/zeromicro/go-zero/core/logx"
)

// PreviewLogic 预览「当前正在编辑的表单」，不写库。
//
// 与「测试构建」（/collections/:id/build）的区别：后者必须先保存才能看到
// 效果，改一次管道就得落库一次；本接口直接吃表单值，因此新建时也能预览。
type PreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreviewLogic {
	return &PreviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *PreviewLogic) Preview(req *types.PreviewReq) (*types.PreviewResp, error) {
	switch req.Kind {
	case "subscription":
		return l.previewSubscription(req)
	case "collection":
		return l.previewCollection(req)
	case "file":
		return l.previewFile(req)
	default:
		return nil, errInvalid("预览类型只能是 subscription、collection 或 file")
	}
}

// previewSubscription 预览单条订阅。
//
// 跑两遍管道以产出对照：先不带算子得到原始节点，再带算子得到处理结果。
// 两遍都命中同一份缓存或同一次拉取结果，代价只是多一次纯内存的管道执行。
func (l *PreviewLogic) previewSubscription(req *types.PreviewReq) (*types.PreviewResp, error) {
	if strings.TrimSpace(req.Url) == "" && strings.TrimSpace(req.Content) == "" {
		return nil, errInvalid("请先填写订阅地址或粘贴节点内容")
	}
	base := substore.ConvertRequest{
		URL:       strings.TrimSpace(req.Url),
		Content:   req.Content,
		UserAgent: strings.TrimSpace(req.UserAgent),
	}
	target := resolveTarget(req.Target, "mihomo-yaml")
	engine := l.svcCtx.ConfigService.SubStoreEngine()

	original, err := engine.Convert(l.ctx, base, nil, nil, target, "")
	if err != nil {
		return nil, err
	}
	processed, err := engine.Convert(l.ctx, base, loadRewriteRules(l.svcCtx), toEngineOperators(req.Operators), target, "")
	if err != nil {
		return nil, err
	}
	return previewResp(target, original, processed, nil), nil
}

// previewCollection 预览组合。
//
// 成员订阅一律走已缓存的节点，不逐条回源：预览是高频交互动作，
// 每次点击都去打上游既慢又容易被机场限流。
func (l *PreviewLogic) previewCollection(req *types.PreviewReq) (*types.PreviewResp, error) {
	if len(req.SubIds) == 0 {
		return nil, errInvalid("请先选择至少一个底层订阅")
	}
	subs, err := l.svcCtx.Database.GetSubscriptionsByIDs(req.SubIds)
	if err != nil {
		return nil, err
	}
	reqs := make([]substore.ConvertRequest, 0, len(subs))
	for _, s := range subs {
		if s.Enabled != 1 {
			continue
		}
		reqs = append(reqs, substore.ConvertRequest{
			URL:       s.URL,
			Source:    s.Name,
			UserAgent: s.UserAgent,
			CacheRaw:  s.CachedNodes,
			Content:   s.Content,
			Operators: service.DecodeOperators(s.Operators),
		})
	}
	if len(reqs) == 0 {
		return nil, errInvalid("所选订阅均已停用，没有可预览的节点")
	}

	target := resolveTarget(req.Target, "mihomo-yaml")
	engine := l.svcCtx.ConfigService.SubStoreEngine()

	// 原始侧保留各订阅自身的管道：那是订阅的既有属性，
	// 本次编辑的是组合级管道，对照才有意义
	original, err := engine.ConvertMany(l.ctx, reqs, nil, nil, target, "")
	if err != nil {
		return nil, err
	}
	processed, err := engine.ConvertMany(l.ctx, reqs, loadRewriteRules(l.svcCtx), toEngineOperators(req.Operators), target, "")
	if err != nil {
		return nil, err
	}
	return previewResp(target, original, processed, nil), nil
}

// previewFile 预览模板文件的输出。
//
// 复用 RenderService 的正文解析器，保证预览看到的正文与分享链接输出的
// 完全一致——两处各自实现是此前最容易产生分歧的地方。
func (l *PreviewLogic) previewFile(req *types.PreviewReq) (*types.PreviewResp, error) {
	configType := firstNonEmpty(req.ConfigType, model.FileConfigTypeFile)
	// 构造临时文件对象，不落库
	f := &model.SubFile{
		Name:               "预览",
		Content:            req.Content,
		SyncURL:            req.SyncUrl,
		SourceMode:         firstNonEmpty(req.SourceMode, model.FileSourceLocal),
		MergeSources:       req.MergeSources,
		IgnoreFailedRemote: req.IgnoreFailedRemote,
		UserAgent:          strings.TrimSpace(req.UserAgent),
		ConfigType:         configType,
		SourceType:         firstNonEmpty(req.SourceType, model.SourceTypeSubscription),
		SourceID:           req.SourceId,
		TemplateLang:       firstNonEmpty(req.TemplateLang, model.TemplateLangGo),
	}
	if err := validateFileSources(f); err != nil {
		return nil, err
	}

	resolved, err := l.svcCtx.RenderService.FileContent().Resolve(l.ctx, f)
	if err != nil {
		return nil, err
	}

	// 原样输出型文件没有「处理」这一步，前后一致
	if configType != model.FileConfigTypeMihomo {
		return &types.PreviewResp{
			Format:    "file",
			Original:  resolved.Content,
			Processed: resolved.Content,
			Warnings:  resolved.Warnings,
		}, nil
	}

	if f.SourceID <= 0 {
		return nil, errInvalid("Mihomo 配置类型必须指定节点来源（单条订阅或组合）")
	}
	if strings.TrimSpace(resolved.Content) == "" {
		return nil, errInvalid("模板内容为空，无法渲染")
	}

	rendered, err := l.svcCtx.RenderService.RenderFile(l.ctx, f)
	if err != nil {
		return nil, fmt.Errorf("模板渲染失败: %w", err)
	}
	return &types.PreviewResp{
		// 原始侧给出模板正文，处理侧给出套用节点后的渲染结果
		Format:    "mihomo-yaml",
		Original:  resolved.Content,
		Processed: rendered,
		Warnings:  resolved.Warnings,
	}, nil
}

// previewResp 组装对照结果。按目标格式取正文，与分享链接的取法一致。
//
// Format 用解析后的输出格式，而非 ConvertResult.Format——后者是引擎探测到的
// 「上游内容是什么格式」（如 share-links）。预览面板问的是「产出是什么格式」，
// 拿探测值去填会答非所问。
func previewResp(target string, original, processed *substore.ConvertResult, warnings []string) *types.PreviewResp {
	return &types.PreviewResp{
		Format:    target,
		Original:  previewPayload(target, original),
		Processed: previewPayload(target, processed),
		Count:     len(processed.Nodes),
		Warnings:  warnings,
	}
}

// previewPayload 选择用于展示的正文。
// base64/明文链接类目标的结果被渲染进 YAML 字段，直接读 Links 会给出未编码内容。
func previewPayload(target string, res *substore.ConvertResult) string {
	if res == nil {
		return ""
	}
	if res.YAML != "" {
		return res.YAML
	}
	return res.Links
}

// toEngineOperators 把请求中的管道转成引擎可执行形态。
// 与 buildOperators 的区别：后者的入参是数据库里的 JSON 串。
func toEngineOperators(ops []types.PipelineOperator) []substore.PipelineOperator {
	if len(ops) == 0 {
		return nil
	}
	out := make([]substore.PipelineOperator, 0, len(ops))
	for _, o := range ops {
		var payload map[string]interface{}
		// 解析失败时按空载荷执行：算子自身会校验必填项并给出可读错误，
		// 在这里静默丢弃整个算子会让用户以为管道生效了
		_ = jsonUnmarshal([]byte(o.Payload), &payload)
		out = append(out, substore.PipelineOperator{
			Type:    substore.OperatorType(o.Type),
			Enabled: o.Enabled,
			Payload: payload,
		})
	}
	return out
}

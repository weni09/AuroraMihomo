package protected

import (
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/service"
	"auroramihomo/backend/internal/substore"
)

// buildOperators 把数据库里存的 operators JSON 还原成引擎可执行的管道
func buildOperators(operatorsJSON string) []substore.PipelineOperator {
	if operatorsJSON == "" {
		return nil
	}
	var raw []types.PipelineOperator
	if err := jsonUnmarshal([]byte(operatorsJSON), &raw); err != nil {
		return nil
	}
	ops := make([]substore.PipelineOperator, 0, len(raw))
	for _, r := range raw {
		var payload map[string]interface{}
		_ = jsonUnmarshal([]byte(r.Payload), &payload)
		ops = append(ops, substore.PipelineOperator{
			Type:    substore.OperatorType(r.Type),
			Enabled: r.Enabled,
			Payload: payload,
		})
	}
	return ops
}

// collectRequests 组装订阅请求，优先使用数据库缓存节点（秒开关键）
func collectRequests(svcCtx *svc.ServiceContext, collectionID int64) ([]substore.ConvertRequest, error) {
	items, err := svcCtx.Database.ListCollectionItems(collectionID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.SubscriptionID)
	}
	subs, err := svcCtx.Database.GetSubscriptionsByIDs(ids)
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
	return reqs, nil
}

// resolveTarget 把 URL 参数里的目标格式映射到内部模板名
func resolveTarget(target, fallback string) string {
	switch target {
	case "":
		return firstNonEmpty(fallback, "mihomo-yaml")
	case "base64":
		return "base64-links"
	case "clash", "mihomo":
		return "mihomo-yaml"
	case "links", "plain":
		return "share-links"
	default:
		return target
	}
}

var _ = model.SubCollection{}

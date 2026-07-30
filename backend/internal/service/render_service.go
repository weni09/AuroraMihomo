package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/substore"
)

// RenderService 负责把「组合 / 订阅」渲染为指定客户端格式，
// 供分享链接与 Sync 产物共用，避免两处逻辑漂移。
type RenderService struct {
	db     *repository.Database
	engine *substore.Engine

	// 分享端点无需鉴权，且每次请求都会完整跑一遍管道
	// （可能含 DNS 解析与 JS 脚本执行）。并发闸门限制同时渲染的请求数，
	// 短期缓存让重复请求直接命中结果，避免被反复压垮 CPU 与上游。
	renderSem chan struct{}
	cacheMu   sync.Mutex
	cache     map[string]*renderCacheEntry

	// fileContent 解析文件的正文（本地内容与多个远程地址的合并结果）。
	// 直链输出、立即同步、以及「文件作为远程配置来源」共用它，
	// 避免三条路径对同一文件产出不同内容。
	fileContent *FileContentResolver
}

type renderCacheEntry struct {
	res      *ShareResult
	expireAt time.Time
}

const (
	maxConcurrentRenders = 4
	renderCacheTTL       = 30 * time.Second
)

func NewRenderService(db *repository.Database, engine *substore.Engine) *RenderService {
	return &RenderService{
		db:        db,
		engine:    engine,
		renderSem: make(chan struct{}, maxConcurrentRenders),
		cache:     map[string]*renderCacheEntry{},

		fileContent: NewFileContentResolver(),
	}
}

// FileContent 暴露正文解析器，供「立即同步」与预览等路径复用同一套取用规则。
func (s *RenderService) FileContent() *FileContentResolver {
	return s.fileContent
}

// cachedRender 为公开分享端点提供缓存 + 并发闸门。
// key 需覆盖影响输出的全部参数，否则会串味返回错误内容。
func (s *RenderService) cachedRender(ctx context.Context, key string, fn func() (*ShareResult, error)) (*ShareResult, error) {
	if hit := s.lookupCache(key); hit != nil {
		return hit, nil
	}

	// 闸门满时排队等待，而不是无上限地并发跑管道
	select {
	case s.renderSem <- struct{}{}:
		defer func() { <-s.renderSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 排队期间可能已被其他请求填充
	if hit := s.lookupCache(key); hit != nil {
		return hit, nil
	}

	res, err := fn()
	if err != nil {
		return nil, err
	}
	s.storeCache(key, res)
	return res, nil
}

func (s *RenderService) lookupCache(key string) *ShareResult {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	e, ok := s.cache[key]
	if !ok {
		return nil
	}
	if time.Now().After(e.expireAt) {
		delete(s.cache, key)
		return nil
	}
	return e.res
}

func (s *RenderService) storeCache(key string, res *ShareResult) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	// 顺带清理过期项，避免 map 随不同 token/filter 组合无上限膨胀
	now := time.Now()
	for k, v := range s.cache {
		if now.After(v.expireAt) {
			delete(s.cache, k)
		}
	}
	s.cache[key] = &renderCacheEntry{res: res, expireAt: now.Add(renderCacheTTL)}
}

// InvalidateRenderCache 在订阅/组合变更后清空缓存，避免用户改完看到旧结果
func (s *RenderService) InvalidateRenderCache() {
	s.cacheMu.Lock()
	s.cache = map[string]*renderCacheEntry{}
	s.cacheMu.Unlock()
}

// persistedOperator 与前端/DB 中存储的算子结构一致（payload 为原始 JSON 串）
type persistedOperator struct {
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	Payload string `json:"payload"`
}

// DecodeOperators 还原持久化的处理管道
func DecodeOperators(raw string) []substore.PipelineOperator {
	if raw == "" {
		return nil
	}
	var items []persistedOperator
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	ops := make([]substore.PipelineOperator, 0, len(items))
	for _, it := range items {
		var payload map[string]interface{}
		_ = json.Unmarshal([]byte(it.Payload), &payload)
		ops = append(ops, substore.PipelineOperator{
			Type:    substore.OperatorType(it.Type),
			Enabled: it.Enabled,
			Payload: payload,
		})
	}
	return ops
}

// RenderCollection 渲染组合订阅。target 为空时使用组合自身的模板设置。
func (s *RenderService) RenderCollection(ctx context.Context, id int64, target string) (string, error) {
	c, err := s.db.GetCollection(id)
	if err != nil {
		return "", err
	}
	return s.renderCollection(ctx, c, target, nil)
}

// ShareResult 是一次公开分享请求的产物
type ShareResult struct {
	Body   string
	Target string
	// Traffic 为上游机场的流量信息，用于回写 subscription-userinfo 响应头，
	// 让 Clash Verge / Stash 等客户端能显示剩余流量
	Traffic *model.Subscription
}

// RenderByToken 供公开分享链接使用，token 可以是组合的也可以是单条订阅的。
// extraFilter 非空时在既有管道末尾追加一个 keep 过滤，实现 ?filter= 的临时筛选。
func (s *RenderService) RenderByToken(ctx context.Context, token, target, extraFilter string) (*ShareResult, error) {
	// 缓存键带上数据版本（updated_at）：订阅/组合一经修改旧键自然失效，
	// 无需在每个写入入口手工清缓存（漏一处就会对外返回陈旧配置）。
	cacheKey := token + "|" + target + "|" + extraFilter + "|" + s.dataVersion(token)
	return s.cachedRender(ctx, cacheKey, func() (*ShareResult, error) {
		return s.renderByTokenUncached(ctx, token, target, extraFilter)
	})
}

// dataVersion 返回该 token 对应记录的变更标记。
// 查不到时返回空串，退化为"仅按 token 缓存"，不影响功能正确性。
func (s *RenderService) dataVersion(token string) string {
	if c, err := s.db.GetCollectionByToken(token); err == nil && c != nil {
		return "c" + strconv.FormatInt(c.UpdatedAt.UnixNano(), 10)
	}
	if sub, err := s.db.GetSubscriptionByToken(token); err == nil && sub != nil {
		// 订阅刷新会更新 updated_at，节点缓存的变化也就体现在版本里
		return "s" + strconv.FormatInt(sub.UpdatedAt.UnixNano(), 10)
	}
	return ""
}

// ErrShareExpired 表示分享链接已过有效期。
// 单独定义以便上层回 410 Gone 而非 404，让用户能区分
// 「链接写错了」和「链接过期了」。
var ErrShareExpired = errors.New("分享链接已过期")

// shareExpired 判断有效期是否已过。零值表示永不过期。
func shareExpired(expiresAt time.Time) bool {
	return !expiresAt.IsZero() && time.Now().After(expiresAt)
}

func (s *RenderService) renderByTokenUncached(ctx context.Context, token, target, extraFilter string) (*ShareResult, error) {
	extra := extraFilterOps(extraFilter)

	if c, err := s.db.GetCollectionByToken(token); err == nil {
		// 组合被禁用后其分享链接应当立即失效，否则「禁用」形同虚设
		if c.Enabled != 1 {
			return nil, fmt.Errorf("组合 %s 已禁用", c.Name)
		}
		if shareExpired(c.ShareExpiresAt) {
			return nil, ErrShareExpired
		}
		effective := ResolveTarget(target, "mihomo-yaml")
		body, err := s.renderCollection(ctx, c, target, extra)
		if err != nil {
			return nil, err
		}
		return &ShareResult{Body: body, Target: effective, Traffic: s.collectionTraffic(c.ID)}, nil
	}

	// 其次尝试单条订阅的分享
	if sub, err := s.db.GetSubscriptionByToken(token); err == nil {
		if sub.Enabled != 1 {
			return nil, fmt.Errorf("订阅 %s 已禁用", sub.Name)
		}
		if shareExpired(sub.ShareExpiresAt) {
			return nil, ErrShareExpired
		}
		effective := ResolveTarget(target, "mihomo-yaml")
		body, err := s.renderSubscription(ctx, sub, effective, extra)
		if err != nil {
			return nil, err
		}
		return &ShareResult{Body: body, Target: effective, Traffic: sub}, nil
	}

	// 最后尝试文件模板的分享
	f, err := s.db.GetFileByToken(token)
	if err != nil {
		return nil, err
	}
	if shareExpired(f.ShareExpiresAt) {
		return nil, ErrShareExpired
	}
	body, err := s.RenderFile(ctx, f)
	if err != nil {
		return nil, err
	}
	// 原样输出型文件没有格式概念，按纯文本处理；
	// mihomo 模板型则声明为 mihomo-yaml，让客户端按 YAML 解析。
	effective := "file"
	if f.ConfigType == model.FileConfigTypeMihomo {
		effective = "mihomo-yaml"
	}
	return &ShareResult{Body: body, Target: effective, Traffic: s.fileTraffic(f)}, nil
}

func extraFilterOps(extraFilter string) []substore.PipelineOperator {
	if strings.TrimSpace(extraFilter) == "" {
		return nil
	}
	return []substore.PipelineOperator{{
		Type:    substore.OpFilter,
		Enabled: true,
		Payload: map[string]interface{}{"action": "keep", "pattern": extraFilter},
	}}
}

// collectionTraffic 取组合下第一条带流量信息的订阅作为整体流量展示，
// 多机场组合无法合并计量，取首个有效值是各客户端的通行做法。
func (s *RenderService) collectionTraffic(collectionID int64) *model.Subscription {
	items, err := s.db.ListCollectionItems(collectionID)
	if err != nil {
		return nil
	}
	for _, it := range items {
		subs, err := s.db.GetSubscriptionsByIDs([]int64{it.SubscriptionID})
		if err != nil || len(subs) == 0 {
			continue
		}
		if subs[0].Total > 0 || subs[0].Expire > 0 {
			return &subs[0]
		}
	}
	return nil
}

func (s *RenderService) renderCollection(ctx context.Context, c *model.SubCollection, target string, extra []substore.PipelineOperator) (string, error) {
	items, err := s.db.ListCollectionItems(c.ID)
	if err != nil {
		return "", err
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.SubscriptionID)
	}
	subs, err := s.db.GetSubscriptionsByIDs(ids)
	if err != nil {
		return "", err
	}
	// SQL 的 IN 查询不保证顺序，按 items 中的优先级重排，
	// 否则用户设置的订阅优先级在输出中不生效。
	subs = orderByIDs(subs, ids)

	// 复用数据库中缓存的节点，避免每次渲染都回源
	reqs := make([]substore.ConvertRequest, 0, len(subs))
	for _, sub := range subs {
		if sub.Enabled != 1 {
			continue
		}
		reqs = append(reqs, substore.ConvertRequest{
			URL:       sub.URL,
			Source:    sub.Name,
			UserAgent: sub.UserAgent,
			CacheRaw:  sub.CachedNodes,
			Content:   sub.Content,
			Operators: DecodeOperators(sub.Operators),
		})
	}
	if len(reqs) == 0 {
		return "", fmt.Errorf("组合 %s 下没有启用的订阅源", c.Name)
	}

	target = ResolveTarget(target, "mihomo-yaml")

	ops := append(DecodeOperators(c.Operators), extra...)
	res, err := s.engine.ConvertMany(ctx, reqs, s.RewriteRules(), ops, target, "")
	if err != nil {
		return "", err
	}
	return pickPayload(target, res), nil
}

// orderByIDs 按给定的 id 顺序重排订阅
func orderByIDs(subs []model.Subscription, ids []int64) []model.Subscription {
	index := make(map[int64]model.Subscription, len(subs))
	for _, s := range subs {
		index[s.ID] = s
	}
	out := make([]model.Subscription, 0, len(subs))
	for _, id := range ids {
		if s, ok := index[id]; ok {
			out = append(out, s)
			delete(index, id)
		}
	}
	return out
}

// RewriteRules 返回渲染时应用的改写规则。
//
// 「全局改写规则」已移除（对所有订阅隐式生效、难以排查），
// 改写统一由订阅/组合各自的处理管道承担，因此这里恒为空。
// 保留该方法是为了让分享与「测试构建」两条路径继续共用同一入口，
// 将来若接入按订阅维度的改写规则，只需改这一处。
func (s *RenderService) RewriteRules() []substore.RewriteRule {
	return nil
}

// RenderSubscription 渲染单条订阅
func (s *RenderService) RenderSubscription(ctx context.Context, id int64, target string) (string, error) {
	subs, err := s.db.GetSubscriptionsByIDs([]int64{id})
	if err != nil {
		return "", err
	}
	if len(subs) == 0 {
		return "", fmt.Errorf("订阅 %d 不存在", id)
	}
	return s.renderSubscription(ctx, &subs[0], ResolveTarget(target, "mihomo-yaml"), nil)
}

func (s *RenderService) renderSubscription(ctx context.Context, sub *model.Subscription, target string, extra []substore.PipelineOperator) (string, error) {
	res, err := s.engine.ConvertMany(ctx, []substore.ConvertRequest{{
		URL:       sub.URL,
		Source:    sub.Name,
		UserAgent: sub.UserAgent,
		CacheRaw:  sub.CachedNodes,
		Content:   sub.Content,
		Operators: DecodeOperators(sub.Operators),
	}}, s.RewriteRules(), extra, target, "")
	if err != nil {
		return "", err
	}
	return pickPayload(target, res), nil
}

// pickPayload 按目标格式选择正文。
// 注意 RenderTemplate 已把 base64-links / share-links 的结果写入 YAML 字段，
// res.Links 始终是未编码的明文链接，直接返回它会让 base64 目标失效。
func pickPayload(target string, res *substore.ConvertResult) string {
	return res.YAML
}

// ResolveTarget 归一化输出格式别名。
// 分享链接与内部构建共用同一份表，避免同一别名在两条路径下行为不同。
func ResolveTarget(target, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "":
		if fallback == "" {
			return "mihomo-yaml"
		}
		return fallback
	case "base64":
		return "base64-links"
	case "clash", "mihomo":
		return "mihomo-yaml"
	case "links", "plain":
		return "share-links"
	case "qx":
		return "quantumultx"
	case "singbox":
		return "sing-box"
	case "surge-mac":
		return "surgemac"
	default:
		return strings.ToLower(strings.TrimSpace(target))
	}
}

// ContentTypeFor 返回目标格式对应的 HTTP Content-Type
func ContentTypeFor(target string) string {
	switch target {
	// file 为原样输出的托管文件（规则片段、Surge 模块等），
	// 内容形态不定，按纯文本返回最安全
	case "file",
		"base64-links", "share-links",
		"surge", "surgemac", "loon", "quantumultx", "surfboard", "shadowrocket":
		return "text/plain; charset=utf-8"
	case "sing-box", "v2ray", "json":
		return "application/json; charset=utf-8"
	default:
		return "text/yaml; charset=utf-8"
	}
}

// ---- 文件模板 ----

// RenderFile 渲染一个文件的对外内容。
//
// 两种配置类型：
//   - file：原样输出文件内容（规则片段、Surge 模块等）
//   - mihomo：把文件内容当作 Go 模板，套用其指定订阅来源的节点渲染
//
// 这是文件直链与「文件作为远程配置来源」共用的唯一入口，
// 避免直链看到的内容与实际参与合并的内容不一致。
func (s *RenderService) RenderFile(ctx context.Context, f *model.SubFile) (string, error) {
	if f == nil {
		return "", fmt.Errorf("文件不存在")
	}

	// 正文可能来自本地编辑器、若干远程地址，或两者的合并结果。
	// 无论哪种配置类型都先解析正文，再决定是原样输出还是当模板渲染。
	resolved, err := s.fileContent.Resolve(ctx, f)
	if err != nil {
		return "", err
	}

	// 未显式设置（存量数据）视为原样输出，保持升级前的行为
	if f.ConfigType != model.FileConfigTypeMihomo {
		return resolved.Content, nil
	}
	if strings.TrimSpace(resolved.Content) == "" {
		return "", fmt.Errorf("文件「%s」的模板内容为空", f.Name)
	}

	reqs, err := s.fileSourceRequests(f)
	if err != nil {
		return "", err
	}
	// 只需要管道/去重/改写跑完后的节点列表，渲染交给 RenderMihomoOverride
	// 按 f.TemplateLang 决定用 Go 模板 / YAML 覆写 / JS 脚本覆写
	res, err := s.engine.ConvertMany(ctx, reqs, s.RewriteRules(), nil, "noop", "")
	if err != nil {
		return "", err
	}
	return substore.RenderMihomoOverride(f.TemplateLang, resolved.Content, res.Nodes)
}

// RenderFileTemplate 按 ID 渲染文件模板，供「文件作为远程配置来源」使用。
func (s *RenderService) RenderFileTemplate(ctx context.Context, id int64) (string, error) {
	f, err := s.db.GetFile(id)
	if err != nil {
		return "", err
	}
	if f.ConfigType != model.FileConfigTypeMihomo {
		return "", fmt.Errorf("文件「%s」的配置类型不是 Mihomo 配置", f.Name)
	}
	return s.RenderFile(ctx, f)
}

// fileSourceRequests 组装文件模板的节点来源请求。
// 来源可以是单条订阅，也可以是一个组合（取其下全部启用订阅）。
func (s *RenderService) fileSourceRequests(f *model.SubFile) ([]substore.ConvertRequest, error) {
	if f.SourceID <= 0 {
		return nil, fmt.Errorf("文件「%s」未指定节点来源", f.Name)
	}

	var subs []model.Subscription
	switch f.SourceType {
	case model.SourceTypeCollection:
		items, err := s.db.ListCollectionItems(f.SourceID)
		if err != nil {
			return nil, err
		}
		ids := make([]int64, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.SubscriptionID)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("组合(%d)下没有订阅", f.SourceID)
		}
		got, err := s.db.GetSubscriptionsByIDs(ids)
		if err != nil {
			return nil, err
		}
		// IN 查询不保证顺序，按组合内的优先级重排
		subs = orderByIDs(got, ids)
	default:
		// 未指定或 subscription：按单条订阅处理
		got, err := s.db.GetSubscriptionsByIDs([]int64{f.SourceID})
		if err != nil {
			return nil, err
		}
		subs = got
	}

	reqs := make([]substore.ConvertRequest, 0, len(subs))
	for _, sub := range subs {
		if sub.Enabled != 1 {
			continue
		}
		reqs = append(reqs, substore.ConvertRequest{
			URL:       sub.URL,
			Source:    sub.Name,
			UserAgent: sub.UserAgent,
			CacheRaw:  sub.CachedNodes,
			Content:   sub.Content,
			Operators: DecodeOperators(sub.Operators),
		})
	}
	if len(reqs) == 0 {
		return nil, fmt.Errorf("文件「%s」的节点来源下没有启用的订阅", f.Name)
	}
	return reqs, nil
}

// fileTraffic 返回文件分享应上报的流量信息。
//
// 文件模板本身没有流量概念，所以取其节点来源的订阅流量：
// 单条订阅直接用它，组合则复用「取首个有流量数据的订阅」的既有做法。
// 这样 Clash Verge / Stash 等客户端订阅该文件时仍能看到剩余流量。
func (s *RenderService) fileTraffic(f *model.SubFile) *model.Subscription {
	if f == nil || f.SourceID <= 0 {
		return nil
	}
	if f.SourceType == model.SourceTypeCollection {
		return s.collectionTraffic(f.SourceID)
	}
	subs, err := s.db.GetSubscriptionsByIDs([]int64{f.SourceID})
	if err != nil || len(subs) == 0 {
		return nil
	}
	if subs[0].Total > 0 || subs[0].Expire > 0 {
		return &subs[0]
	}
	return nil
}

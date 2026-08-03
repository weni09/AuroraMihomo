package service

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/engine"
	"auroramihomo/backend/internal/fetcher"
	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/netcheck"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/substore"

	"github.com/zeromicro/go-zero/core/logx"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	_ "embed"
)

// mergedBaseFingerprintKey 记录「最近一次成功合并时 base 内容的哈希」。
// 界面据此推导本地配置是否未合并（见 BaseUnmerged）：base 一旦保存而未被
// 合并，哈希必然与记录不符，无需在每条写 base 路径上单独置位；该键
// 持久化在 settings 表，服务重启、换浏览器都不丢。
const mergedBaseFingerprintKey = "merged_base_fingerprint"

type ConfigService struct {
	// applyMu 串行化配置合并与恢复。定时任务与手动触发可能并发，
	// 若不互斥会导致备份互相覆盖、临时文件写入交错、config.yaml 损坏。
	applyMu sync.Mutex

	db        *repository.Database
	engine    *engine.MergeEngine
	mihomo    mihomo.Manager
	substore  substore.Manager
	ssEngine  *substore.Engine
	fetcher   *fetcher.Client
	configDir string
	logger    logx.Logger

	// diffMu 保护 lastDiff。合并在 applyMu 下写它，而 GET /config/diff
	// 会并发读，无锁会读到撕裂的 slice header 导致越界 panic。
	diffMu   sync.RWMutex
	lastDiff domain.DiffReport

	// policyProvider 供外部注入用户配置的合并策略（设计 §16）
	policyProvider func() domain.MergePolicy

	// remoteSourceProvider 提供用户选定的远程配置来源。
	// 未注入时回落为"聚合所有启用订阅"，即历史默认行为。
	remoteSourceProvider func() domain.RemoteSource

	// renderCollection / renderFile 由外部注入（RenderService 在本服务之后
	// 构造且依赖本服务的 substore 引擎，直接持有会形成循环依赖）。
	// 返回渲染好的 mihomo YAML。
	renderCollection func(ctx context.Context, id int64) (string, error)
	renderFile       func(ctx context.Context, id int64) (string, error)

	// tproxyManagedProvider 报告宿主上的 TProxy 防火墙规则是否由本面板下发。
	//
	// 合并流程需要它来决定是否注入 TProxy 的技术参数：只配了 tproxy-port
	// 不构成启用 TProxy（把流量引到该端口的规则不在配置里，见 netcheck.Inject），
	// 那种情况下注入 routing-mark 只会在用户配置里留下一个无用且误导的字段——
	// 它的唯一用途是配合面板下发的防火墙规则放行内核自身流量。
	//
	// 用注入而不是在这里直接读 settings 表：那个键归 TransparentService 所有，
	// 键名散落到两个包里会让将来改动漏掉一处。未注入时回落为 false，
	// 即"未托管"——那是不会擅自改用户配置的安全一侧。
	tproxyManagedProvider func() bool

	// transparentResyncFn 在配置生效后让透明代理的防火墙规则跟上新配置。
	//
	// 规则里烧进了 tproxy-port / DNS 端口 / 内核 API 端口，而这些值用户随时能在
	// 配置中心改。少了这个回调，改完端口只有 config.yaml 变了、防火墙还是旧的。
	// 同样用注入打破循环依赖（理由见 tproxyManagedProvider）。
	// 返回 error 供需要如实上报的调用方使用；合并流程末尾仍忽略错误。
	transparentResyncFn func(ctx context.Context) error
}

// SetTransparentResyncFn 注入"让防火墙规则跟上当前配置"的回调。
//
// 用注入而不是直接持有 TransparentService：那个服务在本服务之后构造且依赖本服务
// （读写 base.yaml、读端口），直接互持会形成循环依赖。
func (s *ConfigService) SetTransparentResyncFn(fn func(ctx context.Context) error) {
	s.transparentResyncFn = fn
}

// resyncTransparent 在配置生效后让防火墙规则跟上。
//
// 未注入时是空操作：非 Linux 平台、或透明代理未启用的部署里没有规则要同步。
// 刻意吞掉一切失败（回调内部自己记日志）：配置合并本身已经成功了，
// 规则同步失败不该让"保存配置"这个操作报错——那会让用户以为配置没保存。
func (s *ConfigService) resyncTransparent(ctx context.Context) {
	if s.transparentResyncFn == nil {
		return
	}
	_ = s.transparentResyncFn(ctx)
}

// SetTProxyManagedProvider 注入"TProxy 规则是否由本面板下发"的判据来源。
//
// 单独用 setter 而不进构造函数：TransparentService 在本服务之后构造
// （它依赖本服务读写 base.yaml），构造期还拿不到。
func (s *ConfigService) SetTProxyManagedProvider(fn func() bool) {
	s.tproxyManagedProvider = fn
}

// tproxyManaged 供合并流程判断是否注入 TProxy 技术参数。
func (s *ConfigService) tproxyManaged() bool {
	if s.tproxyManagedProvider == nil {
		return false
	}
	return s.tproxyManagedProvider()
}

// adguardOwnsSystemDNS 表示 AdGuard 正作为系统 DNS 入口（绑 :53 或已弱化 TUN hijack）。
// 合并注入时不得再补 tun.dns-hijack: any:53，否则查询绕过 AGH。
func (s *ConfigService) adguardOwnsSystemDNS() bool {
	if s.db == nil {
		return false
	}
	mode, err := s.db.GetSetting(settingAdGuardDNSMode)
	if err == nil {
		mode = strings.TrimSpace(mode)
		if mode == "1" {
			return true
		}
	}
	// 入口/wiring 快照：DidBind53 或 DidWeakenTUN 说明曾为 AGH 清空 hijack
	raw, err := s.db.GetSetting(settingAdGuardSnapshot)
	if err != nil || strings.TrimSpace(raw) == "" {
		return false
	}
	plan, err := unmarshalWiringSnapshot(raw)
	if err != nil {
		return false
	}
	return plan.DidBind53 || plan.DidWeakenTUN
}

// SetPolicyProvider 注入合并策略来源，未注入时使用引擎默认策略
func (s *ConfigService) SetPolicyProvider(fn func() domain.MergePolicy) {
	s.policyProvider = fn
}

// SetRemoteSourceProvider 注入远程来源选择，未注入时聚合所有启用订阅
func (s *ConfigService) SetRemoteSourceProvider(fn func() domain.RemoteSource) {
	s.remoteSourceProvider = fn
}

// SetRenderers 注入组合与文件模板的渲染入口。
// 这两类来源的产出必须是 mihomo YAML，否则无法参与配置合并。
func (s *ConfigService) SetRenderers(
	collection func(ctx context.Context, id int64) (string, error),
	file func(ctx context.Context, id int64) (string, error),
) {
	s.renderCollection = collection
	s.renderFile = file
}

// remoteSource 返回当前生效的远程来源，非法配置回落为默认值（none）。
//
// 回落而非报错：设置可能因所选实体被删、手工改库或版本回退而失效，
// 此时以「仅本地配置」产出可用结果，比让整个合并失败更稳妥——
// 用户的本地配置仍能生效，不会连内核都起不来。
func (s *ConfigService) remoteSource() domain.RemoteSource {
	if s.remoteSourceProvider == nil {
		return domain.DefaultRemoteSource()
	}
	src := s.remoteSourceProvider()
	if !src.Valid() {
		s.logger.Errorf("远程来源配置非法(type=%s id=%d)，回落为不使用远程配置", src.Type, src.ID)
		return domain.DefaultRemoteSource()
	}
	return src
}

type MergeApplyResult struct {
	Message       string
	ConflictCount int
	Diff          domain.DiffReport
	// Warnings 是合并时自动做出的修正说明（如摘掉指向已下线节点的引用），
	// 必须回报给用户，否则他会困惑于「我配的策略组怎么不见了」
	Warnings []string
}

func NewConfigService(
	db *repository.Database,
	eng *engine.MergeEngine,
	mgr mihomo.Manager,
	ss substore.Manager,
	configDir string,
) *ConfigService {
	return &ConfigService{
		db:        db,
		engine:    eng,
		mihomo:    mgr,
		substore:  ss,
		ssEngine:  substore.NewEngine(),
		fetcher:   fetcher.New(30 * time.Second),
		configDir: configDir,
		logger:    logx.WithContext(context.Background()),
	}
}

func (s *ConfigService) configPath() string {
	return filepath.Join(s.configDir, "config.yaml")
}

// ConfigExists 判断磁盘上是否已有生成过的 config.yaml，
// 供启动流程判断本次是首次生成还是常规刷新。
func (s *ConfigService) ConfigExists() bool {
	_, err := os.Stat(s.configPath())
	return err == nil
}

func (s *ConfigService) backupDir() string {
	return filepath.Join(s.configDir, "backups")
}

// default_base.yaml 是开箱默认基础配置：从真实部署提炼并去掉个人数据
// （内网 DNS、设备 SRC-IP、订阅/节点等）。详见文件头注释。
//
//go:embed default_base.yaml
var defaultBaseYAMLFile string

func (s *ConfigService) defaultBaseYAML() string {
	// embed 可能带尾部空白；保持与手写 YAML 一致的可读性
	return strings.TrimSpace(defaultBaseYAMLFile) + "\n"
}

// EnsureDefaultBase 在库中尚无 base 配置时写入开箱默认值。
//
// 只在「完全没有 base」时落库：已有任意内容（含用户清空后的占位）都不覆盖，
// 避免升级/迁移误伤。新装部署走这里即可在配置中心看到完整可编辑默认。
func (s *ConfigService) EnsureDefaultBase() error {
	cfg, err := s.db.GetConfigByType("base")
	if err == nil && cfg != nil && strings.TrimSpace(cfg.Content) != "" {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	s.logger.Info("未检测到基础配置，写入开箱默认 base（default_base.yaml）")
	return s.UpdateBaseConfig(s.defaultBaseYAML())
}

func (s *ConfigService) loadBaseYAML() (string, error) {
	cfg, err := s.db.GetConfigByType("base")
	if err == nil && cfg != nil && cfg.Content != "" {
		return cfg.Content, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	return s.defaultBaseYAML(), nil
}

// loadRewriteRules 曾从「全局改写规则」表读取跨订阅生效的改写规则。
//
// 全局规则已移除：它对所有订阅隐式生效，用户在单条订阅上看不到它的存在，
// 节点被改名或过滤后极难定位原因。改写需求现由各订阅/组合自身的
// 处理管道（PipelineOperator）承担，作用范围明确可见。
//
// 这里返回 nil 而非删除函数：substore 引擎的 rules 参数是通用能力
// （仍被 ApplyRewrite 与其测试使用），保留调用点便于将来接入
// 「按订阅维度的改写规则」，也避免大面积改动引擎签名。
func (s *ConfigService) loadRewriteRules() []substore.RewriteRule {
	return nil
}

// persistSubscriptionCache 把转换结果里的节点缓存与流量信息落库。
//
// cached_nodes 供分享链路（RenderService）直接复用，免得每次访问分享
// 链接都回源上游；流量信息来自机场下发的 subscription-userinfo。
// 由「刷新缓存」与配置合并两条路径共用，避免两处各写一遍而产生分歧。
func (s *ConfigService) persistSubscriptionCache(subID int64, res *substore.ConvertResult) {
	if res == nil {
		return
	}
	updates := map[string]interface{}{}
	if res.RawNodesJSON != "" {
		updates["cached_nodes"] = res.RawNodesJSON
	}
	if !res.UserInfo.IsZero() {
		updates["upload"] = res.UserInfo.Upload
		updates["download"] = res.UserInfo.Download
		updates["total"] = res.UserInfo.Total
		updates["expire"] = res.UserInfo.Expire
	}
	if len(updates) == 0 {
		return
	}
	if err := s.db.DB.Model(&model.Subscription{}).Where("id = ?", subID).Updates(updates).Error; err != nil {
		s.logger.Errorf("写入订阅(%d)节点缓存失败: %v", subID, err)
	}
}

// fetchRemoteYAML 取得单条订阅的节点 YAML。
// useCache 为 true 时优先复用已缓存的节点，不回源上游 —— 用于"只刷新某一条订阅，
// 其余订阅仍需参与合并"的场景，避免无谓地重拉所有机场。
func (s *ConfigService) fetchRemoteYAML(ctx context.Context, sub model.Subscription, useCache bool) ([]byte, error) {
	cacheRaw := ""
	if useCache {
		cacheRaw = sub.CachedNodes
	}
	// Prefer Go native engine.
	res, err := s.ssEngine.Convert(ctx, substore.ConvertRequest{
		URL:       sub.URL,
		Content:   sub.Content, // 手动粘贴的节点无需回源
		Source:    sub.Name,
		UserAgent: sub.UserAgent,
		CacheRaw:  cacheRaw,
	}, s.loadRewriteRules(), DecodeOperators(sub.Operators), "mihomo-yaml", "")

	if err == nil && strings.TrimSpace(res.YAML) != "" {
		s.persistSubscriptionCache(sub.ID, res)
		// Sub-Store 管道只输出节点，会丢掉订阅原有的顶层参数。
		// 这里把可安全采纳的部分（已剔除管理接口等敏感键）拼回去，
		// 让"远程优先"策略对 mode / dns / tun 等运行参数真正生效。
		return mergeUpstreamParams([]byte(res.YAML), res.UpstreamParams), nil
	}
	// 订阅 URL 通常内嵌机场账户 token，不能进日志
	s.logger.Errorf("订阅 %s(%d) 转换失败: %v", sub.Name, sub.ID, err)

	// Fallback to basic fetcher
	if s.substore != nil {
		data, err2 := s.substore.FetchAndConvert(ctx, sub.URL)
		if err2 == nil && len(data) > 0 {
			return data, nil
		}
	}
	return s.fetcher.FetchWithUA(ctx, sub.URL, sub.UserAgent)
}

// buildRemoteConfig 按用户选定的来源构建远程配置层，并写入聚合行。
//
// 来源见 domain.RemoteSource：
//   - none：不使用远程配置（默认）
//   - subscription：只用指定的一条订阅
//   - collection：用组合的渲染结果，组合上的管道与模板一并生效
//   - file：用文件模板分享的渲染结果
//   - url：直接使用外部订阅链接
//   - all：聚合所有启用的订阅（已从界面移除，仅为存量数据保留）
//
// onlyID 与来源是两个正交概念：它只表示"这一条订阅需要回源刷新"，
// 供「刷新单条订阅」使用，不影响来源选择。
func (s *ConfigService) buildRemoteConfig(ctx context.Context, onlyID int64) error {
	src := s.remoteSource()
	switch src.Type {
	case domain.RemoteSourceNone:
		return s.clearRemoteConfig()
	case domain.RemoteSourceURL:
		return s.buildRemoteFromURL(ctx, src.URL)
	case domain.RemoteSourceCollection:
		return s.buildRemoteFromRenderer(ctx, "组合", src.ID, s.renderCollection)
	case domain.RemoteSourceFile:
		return s.buildRemoteFromRenderer(ctx, "文件模板", src.ID, s.renderFile)
	case domain.RemoteSourceSubscription:
		return s.buildRemoteFromSubscriptions(ctx, onlyID, src.ID)
	case domain.RemoteSourceAll:
		return s.buildRemoteFromSubscriptions(ctx, onlyID, 0)
	default:
		// remoteSource() 已做校验并回落，正常到不了这里
		return s.clearRemoteConfig()
	}
}

// clearRemoteConfig 写入空的远程聚合层，使后续合并走「仅本地配置」路径。
//
// 内容留空而非 "proxies: []"：MergeAndApplyDetailed 用 TrimSpace 判空来决定
// 是否给引擎传 nil 远程层，写成空数组会被当作「确有远程内容但节点为空」，
// 从而把本地的 proxies 抹掉。
func (s *ConfigService) clearRemoteConfig() error {
	return s.db.SaveConfig(&model.Config{
		Name:    repository.RemoteMergedConfigName,
		Type:    "remote",
		Content: "",
		Version: int(time.Now().Unix()),
	})
}

// buildRemoteFromURL 直接抓取外部分享的订阅链接作为远程层。
//
// 与「先建订阅再选」相比，这条路径不落库、不参与订阅的定时刷新，
// 每次合并都重新抓取，适合一次性使用别人分享的地址。
func (s *ConfigService) buildRemoteFromURL(ctx context.Context, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("外部订阅链接为空")
	}

	// 先走 Sub-Store 引擎：外部链接可能是 base64 / 明文分享链接 / Clash YAML，
	// 引擎会统一解析成节点再输出 mihomo 配置。
	res, err := s.ssEngine.Convert(ctx, substore.ConvertRequest{
		URL:    rawURL,
		Source: "external",
	}, s.loadRewriteRules(), nil, "mihomo-yaml", "")
	if err == nil && strings.TrimSpace(res.YAML) != "" {
		data := mergeUpstreamParams([]byte(res.YAML), res.UpstreamParams)
		if _, perr := s.engine.LoadAndParse(data); perr == nil {
			return s.db.SaveConfig(&model.Config{
				Name:    repository.RemoteMergedConfigName,
				Type:    "remote",
				Content: string(data),
				Version: int(time.Now().Unix()),
			})
		}
	}
	if err != nil {
		// 链接可能内嵌机场 token，不能进日志
		s.logger.Errorf("外部订阅链接转换失败: %v", err)
	}

	// 回退为直接抓取原文：链接本身可能已经是一份完整的 mihomo 配置
	data, err := s.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return fmt.Errorf("抓取外部订阅链接失败: %w", err)
	}
	if _, err := s.engine.LoadAndParse(data); err != nil {
		return fmt.Errorf("外部订阅链接的内容不是合法的 Mihomo 配置: %w", err)
	}
	return s.db.SaveConfig(&model.Config{
		Name:    repository.RemoteMergedConfigName,
		Type:    "remote",
		Content: string(data),
		Version: int(time.Now().Unix()),
	})
}

// buildRemoteFromRenderer 用组合/文件模板的渲染结果作为远程层。
// 渲染失败时直接返回错误、不写空聚合行，避免把可用的旧配置覆盖掉。
func (s *ConfigService) buildRemoteFromRenderer(
	ctx context.Context,
	kind string,
	id int64,
	render func(ctx context.Context, id int64) (string, error),
) error {
	if render == nil {
		return fmt.Errorf("%s 渲染入口未注入，无法作为远程来源", kind)
	}
	body, err := render(ctx, id)
	if err != nil {
		return fmt.Errorf("渲染%s(%d)失败: %w", kind, id, err)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%s(%d) 渲染结果为空", kind, id)
	}
	// 必须能被解析为 mihomo 配置，否则后续合并会拿到垃圾内容。
	// 组合/文件的输出模板可能被设成 base64/明文链接等非 YAML 格式，
	// 这里如实报错，提示用户改用 mihomo-yaml 模板。
	if _, err := s.engine.LoadAndParse([]byte(body)); err != nil {
		return fmt.Errorf("%s(%d) 的输出不是合法的 mihomo 配置（请确认输出模板为 Mihomo YAML）: %w", kind, id, err)
	}
	return s.db.SaveConfig(&model.Config{
		Name:    repository.RemoteMergedConfigName,
		Type:    "remote",
		Content: body,
		Version: int(time.Now().Unix()),
	})
}

// buildRemoteFromSubscriptions 用订阅作为远程层。
// onlySubID > 0 时只采用该条订阅，否则聚合所有启用订阅。
func (s *ConfigService) buildRemoteFromSubscriptions(ctx context.Context, onlyID, onlySubID int64) error {
	subs, err := s.db.GetSubscriptions()
	if err != nil {
		return err
	}
	var remoteYAMLs [][]byte
	var lastErr error
	attempted := 0
	for i := range subs {
		sub := subs[i]
		if sub.Enabled != 1 {
			continue
		}
		// 指定了单条订阅作为来源时，其余订阅一概不参与
		if onlySubID > 0 && sub.ID != onlySubID {
			continue
		}
		// onlyID 只表示"这一条需要回源刷新"，其余订阅改为复用缓存参与合并。
		// 此前是直接跳过其余订阅，导致更新单条订阅后其他订阅的节点
		// 全部从最终配置中消失。
		useCache := onlyID > 0 && sub.ID != onlyID

		// 逐条订阅前检查取消：这个循环是串行的，每条最长 30 秒，
		// N 个机场就是 N×30 秒，是整个合并里最耗时的一段。
		// 关停时若不在此中断，只等 30 秒的关停流程会先超时、关掉数据库，
		// 而循环仍在跑并继续写库（下面几处 MarkSubscriptionStatus / SaveConfig）。
		//
		// 必须在这里返回而不是 break：break 会带着不完整的 remoteYAMLs
		// 走到下面的空结果判断，被当成"所有订阅均更新失败"，
		// 甚至可能用残缺内容覆盖掉可用的旧聚合配置。
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("合并已取消（进程关停或请求超时），已处理 %d/%d 条订阅: %w",
				attempted, len(subs), err)
		}

		attempted++
		// 单条订阅失败不应中断整次合并，否则一个机场挂掉
		// 会导致所有其它订阅的更新全部丢失、配置永不刷新
		data, err := s.fetchRemoteYAML(ctx, sub, useCache)
		if err != nil {
			_ = s.db.MarkSubscriptionStatus(sub.ID, "error", err.Error())
			s.logger.Errorf("订阅 %s(%d) 拉取失败，已跳过: %v", sub.Name, sub.ID, err)
			lastErr = fmt.Errorf("subscription %s(%d): %w", sub.Name, sub.ID, err)
			continue
		}
		if _, err := s.engine.LoadAndParse(data); err != nil {
			_ = s.db.MarkSubscriptionStatus(sub.ID, "error", err.Error())
			s.logger.Errorf("订阅 %s(%d) 内容非法，已跳过: %v", sub.Name, sub.ID, err)
			lastErr = fmt.Errorf("subscription %s(%d) invalid yaml: %w", sub.Name, sub.ID, err)
			continue
		}
		_ = s.db.MarkSubscriptionStatus(sub.ID, "ok", "")
		_ = s.db.SaveConfig(&model.Config{
			Name:    fmt.Sprintf("remote-%d", sub.ID),
			Type:    "remote",
			Content: string(data),
			Version: int(time.Now().Unix()),
		})
		remoteYAMLs = append(remoteYAMLs, data)
	}
	if len(remoteYAMLs) == 0 {
		// 有订阅但全部失败时如实报错，避免用空配置覆盖掉可用的旧配置
		if attempted > 0 && lastErr != nil {
			return fmt.Errorf("所有订阅均更新失败，最后一个错误: %w", lastErr)
		}
		// 指定了单条订阅却一条都没轮到：该订阅已被删除或禁用。
		// 这是配置错误而非"没有远程内容"，必须如实报错，
		// 否则会静默退化成仅本地配置，用户以为选中的订阅仍在生效。
		if onlySubID > 0 && attempted == 0 {
			return fmt.Errorf("指定作为远程来源的订阅(%d)不存在或已禁用", onlySubID)
		}
		// 没有任何启用的订阅：写入空聚合结果，让后续合并走"仅本地配置"路径。
		// 内容留空（而非 "proxies: []"）以便与"确有远程内容"区分开。
		if err := s.db.SaveConfig(&model.Config{
			Name:    repository.RemoteMergedConfigName,
			Type:    "remote",
			Content: "",
			Version: int(time.Now().Unix()),
		}); err != nil {
			s.logger.Errorf("保存空的远程聚合配置失败: %v", err)
		}
		return nil
	}
	mergedRemote, err := s.engine.LoadAndParse(remoteYAMLs[0])
	if err != nil {
		return err
	}
	for i := 1; i < len(remoteYAMLs); i++ {
		next, err := s.engine.LoadAndParse(remoteYAMLs[i])
		if err != nil {
			return err
		}
		mergedRemote = s.engine.Merge(mergedRemote, next)
	}
	b, err := s.engine.GenerateYAML(mergedRemote)
	if err != nil {
		return err
	}
	return s.db.SaveConfig(&model.Config{
		Name:    repository.RemoteMergedConfigName,
		Type:    "remote",
		Content: string(b),
		Version: int(time.Now().Unix()),
	})
}

func (s *ConfigService) loadResolvedConflicts() []domain.Conflict {
	rows, err := s.db.ListResolvedConflicts()
	if err != nil {
		return nil
	}
	out := make([]domain.Conflict, 0, len(rows))
	for _, r := range rows {
		c := domain.Conflict{
			ID:         r.Key,
			Type:       r.Type,
			Path:       r.Path,
			Resolution: r.Resolution,
		}
		_ = json.Unmarshal([]byte(r.LocalValue), &c.Local)
		_ = json.Unmarshal([]byte(r.RemoteValue), &c.Remote)
		if r.ManualValue != "" {
			var manual any
			if json.Unmarshal([]byte(r.ManualValue), &manual) == nil {
				c.Manual = manual
			} else {
				c.Manual = r.ManualValue
			}
		}
		out = append(out, c)
	}
	return out
}

func (s *ConfigService) persistConflicts(conflicts []domain.Conflict) error {
	rows := make([]model.Conflict, 0, len(conflicts))
	for _, c := range conflicts {
		lb, _ := json.Marshal(c.Local)
		rb, _ := json.Marshal(c.Remote)
		rows = append(rows, model.Conflict{
			Key:         c.ID,
			Type:        c.Type,
			Path:        c.Path,
			LocalValue:  string(lb),
			RemoteValue: string(rb),
			Resolution:  c.Resolution,
			Resolved:    0,
		})
	}
	return s.db.UpsertConflicts(rows)
}

func (s *ConfigService) backupCurrentConfig() (string, error) {
	src := s.configPath()
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if err := os.MkdirAll(s.backupDir(), 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(s.backupDir(), fmt.Sprintf("config-%s.yaml", time.Now().Format("20060102-150405")))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	// 设计 §18：写入新备份后清理，只保留最近 10 份
	s.pruneBackups(defaultBackupRetain)

	// also store version snapshot
	sum := sha1.Sum(data)
	_ = s.db.SaveConfigVersion(&model.ConfigVersion{
		Hash:      hex.EncodeToString(sum[:]),
		Content:   string(data),
		FilePath:  dst,
		Note:      "auto-backup-before-merge",
		CreatedAt: time.Now(),
	})
	return dst, nil
}

func (s *ConfigService) writeConfigAtomically(content []byte) error {
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	// 用唯一临时文件名，避免并发写入互相覆盖产生截断内容
	f, err := os.CreateTemp(s.configDir, "config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(content)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.configPath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// MergeOptions 控制一次合并的行为。
type MergeOptions struct {
	// RefreshRemote 决定是否重新拉取远程来源。
	//
	// false 时复用数据库中已缓存的远程层直接合并，适用于「保存本地配置」——
	// 用户改的是自己的 mihomo 配置，不应因此去打上游机场；否则每次保存都
	// 触发一轮网络请求，机场侧还可能限流。
	//
	// true 时先重新拉取再合并，适用于定时拉取与「手动拉取并合并」按钮。
	RefreshRemote bool

	// OnlyID 仅在 RefreshRemote 为 true 且来源为订阅时有意义：
	// 表示只有这一条订阅回源刷新，其余复用各自的节点缓存。
	OnlyID int64
}

// MergeLocalOnly 用已缓存的远程层合并，不触碰网络。
func MergeLocalOnly() MergeOptions { return MergeOptions{RefreshRemote: false} }

// MergeWithRefresh 重新拉取远程来源后合并。
func MergeWithRefresh(onlyID int64) MergeOptions {
	return MergeOptions{RefreshRemote: true, OnlyID: onlyID}
}

func (s *ConfigService) MergeAndApply(ctx context.Context, opts MergeOptions) error {
	_, err := s.MergeAndApplyDetailed(ctx, opts)
	return err
}

// MergeAndApplyDetailed 合并本地与远程配置、落盘、校验并让内核生效。
//
// 关于取消（ctx）：本函数会在若干处检查 ctx.Err() 并提前返回，
// 检查点全部设在 writeConfigAtomically 之前。
//
// 这条分界线是刻意的：
//   - 落盘之前取消，什么都还没改，直接返回即可，无需回滚；
//   - 落盘之后取消，磁盘上已是新配置，此时中止会留下"磁盘新、数据库旧"
//     的不一致（界面读 merged 记录，会一直显示旧内容）。因此落盘之后
//     不再检查取消，宁可把剩下的校验、写库、内核重载跑完。
//     这几步都很快（本地 exec 与本地写库），不会明显拖长关停。
//
// 换言之：取消能避免"白做一场"，但不会制造"做一半"。
func (s *ConfigService) MergeAndApplyDetailed(ctx context.Context, opts MergeOptions) (*MergeApplyResult, error) {
	// 串行化，防止定时任务与手动触发并发损坏配置文件。
	//
	// 取消检查放在拿锁之后：此前若在拿锁前就返回，会让"等锁期间被取消"
	// 与"根本没轮到"两种情况难以区分；而拿到锁后立刻检查的代价可忽略。
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	// 等锁可能等了很久（前一次合并最长 10 分钟），期间进程可能已开始关停
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("合并已取消（等待其它合并完成时进程开始关停）: %w", err)
	}

	baseYAML, err := s.loadBaseYAML()
	if err != nil {
		return nil, fmt.Errorf("load base config: %w", err)
	}
	// 配置中心开启 TUN（只改 tun.enable）时不会走系统设置的 enable() patches。
	// 若 base 里 auto-redirect 未声明，合并后旁路由会缺默认 Redir 路径——此处仅补未声明项；
	// 显式 false 尊重用户。残留 aurora_tproxy 由合并末尾 Resync 在 TUN 模式下拆除。
	if normalized, changed, nerr := ensureBaseTUNGatewayDefaults(baseYAML); nerr != nil {
		s.logger.Errorf("规范化 base TUN 网关默认值失败（继续用原文合并）: %v", nerr)
	} else if changed {
		if err := s.UpdateBaseConfig(normalized); err != nil {
			s.logger.Errorf("回写 base TUN 网关默认值失败（继续用规范化文本合并）: %v", err)
		} else {
			s.logger.Info("base 已开启 TUN：已按需补齐 auto-route / 未声明的 auto-redirect")
		}
		baseYAML = normalized
	}
	baseCfg, err := s.engine.LoadAndParse([]byte(baseYAML))
	if err != nil {
		return nil, fmt.Errorf("parse base config: %w", err)
	}
	// 只在需要时才重建远程层。保存本地配置走这里的 false 分支，
	// 直接复用上一次拉取的结果，既快也不打扰上游。
	if opts.RefreshRemote {
		if err := s.buildRemoteConfig(ctx, opts.OnlyID); err != nil {
			return nil, err
		}
	}

	// 取多订阅聚合后的远程配置。必须按 name 定位聚合行：type="remote" 下还存着
	// 每条订阅的原始快照（remote-<id>），且写入时间通常晚于聚合结果，
	// 按 type 取最近一条会退化成"只用某一条订阅"。
	remoteModel, err := s.db.GetRemoteMergedConfig()
	remoteYAML := ""
	if err == nil && remoteModel != nil {
		remoteYAML = remoteModel.Content
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 需求：远程配置为空时直接使用本地配置。
	// 传 nil 让引擎走"无远程层"路径，既不会产生冲突，也不会被空的
	// proxies/rules 抹掉本地内容。
	var remoteCfg *domain.Config
	if strings.TrimSpace(remoteYAML) != "" {
		remoteCfg, err = s.engine.LoadAndParse([]byte(remoteYAML))
		if err != nil {
			return nil, fmt.Errorf("parse remote config: %w", err)
		}
	}

	var previous *domain.Config
	if cur, err := os.ReadFile(s.configPath()); err == nil {
		previous, _ = s.engine.LoadAndParse(cur)
	}

	// 设计 §16：应用用户配置的合并策略。
	// 策略按次传入而非写进引擎，避免并发合并互相覆盖。
	policy := domain.DefaultMergePolicy()
	if s.policyProvider != nil {
		policy = s.policyProvider()
	}

	resolvedConflicts := s.loadResolvedConflicts()
	// 设计 §3 Layer 3 / §15：持久化 override 配置层
	s.persistOverrideConfig(resolvedConflicts)

	result := s.engine.MergeDetailedWithPolicy(baseCfg, remoteCfg, previous, resolvedConflicts, policy)
	if err := s.persistConflicts(result.Conflicts); err != nil {
		s.logger.Errorf("persist conflicts failed: %v", err)
	}
	s.setLastDiff(result.Diff)
	for _, w := range result.Warnings {
		// 自动修正必须留痕，否则用户只会发现策略组莫名变少却查不到原因
		s.logger.Errorf("合并自动修正: %s", w)
	}

	// 注入透明代理技术参数（不覆盖 tun.enable 和 tproxy-port）。
	// 设计变更：开关状态现在直接存储在 base.yaml 中，通过配置合并流程生效。
	// 这里只负责注入必要的技术参数（auto-route、dns-hijack、routing-mark 等）。
	//
	// 根据合并后配置的实际值决定注入内容：
	//   - result.Config.TUN.Enable == true → 注入 TUN 技术参数
	//   - result.Config.TProxyPort > 0 且规则由面板托管 → 注入 TProxy 技术参数
	//
	// TProxy 那侧多一个托管条件：tproxy-port 只让内核监听端口，把流量引过去的
	// 防火墙规则不在配置里，所以"配了端口"不等于"启用了 TProxy"。用户在
	// 「配置中心 → 端口设置」里把它当普通端口填上是正常用法，此时注入
	// routing-mark 会在他的配置里留下一个无用且误导排障的字段。
	report := netcheck.Detect()
	injectOpts := netcheck.InjectOptions{
		TUNStack:             "mixed", // 默认协议栈
		AutoRedirect:         report.OS == "linux",
		TProxyManaged:        s.tproxyManaged(),
		SkipDefaultDNSHijack: s.adguardOwnsSystemDNS(),
	}
	if err := netcheck.Inject(result.Config, injectOpts); err != nil {
		s.logger.Errorf("注入透明代理技术参数失败: %v", err)
		// 技术参数注入失败不阻断流程，配置仍可生效，只是某些优化参数缺失
	}

	mergedBytes, err := s.engine.GenerateYAML(result.Config)
	if err != nil {
		return nil, fmt.Errorf("generate merged yaml: %w", err)
	}

	// 最后一个取消检查点：过了这里就开始动磁盘，中途放弃会留下
	// "磁盘新、数据库旧"的不一致，所以后续步骤一律跑完（见函数头注释）。
	// 这里检查是有价值的：上面的远程重建可能刚花了几分钟，
	// 若期间进程已开始关停，此时放弃还完全无副作用。
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("合并已取消（配置尚未落盘，无需回滚）: %w", err)
	}

	backupPath, err := s.backupCurrentConfig()
	if err != nil {
		return nil, fmt.Errorf("backup current config: %w", err)
	}
	if err := s.writeConfigAtomically(mergedBytes); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	if err := s.mihomo.ValidateConfig(ctx, s.configPath()); err != nil {
		if isBinaryMissing(err) {
			s.logger.Errorf("mihomo binary missing, skip validate/reload: %v", err)
		} else {
			// 用拷贝而非移动回滚，否则这份备份会从 backups/ 目录消失
			if backupPath != "" {
				if rbErr := copyFile(backupPath, s.configPath()); rbErr != nil {
					s.logger.Errorf("回滚配置失败: %v", rbErr)
				}
			} else {
				_ = os.Remove(s.configPath())
			}
			return nil, fmt.Errorf("validate config failed, rolled back: %w", err)
		}
	}

	// config.yaml 此时已落盘。下面两次写库若失败，就会出现「磁盘是新配置、
	// 数据库记录仍描述旧配置」的不一致，而界面读的是 merged 记录
	// （见 GetFinalConfig），于是一直显示旧内容。
	// 关停时连接池已关闭，这两次写入必然失败——原先用 `_ =` 丢弃错误
	// 使这种不一致完全无声，排查时无从下手，故至少要留下日志。
	sum := sha1.Sum(mergedBytes)
	if s.db.Closed() {
		logx.Errorf("配置已落盘但数据库已关闭（进程正在关停）：磁盘是新配置、"+
			"merged 记录仍是旧的，下次启动后界面需手动重新合并。path=%s", s.configPath())
	} else {
		if err := s.db.SaveConfigVersion(&model.ConfigVersion{
			Hash:      hex.EncodeToString(sum[:]),
			Content:   string(mergedBytes),
			FilePath:  s.configPath(),
			Note:      "merged",
			CreatedAt: time.Now(),
		}); err != nil {
			logx.Errorf("保存配置版本失败（磁盘已是新配置，版本历史缺这一条）: %v", err)
		}
		if err := s.db.SaveConfig(&model.Config{
			Name:    "merged",
			Type:    "merged",
			Content: string(mergedBytes),
			Version: int(time.Now().Unix()),
		}); err != nil {
			logx.Errorf("保存 merged 配置记录失败（磁盘已是新配置，界面会显示旧内容）: %v", err)
		}
		// 合并已完整成功，更新「已合并 base 指纹」。
		// 放在 merged 保存之后：若上面的写库失败，宁可不更新指纹，
		// 让界面继续提示未应用，也不要在配置实际未生效时误报已对齐。
		s.persistMergedBaseFingerprint()
	}

	// 优先走 external-controller 的 PUT /configs 热重载，不重启进程、不断开现有连接；
	// 未配置 controller 或请求失败时 ReloadConfig 会自行回退为重启。
	//
	// 但控制接口自身的参数（监听地址与密钥）是例外：mihomo 的热重载只换
	// 运行期配置，不会重开 API 监听套接字，也不会换掉已生效的密钥
	// （实测 PUT /configs 返回 204，而 external-controller 从
	// 127.0.0.1:19090 改成 0.0.0.0:19090 后监听仍停留在回环，
	// 新加的 secret 也不生效）。此时只有重启进程才能让新地址生效，
	// 否则用户改完配置、界面提示"已生效"，局域网却依旧连不上面板。
	if s.controllerChanged(previous, result.Config) {
		s.logger.Infof("external-controller 或 secret 已变更，重启内核使其生效（热重载不会重开 API 监听）")
		if err := s.mihomo.Reload(ctx); err != nil && !isBinaryMissing(err) {
			return &MergeApplyResult{
				Message:       fmt.Sprintf("配置已合并落盘，但内核重启失败：%v", err),
				ConflictCount: len(result.Conflicts),
				Diff:          result.Diff,
				Warnings:      result.Warnings,
			}, nil
		}
	} else if err := s.mihomo.ReloadConfig(ctx, result.Config.ExternalController, result.Config.Secret, s.configPath()); err != nil {
		if !isBinaryMissing(err) {
			return &MergeApplyResult{
				Message:       fmt.Sprintf("配置已合并落盘，但内核重载失败：%v", err),
				ConflictCount: len(result.Conflicts),
				Diff:          result.Diff,
				Warnings:      result.Warnings,
			}, nil
		}
	}

	// 让透明代理的防火墙规则跟上新配置。
	//
	// 位置是刻意的——必须在 config.yaml 落盘且内核重载之后：Resync 要从最终配置
	// 里读 dns.listen 与 external-controller 端口，早于落盘读到的是旧值。
	//
	// 规则里烧进了这些端口，而合并流程本身只生成配置、不碰防火墙。少了这一步，
	// 用户在配置中心改完端口会看到"已生效"，但内核听在新端口、规则还往旧端口投。
	s.resyncTransparent(ctx)

	return &MergeApplyResult{
		Message:       "配置已合并、校验并生效",
		ConflictCount: len(result.Conflicts),
		Diff:          result.Diff,
		Warnings:      result.Warnings,
	}, nil
}

// controllerChanged 判断本次合并是否动了控制接口的监听地址或密钥。
//
// previous 为 nil 表示磁盘上原本没有配置（首次生成）：这种情况下内核
// 要么还没起，要么随后会被 Start 拉起，都不需要额外重启，返回 false。
//
// 只比这两个字段：其余配置项热重载都能正常生效，无谓的重启会断掉
// 所有现有代理连接。
func (s *ConfigService) controllerChanged(previous, next *domain.Config) bool {
	if previous == nil || next == nil {
		return false
	}
	return previous.ExternalController != next.ExternalController ||
		previous.Secret != next.Secret
}

func isBinaryMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "not found in %path%") ||
		strings.Contains(msg, "no such file or directory")
}

// UpdateSubscription 刷新指定订阅并重新合并。
// 这是显式的"更新订阅"动作，需要回源，故走 RefreshRemote。
// RefreshSubscriptionCache 只刷新一条订阅自身的节点缓存：
// 回源拉取、跑该订阅的独立管道、把节点与流量信息写回数据库。
//
// 刻意不碰配置中心——不读远程来源、不做本地/远程合并、
// 不写 config.yaml、不重启内核。
//
// 这样拆开是因为原先「刷新缓存」走的是完整合并流程，带来两个实际故障：
//  1. 配置中心的远程来源一旦失效（如指向了已删除的订阅），
//     刷新任意一条订阅的缓存都会连带失败，报「指定作为远程来源的
//     订阅(N)不存在或已禁用」——而这跟"刷新某条订阅的缓存"毫无关系。
//  2. 更隐蔽的是静默失效：远程来源指定为订阅B时，
//     buildRemoteFromSubscriptions 会用 onlySubID 过滤掉其余订阅，
//     于是对订阅A点「刷新缓存」时 A 压根不会被回源，
//     界面却提示成功——用户以为刷新了，缓存其实一个字节都没变。
//
// 订阅的节点缓存本就只服务于 substore 自身的分享/预览链路
// （RenderService 只读 sub.CachedNodes），与最终配置的生成是两件事。
func (s *ConfigService) RefreshSubscriptionCache(ctx context.Context, id int64) error {
	sub, err := s.db.GetSubscription(id)
	if err != nil {
		return err
	}
	if sub == nil {
		return fmt.Errorf("订阅(%d)不存在", id)
	}

	res, err := s.ssEngine.Convert(ctx, substore.ConvertRequest{
		URL:       sub.URL,
		Content:   sub.Content, // 手动粘贴的节点无需回源
		Source:    sub.Name,
		UserAgent: sub.UserAgent,
		// 刻意不传 CacheRaw：用户点的就是"刷新"，必须真的回源，
		// 否则会拿旧缓存冒充刷新结果
	}, s.loadRewriteRules(), DecodeOperators(sub.Operators), "mihomo-yaml", "")
	if err != nil {
		// 订阅 URL 通常内嵌机场账户 token，不能进日志
		s.logger.Errorf("刷新订阅 %s(%d) 失败: %v", sub.Name, sub.ID, err)
		_ = s.db.MarkSubscriptionStatus(sub.ID, "error", err.Error())
		return fmt.Errorf("刷新订阅「%s」失败: %w", sub.Name, err)
	}
	if strings.TrimSpace(res.YAML) == "" {
		msg := "转换结果为空（可能是管道过滤过严或上游无可用节点）"
		_ = s.db.MarkSubscriptionStatus(sub.ID, "error", msg)
		return fmt.Errorf("刷新订阅「%s」失败: %s", sub.Name, msg)
	}

	s.persistSubscriptionCache(sub.ID, res)
	_ = s.db.MarkSubscriptionStatus(sub.ID, "ok", "")
	return nil
}

// PullAndMerge 手动拉取远程来源并与本地配置合并。
//
// 对应界面上的「拉取远程并合并」按钮：与定时拉取做同一件事，
// 只是由用户即时触发，用于改完远程来源或上游更新后立刻生效，
// 不必等到下一个定时周期。
func (s *ConfigService) PullAndMerge(ctx context.Context) (*MergeApplyResult, error) {
	return s.MergeAndApplyDetailed(ctx, MergeWithRefresh(0))
}

// ApplyLocalOnly 仅用本地配置与已缓存的远程层重新生成最终配置。
//
// 对应「保存本地配置」：用户改的是自己的 mihomo 配置，
// 不应触发远程拉取。
func (s *ConfigService) ApplyLocalOnly(ctx context.Context) (*MergeApplyResult, error) {
	return s.MergeAndApplyDetailed(ctx, MergeLocalOnly())
}

func (s *ConfigService) GetBaseConfig() (string, error) {
	return s.loadBaseYAML()
}

// BaseUnmerged 报告本地基础配置是否已保存但尚未合并进最终配置。
//
// 判定靠「合并指纹」而非显式标记：每次合并成功时把当时 base 内容的哈希
// 写入 settings 表（persistMergedBaseFingerprint），查询时比较当前 base
// 的哈希。任何修改 base 的路径（配置中心保存、透明代理开关改写
// tun.enable 等）都会让哈希失配，无需逐路径置位；服务重启后记录仍在
// 数据库里，不会像内存标记那样丢失。
//
// 边界：
//   - 从未合并过（无 merged 记录）：不存在「未生效」概念，返回 false；
//   - merged 已存在但指纹缺失（旧版本升级而来）：无法证明已对齐，
//     保守返回 true，用户应用一次合并后即补齐指纹。
func (s *ConfigService) BaseUnmerged() (bool, error) {
	if _, err := s.db.GetConfigByType("merged"); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	fp, err := s.db.GetSetting(mergedBaseFingerprintKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	}

	baseYAML, err := s.loadBaseYAML()
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(baseYAML))
	return hex.EncodeToString(sum[:]) != fp, nil
}

// persistMergedBaseFingerprint 记录本次合并所用 base 内容的哈希。
//
// 必须在 merged 记录保存成功之后调用（与 merged 同一条「落盘后跑完」的
// 分界线）：只有整次合并真正成功，指纹才代表「已对齐」。写失败只记日志
// ——最坏结果是界面多提示一次「未应用」，方向无害。
func (s *ConfigService) persistMergedBaseFingerprint() {
	baseYAML, err := s.loadBaseYAML()
	if err != nil {
		s.logger.Errorf("记录合并指纹失败（读 base 失败）: %v", err)
		return
	}
	sum := sha256.Sum256([]byte(baseYAML))
	if err := s.db.SetSetting(mergedBaseFingerprintKey, hex.EncodeToString(sum[:])); err != nil {
		s.logger.Errorf("记录合并指纹失败: %v", err)
	}
}

// GetFinalConfig 返回最近一次合并生成的最终配置（mihomo 内核实际加载的内容）。
// 尚未合并过时返回空字符串而非报错，供「配置差异」页面判断是否已生成过。
func (s *ConfigService) GetFinalConfig() (string, error) {
	cfg, err := s.db.GetConfigByType("merged")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return cfg.Content, nil
}

// GetRemoteConfig 返回当前远程来源拉取后的原始层内容（合并前）。
// 未配置远程来源或尚未拉取过时返回空字符串而非报错。
func (s *ConfigService) GetRemoteConfig() (string, error) {
	cfg, err := s.db.GetRemoteMergedConfig()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return cfg.Content, nil
}

func (s *ConfigService) UpdateBaseConfig(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("base config content is empty")
	}
	if _, err := s.engine.LoadAndParse([]byte(content)); err != nil {
		return fmt.Errorf("invalid base config yaml: %w", err)
	}
	return s.db.SaveConfig(&model.Config{
		Name:    "base",
		Type:    "base",
		Content: content,
		Version: int(time.Now().Unix()),
	})
}

// RunScheduledPull 由用户配置的 Cron 任务调用，拉取远程来源并合并。
//
// 这是远程内容更新的唯一自动路径。此前另有一条「每分钟轮询、按各订阅
// interval 逐条判断到期」的机制，已移除：两套调度并存时，用户在配置中心
// 关掉定时拉取后后台仍在回源，与界面呈现不一致。现在订阅的刷新时机
// 完全由这里的 Cron 与手动「立即拉取并合并」决定。
func (s *ConfigService) RunScheduledPull(ctx context.Context) error {
	src := s.remoteSource()
	if src.Type == domain.RemoteSourceNone {
		// 未配置远程来源时无需拉取
		return nil
	}
	_, err := s.PullAndMerge(ctx)
	return err
}

// ReloadKernel 让内核重新加载当前磁盘上的 config.yaml。
// 优先走 external-controller 热重载（不断连），仅在其不可用时才重启进程；
// 供"重载配置"按钮等不涉及重新合并的场景使用。
func (s *ConfigService) ReloadKernel(ctx context.Context) error {
	controller, secret := "", ""
	if raw, err := os.ReadFile(s.configPath()); err == nil {
		if cfg, err := s.engine.LoadAndParse(raw); err == nil && cfg != nil {
			controller, secret = cfg.ExternalController, cfg.Secret
		}
	}
	return s.mihomo.ReloadConfig(ctx, controller, secret, s.configPath())
}

// GetLastDiff 返回最近一次合并的 Diff。lastDiff 只存在内存中，
// 进程重启后会归零；此时从数据库里最近两条版本记录重新计算，
// 避免 Diff 页面在重启后永久空白。
func (s *ConfigService) GetLastDiff() domain.DiffReport {
	cached := s.snapshotLastDiff()
	if len(cached.Added)+len(cached.Removed)+len(cached.Changed) > 0 {
		return cached
	}

	versions, err := s.db.ListConfigVersions(2)
	if err != nil || len(versions) < 2 {
		return cached
	}
	// ListConfigVersions 按 id desc 排列，[0] 是最新的，[1] 是上一份
	next, err1 := s.engine.LoadAndParse([]byte(versions[0].Content))
	prev, err2 := s.engine.LoadAndParse([]byte(versions[1].Content))
	if err1 != nil || err2 != nil {
		return cached
	}
	return engine.BuildDiff(prev, next)
}

// setLastDiff / snapshotLastDiff 用 diffMu 隔离"合并时写入"与"接口读取"，
// 否则并发下会读到撕裂的 slice 头并在遍历时越界 panic。
func (s *ConfigService) setLastDiff(d domain.DiffReport) {
	s.diffMu.Lock()
	s.lastDiff = d
	s.diffMu.Unlock()
}

func (s *ConfigService) snapshotLastDiff() domain.DiffReport {
	s.diffMu.RLock()
	defer s.diffMu.RUnlock()
	// 返回副本，避免调用方在锁外遍历时与后续写入竞争
	return domain.DiffReport{
		Added:   append([]domain.DiffItem(nil), s.lastDiff.Added...),
		Removed: append([]domain.DiffItem(nil), s.lastDiff.Removed...),
		Changed: append([]domain.DiffItem(nil), s.lastDiff.Changed...),
	}
}

func (s *ConfigService) RestoreVersion(ctx context.Context, id int64) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	// 与 MergeAndApplyDetailed 同一条分界线：落盘前可以随意取消，
	// 落盘后必须把校验与内核重载跑完，否则磁盘配置已换而内核仍在跑旧的。
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("恢复已取消（配置尚未落盘）: %w", err)
	}

	v, err := s.db.GetConfigVersion(id)
	if err != nil {
		return err
	}
	backupPath, err := s.backupCurrentConfig()
	if err != nil {
		return err
	}
	if err := s.writeConfigAtomically([]byte(v.Content)); err != nil {
		return err
	}
	if err := s.mihomo.ValidateConfig(ctx, s.configPath()); err != nil && !isBinaryMissing(err) {
		// 用拷贝而非移动回滚，否则这份备份会从 backups/ 目录消失
		if backupPath != "" {
			if rbErr := copyFile(backupPath, s.configPath()); rbErr != nil {
				s.logger.Errorf("恢复版本失败后回滚也失败: %v", rbErr)
			}
		}
		return fmt.Errorf("restore validate failed: %w", err)
	}

	restored, err := s.engine.LoadAndParse([]byte(v.Content))
	controller, secret := "", ""
	if err == nil && restored != nil {
		controller, secret = restored.ExternalController, restored.Secret
	}
	_ = s.mihomo.ReloadConfig(ctx, controller, secret, s.configPath())
	return nil
}

func (s *ConfigService) SubStoreEngine() *substore.Engine {
	return s.ssEngine
}

// defaultBackupRetain 设计 §18 规定的默认备份保留份数
const defaultBackupRetain = 10

// pruneBackups 清理超出保留份数的历史备份，防止磁盘无限增长。
func (s *ConfigService) pruneBackups(retain int) {
	if retain <= 0 {
		retain = defaultBackupRetain
	}
	entries, err := os.ReadDir(s.backupDir())
	if err != nil {
		return
	}

	type backupFile struct {
		name    string
		modTime time.Time
	}
	files := make([]backupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "config-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, backupFile{name: e.Name(), modTime: info.ModTime()})
	}
	if len(files) <= retain {
		return
	}

	// 按时间倒序，保留最新 retain 份
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	for _, f := range files[retain:] {
		_ = os.Remove(filepath.Join(s.backupDir(), f.name))
	}
	logx.Infof("备份清理完成：保留 %d 份，删除 %d 份", retain, len(files)-retain)
}

// buildOverrideYAML 依据已解决的冲突生成 Layer 3 覆盖配置（设计 §3 / §15）。
// 该配置持久化到 configs 表 type=override，便于审计与回溯。
func (s *ConfigService) buildOverrideYAML(resolved []domain.Conflict) string {
	if len(resolved) == 0 {
		return ""
	}

	ov := domain.Config{}
	hasContent := false

	for _, c := range resolved {
		switch c.Type {
		case "proxy":
			var src any
			switch strings.ToLower(c.Resolution) {
			case "local":
				src = c.Local
			case "remote":
				src = c.Remote
			case "manual":
				src = c.Manual
			case "merge":
				// 设计 §13：自动合并以本地为基准，远程补齐缺失字段
				src = mergeConflictValues(c.Local, c.Remote)
			default:
				continue
			}
			b, err := json.Marshal(src)
			if err != nil {
				continue
			}
			var px domain.Proxy
			if json.Unmarshal(b, &px) == nil && px.Name != "" {
				ov.Proxies = append(ov.Proxies, px)
				hasContent = true
			}
		case "rule":
			var raw any
			switch strings.ToLower(c.Resolution) {
			case "local", "merge":
				// 规则有顺序语义，merge 退化为保留本地
				raw = c.Local
			case "remote":
				raw = c.Remote
			case "manual":
				raw = c.Manual
			default:
				continue
			}
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				ov.Rules = append(ov.Rules, s)
				hasContent = true
			}
		}
	}

	if !hasContent {
		return ""
	}
	b, err := s.engine.GenerateYAML(&ov)
	if err != nil {
		return ""
	}
	return string(b)
}

// persistOverrideConfig 将 override 层写入 configs 表（type=override）
func (s *ConfigService) persistOverrideConfig(resolved []domain.Conflict) {
	content := s.buildOverrideYAML(resolved)
	if content == "" {
		return
	}
	_ = s.db.SaveConfig(&model.Config{
		Name:    "override",
		Type:    "override",
		Content: content,
		Version: int(time.Now().Unix()),
	})
}

// mergeConflictValues 将本地与远程值做浅层合并：以本地为基准，
// 远程仅补齐本地缺失（零值）的字段。用于 override 层持久化。
func mergeConflictValues(local, remote any) map[string]interface{} {
	toMap := func(v any) map[string]interface{} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var m map[string]interface{}
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		return m
	}
	lm := toMap(local)
	rm := toMap(remote)
	if lm == nil {
		return rm
	}
	out := make(map[string]interface{}, len(lm)+len(rm))
	for k, v := range lm {
		out[k] = v
	}
	for k, v := range rm {
		cur, exists := out[k]
		if !exists || isZeroValue(cur) {
			out[k] = v
		}
	}
	return out
}

// isZeroValue 判断字段是否为「未设置」的零值
func isZeroValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case float64:
		return t == 0
	case bool:
		return !t
	case map[string]interface{}:
		return len(t) == 0
	case []interface{}:
		return len(t) == 0
	default:
		return false
	}
}

// copyFile 以内容拷贝的方式还原文件，保留源文件不动
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// mergeUpstreamParams 把订阅的顶层参数并入渲染后的节点 YAML。
// 节点侧已有的键优先（proxies/proxy-groups/rules 由模板生成），
// 其余参数追加进去，随后交由合并引擎按用户策略决定取本地还是远程。
func mergeUpstreamParams(nodeYAML []byte, params map[string]interface{}) []byte {
	if len(params) == 0 {
		return nodeYAML
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(nodeYAML, &doc); err != nil || doc == nil {
		return nodeYAML
	}
	for k, v := range params {
		if _, exists := doc[k]; !exists {
			doc[k] = v
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nodeYAML
	}
	return out
}

// DashboardTarget 描述内置 zashboard 面板对接本地内核所需的连接信息。
type DashboardTarget struct {
	Host   string
	Port   string
	Secret string
}

// KernelAPITarget 从当前生效的 config.yaml 里解析出内核 external-controller
// 的主机与端口，供内置面板自动完成对接（zashboard 支持通过
// ?hostname=&port=&secret= 免手填直连内核）。
//
// controller 常见写法是 ":9090" 或 "0.0.0.0:9090" —— 这类监听地址不能直接
// 交给浏览器，需要回落到用户访问管理端时使用的主机名，由调用方补齐。
// KernelDNSPort 取 mihomo 实际的 DNS 监听端口（config.yaml 的 dns.listen）。
//
// 透明代理的防火墙规则必须把 53 端口的查询重定向到这个端口，而不是 tproxy-port：
// TPROXY 保留原始目的端口，送到 tproxy-port 的话 mihomo 只会看到"目的端口 53 的
// 普通流量"，不会按 DNS 协议应答，域名解析就始终没被接管。
//
// 读磁盘上的最终配置而不是 base.yaml：dns.listen 可能来自 base、也可能来自订阅
// 合并进来，只有最终配置才是内核真正加载的那份。
//
// 返回 0 表示未配置 dns.listen（此时调用方回落到默认端口）。解析失败一律返回 0
// 并附带错误，由调用方决定是回落还是拒绝——这里不猜。
func (s *ConfigService) KernelDNSPort() (int, error) {
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		return 0, fmt.Errorf("读取配置失败: %w", err)
	}
	cfg, err := s.engine.LoadAndParse(raw)
	if err != nil || cfg == nil {
		return 0, fmt.Errorf("解析配置失败: %w", err)
	}
	// listen 未建模，走 DNS 段的 inline Extra
	v, ok := cfg.DNS.Extra["listen"]
	if !ok {
		return 0, nil
	}
	addr := strings.TrimSpace(fmt.Sprintf("%v", v))
	if addr == "" {
		return 0, nil
	}
	// 形如 0.0.0.0:1053 / :1053 / [::]:1053，取最后一段
	portStr := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		portStr = strings.TrimSpace(addr[i+1:])
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("dns.listen 端口无法解析: %q", addr)
	}
	return port, nil
}

// BaseDNSListen 读取 base 配置中的 dns.listen 原文（如 "0.0.0.0:1053"）。
// 未配置时返回空串。wiring 冲突解决与回滚都依赖这份「用户本地意图」，
// 不能用最终 config.yaml——那可能混入了订阅层的 listen。
func (s *ConfigService) BaseDNSListen() (string, error) {
	raw, err := s.loadBaseYAML()
	if err != nil {
		return "", err
	}
	return readBaseDNSListen(raw), nil
}

// SetBaseDNSListen 通过 base 定点改写 dns.listen 并落库。
//
// AdGuard wiring 开启时若 AGH 与 mihomo 抢同一端口，需要把 mihomo 的
// listen 挪到 127.0.0.1:<空闲>；必须走 base 路径（而不是只改最终
// config.yaml），否则下次合并会把改动冲掉。wiring=off 时同样可用，
// 本方法本身不检查 wiring 状态——是否该改由编排层决定。
func (s *ConfigService) SetBaseDNSListen(listen string) error {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return fmt.Errorf("dns.listen 不能为空")
	}
	raw, err := s.loadBaseYAML()
	if err != nil {
		return err
	}
	patched, err := patchBaseYAML(raw, "dns.listen", listen)
	if err != nil {
		return fmt.Errorf("改写 dns.listen 失败: %w", err)
	}
	return s.UpdateBaseConfig(patched)
}

// SetBaseDNSEnable 定点改写 dns.enable 并落库。
// AdGuard 入口把上游指到 mihomo :1053 时，必须保证内核 DNS 已开启，
// 否则 AGH→1053 会拒绝/超时。
func (s *ConfigService) SetBaseDNSEnable(enable bool) error {
	raw, err := s.loadBaseYAML()
	if err != nil {
		return err
	}
	patched, err := patchBaseYAML(raw, "dns.enable", enable)
	if err != nil {
		return fmt.Errorf("改写 dns.enable 失败: %w", err)
	}
	return s.UpdateBaseConfig(patched)
}

// readBaseDNSListen 从 base YAML 文本取出 dns.listen 标量值。
func readBaseDNSListen(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return ""
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return ""
	}
	_, dnsNode := findMapEntry(doc.Content[0], "dns")
	if dnsNode == nil || dnsNode.Kind != yaml.MappingNode {
		return ""
	}
	_, listenNode := findMapEntry(dnsNode, "listen")
	if listenNode == nil {
		return ""
	}
	return strings.TrimSpace(listenNode.Value)
}

// parseListenPort 从 "host:port" / ":port" 中取出端口；失败返回 0。
func parseListenPort(addr string) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 0
	}
	portStr := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		portStr = strings.TrimSpace(addr[i+1:])
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func (s *ConfigService) KernelAPITarget() (DashboardTarget, error) {
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		return DashboardTarget{}, fmt.Errorf("读取配置失败: %w", err)
	}
	cfg, err := s.engine.LoadAndParse(raw)
	if err != nil || cfg == nil {
		return DashboardTarget{}, fmt.Errorf("解析配置失败: %w", err)
	}
	addr := strings.TrimSpace(cfg.ExternalController)
	if addr == "" {
		return DashboardTarget{}, fmt.Errorf("当前配置未启用 external-controller，面板无法连接内核")
	}

	host, port := "", ""
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host, port = strings.TrimSpace(addr[:i]), strings.TrimSpace(addr[i+1:])
	} else {
		port = addr
	}
	// 监听所有网卡的写法对浏览器没有意义，留空让调用方用请求主机名补齐
	if host == "0.0.0.0" || host == "[::]" || host == "::" {
		host = ""
	}
	return DashboardTarget{Host: host, Port: port, Secret: cfg.Secret}, nil
}

// LocalProxyURL 返回本地 mihomo 的 HTTP 代理地址，供更新器出网使用。
//
// 端口取自当前生效的 config.yaml：优先 mixed-port（同时支持 HTTP 与 SOCKS），
// 其次 port（纯 HTTP）。socks-port 不用——标准库的 http.Transport 只认
// http/https 代理，给它一个 SOCKS 端口会握手失败。
//
// 返回空串表示代理不可用：内核未运行、或配置里没开放任何 HTTP 代理端口。
// 调用方据此回落到直连，不应把空串当作错误。
func (s *ConfigService) LocalProxyURL() string {
	// 内核没跑起来时端口上没人监听，返回地址只会让调用方白等一次连接超时
	if s.mihomo == nil || !s.mihomo.Status().IsRunning {
		return ""
	}
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		return ""
	}
	cfg, err := s.engine.LoadAndParse(raw)
	if err != nil || cfg == nil {
		return ""
	}
	port := cfg.MixedPort
	if port <= 0 {
		port = cfg.Port
	}
	if port <= 0 {
		return ""
	}
	// 固定连 127.0.0.1：即使 bind-address 指定了其他地址，
	// 本机回环也总是可达，且不受 allow-lan 影响
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

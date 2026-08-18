package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"auroramihomo/backend/internal/domain"

	"gopkg.in/yaml.v3"
)

type MergeEngine struct {
	policy domain.MergePolicy
}

// matchRuleKinds 是 mihomo 中「终态兜底」类规则：
// 匹配全部流量、只能位于规则列表末尾（其后任何规则都不可达）。
// 与普通规则不同，MATCH 之间不存在「同 matcher 冲突」——
// MATCH 只能有一条，合并时按「目标策略是否变化」单独比较，
// 不能被整行去重或 target 差异逻辑误伤。sub-rule 是段落匹配，
// 可出现在 MATCH 之前，不在该集合内。
var matchRuleKinds = map[string]bool{"MATCH": true}

// isMatchRule 按规则首段（如 MATCH / DOMAIN-SUFFIX / RULE-SET）判断
// 是否属于终态兜底规则，大小写不敏感（Mihomo 规则名不区分大小写）。
func isMatchRule(r string) bool {
	first, _, _ := strings.Cut(r, ",")
	return matchRuleKinds[strings.ToUpper(strings.TrimSpace(first))]
}

// matchConflictKey 为 MATCH 类规则生成稳定的冲突 ID：
// 所有 MATCH 共用同一 key（"match"），因此目标从 DIRECT 换成 Proxy
// 时产生的冲突记录不会被当成两条不同 matcher 分开存，持久层按 key
// 去重后只会保留一条，避免冲突表里 MATCH 历史记录越积越多。
func matchConflictKey() string {
	return "match"
}

func NewMergeEngine() *MergeEngine {
	return &MergeEngine{policy: domain.DefaultMergePolicy()}
}

// WithPolicy 就地修改引擎策略。
// 已废弃：MergeEngine 是全局单例，就地改写会让并发合并互相污染策略，
// 请改用 MergeDetailedWithPolicy 按次传入。
func (e *MergeEngine) WithPolicy(p domain.MergePolicy) *MergeEngine {
	e.policy = p
	return e
}

func (e *MergeEngine) LoadAndParse(data []byte) (*domain.Config, error) {
	var cfg domain.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}
	return &cfg, nil
}

func (e *MergeEngine) GenerateYAML(cfg *domain.Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// Merge keeps backward-compatible simple merge.
func (e *MergeEngine) Merge(base *domain.Config, remote *domain.Config) *domain.Config {
	res := e.MergeDetailed(base, remote, nil, nil)
	return res.Config
}

// MergeDetailed performs merge + conflict detection + optional override + diff.
// MergeDetailedWithPolicy 用显式传入的策略执行合并，不触碰引擎共享状态，
// 因此可安全地被并发调用。
func (e *MergeEngine) MergeDetailedWithPolicy(base, remote, previous *domain.Config, resolved []domain.Conflict, policy domain.MergePolicy) *domain.MergeResult {
	// 复制一个仅供本次调用使用的引擎，隔离策略
	local := &MergeEngine{policy: policy}
	return local.MergeDetailed(base, remote, previous, resolved)
}

func (e *MergeEngine) MergeDetailed(base, remote, previous *domain.Config, resolved []domain.Conflict) *domain.MergeResult {
	if base == nil {
		base = &domain.Config{}
	}
	if remote == nil {
		remote = &domain.Config{}
	}

	// 设计 §5/§6：合并前先做标准化，避免大小写/空白差异造成假冲突
	NormalizeConfig(base)
	NormalizeConfig(remote)

	// system local-first：先整体浅拷贝 base，一次性带上所有标量/系统级字段
	// （mode、端口、external-controller、find-process-mode、tcp-concurrent 等）。
	// 这样以后在 domain.Config 里新增官方参数字段时不必再逐个手写赋值语句，
	// 默认即遵循 Local First，只有下面显式处理的少数字段（DNS/TUN/Proxies/
	// ProxyGroups/Rules/RuleProviders/General）才有专门的合并语义。
	mergedVal := *base
	merged := &mergedVal
	// 上面的浅拷贝会让 merged 与 base 共享同一批 slice/map 底层存储；
	// 这些字段接下来都会被完全重建，先清空引用避免意外别名/越界复用。
	merged.Proxies = nil
	merged.ProxyGroups = nil
	merged.Rules = nil
	merged.RuleProviders = nil
	merged.General = nil

	// 通用标量字段（mode、各监听端口、Geo 系列、external-controller 系列、
	// 认证、profile、hosts 等）：远程优先时逐字段采用远程的非零值。
	// 逐字段而非整体替换，才能保证"远程没声明的键仍然沿用本地值"。
	if e.policy.GeneralPriority == "remote" {
		overlayNonZeroFields(merged, remote)
	}

	// 默认 Local First（设计 §11），仅当用户显式选择 remote 策略时才采用远程值，
	// 且远程必须真的声明了该段（非零值），否则会用空配置抹掉本地设置
	if e.policy.DNSPriority == "remote" && !jsonEqual(remote.DNS, domain.DNSConfig{}) {
		merged.DNS = remote.DNS
	}
	if e.policy.TUNPriority == "remote" && !jsonEqual(remote.TUN, domain.TUNConfig{}) {
		merged.TUN = remote.TUN
	}
	// Sniffer 同属系统级配置，跟随通用策略（此前完全没有接入任何策略）
	if e.policy.GeneralPriority == "remote" && !jsonEqual(remote.Sniffer, domain.SnifferConfig{}) {
		merged.Sniffer = remote.Sniffer
	}

	// General 兜底字段（listeners/proxy-providers/sub-rules/tls/experimental 等未显式建模的官方参数）：
	// 本地优先时远程只补齐本地缺失的键；远程优先时远程的同名键覆盖本地。
	// 两种策略下都不会删除本地独有的键。
	merged.General = map[string]interface{}{}
	for k, v := range base.General {
		merged.General[k] = v
	}
	remoteGeneralWins := e.policy.GeneralPriority == "remote"
	for k, v := range remote.General {
		if _, exists := merged.General[k]; !exists || remoteGeneralWins {
			merged.General[k] = v
		}
	}

	var conflicts []domain.Conflict

	// rule providers local first
	merged.RuleProviders = map[string]domain.RuleProvider{}
	for k, v := range base.RuleProviders {
		merged.RuleProviders[k] = v
	}
	for k, v := range remote.RuleProviders {
		if lv, ok := merged.RuleProviders[k]; ok {
			// 设计 §10/§12：provider 同 key 内容不同 -> 冲突（Base 优先）
			if !jsonEqual(lv, v) {
				conflicts = append(conflicts, domain.Conflict{
					ID:         hashKey("provider", k),
					Type:       "provider",
					Path:       "rule-providers." + k,
					Local:      lv,
					Remote:     v,
					Resolution: "local",
				})
			}
			continue
		}
		merged.RuleProviders[k] = v
	}

	// 设计 §11/§12：系统配置默认 Local First，但仍需检测并上报冲突
	conflicts = append(conflicts, detectSystemConflicts(base, remote)...)

	// proxies
	proxyIndex := map[string]domain.Proxy{}
	for _, p := range base.Proxies {
		proxyIndex[normalizeKey(p.Name)] = p
		merged.Proxies = append(merged.Proxies, p)
	}
	for _, rp := range remote.Proxies {
		if lp, ok := proxyIndex[normalizeKey(rp.Name)]; ok {
			if !proxyEqual(lp, rp) {
				c := domain.Conflict{
					ID:     hashKey("proxy", rp.Name),
					Type:   "proxy",
					Path:   "proxies." + rp.Name,
					Local:  lp,
					Remote: rp,
					// 按策略自动决定：local/remote/merge 由引擎处理进结果配置，
					// 只有 manual 需要用户介入。持久层据此标记 resolved。
					Resolution: e.policy.ProxyPriority,
				}
				conflicts = append(conflicts, c)
				// default keep local unless policy remote
				if e.policy.ProxyPriority == "remote" {
					replaceProxy(merged, rp, false)
				}
			}
		} else {
			merged.Proxies = append(merged.Proxies, rp)
			proxyIndex[normalizeKey(rp.Name)] = rp
		}
	}

	// proxy groups
	groupIndex := map[string]int{}
	for i, g := range base.ProxyGroups {
		merged.ProxyGroups = append(merged.ProxyGroups, g)
		groupIndex[normalizeKey(g.Name)] = i
	}
	for _, rg := range remote.ProxyGroups {
		if idx, ok := groupIndex[normalizeKey(rg.Name)]; ok {
			lg := merged.ProxyGroups[idx]
			// keep local behavior fields, merge node lists
			nodeSet := map[string]bool{}
			for _, n := range lg.Proxies {
				nodeSet[normalizeKey(n)] = true
			}
			for _, n := range rg.Proxies {
				if !nodeSet[normalizeKey(n)] {
					lg.Proxies = append(lg.Proxies, n)
					nodeSet[normalizeKey(n)] = true
				}
			}
			useSet := map[string]bool{}
			for _, n := range lg.Use {
				useSet[normalizeKey(n)] = true
			}
			for _, n := range rg.Use {
				if !useSet[normalizeKey(n)] {
					lg.Use = append(lg.Use, n)
					useSet[normalizeKey(n)] = true
				}
			}
			// Extra（include-all / filter / icon 等官方参数）同样按 Local First：
			// 本地已声明的键保持不动，只补齐本地缺失的键，
			// 否则订阅提供的 icon、expected-status 之类会被整体丢弃。
			if len(rg.Extra) > 0 {
				if lg.Extra == nil {
					lg.Extra = map[string]interface{}{}
				}
				for k, v := range rg.Extra {
					if _, exists := lg.Extra[k]; !exists {
						lg.Extra[k] = v
					}
				}
			}

			// if behavioral fields differ, record conflict but keep local
			if lg.Type != rg.Type || lg.URL != rg.URL || lg.Interval != rg.Interval || lg.Strategy != rg.Strategy {
				conflicts = append(conflicts, domain.Conflict{
					ID:     hashKey("proxy-group", rg.Name),
					Type:   "proxy-group",
					Path:   "proxy-groups." + rg.Name,
					Local:  lg,
					Remote: rg,
					// 策略组行为字段冲突没有 local/remote 选择——引擎总是保留
					// 本地行为、合并节点列表，视为自动 merge 解决
					Resolution: "merge",
				})
			}
			merged.ProxyGroups[idx] = lg
		} else {
			merged.ProxyGroups = append(merged.ProxyGroups, rg)
			groupIndex[normalizeKey(rg.Name)] = len(merged.ProxyGroups) - 1
		}
	}

	// rules: local first then remote, detect same matcher conflicts
	// MATCH 规则具有终态兜底语义（且只能有一条真正生效）：
	// 普通规则按 matcher 对齐合并（本地优先插入，远程独有规则追加），
	// MATCH 规则单独提取并在全部普通规则之后作为最后一条沉底。
	parseRule := parseRuleParts
	localMatchers := map[string]ruleParts{}
	seen := map[string]bool{}

	var localMatchRule, remoteMatchRule string

	for _, r := range base.Rules {
		if isMatchRule(r) {
			if localMatchRule == "" {
				localMatchRule = r
			}
			continue
		}
		p := parseRule(r)
		localMatchers[normalizeKey(p.matcher)] = p
		if !seen[normalizeKey(r)] {
			merged.Rules = append(merged.Rules, r)
			seen[normalizeKey(r)] = true
		}
	}

	for _, r := range remote.Rules {
		if isMatchRule(r) {
			if remoteMatchRule == "" {
				remoteMatchRule = r
			}
			continue
		}
		p := parseRule(r)
		if lp, ok := localMatchers[normalizeKey(p.matcher)]; ok {
			if lp.target != p.target {
				conflicts = append(conflicts, domain.Conflict{
					ID:         hashKey("rule", p.matcher),
					Type:       "rule",
					Path:       "rules." + p.matcher,
					Local:      lp.raw,
					Remote:     p.raw,
					Resolution: e.policy.RulePriority,
				})
				if e.policy.RulePriority == "remote" {
					for i, existing := range merged.Rules {
						if normalizeKey(parseRule(existing).matcher) == normalizeKey(p.matcher) {
							merged.Rules[i] = p.raw
							break
						}
					}
				}
			}
			continue
		}
		if !seen[normalizeKey(r)] {
			merged.Rules = append(merged.Rules, r)
			seen[normalizeKey(r)] = true
		}
	}

	// MATCH 规则冲突检测：目标不同即冲突
	if localMatchRule != "" && remoteMatchRule != "" {
		lp := parseRule(localMatchRule)
		rp := parseRule(remoteMatchRule)
		if lp.target != rp.target {
			conflicts = append(conflicts, domain.Conflict{
				ID:         matchConflictKey(),
				Type:       "rule",
				Path:       "rules.MATCH",
				Local:      localMatchRule,
				Remote:     remoteMatchRule,
				Resolution: e.policy.RulePriority,
			})
		}
	}

	// MATCH 规则沉底：按合并策略确定采用哪一侧，并追加在末尾
	chosenMatch := localMatchRule
	if e.policy.RulePriority == "remote" && remoteMatchRule != "" {
		chosenMatch = remoteMatchRule
	} else if chosenMatch == "" {
		chosenMatch = remoteMatchRule
	}
	if chosenMatch != "" {
		merged.Rules = append(merged.Rules, chosenMatch)
	}
	// apply resolved overrides
	applyResolvedOverrides(merged, resolved)

	// 清理悬空引用：机场下线节点后，本地策略组仍会引用旧节点名，
	// mihomo 遇到不存在的引用会直接拒绝加载整份配置 —— 表现为每次合并
	// 都校验失败并回滚，配置再也更新不了，而用户看不出根因。
	// 因此在产出前主动摘掉失效引用，并把它作为告警回报。
	warnings := pruneDanglingRefs(merged)

	res := &domain.MergeResult{
		Config:    merged,
		Conflicts: conflicts,
		Warnings:  warnings,
	}
	if previous != nil {
		res.Diff = BuildDiff(previous, merged)
	}
	return res
}

func applyResolvedOverrides(cfg *domain.Config, resolved []domain.Conflict) {
	for _, c := range resolved {
		switch strings.ToLower(c.Resolution) {
		case "local":
			switch c.Type {
			case "proxy":
				if p, ok := decodeProxy(c.Local); ok {
					replaceProxy(cfg, p, false)
				}
			case "rule":
				if s, ok := decodeString(c.Local); ok {
					replaceRule(cfg, s, false)
				}
			}
		case "remote":
			switch c.Type {
			case "proxy":
				if p, ok := decodeProxy(c.Remote); ok {
					replaceProxy(cfg, p, false)
				}
			case "rule":
				if s, ok := decodeString(c.Remote); ok {
					replaceRule(cfg, s, false)
				}
			}
		case "merge":
			// 设计 §13：自动合并 —— 本地为基准，远程补齐缺失字段
			switch c.Type {
			case "proxy":
				lp, okL := decodeProxy(c.Local)
				rp, okR := decodeProxy(c.Remote)
				if okL && okR {
					replaceProxy(cfg, mergeProxyFields(lp, rp), false)
				} else if okR {
					replaceProxy(cfg, rp, false)
				} else if okL {
					replaceProxy(cfg, lp, false)
				}
			case "rule":
				// 规则有顺序语义，无法安全自动合并，退化为保留本地
				if s, ok := decodeString(c.Local); ok {
					replaceRule(cfg, s, false)
				}
			}
		case "manual":
			switch c.Type {
			case "proxy":
				if p, ok := decodeProxy(c.Manual); ok {
					replaceProxy(cfg, p, true)
				}
			case "rule":
				if s, ok := decodeString(c.Manual); ok {
					replaceRule(cfg, s, true)
				}
			case "proxy-group":
				// currently keep as no-op for complex group manual edits
			}
		}
	}
}

func decodeString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		// may be json-encoded string
		var s string
		if json.Unmarshal([]byte(t), &s) == nil {
			return s, true
		}
		return t, t != ""
	case []byte:
		return string(t), len(t) > 0
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		var s string
		if json.Unmarshal(b, &s) == nil {
			return s, true
		}
		return string(b), true
	}
}

func decodeProxy(v any) (domain.Proxy, bool) {
	if p, ok := v.(domain.Proxy); ok && p.Name != "" {
		return p, true
	}
	// raw json string
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		var p domain.Proxy
		if json.Unmarshal([]byte(s), &p) == nil && p.Name != "" {
			return p, true
		}
		// maybe yaml-ish not supported here
	}
	if m, ok := asMap(v); ok {
		b, _ := json.Marshal(m)
		var p domain.Proxy
		if json.Unmarshal(b, &p) == nil && p.Name != "" {
			return p, true
		}
	}
	return domain.Proxy{}, false
}

// replaceProxy 用解决结果覆盖同名节点。
// allowInsert 为 false 时，找不到同名节点就不做任何事 —— 这是必需的：
// conflicts 表保留历史记录，若允许插入，机场早已下线的节点会被
// 历史冲突永久重新注入 config.yaml，内核连它必然失败。
// 只有 manual（用户手写值）才允许新增。
func replaceProxy(cfg *domain.Config, p domain.Proxy, allowInsert bool) {
	for i := range cfg.Proxies {
		if normalizeKey(cfg.Proxies[i].Name) == normalizeKey(p.Name) {
			cfg.Proxies[i] = p
			return
		}
	}
	if allowInsert {
		cfg.Proxies = append(cfg.Proxies, p)
	}
}

// replaceRule 同 replaceProxy：allowInsert 仅对 manual 开放，
// 避免历史冲突把早已移除的规则重新塞回配置。
func replaceRule(cfg *domain.Config, raw string, allowInsert bool) {
	np := parseRuleParts(raw)
	if np.target == "" && !isMatchRule(raw) {
		return
	}
	for i, r := range cfg.Rules {
		rp := parseRuleParts(r)
		if normalizeKey(rp.matcher) == normalizeKey(np.matcher) {
			cfg.Rules[i] = raw
			return
		}
	}
	if allowInsert {
		cfg.Rules = append(cfg.Rules, raw)
	}
}

// ruleParts 拆出规则的匹配部分（matcher，即去掉 target 与可选修饰词后的前缀）
// 与目标策略（target）。合并与 Diff 都按 matcher 判定"是否为同一条规则"。
type ruleParts struct{ matcher, target, raw string }

func parseRuleParts(r string) ruleParts {
	parts := strings.Split(r, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return ruleParts{matcher: r, target: "", raw: r}
	}

	firstUpper := strings.ToUpper(parts[0])

	// MATCH 规则：MATCH,<target>[,no-resolve]
	if firstUpper == "MATCH" {
		if len(parts) >= 2 {
			return ruleParts{matcher: "MATCH", target: parts[1], raw: r}
		}
		return ruleParts{matcher: "MATCH", target: "", raw: r}
	}

	// 普通规则：最后一段可能是修饰词（如 no-resolve）
	if len(parts) >= 3 && strings.EqualFold(parts[len(parts)-1], "no-resolve") {
		target := parts[len(parts)-2]
		matcher := strings.Join(parts[:len(parts)-2], ",")
		return ruleParts{matcher: matcher, target: target, raw: r}
	}

	if len(parts) >= 2 {
		target := parts[len(parts)-1]
		matcher := strings.Join(parts[:len(parts)-1], ",")
		return ruleParts{matcher: matcher, target: target, raw: r}
	}

	return ruleParts{matcher: r, target: "", raw: r}
}

// BuildDiff 对比两份合并后的配置，产出新增/删除/修改三态报告。
// 设计 §14 要求覆盖 proxy / proxy-group / rule / rule-provider 四类，
// 且规则变更（如 `DOMAIN,x,DIRECT` -> `DOMAIN,x,PROXY`）应体现为 changed
// 而非"先删后增"两条互不相关的记录。
func BuildDiff(prev, next *domain.Config) domain.DiffReport {
	out := domain.DiffReport{}
	if prev == nil {
		prev = &domain.Config{}
	}
	if next == nil {
		next = &domain.Config{}
	}

	diffProxies(prev, next, &out)
	diffProxyGroups(prev, next, &out)
	diffRuleProviders(prev, next, &out)
	diffRules(prev, next, &out)
	return out
}

func diffProxies(prev, next *domain.Config, out *domain.DiffReport) {
	prevProxies := map[string]domain.Proxy{}
	for _, p := range prev.Proxies {
		prevProxies[p.Name] = p
	}
	nextProxies := map[string]domain.Proxy{}
	for _, p := range next.Proxies {
		nextProxies[p.Name] = p
		if op, ok := prevProxies[p.Name]; !ok {
			out.Added = append(out.Added, domain.DiffItem{Kind: "proxy", Name: p.Name, To: p})
		} else if !proxyEqual(op, p) {
			out.Changed = append(out.Changed, domain.DiffItem{Kind: "proxy", Name: p.Name, From: op, To: p})
		}
	}
	for name, p := range prevProxies {
		if _, ok := nextProxies[name]; !ok {
			out.Removed = append(out.Removed, domain.DiffItem{Kind: "proxy", Name: name, From: p})
		}
	}
}

func diffProxyGroups(prev, next *domain.Config, out *domain.DiffReport) {
	prevGroups := map[string]domain.ProxyGroup{}
	for _, g := range prev.ProxyGroups {
		prevGroups[g.Name] = g
	}
	nextGroups := map[string]domain.ProxyGroup{}
	for _, g := range next.ProxyGroups {
		nextGroups[g.Name] = g
		if og, ok := prevGroups[g.Name]; !ok {
			out.Added = append(out.Added, domain.DiffItem{Kind: "proxy-group", Name: g.Name, To: g})
		} else if !jsonEqual(og, g) {
			out.Changed = append(out.Changed, domain.DiffItem{Kind: "proxy-group", Name: g.Name, From: og, To: g})
		}
	}
	for name, g := range prevGroups {
		if _, ok := nextGroups[name]; !ok {
			out.Removed = append(out.Removed, domain.DiffItem{Kind: "proxy-group", Name: name, From: g})
		}
	}
}

func diffRuleProviders(prev, next *domain.Config, out *domain.DiffReport) {
	for name, np := range next.RuleProviders {
		if op, ok := prev.RuleProviders[name]; !ok {
			out.Added = append(out.Added, domain.DiffItem{Kind: "provider", Name: name, To: np})
		} else if !jsonEqual(op, np) {
			out.Changed = append(out.Changed, domain.DiffItem{Kind: "provider", Name: name, From: op, To: np})
		}
	}
	for name, op := range prev.RuleProviders {
		if _, ok := next.RuleProviders[name]; !ok {
			out.Removed = append(out.Removed, domain.DiffItem{Kind: "provider", Name: name, From: op})
		}
	}
}

// diffRules 按 matcher 对齐规则，而不是简单的整行字符串集合比对，
// 否则同一 matcher 换了 target（如 DIRECT -> PROXY）会被误判成"删除一条+新增一条"。
func diffRules(prev, next *domain.Config, out *domain.DiffReport) {
	prevByMatcher := map[string][]ruleParts{}
	for _, r := range prev.Rules {
		p := parseRuleParts(r)
		key := normalizeKey(p.matcher)
		prevByMatcher[key] = append(prevByMatcher[key], p)
	}

	for _, r := range next.Rules {
		np := parseRuleParts(r)
		key := normalizeKey(np.matcher)
		bucket := prevByMatcher[key]

		// 完全相同的行视为未变化，直接消费掉，避免被后续误判为新增/删除
		matchedIdx := -1
		for i, op := range bucket {
			if op.raw == np.raw {
				matchedIdx = i
				break
			}
		}
		if matchedIdx >= 0 {
			prevByMatcher[key] = append(bucket[:matchedIdx], bucket[matchedIdx+1:]...)
			continue
		}

		// 同 matcher 但内容不同：视为修改，消费掉一条旧记录
		if len(bucket) > 0 {
			old := bucket[0]
			prevByMatcher[key] = bucket[1:]
			out.Changed = append(out.Changed, domain.DiffItem{Kind: "rule", Name: np.matcher, From: old.raw, To: np.raw})
			continue
		}

		out.Added = append(out.Added, domain.DiffItem{Kind: "rule", Name: r, To: r})
	}

	// 剩下未被消费的旧规则即为被删除的
	for _, bucket := range prevByMatcher {
		for _, op := range bucket {
			out.Removed = append(out.Removed, domain.DiffItem{Kind: "rule", Name: op.raw, From: op.raw})
		}
	}
}

func proxyEqual(a, b domain.Proxy) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func hashKey(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:8])
}

func asMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return m, true
}

// jsonEqual 通过 JSON 序列化比较两个任意值是否等价
func jsonEqual(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
}

// detectSystemConflicts 检测 dns / tun / sniffer 等系统级配置冲突。
// 设计 §11 规定这些字段禁止远程覆盖（Local First），
// 但设计 §12 要求把 dns / tun 列为冲突类型并上报，供用户知情。
//
// Resolution 填 "local"：系统级冲突没有 manual 语义，引擎始终按策略
// 解决进结果配置，持久层据此标记 resolved=1（自动解决，不显示待处理）。
func detectSystemConflicts(base, remote *domain.Config) []domain.Conflict {
	var out []domain.Conflict

	// remote 未声明该段时视为无冲突（零值不参与比较）
	emptyDNS := domain.DNSConfig{}
	if !jsonEqual(remote.DNS, emptyDNS) && !jsonEqual(base.DNS, remote.DNS) {
		out = append(out, domain.Conflict{
			ID:         hashKey("dns", "dns"),
			Type:       "dns",
			Path:       "dns",
			Local:      base.DNS,
			Remote:     remote.DNS,
			Resolution: "local",
		})
	}

	emptyTUN := domain.TUNConfig{}
	if !jsonEqual(remote.TUN, emptyTUN) && !jsonEqual(base.TUN, remote.TUN) {
		out = append(out, domain.Conflict{
			ID:         hashKey("tun", "tun"),
			Type:       "tun",
			Path:       "tun",
			Local:      base.TUN,
			Remote:     remote.TUN,
			Resolution: "local",
		})
	}

	emptySniffer := domain.SnifferConfig{}
	if !jsonEqual(remote.Sniffer, emptySniffer) && !jsonEqual(base.Sniffer, remote.Sniffer) {
		out = append(out, domain.Conflict{
			ID:         hashKey("sniffer", "sniffer"),
			Type:       "sniffer",
			Path:       "sniffer",
			Local:      base.Sniffer,
			Remote:     remote.Sniffer,
			Resolution: "local",
		})
	}

	return out
}

// mergeProxyFields 实现设计 §13 的 "Merge" 解决策略：
// 以本地为基准，用远程的非零字段补齐本地缺失的部分。
func mergeProxyFields(local, remote domain.Proxy) domain.Proxy {
	out := local
	if out.Type == "" {
		out.Type = remote.Type
	}
	if out.Server == "" {
		out.Server = remote.Server
	}
	if out.Port == 0 {
		out.Port = remote.Port
	}
	if out.Extra == nil {
		out.Extra = map[string]interface{}{}
	}
	for k, v := range remote.Extra {
		if _, exists := out.Extra[k]; !exists {
			out.Extra[k] = v
		}
	}
	return out
}

// overlayNonZeroFields 把 src 中所有"已声明"（非零值）的通用标量字段覆盖到 dst。
// 用反射逐字段处理，好处是 domain.Config 以后新增官方参数时无需修改合并逻辑。
//
// 跳过的字段有专门的合并语义，由调用方单独处理：
// DNS / TUN / Sniffer（整段替换）、Proxies / ProxyGroups / Rules / RuleProviders
// （按名称对齐并产出冲突记录）、General（兜底 map 逐键合并）。
//
// 只覆盖非零值，是为了满足"远程未声明该键时保留本地配置"这一要求 —— 否则远程
// 缺失的字段会被零值抹掉本地设置。代价是无法表达"远程显式把某布尔项设为 false"，
// 这在订阅场景下是可接受的取舍：订阅通常只下发它关心的少数键。
func overlayNonZeroFields(dst, src *domain.Config) {
	skip := map[string]bool{
		"DNS": true, "TUN": true, "Sniffer": true,
		"Proxies": true, "ProxyGroups": true, "Rules": true,
		"RuleProviders": true, "General": true,
	}

	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	t := dv.Type()

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if skip[f.Name] || !dv.Field(i).CanSet() {
			continue
		}
		s := sv.Field(i)
		if s.IsZero() {
			continue // 远程未声明，保留本地值
		}
		dv.Field(i).Set(s)
	}
}

// mihomo 内置的策略目标，出现在策略组成员或规则 target 位置都是合法的，
// 不需要在 proxies / proxy-groups 里有对应定义。
var builtinProxyTargets = map[string]bool{
	"DIRECT":      true,
	"REJECT":      true,
	"REJECT-DROP": true,
	"PASS":        true,
	"GLOBAL":      true,
	"COMPATIBLE":  true,
}

// pruneDanglingRefs 摘掉配置里指向不存在目标的引用，返回人类可读的告警列表。
//
// 处理两类：
//  1. 策略组的 proxies 成员指向既不是节点、也不是其他策略组的名字；
//  2. 规则的 target 指向不存在的策略组。
//
// 规则无法「部分保留」，只能整条丢弃；策略组则是移除失效成员，
// 若移除后成员为空，该组本身也会失效（mihomo 不接受空 proxies 的组），
// 此时连同引用它的规则一并清理。
func pruneDanglingRefs(cfg *domain.Config) []string {
	if cfg == nil {
		return nil
	}
	var warnings []string

	proxyNames := make(map[string]bool, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxyNames[p.Name] = true
	}

	// 策略组可以互相引用，需要迭代到稳定：移除空组后可能让引用它的组又变空
	for {
		groupNames := make(map[string]bool, len(cfg.ProxyGroups))
		for _, g := range cfg.ProxyGroups {
			groupNames[g.Name] = true
		}

		changed := false
		kept := make([]domain.ProxyGroup, 0, len(cfg.ProxyGroups))
		for _, g := range cfg.ProxyGroups {
			members := make([]string, 0, len(g.Proxies))
			for _, ref := range g.Proxies {
				if builtinProxyTargets[ref] || proxyNames[ref] || groupNames[ref] {
					members = append(members, ref)
					continue
				}
				changed = true
				warnings = append(warnings,
					fmt.Sprintf("策略组 %q 引用的 %q 已不存在，已自动移除该成员", g.Name, ref))
			}
			g.Proxies = members

			// 成员为空不代表这个组坏了：include-all / include-all-proxies /
			// include-all-providers 由内核自动纳入节点（filter 就依赖它们）。
			// 误删这类组会让订阅里最常见的写法直接失效。
			if len(g.Proxies) == 0 && len(g.Use) == 0 && !groupAutoIncludes(g) {
				changed = true
				warnings = append(warnings,
					fmt.Sprintf("策略组 %q 已无任何可用成员，已自动移除该策略组", g.Name))
				continue
			}
			kept = append(kept, g)
		}
		cfg.ProxyGroups = kept
		if !changed {
			break
		}
	}

	// 规则 target 必须是内置策略、已存在的节点或策略组。
	// no-resolve 是规则的可选第 4 段修饰词，不是 target：
	// 解析时必须剥掉它再校验（否则 "MATCH,DIRECT,no-resolve" 会被当成
	// 指向名为 no-resolve 的策略而误删）。MATCH 类规则同理——其 target
	// 由第 2 段给出，不应让 no-resolve 抢占 target 位置。
	finalGroups := make(map[string]bool, len(cfg.ProxyGroups))
	for _, g := range cfg.ProxyGroups {
		finalGroups[g.Name] = true
	}
	keptRules := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		if isMatchRule(r) {
			// MATCH 是终态兜底：即使目标组缺失（订阅导入的组在合并时被
			// prune 删掉），也绝不整行删除，否则最终配置连兜底都没有。
			// 缺失的是该组的其它规则，这里只保留提示。
			target := parseRuleParts(r).target
			if target != "" {
				if builtinProxyTargets[target] || finalGroups[target] || proxyNames[target] {
					keptRules = append(keptRules, r)
					continue
				}
				warnings = append(warnings,
					fmt.Sprintf("规则 %q 指向的策略 %q 不存在，已保留该规则（缺少兜底出口可能导致流量无规则匹配）", r, target))
				keptRules = append(keptRules, r)
				continue
			}
			keptRules = append(keptRules, r)
			continue
		}
		p := parseRuleParts(r)
		target := p.target
		if target == "" {
			// 单段规则（如缺 target）本身非法，交给内核报错更清晰
			keptRules = append(keptRules, r)
			continue
		}
		// 子规则 sub-rule 的目标写法为 sub-rule:name，不在此校验范围
		if strings.HasPrefix(strings.ToLower(target), "sub-rule") {
			keptRules = append(keptRules, r)
			continue
		}
		if builtinProxyTargets[target] || finalGroups[target] || proxyNames[target] {
			keptRules = append(keptRules, r)
			continue
		}
		warnings = append(warnings,
			fmt.Sprintf("规则 %q 指向的策略 %q 不存在，已自动移除该规则", r, target))
	}
	cfg.Rules = keptRules

	return warnings
}

// groupAutoIncludes 判断策略组是否依赖内核自动纳入节点。
// 这类组即使没有显式 proxies / use 也是合法且可工作的，不能按空组清理。
func groupAutoIncludes(g domain.ProxyGroup) bool {
	for _, k := range []string{"include-all", "include-all-proxies", "include-all-providers"} {
		v, ok := g.Extra[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case bool:
			if t {
				return true
			}
		case string:
			// YAML 里写成 "true" 的情况
			if strings.EqualFold(strings.TrimSpace(t), "true") {
				return true
			}
		}
	}
	return false
}

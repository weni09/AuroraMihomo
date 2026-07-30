package domain

import "strings"

// Conflict represents a merge conflict between local and remote config fragments.
type Conflict struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // proxy|proxy-group|rule|provider
	Path       string `json:"path"`
	Local      any    `json:"local"`
	Remote     any    `json:"remote"`
	Resolution string `json:"resolution"` // local|remote|merge|manual|""
	Manual     any    `json:"manual,omitempty"`
}

// DiffItem is one added/removed/changed entry.
type DiffItem struct {
	Kind string `json:"kind"` // proxy|proxy-group|rule|provider
	Name string `json:"name"`
	From any    `json:"from,omitempty"`
	To   any    `json:"to,omitempty"`
}

// DiffReport compares previous and new merged configs.
type DiffReport struct {
	Added   []DiffItem `json:"added"`
	Removed []DiffItem `json:"removed"`
	Changed []DiffItem `json:"changed"`
}

// MergePolicy controls default automatic resolutions.
type MergePolicy struct {
	ProxyPriority string // local|remote
	RulePriority  string // local|remote
	DNSPriority   string // local|remote，设计 §16 要求但此前未接入引擎
	TUNPriority   string // local|remote，设计 §16 要求但此前字段缺失

	// GeneralPriority 作用于其余所有顶层通用参数（mode、各监听端口、Geo 系列、
	// external-controller 系列、认证、profile、hosts，以及 General 兜底 map 里
	// 未显式建模的官方参数）。此前这些字段被硬编码为永远本地优先，
	// 导致"远程优先"对绝大多数键根本不生效。
	// 无论选哪种策略，远程未声明（零值/缺键）时一律保留本地值。
	GeneralPriority string // local|remote
}

func DefaultMergePolicy() MergePolicy {
	return MergePolicy{
		ProxyPriority:   "local",
		RulePriority:    "local",
		DNSPriority:     "local",
		TUNPriority:     "local",
		GeneralPriority: "local",
	}
}

// 远程配置来源类型。决定 config.yaml 的远程层从何而来。
const (
	// RemoteSourceNone 不使用远程配置，最终配置完全等于配置中心的本地配置。
	// 这是默认值：不做任何选择时不应擅自把订阅内容混进用户的配置。
	RemoteSourceNone = "none"
	// RemoteSourceAll 聚合所有启用的订阅
	RemoteSourceAll = "all"
	// RemoteSourceSubscription 只用指定的一条订阅
	RemoteSourceSubscription = "subscription"
	// RemoteSourceCollection 用指定组合的渲染结果，
	// 组合上配置的处理管道与输出模板一并生效
	RemoteSourceCollection = "collection"
	// RemoteSourceFile 用指定的文件模板分享的渲染结果
	RemoteSourceFile = "file"
	// RemoteSourceURL 直接使用外部订阅链接（别人分享过来的地址），
	// 无需先在 Sub-Store 里建订阅
	RemoteSourceURL = "url"
)

// RemoteSource 描述远程配置层的来源。
//
// 引入动机：此前 buildRemoteConfig 无条件把所有启用订阅聚合在一起，
// 用户无法指定"只用某一条订阅"或"用我已经配好管道的那个组合"，
// 多机场场景下会把互相冲突的节点与策略组全部混进最终配置。
type RemoteSource struct {
	Type string // none|all|subscription|collection|file|url
	// ID 为 Type 对应实体的主键；Type 为 none/all/url 时无意义
	ID int64
	// URL 仅在 Type=url 时使用，为外部分享的订阅地址
	URL string
	// Cron 为定时拉取的调度表达式（6 段，含秒），
	// 与系统设置里的自动更新共用同一套语法。
	Cron string
	// CronEnabled 是否启用定时拉取。关闭后只能手动拉取。
	CronEnabled bool
}

// DefaultRemoteSource 默认不使用远程配置。
//
// 语义要点：默认值是"不填"而非"聚合全部订阅"。用户没做选择时，
// 最终配置就等于他在配置中心写的内容，不会被订阅里的节点、
// 策略组与规则意外覆盖。
func DefaultRemoteSource() RemoteSource {
	return RemoteSource{Type: RemoteSourceNone}
}

// Valid 校验来源配置是否自洽。
// none/all 无需附加信息；实体类需要 ID；url 类需要非空地址。
func (r RemoteSource) Valid() bool {
	switch r.Type {
	case RemoteSourceNone, RemoteSourceAll:
		return true
	case RemoteSourceSubscription, RemoteSourceCollection, RemoteSourceFile:
		return r.ID > 0
	case RemoteSourceURL:
		return strings.TrimSpace(r.URL) != ""
	default:
		return false
	}
}

// MergeResult is the full output of a merge execution.
type MergeResult struct {
	Config    *Config    `json:"config"`
	Conflicts []Conflict `json:"conflicts"`
	Diff      DiffReport `json:"diff"`
	// Warnings 记录合并时自动做出的修正（如摘掉指向已下线节点的引用）。
	// 这些修正不算失败，但用户需要知情，否则会困惑于"我配的策略组怎么没了"。
	Warnings []string `json:"warnings,omitempty"`
}

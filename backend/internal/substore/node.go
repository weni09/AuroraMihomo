package substore

import "auroramihomo/backend/internal/fetcher"

// Node is the unified intermediate proxy representation.
type Node struct {
	Name   string                 `json:"name"`
	Type   string                 `json:"type"`
	Server string                 `json:"server"`
	Port   int                    `json:"port"`
	UDP    bool                   `json:"udp,omitempty"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
	Source string                 `json:"source,omitempty"`
}

type RewriteRule struct {
	Name       string
	Scope      string // name|server|type
	Pattern    string
	Replace    string
	FilterMode string // rewrite|include|exclude
	Enabled    bool
	Priority   int
}

type ConvertRequest struct {
	URL       string
	Content   string
	Source    string
	UserAgent string
	CacheRaw  string
	// Operators 为该条订阅独立的处理管道，在进入组合级管道前先执行
	Operators []PipelineOperator
}

type ConvertResult struct {
	Format       string
	Nodes        []Node
	YAML         string
	Links        string
	RawNodesJSON string
	// UserInfo 为机场下发的流量信息，仅在实际发起网络请求时有值
	UserInfo fetcher.UserInfo
	// UpstreamParams 为订阅原文里可安全采纳的顶层参数（已剔除管理接口等敏感键）。
	// 供"远程优先"策略使用；节点/规则不走这里。
	UpstreamParams map[string]interface{}
}

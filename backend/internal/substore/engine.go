package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/fetcher"
	"auroramihomo/backend/internal/substore/parser"
)

type Engine struct {
	fetcher *fetcher.Client
}

func NewEngine() *Engine {
	return &Engine{fetcher: fetcher.New(0)}
}

// SetRawCDNProviders 把 raw 加速源列表推给内部 fetcher。
// 订阅 URL 可能是 raw.githubusercontent.com 直链，同样享受加速。
func (e *Engine) SetRawCDNProviders(providers []string) {
	e.fetcher.SetRawCDNProviders(providers)
}

// SetRawSuccessCallback 把 raw 加速源成功回调转发给内部 fetcher。
func (e *Engine) SetRawSuccessCallback(fn func(string)) {
	e.fetcher.SetRawSuccessCallback(fn)
}

// SetProxyURLFunc 把本地 mihomo 代理查询回调转发给内部 fetcher。
func (e *Engine) SetProxyURLFunc(fn func() string) {
	e.fetcher.SetProxyURLFunc(fn)
}

// Nodes 只解析出节点，不做改写、管道与渲染，
// 供 ConvertMany 逐个处理子订阅时复用。
func (e *Engine) Nodes(ctx context.Context, req ConvertRequest) ([]Node, error) {
	nodes, _, _, _, err := e.parseNodes(ctx, req)
	return nodes, err
}

// 末位返回订阅原文，供上层提取顶层参数（缓存命中时为 nil）
func (e *Engine) parseNodes(ctx context.Context, req ConvertRequest) ([]Node, Format, fetcher.UserInfo, []byte, error) {
	var nodes []Node
	var format Format
	var userInfo fetcher.UserInfo

	if req.CacheRaw != "" {
		if err := json.Unmarshal([]byte(req.CacheRaw), &nodes); err == nil && len(nodes) > 0 {
			format = FormatUnknown // cached
		}
	}

	var rawDoc []byte
	if len(nodes) == 0 {
		raw := []byte(strings.TrimSpace(req.Content))
		if len(raw) == 0 {
			if strings.TrimSpace(req.URL) == "" {
				return nil, format, userInfo, nil, fmt.Errorf("url or content is required")
			}
			b, info, err := e.fetcher.FetchWithMeta(ctx, req.URL, req.UserAgent)
			if err != nil {
				return nil, format, userInfo, nil, err
			}
			raw, userInfo = b, info
		}

		rawDoc = raw
		format = DetectFormat(raw)
		parsed, err := parseByFormat(format, raw, firstNonEmpty(req.Source, req.URL, "input"))
		if err != nil {
			return nil, format, userInfo, rawDoc, err
		}
		nodes = parsed
	}
	return nodes, format, userInfo, rawDoc, nil
}

func (e *Engine) Convert(ctx context.Context, req ConvertRequest, rules []RewriteRule, ops []PipelineOperator, templateName, templateContent string) (*ConvertResult, error) {
	nodes, format, userInfo, rawDoc, err := e.parseNodes(ctx, req)
	if err != nil {
		return nil, err
	}
	upstreamParams := ExtractUpstreamParams(rawDoc)

	nodes = ApplyRewrite(nodes, rules)
	nodes, err = ApplyPipelineCtx(ctx, nodes, ops)
	if err != nil {
		return nil, err
	}
	nodes = dedupeNodes(nodes)

	// 部分机场不下发 subscription-userinfo 响应头（userInfo 为零值），
	// 只在节点名里写「剩余流量：1000 GB」——从处理后的节点名兜底解析，
	// 让订阅列表仍能显示流量与到期信息（实现见 userinfo.go）。
	if userInfo.IsZero() {
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, n.Name)
		}
		userInfo = parseUserInfoFromNames(names)
	}

	_, rendered, err := RenderTemplate(templateName, templateContent, nodes)
	if err != nil {
		return nil, err
	}
	links := NodesToShareLinks(nodes)

	nodesJson, _ := json.Marshal(nodes)

	return &ConvertResult{
		Format:         string(format),
		Nodes:          nodes,
		YAML:           rendered,
		Links:          links,
		RawNodesJSON:   string(nodesJson),
		UserInfo:       userInfo,
		UpstreamParams: upstreamParams,
	}, nil
}

func (e *Engine) ConvertMany(ctx context.Context, reqs []ConvertRequest, rules []RewriteRule, ops []PipelineOperator, templateName, templateContent string) (*ConvertResult, error) {
	all := make([]Node, 0)
	for _, req := range reqs {
		// 只取节点，不渲染各子订阅的完整配置（此前每个子订阅都白做一次序列化）
		nodes, err := e.Nodes(ctx, req)
		if err != nil {
			return nil, err
		}
		// 先跑单条订阅自己的处理管道，再进入组合级管道，
		// 构成 Sub-Store 的两级流水线
		nodes, err = ApplyPipelineCtx(ctx, nodes, req.Operators)
		if err != nil {
			return nil, fmt.Errorf("订阅 %s 的处理管道执行失败: %w", firstNonEmpty(req.Source, req.URL), err)
		}
		all = append(all, nodes...)
	}
	all = ApplyRewrite(all, rules)
	var err error
	all, err = ApplyPipelineCtx(ctx, all, ops)
	if err != nil {
		return nil, err
	}
	all = dedupeNodes(all)
	_, rendered, err := RenderTemplate(templateName, templateContent, all)
	if err != nil {
		return nil, err
	}
	nodesJSON, _ := json.Marshal(all)
	return &ConvertResult{
		Format:       "collection",
		Nodes:        all,
		YAML:         rendered,
		Links:        NodesToShareLinks(all),
		RawNodesJSON: string(nodesJSON),
	}, nil
}

func parseByFormat(format Format, raw []byte, source string) ([]Node, error) {
	if format == FormatShareLinks {
		if dec, err := decodeMaybeBase64(string(raw)); err == nil && looksLikeShareLinks(string(dec)) {
			raw = dec
		}
	}
	switch format {
	case FormatClashYAML:
		return mapParserNodes(parser.ParseClashYAML(raw, source))
	case FormatV2RayJSON:
		return mapParserNodes(parser.ParseV2RayJSON(raw, source))
	case FormatShareLinks:
		return mapParserNodes(parser.ParseShareLinks(string(raw), source))
	case FormatSIP008:
		return mapParserNodes(parser.ParseSIP008(raw, source))
	case FormatSurge:
		return mapParserNodes(parser.ParseSurge(string(raw), source))
	case FormatQuantumultX:
		return mapParserNodes(parser.ParseQuantumultX(string(raw), source))
	default:
		if nodes, err := parser.ParseClashYAML(raw, source); err == nil && len(nodes) > 0 {
			return mapParserNodes(nodes, nil)
		}
		if nodes, err := parser.ParseShareLinks(string(raw), source); err == nil && len(nodes) > 0 {
			return mapParserNodes(nodes, nil)
		}
		if nodes, err := parser.ParseV2RayJSON(raw, source); err == nil && len(nodes) > 0 {
			return mapParserNodes(nodes, nil)
		}
		return nil, fmt.Errorf("unsupported or undetectable subscription format")
	}
}

func mapParserNodes(nodes []parser.Node, err error) ([]Node, error) {
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, Node{
			Name:   n.Name,
			Type:   n.Type,
			Server: n.Server,
			Port:   n.Port,
			UDP:    n.UDP,
			Extra:  n.Extra,
			Source: n.Source,
		})
	}
	return out, nil
}

// dedupeNodes 按「协议+服务器+端口(+凭据)」判定真正的重复节点。
// 曾经按 Name 去重，会把多机场里同叫「香港 01」的不同节点静默删掉一个。
// 去重后仍可能重名，而 Clash 系配置以节点名作为引用键，
// 因此对重名节点追加序号后缀以保证配置可用。
func dedupeNodes(nodes []Node) []Node {
	seen := map[string]bool{}
	nameCount := map[string]int{}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		key := fmt.Sprintf("%s|%s|%d", strings.ToLower(n.Type), n.Server, n.Port)
		if extra := identitySuffix(n); extra != "" {
			key += "|" + extra
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		if n.Name == "" {
			n.Name = fmt.Sprintf("%s-%s-%d", n.Type, n.Server, n.Port)
		}
		nameCount[n.Name]++
		if c := nameCount[n.Name]; c > 1 {
			n.Name = fmt.Sprintf("%s #%d", n.Name, c)
		}
		out = append(out, n)
	}
	return out
}

// identitySuffix 补充参与去重的身份字段，
// 同一 server:port 上以不同凭据区分的节点不应被误判为重复。
func identitySuffix(n Node) string {
	for _, k := range []string{"uuid", "password", "psk", "username"} {
		if v, ok := n.Extra[k].(string); ok && v != "" {
			return k + "=" + v
		}
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/substore"
)

// 订阅流量参数探测。
//
// V2Board 类机场只在特定 flag 查询参数下才下发 subscription-userinfo
// 响应头（实测：flag=clashmeta 返回完整流量+节点、flag=clash 返回占位
// 提示节点、无参数则完全没有流量信息）。探测接口按候选参数逐一拉取
// 订阅，找出「有流量信息且节点完整」的组合，供前端一键应用，
// 替代用户手工在地址后加 &flag=clashmeta。

// ProbeCandidate 是单条参数组合的探测结果。
type ProbeCandidate struct {
	Params      string `json:"params"`      // 追加的查询参数原文（"" 表示不追加）
	URL         string `json:"url"`         // 完整探测 URL
	HasUserInfo bool   `json:"hasUserInfo"` // 响应带 subscription-userinfo 且非零
	UsedBytes   int64  `json:"usedBytes"`   // 已用流量（upload+download）
	TotalBytes  int64  `json:"totalBytes"`  // 套餐总量
	NodeCount   int    `json:"nodeCount"`   // 解析出的节点数
	Placeholder bool   `json:"placeholder"` // 节点疑似占位（机场对不支持的客户端格式返回提示节点）
	Error       string `json:"error,omitempty"`
}

// probeCandidates 候选参数：V2Board 系常见 flag，无参数放首位作基线对照。
var probeCandidates = []string{"", "flag=clashmeta", "flag=meta", "flag=singbox", "flag=clash", "flag=sub", "flag=v2ray"}

// probeTimeout 单条候选的拉取超时。探测是显式按钮操作，候选较多时
// 整体可达一分钟量级，可接受；单条超时只标记 Error，不中断其余候选。
const probeTimeout = 10 * time.Second

// ProbeSubscriptionParams 并发尝试候选参数并返回全部结果。
// 第二个返回值 bestURL 为「有流量信息、非占位、节点数最多」的最佳
// 组合的完整 URL；没有任何可用组合时为空串。
//
// 并发而非串行：候选各自拉取外部 URL，DNS/网络慢时串行会把总时长
// 放大成「候选数 × 单条超时」（7 × 10s），并发后总时长收敛到单条
// 上限，探测按钮的等待体验才可接受。
func (s *ConfigService) ProbeSubscriptionParams(ctx context.Context, baseURL, userAgent string) ([]ProbeCandidate, string) {
	baseURL = strings.TrimSpace(baseURL)
	candidates := make([]ProbeCandidate, len(probeCandidates))
	var wg sync.WaitGroup
	for i, params := range probeCandidates {
		wg.Add(1)
		go func(i int, params string) {
			defer wg.Done()
			probeURL := appendQueryParam(baseURL, params)
			candidates[i] = s.probeOne(ctx, probeURL, params, userAgent)
		}(i, params)
	}
	wg.Wait()

	bestURL, bestScore := "", -1
	for _, c := range candidates {
		if c.HasUserInfo && !c.Placeholder && c.NodeCount > 0 && c.NodeCount > bestScore {
			bestScore = c.NodeCount
			bestURL = c.URL
		}
	}
	return candidates, bestURL
}

func (s *ConfigService) probeOne(ctx context.Context, probeURL, params, userAgent string) ProbeCandidate {
	c := ProbeCandidate{Params: params, URL: probeURL}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	// 走 substore 全链路：fetch（含 SSRF 校验与重定向逐跳检查）+
	// 节点解析 + userinfo 兜底解析。渲染内置模板的开销可忽略。
	res, err := s.ssEngine.Convert(pctx, substore.ConvertRequest{
		URL:       probeURL,
		UserAgent: strings.TrimSpace(userAgent),
	}, nil, nil, "mihomo-yaml", "")
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.NodeCount = len(res.Nodes)
	if !res.UserInfo.IsZero() {
		c.HasUserInfo = true
		c.UsedBytes = res.UserInfo.Used()
		c.TotalBytes = res.UserInfo.Total
	}
	c.Placeholder = nodesArePlaceholder(res.Nodes)
	return c
}

// nodesArePlaceholder 机场对不支持的客户端格式常返回单个提示节点
// （如 V2Board 的「当前Clash客户端不支持本机场协议」），此时该组合
// 应视为不可用而非可解析成功。
func nodesArePlaceholder(nodes []substore.Node) bool {
	if len(nodes) == 0 || len(nodes) > 3 {
		return false
	}
	for _, n := range nodes {
		name := strings.ToLower(n.Name)
		if strings.Contains(name, "不支持") || strings.Contains(name, "占位") ||
			strings.Contains(name, "not support") {
			return true
		}
	}
	return false
}

// appendQueryParam 在 URL 上追加查询参数。用字符串拼接而非 url.Values
// 重编码：保持用户原始 URL 的编码风格不变（探测结果会直接回填表单）。
func appendQueryParam(raw, params string) string {
	params = strings.TrimSpace(params)
	if params == "" {
		return raw
	}
	if strings.Contains(raw, "?") {
		return raw + "&" + params
	}
	return raw + "?" + params
}

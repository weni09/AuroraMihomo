// Package diagnostics 提供网络诊断能力。
//
// 本文件实现探测框架：Probe 接口、统一结果结构 ProbeResult、
// Run 执行器（目标×探测顺序展开、每探测独立超时、进度回调）。
// 具体探测器（ping/dns/tcp/http/traceroute）在后续任务中实现，
// 并注册进 Probes()；本阶段注册表为空。
package diagnostics

import (
	"context"
	"sync"
	"time"
)

// 探测类型。
const (
	TypePing       = "ping"
	TypeDNS        = "dns"
	TypeTCP        = "tcp"
	TypeHTTP       = "http"
	TypeTraceroute = "traceroute"
)

// 出网路径。
const (
	PathDirect = "direct"
	PathProxy  = "proxy"
)

// 单步探测结果状态。
const (
	StatusSuccess = "success"
	StatusFail    = "fail"
	StatusTimeout = "timeout"
	StatusError   = "error"
)

// DiagnosticTarget 描述一次探测的目标。
type DiagnosticTarget struct {
	Type   string `json:"type"`           // ping|dns|tcp|http|traceroute
	Target string `json:"target"`         // 主机/域名/URL
	Port   int    `json:"port,omitempty"` // 仅 tcp 探测使用
}

// ProbeResult 是单个探测的统一结果，直接序列化给前端展示。
type ProbeResult struct {
	Target    string `json:"target"`
	Type      string `json:"type"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Detail    any    `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ProgressFunc 在单步探测完成时回调，用于发布进度事件。
type ProgressFunc func(ProbeResult)

// Probe 是单个探测器的抽象。
//
// 实现方须遵守：
//   - 用 ctx 控制生命周期：ctx 超时/取消后应尽快返回，把超时表现为
//     StatusTimeout——框架只负责把带超时的 context 交给探测，不替探测
//     判断结果状态（目标无效等错误由探测内部处理并回填 StatusError）。
//   - 有阶段性进展（如 traceroute 逐跳）时通过 cb 上报，完成时也应
//     调用一次 cb，让进度流不缺最后一步。
type Probe interface {
	Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult
}

// ProbeFunc 把普通函数适配为 Probe，便于测试注入与简单实现。
type ProbeFunc func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult

// Run 实现 Probe 接口。
func (f ProbeFunc) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	return f(ctx, target, path, cb)
}

// Run 是一次诊断执行：一组目标 × 一组探测，顺序展开。
//
// RequestID/Targets/Path/Timeout/Probes 是执行配置，Execute 期间不应修改。
// Results 与 Done 由 Execute 写入并受 mu 保护；Execute 同步返回后直接
// 读取是安全的，并发读取须自行持锁。
type Run struct {
	RequestID string
	Targets   []DiagnosticTarget
	Path      string
	Timeout   time.Duration
	Probes    []Probe

	mu      sync.Mutex
	Results []ProbeResult
	Done    bool
}

// NewRun 构造一次诊断运行。
func NewRun(requestID string, targets []DiagnosticTarget, path string, timeout time.Duration, probes []Probe) *Run {
	return &Run{
		RequestID: requestID,
		Targets:   targets,
		Path:      path,
		Timeout:   timeout,
		Probes:    probes,
	}
}

// Execute 顺序执行所有探测（目标 × 探测展开）。
//
// 语义：
//   - 每个探测拿到独立 context：继承外部 ctx 并叠加 Timeout 超时，
//     单个探测卡死不会拖垮整轮；
//   - 单探测失败/超时不中断整体，结果照常回填；
//   - 每步完成后回调 onProgress（与传给探测的 cb 是同一个），
//     onProgress 为 nil 时跳过；
//   - 全部完成后置 Done。
func (r *Run) Execute(ctx context.Context, onProgress ProgressFunc) {
	for _, target := range r.Targets {
		for _, probe := range r.Probes {
			// 每个探测独立超时：目标无效等由探测内部处理，这里只保护单步总时长
			pctx, cancel := context.WithTimeout(ctx, r.Timeout)
			res := probe.Run(pctx, target, r.Path, onProgress)
			cancel()
			r.mu.Lock()
			r.Results = append(r.Results, res)
			r.mu.Unlock()
			if onProgress != nil {
				onProgress(res)
			}
		}
	}
	r.mu.Lock()
	r.Done = true
	r.mu.Unlock()
}

// Probes 返回已注册的探测器集合，key 为探测类型（TypePing 等）。
// 具体探测器在后续任务中实现并注册；本阶段返回空集合，
// 未注册的探测类型由调用方负责给出错误结果。
func Probes() map[string]Probe {
	// 后续任务（ping/dns/tcp/http/traceroute）落地后在此填充
	return map[string]Probe{}
}

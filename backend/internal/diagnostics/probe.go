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

// ProgressFunc 是进度回调：框架在单个探测完成后统一回调一次，
// 携带该探测的最终结果。
type ProgressFunc func(ProbeResult)

// Probe 是单个探测器的抽象。
//
// 实现方须遵守：
//   - 用 ctx 控制生命周期：ctx 超时/取消后应尽快返回，把超时表现为
//     StatusTimeout——框架只负责把带超时的 context 交给探测，不替探测
//     判断结果状态（目标无效等错误由探测内部处理并回填 StatusError）。
//   - cb 是「可选」的分步进度回调（如 traceroute 逐跳），仅用于上报中间
//     步骤；当前框架不会传入（为 nil），使用前必须先判空。探测完成时
//     不得调用 cb 上报最终结果——最终结果由框架在 Run 返回后统一回调
//     一次，避免同一结果触发两次进度事件。
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
//   - 进度回调单一所有者：每个探测返回后框架统一调用一次 onProgress(res)
//     （onProgress 为 nil 时跳过）；传给探测的 cb 当前为 nil，仅保留给
//     未来的分步探测，探测不得用它上报最终结果；
//   - 全部完成后置 Done。
func (r *Run) Execute(ctx context.Context, onProgress ProgressFunc) {
	for _, target := range r.Targets {
		for _, probe := range r.Probes {
			// 每个探测独立超时：目标无效等由探测内部处理，这里只保护单步总时长
			pctx, cancel := context.WithTimeout(ctx, r.Timeout)
			// 当前无分步探测器，cb 传 nil；最终结果统一由下方 onProgress 回调
			res := probe.Run(pctx, target, r.Path, nil)
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

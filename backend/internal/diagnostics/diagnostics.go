// Package diagnostics 提供网络诊断能力。
//
// 本文件实现诊断服务生命周期：并发信号量（满则 ErrBusy）、
// 结果缓存（TTL 后淘汰）、Cancel/Close 生命周期管理。
// 执行层复用 probe.go 的 Run/Execute/Snapshot 契约。
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

// diagSeq 是进程内单调递增序列，用于保证 requestId 唯一。
//
// 实测（Windows Server 2025/虚拟机）time.Now().UnixNano() 时钟粒度极粗，
// 连续调用大量返回同一值（10 万次调用仅 5 个不同值），裸 diag-<unixnano>
// 会碰撞、互相覆盖缓存条目。追加序列号后既保留时间戳前缀（规范格式的意图），
// 又在任意平台上保证唯一。
var diagSeq atomic.Uint64

// newRequestID 生成唯一请求 ID：diag-<unixnano>-<seq>。
func newRequestID() string {
	return fmt.Sprintf("diag-%d-%d", time.Now().UnixNano(), diagSeq.Add(1))
}

// EventTypeProgress 是单步进度事件的发布类型，订阅方（前端 WebSocket）据此过滤。
const EventTypeProgress = "diagnostic.progress"

// totalRunTimeout 单次诊断请求的总时限：单探测各有超时（服务级或
// TimeoutProbe 覆盖），但整个请求（both 路径 × 多目标）仍可能累积很久，
// 以 60s 上限统一收口，超时后已完成的探测结果照常保留（finish 置 Done）。
const totalRunTimeout = 60 * time.Second

// 服务级错误。
var (
	// ErrBusy 表示并发上限已满，本次诊断请求被拒绝。
	ErrBusy = errors.New("诊断并发已满，请稍后重试")
	// ErrClosed 表示诊断服务已关停，不再接受新的诊断请求。
	ErrClosed = errors.New("诊断服务已关停")
)

// Config 是诊断服务的配置。
type Config struct {
	// MaxConcurrent 同时执行的诊断请求上限；<=0 时默认 3。
	MaxConcurrent int
	// ResultTTL 结果缓存保留时长，到期后 GetResult 返回不存在；<=0 时默认 10 分钟。
	ResultTTL time.Duration
	// ProbeTimeout 单个探测的超时；<=0 时默认 5 秒。
	ProbeTimeout time.Duration
	// Probes 探测器注册表，key 为探测类型（TypePing 等）；空时退化为 Probes()。
	Probes map[string]Probe
	// Publish 进度事件发布回调（对接 realtime.Hub.Publish）；为 nil 时跳过发布。
	Publish func(eventType string, data interface{})
	// ProxyURL 返回当前代理地址，供 defaultClientSelector 构造 proxy 路径客户端；
	// 代理不可用（nil/空串）时 proxy 路径回落直连。与 updater 同一来源。
	ProxyURL func() string
	// ClientSelector 按出网路径返回 http.Client；nil 时 New 用 defaultClientSelector
	// （基于 ProxyURL）构造。HTTP/TCP/Ping 探测器的 proxy 路径据此走代理而非直连，
	// 直连/代理对比输出真实路径的结果。
	ClientSelector ClientSelector
	// CapNetAdminFn 返回当前进程是否持有 CAP_NET_ADMIN；nil 表示未知。
	// TProxy 下缺 CAP_NET_ADMIN 时面板无法给直连流量打 PanelMark 绕开自身规则
	// （netcheck/sockmark.go：打标失败不阻断拨号），直连探测实际被 TPROXY 接管、
	// 与代理结果趋同且无提示。仅在明确无权限（非 nil 且返回 false）时对 direct
	// 路径结果标注 transparentNote；nil/未知不标，避免误报。
	CapNetAdminFn func() bool
}

// DiagnosticRequest 是一次诊断请求。
type DiagnosticRequest struct {
	Targets []DiagnosticTarget
	// Path 出网路径：both（默认，展开为 direct+proxy）/direct/proxy。
	Path string
}

// DiagnosticEvent 是单步进度事件，经 Publish 以 EventTypeProgress 发布。
type DiagnosticEvent struct {
	RequestID string      `json:"requestId"`
	Target    string      `json:"target"`
	Type      string      `json:"type"`
	Path      string      `json:"path"`
	Status    string      `json:"status"`
	LatencyMs int64       `json:"latencyMs,omitempty"`
	Detail    interface{} `json:"detail,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// runEntry 是缓存中的一条运行记录。
type runEntry struct {
	run    *Run
	cancel context.CancelFunc
	// done 在全部执行阶段完成（或被取消）后关闭；GetResult 据此给出正确的 Done 语义——
	// both 路径会执行 direct+proxy 两个阶段，不能直接透传 Run.done（第一阶段结束即置位）。
	done chan struct{}
}

// Service 管理诊断生命周期：并发信号量、结果缓存、取消与关停。
type Service struct {
	cfg Config
	// capNetAdminFn 取 Config.CapNetAdminFn：直连探测完成后决定是否追加
	// 透明代理接管标注（见 Config.CapNetAdminFn 注释）。
	capNetAdminFn func() bool

	mu     sync.Mutex
	sem    chan struct{}
	runs   map[string]runEntry
	closed bool
}

// New 构造诊断服务，对未设置的配置项填充默认值。
func New(cfg Config) *Service {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	if cfg.ResultTTL <= 0 {
		cfg.ResultTTL = 10 * time.Minute
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 5 * time.Second
	}
	if len(cfg.Probes) == 0 {
		cfg.Probes = Probes()
	}
	if cfg.ClientSelector == nil {
		cfg.ClientSelector = defaultClientSelector(cfg.ProxyURL)
	}
	return &Service{
		cfg:           cfg,
		capNetAdminFn: cfg.CapNetAdminFn,
		sem:           make(chan struct{}, cfg.MaxConcurrent),
		runs:          map[string]runEntry{},
	}
}

// ProxyURLFunc 返回当前代理地址获取函数（与 proxy 路径探测同一来源，
// 即注入 Config.ProxyURL 的回调）。预设目标端点用它解析代理主机+端口，
// 追加代理连通性探测目标；回调本身可能返回空串（代理未配置/未运行），
// 由调用方（DefaultTargets）负责兜底跳过。
func (s *Service) ProxyURLFunc() func() string {
	return s.cfg.ProxyURL
}

// Run 提交一次诊断请求，立即返回 requestId。
//
//   - 并发信号量已满时返回 ErrBusy（不排队、不阻塞）；
//   - requestId 为 diag-<unixnano>-<seq>，保证进程内唯一；
//   - 执行在后台 goroutine 中进行：Path 为 both 时按 direct、proxy 两个阶段各执行
//     一次 Execute（单路径直接一次），每步结果经 publishProgress 发布进度事件；
//   - 完成后释放信号量；结果缓存保留 ResultTTL 后自动淘汰；
//   - Cancel/Close 通过取消执行 context 中断运行。
func (s *Service) Run(ctx context.Context, req DiagnosticRequest) (string, error) {
	if len(req.Targets) == 0 {
		return "", errors.New("诊断目标为空")
	}
	path := req.Path
	if path == "" {
		path = "both"
	}
	requestID := newRequestID()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", ErrClosed
	}
	select {
	case s.sem <- struct{}{}:
	default:
		s.mu.Unlock()
		return "", ErrBusy
	}
	// 总时限收口（见 totalRunTimeout）：与调用方 ctx 组合，取更早截止。
	rctx, cancel := context.WithTimeout(ctx, totalRunTimeout)
	// 每轮注入出网选择器：探测器的 proxy 路径需要它取代理地址/客户端。
	// 克隆探测实例（见 withSelector），不污染 cfg.Probes 上的共享实例。
	probes := s.cfg.Probes
	if s.cfg.ClientSelector != nil {
		probes = withSelector(s.cfg.Probes, s.cfg.ClientSelector)
	}
	run := NewRun(requestID, req.Targets, path, s.cfg.ProbeTimeout, probes)
	s.runs[requestID] = runEntry{run: run, cancel: cancel, done: make(chan struct{})}
	s.mu.Unlock()

	go s.execute(requestID, run, rctx, path)
	return requestID, nil
}

// execute 在后台执行一次诊断：按路径展开逐阶段执行 Execute，完成后收尾。
func (s *Service) execute(requestID string, run *Run, ctx context.Context, path string) {
	paths := expandPaths(path)
	for i, p := range paths {
		// 阶段间检查取消：Cancel/Close/父 context 取消后跳过剩余阶段，
		// 避免已取消的 context 再产生 spurious timeout 结果与事件。
		// 总时限（totalRunTimeout）到达同样在此跳过——但会补发 synthetic
		// error 结果，保证 both 运行每个阶段都有结果，前端能看到被跳过
		// 路径的对比占位；主动取消则静默跳过（Cancel 语义：用户中断不再
		// 产出剩余阶段结果）。
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				s.emitSkippedStages(requestID, run, paths[i:])
			}
			break
		}
		// 两个阶段复用同一个 Run：Execute 会把 r.Path 传给探测器并写进结果。
		// 在两次 Execute 之间（而非期间）改 Path，让 direct/proxy 阶段产出
		// 各自路径的结果；Snapshot 不读 Path，无并发风险。
		run.Path = p
		// TProxy 下无 CAP_NET_ADMIN 时面板无法给直连流量打 PanelMark 绕开
		// 自身规则（netcheck/sockmark.go：打标失败不阻断拨号），直连探测实际
		// 被 TPROXY 接管、与代理结果趋同。对 direct 路径结果统一标注透明代理
		// 接管提示：进度事件（withTransparentNote 复制后标注）与最终结果
		// （AnnotateTransparentNote 锁内替换）都带上。仅明确无权限（fn 非 nil
		// 且返回 false）时标注；未知不标，避免误报。
		annotate := p == PathDirect && s.capNetAdminFn != nil && !s.capNetAdminFn()
		run.Execute(ctx, func(res ProbeResult) {
			if annotate {
				res.Detail = withTransparentNote(res.Detail)
			}
			s.publishProgress(requestID, res)
		})
		if annotate {
			run.AnnotateTransparentNote(p)
		}
	}
	s.finish(requestID)
}

// emitSkippedStages 为被总时限跳过（未执行）的阶段补发 synthetic error 结果：
// 每个目标一条（Path=被跳过路径，Status=error，Error 说明总时限），并发布
// 对应进度事件。让 both 运行的每个阶段都有结果，前端直连/代理对比不丢一侧。
func (s *Service) emitSkippedStages(requestID string, run *Run, skipped []string) {
	for _, p := range skipped {
		for _, t := range run.Targets {
			res := ProbeResult{
				Target: t.Target,
				Type:   t.Type,
				Path:   p,
				Status: StatusError,
				Error:  "总时限已到，跳过该路径阶段",
			}
			run.appendResult(res)
			s.publishProgress(requestID, res)
		}
	}
}

// finish 标记整个请求完成（含被取消的情况）、释放信号量，并安排 TTL 淘汰缓存。
func (s *Service) finish(requestID string) {
	s.mu.Lock()
	var cancel context.CancelFunc
	if entry, ok := s.runs[requestID]; ok {
		close(entry.done)
		cancel = entry.cancel
	}
	s.mu.Unlock()

	// 锁外释放派生 context：父 ctx 为 cancelCtx 时子 context 滞留父 children map，
	// 长父 ctx 下会线性累积；Cancel/Close 也会调 cancel，CancelFunc 幂等且并发安全。
	if cancel != nil {
		cancel()
	}

	<-s.sem // 释放并发信号量

	if ttl := s.cfg.ResultTTL; ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.runs, requestID)
			s.mu.Unlock()
		})
	}
}

// GetResult 返回一次诊断的只读快照。id 不存在或缓存已淘汰时返回 false。
// 返回的是 RunSnapshot 深拷贝，不暴露内部 *Run。
func (s *Service) GetResult(id string) (RunSnapshot, bool) {
	s.mu.Lock()
	entry, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		return RunSnapshot{}, false
	}
	// 先观察 done 再取快照：finish 在所有结果 append 完成后才 close(done)，
	// close 与其后的快照建立 happens-before，保证 Done=true 的快照必含
	// both 路径的全部阶段结果。旧顺序（先快照后观察）可能快照缺最后阶段
	// 却返回 Done=true，轮询方停止轮询丢结果。
	select {
	case <-entry.done:
		snap := entry.run.Snapshot()
		snap.Done = true
		return snap, true
	default:
		return entry.run.Snapshot(), true
	}
}

// Cancel 中断一次正在执行的诊断：取消执行 context，探测尽快返回后置 Done。
// 对已完成/已淘汰的 id 是空操作。
func (s *Service) Cancel(id string) {
	s.mu.Lock()
	entry, ok := s.runs[id]
	s.mu.Unlock()
	if ok && entry.cancel != nil {
		entry.cancel()
	}
}

// Close 关停服务：不再接受新请求，并取消所有在途诊断。幂等。
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	entries := make([]runEntry, 0, len(s.runs))
	for _, e := range s.runs {
		entries = append(entries, e)
	}
	s.mu.Unlock()
	for _, e := range entries {
		if e.cancel != nil {
			e.cancel()
		}
	}
}

// publishProgress 把单步探测结果包装为 DiagnosticEvent 并经配置的 Publish 发布。
func (s *Service) publishProgress(requestID string, res ProbeResult) {
	if s.cfg.Publish == nil {
		return
	}
	s.cfg.Publish(EventTypeProgress, DiagnosticEvent{
		RequestID: requestID,
		Target:    res.Target,
		Type:      res.Type,
		Path:      res.Path,
		Status:    res.Status,
		LatencyMs: res.LatencyMs,
		Detail:    res.Detail,
		Error:     res.Error,
	})
}

// expandPaths 把请求路径展开为实际执行阶段：both（含空）展开为 direct+proxy，
// 单路径保持原样。
func expandPaths(path string) []string {
	switch path {
	case "", "both":
		return []string{PathDirect, PathProxy}
	default:
		return []string{path}
	}
}

// ValidateTarget 校验诊断目标 URL。
//
// 复用 fetcher 的协议/SSRF 校验（订阅源同款防线）：拒绝非 http/https 协议、
// 云 metadata / 链路本地地址。DNS 重绑定由探测时的 guardedDialContext 兜底。
func ValidateTarget(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("诊断目标为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("诊断目标不是合法 URL: %w", err)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("诊断目标缺少主机名")
	}
	return fetcher.ValidateFetchURLExternal(raw)
}

// transparentNoteText 是 direct 路径在无 CAP_NET_ADMIN 时追加的 Detail 标注。
//
// 透明代理（TProxy）下，缺 CAP_NET_ADMIN 无法给直连流量打 PanelMark 绕开
// 自身规则（netcheck/sockmark.go 明确「失败不阻断拨号」），直连探测实际被
// TPROXY 接管——直连/代理结果趋同且无提示。标注提醒用户当前直连结果可能
// 并非真实直连。仅明确无权限时标注，未知（CapNetAdminFn 为 nil）不标。
const transparentNoteText = "直连可能被透明代理接管（无 CAP_NET_ADMIN）"

// withTransparentNote 把透明代理接管标注并入 Detail：复制探测器回填的 map
// 再加键（Detail 为 nil 或非 map 时新建），返回新 map，不修改原 map——
// 进度事件与并发 Snapshot 读到的都是完整、不可变的数据。
func withTransparentNote(detail any) any {
	m, ok := detail.(map[string]interface{})
	if !ok || m == nil {
		m = map[string]interface{}{}
	} else {
		cp := make(map[string]interface{}, len(m)+1)
		for k, v := range m {
			cp[k] = v
		}
		m = cp
	}
	m["transparentNote"] = transparentNoteText
	return m
}

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
	// ProxyURL 返回当前代理地址，供后续探测器（proxy 路径）使用；本阶段仅存储。
	ProxyURL func() string
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
	return &Service{
		cfg:  cfg,
		sem:  make(chan struct{}, cfg.MaxConcurrent),
		runs: map[string]runEntry{},
	}
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
	rctx, cancel := context.WithCancel(ctx)
	run := NewRun(requestID, req.Targets, path, s.cfg.ProbeTimeout, s.cfg.Probes)
	s.runs[requestID] = runEntry{run: run, cancel: cancel, done: make(chan struct{})}
	s.mu.Unlock()

	go s.execute(requestID, run, rctx, path)
	return requestID, nil
}

// execute 在后台执行一次诊断：按路径展开逐阶段执行 Execute，完成后收尾。
func (s *Service) execute(requestID string, run *Run, ctx context.Context, path string) {
	for _, p := range expandPaths(path) {
		// 两个阶段复用同一个 Run：Execute 会把 r.Path 传给探测器并写进结果。
		// 在两次 Execute 之间（而非期间）改 Path，让 direct/proxy 阶段产出
		// 各自路径的结果；Snapshot 不读 Path，无并发风险。
		run.Path = p
		run.Execute(ctx, func(res ProbeResult) {
			s.publishProgress(requestID, res)
		})
	}
	s.finish(requestID)
}

// finish 标记整个请求完成（含被取消的情况）、释放信号量，并安排 TTL 淘汰缓存。
func (s *Service) finish(requestID string) {
	s.mu.Lock()
	if entry, ok := s.runs[requestID]; ok {
		close(entry.done)
	}
	s.mu.Unlock()

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
	snap := entry.run.Snapshot()
	// Done 表示整个请求（含 both 的两个阶段）全部完成，而非单个 Execute 阶段。
	select {
	case <-entry.done:
		snap.Done = true
	default:
		snap.Done = false
	}
	return snap, true
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
// TODO(Task 3): fetcher.ValidateFetchURLExternal 落地后改为调用该导出函数，
// 复用订阅源已有的协议/SSRF 校验；当前实现仅做纯 URL 净校验：
// scheme 必须为 http/https、host 非空。
func ValidateTarget(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("诊断目标为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("诊断目标不是合法 URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("诊断目标协议 %q 不受支持，仅支持 http/https", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("诊断目标缺少主机名")
	}
	return nil
}

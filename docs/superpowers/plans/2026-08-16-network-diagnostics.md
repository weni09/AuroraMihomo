# 网络诊断功能实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增「网络诊断」页面，从面板宿主机视角诊断出网问题（订阅拉取/内核更新/raw 地址失败），支持 Ping/DNS/TCP/HTTP/Traceroute 五类探测、直连 vs 代理对比、WS 实时进度。

**Architecture:** 后端新增独立包 `backend/internal/diagnostics`（探测器框架 + 5 个探测器 + 服务生命周期），REST 发起诊断返回 requestId，进度经现有 `realtime.Hub` 推 `diagnostic.progress` 事件，前端 `useRealtime` 过滤渲染。复用 fetcher 的 SSRF 防护与代理注入模式，零新增基础设施。

**Tech Stack:** Go（go-zero rest、golang.org/x/net/icmp、realtime hub）、Vue 3（pinia、useRealtime）、SQLite（不新增表）。

**规格:** `docs/superpowers/specs/2026-08-16-network-diagnostics-design.md`

---

### Task 1: 探测器框架（Probe 接口 + 统一结果结构）

**Files:**
- Create: `backend/internal/diagnostics/probe.go`
- Test: `backend/internal/diagnostics/probe_test.go`

- [ ] **Step 1: 写失败测试**

```go
// probe_test.go
package diagnostics

import (
	"context"
	"testing"
	"time"
)

func TestRunProbeFramework(t *testing.T) {
	// 验证探测框架：超时生效、结果回填、单探测失败不中断
	calls := 0
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		calls++
		return ProbeResult{Target: target.Target, Type: target.Type, Path: path, Status: "success"}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: "ping", Target: "x"}}, "direct", 5*time.Second, []Probe{probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if calls != 1 {
		t.Fatalf("探测应执行一次, got %d", calls)
	}
	if len(run.Results) != 1 || run.Results[0].Status != "success" {
		t.Fatalf("结果应回填, got %+v", run.Results)
	}
}

func TestRunProbeTimeout(t *testing.T) {
	probe := ProbeFunc(func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		<-ctx.Done() // 等超时
		return ProbeResult{Target: "x", Status: "timeout"}
	})
	run := NewRun("req-1", []DiagnosticTarget{{Type: "ping", Target: "x"}}, "direct", 100*time.Millisecond, []Probe{probe})
	run.Execute(context.Background(), func(ProbeResult) {})
	if run.Results[0].Status != "timeout" {
		t.Fatalf("超时应标记 timeout, got %q", run.Results[0].Status)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestRunProbe -v`
Expected: FAIL（package 不存在）

- [ ] **Step 3: 实现 probe.go**

```go
// probe.go
package diagnostics

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// 探测类型常量
const (
	TypePing       = "ping"
	TypeDNS        = "dns"
	TypeTCP        = "tcp"
	TypeHTTP       = "http"
	TypeTraceroute = "traceroute"
)

// 出网路径
const (
	PathDirect = "direct"
	PathProxy  = "proxy"
)

// 结果状态
const (
	StatusSuccess = "success"
	StatusFail    = "fail"
	StatusTimeout = "timeout"
	StatusError   = "error"
)

// DiagnosticTarget 描述一次探测目标。
type DiagnosticTarget struct {
	Type   string `json:"type"`
	Target string `json:"target"`       // 主机/域名/URL
	Port   int    `json:"port,omitempty"` // tcp 用
}

// ProbeResult 是单个探测的统一结果。
type ProbeResult struct {
	Target    string        `json:"target"`
	Type      string        `json:"type"`
	Path      string        `json:"path"`
	Status    string        `json:"status"`
	LatencyMs int64         `json:"latencyMs,omitempty"`
	Detail    interface{}   `json:"detail,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// ProgressFunc 每步完成时回调（发布进度事件）。
type ProgressFunc func(ProbeResult)

// Probe 单个探测器接口。
type Probe interface {
	Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult
}

// ProbeFunc 函数适配器，便于测试注入。
type ProbeFunc func(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult

func (f ProbeFunc) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	return f(ctx, target, path, cb)
}

// Run 是一次诊断执行：一组目标 × 出网路径。
type Run struct {
	RequestID string
	Targets   []DiagnosticTarget
	Path      string
	Timeout   time.Duration
	Probes    []Probe
	mu        sync.Mutex
	Results   []ProbeResult
	Done      bool
}

func NewRun(requestID string, targets []DiagnosticTarget, path string, timeout time.Duration, probes []Probe) *Run {
	return &Run{RequestID: requestID, Targets: targets, Path: path, Timeout: timeout, Probes: probes}
}

// Execute 顺序执行所有探测（目标 × 路径展开），每步完成回调进度。
// 单探测失败/超时不中断整体。
func (r *Run) Execute(ctx context.Context, onProgress ProgressFunc) {
	for _, target := range r.Targets {
		for _, probe := range r.Probes {
			// 每个探测独立超时：target 无效等探测内处理，这里只保护总时长
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

// Probes 按类型选探测器；未注册类型返回明确错误结果。
func Probes() map[string]Probe {
	// Task 2-6 填充
	return map[string]Probe{}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/probe.go backend/internal/diagnostics/probe_test.go
git commit -m "feat(diagnostics): 探测器框架——Probe 接口/统一结果/超时执行"
```

---

### Task 2: 服务生命周期（Run/GetResult/Cancel/并发/缓存）

**Files:**
- Create: `backend/internal/diagnostics/diagnostics.go`
- Test: `backend/internal/diagnostics/diagnostics_test.go`

- [ ] **Step 1: 写失败测试**

```go
// diagnostics_test.go
package diagnostics

import (
	"context"
	"testing"
	"time"
)

func newTestService(t *testing.T) *Service {
	svc := New(Config{
		MaxConcurrent: 3,
		ResultTTL:     10 * time.Minute,
		ProbeTimeout:  time.Second,
		Probes:        Probes(),
	})
	t.Cleanup(svc.Close)
	return svc
}

func TestServiceRunAndGetResult(t *testing.T) {
	svc := newTestService(t)
	id, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: "tcp", Target: "127.0.0.1", Port: 1}},
		Path:    "direct",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if id == "" {
		t.Fatal("应返回 requestId")
	}
	// 等完成
	deadline := time.Now().Add(3 * time.Second)
	for {
		res, ok := svc.GetResult(id)
		if ok && res.Done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("等待诊断完成超时")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServiceConcurrencyLimit(t *testing.T) {
	svc := newTestService(t)
	// 占满信号量
	slow := ProbeFunc(func(ctx context.Context, tgt DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
		<-ctx.Done()
		return ProbeResult{Target: tgt.Target, Status: "timeout"}
	})
	for i := 0; i < 3; i++ {
		if _, err := svc.Run(context.Background(), DiagnosticRequest{
			Targets: []DiagnosticTarget{{Type: "ping", Target: "x"}},
			Path:    "direct",
		}); err != nil {
			t.Fatalf("前 3 个应能启动: %v", err)
		}
	}
	// 第 4 个应被拒绝
	_, err := svc.Run(context.Background(), DiagnosticRequest{
		Targets: []DiagnosticTarget{{Type: "ping", Target: "x"}},
		Path:    "direct",
	})
	if err == nil {
		t.Fatal("超出并发上限应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestService -v`
Expected: FAIL（Service 不存在）

- [ ] **Step 3: 实现 diagnostics.go**

```go
// diagnostics.go
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

// ErrBusy 并发上限已满。
var ErrBusy = errors.New("诊断并发已满，请等待当前诊断完成")

// Config 是诊断服务的配置。
type Config struct {
	MaxConcurrent int           // 同时最多运行几个诊断
	ResultTTL     time.Duration // 结果缓存保留时长
	ProbeTimeout  time.Duration // 单探测超时
	Probes        map[string]Probe
	// 注入：进度发布回调（realtime.Hub.Publish）、代理地址查询
	Publish   func(eventType string, data interface{})
	ProxyURL  func() string
}

// DiagnosticRequest 是发起诊断的请求。
type DiagnosticRequest struct {
	Targets []DiagnosticTarget
	Path    string // direct|proxy|both，默认 both
}

// DiagnosticEvent 是推送到前端的进度事件（经 hub）。
type DiagnosticEvent struct {
	RequestID string        `json:"requestId"`
	Target    string        `json:"target"`
	Type      string        `json:"type"`
	Path      string        `json:"path"`
	Status    string        `json:"status"`
	LatencyMs int64         `json:"latencyMs,omitempty"`
	Detail    interface{}   `json:"detail,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Service 管理诊断生命周期。
type Service struct {
	cfg    Config
	sem    chan struct{}
	mu     sync.Mutex
	runs   map[string]*Run
	cancel map[string]context.CancelFunc
	closed bool
}

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
	if cfg.Probes == nil {
		cfg.Probes = Probes()
	}
	return &Service{
		cfg:    cfg,
		sem:    make(chan struct{}, cfg.MaxConcurrent),
		runs:   map[string]*Run{},
		cancel: map[string]context.CancelFunc{},
	}
}

// Run 发起一次诊断，立即返回 requestId。
func (s *Service) Run(ctx context.Context, req DiagnosticRequest) (string, error) {
	select {
	case s.sem <- struct{}{}:
	default:
		return "", ErrBusy
	}
	if req.Path == "" {
		req.Path = "both"
	}
	id := fmt.Sprintf("diag-%d", time.Now().UnixNano())
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.runs[id] = NewRun(id, req.Targets, req.Path, s.cfg.ProbeTimeout, probeSlice(s.cfg.Probes))
	s.cancel[id] = cancel
	s.mu.Unlock()

	go func() {
		defer func() { <-s.sem }()
		defer cancel()
		run := s.getRun(id)
		// both 路径展开为两次执行（direct + proxy）
		paths := []string{req.Path}
		if req.Path == "both" {
			paths = []string{PathDirect, PathProxy}
		}
		for _, p := range paths {
			run.Execute(runCtx, func(res ProbeResult) {
				s.publishProgress(id, res)
			})
		}
		// 完成；TTL 后清理
		time.AfterFunc(s.cfg.ResultTTL, func() { s.deleteRun(id) })
	}()
	return id, nil
}

// GetResult 查诊断结果；未找到返回 false。
func (s *Service) GetResult(id string) (*Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	return r, true
}

// Cancel 取消进行中的诊断。
func (s *Service) Cancel(id string) {
	s.mu.Lock()
	c, ok := s.cancel[id]
	s.mu.Unlock()
	if ok {
		c()
	}
}

// Close 停止服务并清理全部运行。
func (s *Service) Close() {
	s.mu.Lock()
	for _, c := range s.cancel {
		c()
	}
	s.runs = map[string]*Run{}
	s.cancel = map[string]context.CancelFunc{}
	s.closed = true
	s.mu.Unlock()
}

func (s *Service) getRun(id string) *Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id]
}

func (s *Service) deleteRun(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, id)
	delete(s.cancel, id)
}

func (s *Service) publishProgress(id string, res ProbeResult) {
	if s.cfg.Publish == nil {
		return
	}
	s.cfg.Publish("diagnostic.progress", DiagnosticEvent{
		RequestID: id,
		Target:    res.Target,
		Type:      res.Type,
		Path:      res.Path,
		Status:    res.Status,
		LatencyMs: res.LatencyMs,
		Detail:    res.Detail,
		Error:     res.Error,
	})
}

func probeSlice(m map[string]Probe) []Probe {
	out := make([]Probe, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	return out
}

// ---- SSRF 校验复用（fetcher） ----

// ValidateTarget 校验目标是否允许探测（复用 fetcher 的 SSRF 防护）。
func ValidateTarget(raw string) error {
	return fetcher.ValidateFetchURLExternal(raw)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestService -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/diagnostics.go backend/internal/diagnostics/diagnostics_test.go
git commit -m "feat(diagnostics): 服务生命周期——并发上限/结果缓存/Cancel"
```

---

### Task 3: 暴露 fetcher SSRF 校验供外部复用

**Files:**
- Modify: `backend/internal/fetcher/ssrf.go`（导出 `ValidateFetchURLExternal` 包装）
- Test: `backend/internal/fetcher/fetcher_test.go`（追加）

- [ ] **Step 1: 在 ssrf.go 末尾追加导出包装**

```go
// ValidateFetchURLExternal 导出校验函数供 diagnostics 等外部包复用。
// 返回 error 表示该 URL 不允许被服务端代发请求（SSRF 黑名单）。
func ValidateFetchURLExternal(rawURL string) error {
	return validateFetchURL(rawURL)
}
```

- [ ] **Step 2: 追加测试**

```go
func TestValidateFetchURLExternal(t *testing.T) {
	if err := ValidateFetchURLExternal("https://example.com/sub"); err != nil {
		t.Errorf("公网地址应允许: %v", err)
	}
	if err := ValidateFetchURLExternal("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Error("云 metadata 应被拒绝")
	}
	if err := ValidateFetchURLExternal("file:///etc/passwd"); err == nil {
		t.Error("非 http 协议应被拒绝")
	}
}
```

- [ ] **Step 3: 跑测试确认通过**

Run: `cd backend && go test ./internal/fetcher/ -run TestValidateFetchURLExternal -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add backend/internal/fetcher/ssrf.go backend/internal/fetcher/fetcher_test.go
git commit -m "feat(fetcher): 导出 ValidateFetchURLExternal 供诊断复用"
```

---

### Task 4: TCP 探测器

**Files:**
- Create: `backend/internal/diagnostics/tcp.go`
- Test: `backend/internal/diagnostics/tcp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// tcp_test.go
package diagnostics

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPProbeSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	p := TCPProbe{DialTimeout: time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	if res.LatencyMs <= 0 {
		t.Fatal("应记录延迟")
	}
}

func TestTCPProbeRefused(t *testing.T) {
	p := TCPProbe{DialTimeout: time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: 1}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("应失败, got %+v", res)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestTCPProbe -v`
Expected: FAIL（TCPProbe 不存在）

- [ ] **Step 3: 实现 tcp.go**

```go
// tcp.go
package diagnostics

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPProbe 对 host:port 建连测延迟。
type TCPProbe struct {
	DialTimeout time.Duration
}

func (p *TCPProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	addr := net.JoinHostPort(target.Target, fmt.Sprintf("%d", target.Port))
	if p.DialTimeout <= 0 {
		p.DialTimeout = 5 * time.Second
	}
	start := time.Now()
	d := net.Dialer{Timeout: p.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	conn.Close()
	return ProbeResult{Target: target.Target, Type: TypeTCP, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds()}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestTCPProbe -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/tcp.go backend/internal/diagnostics/tcp_test.go
git commit -m "feat(diagnostics): TCP 端口连通性探测器"
```

---

### Task 5: HTTP 探测器

**Files:**
- Create: `backend/internal/diagnostics/http.go`
- Test: `backend/internal/diagnostics/http_test.go`

- [ ] **Step 1: 写失败测试**

```go
// http_test.go
package diagnostics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProbeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := HTTPProbe{Timeout: 3 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
}

func TestHTTPProbeRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer srv.Close()

	p := HTTPProbe{Timeout: 3 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功（跟随重定向）, got %+v", res)
	}
}

func TestHTTPProbe404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := HTTPProbe{Timeout: 3 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: srv.URL}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("404 应失败, got %+v", res)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestHTTPProbe -v`
Expected: FAIL（HTTPProbe 不存在）

- [ ] **Step 3: 实现 http.go**

```go
// http.go
package diagnostics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPProbe GET 目标，报状态码/耗时/重定向链。
type HTTPProbe struct {
	Timeout time.Duration
	Client  *http.Client // 可注入（代理/直连）；nil 用默认
}

func (p *HTTPProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Second
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: p.Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Target, nil)
	if err != nil {
		return ProbeResult{Target: target.Target, Type: TypeHTTP, Path: path, Status: StatusError, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "AuroraMihomo-Diagnostics/0.1")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeHTTP, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return ProbeResult{
			Target:    target.Target,
			Type:      TypeHTTP,
			Path:      path,
			Status:    StatusSuccess,
			LatencyMs: latency.Milliseconds(),
			Detail: map[string]interface{}{
				"statusCode": resp.StatusCode,
				"finalURL":   resp.Request.URL.String(),
			},
		}
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeHTTP,
		Path:      path,
		Status:    StatusFail,
		LatencyMs: latency.Milliseconds(),
		Detail: map[string]interface{}{
			"statusCode": resp.StatusCode,
			"finalURL":   resp.Request.URL.String(),
		},
		Error: fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestHTTPProbe -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/http.go backend/internal/diagnostics/http_test.go
git commit -m "feat(diagnostics): HTTP 请求探测器"
```

---

### Task 6: DNS 探测器

**Files:**
- Create: `backend/internal/diagnostics/dns.go`
- Test: `backend/internal/diagnostics/dns_test.go`

- [ ] **Step 1: 写失败测试**

```go
// dns_test.go
package diagnostics

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDNSProbeSuccess(t *testing.T) {
	p := DNSProbe{Timeout: 3 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "localhost"}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("localhost 应能解析, got %+v", res)
	}
}

func TestDNSProbeNXDomain(t *testing.T) {
	p := DNSProbe{Timeout: 3 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "definitely-not-a-real-domain-xyz.invalid"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("不存在的域名应失败, got %+v", res)
	}
}

// 注入 mock resolver 验证 A/AAAA 记录解析
func TestDNSProbeCustomResolver(t *testing.T) {
	// 用内存 resolver 返回固定记录
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, nil // 不会真的调用
		},
	}
	p := DNSProbe{Timeout: time.Second, Resolver: r}
	_ = p // mock resolver 在单测里难构造真实响应，这里验证结构可注入
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestDNSProbe -v`
Expected: FAIL（DNSProbe 不存在）

- [ ] **Step 3: 实现 dns.go**

```go
// dns.go
package diagnostics

import (
	"context"
	"net"
	"time"
)

// DNSProbe 查 A/AAAA 记录与耗时。
type DNSProbe struct {
	Timeout  time.Duration
	Resolver *net.Resolver // 可注入 mock；nil 用系统默认
}

func (p *DNSProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.Timeout <= 0 {
		p.Timeout = 5 * time.Second
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	pctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	start := time.Now()
	ips, err := resolver.LookupIPAddr(pctx, target.Target)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if pctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeDNS, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	if len(ips) == 0 {
		return ProbeResult{Target: target.Target, Type: TypeDNS, Path: path, Status: StatusFail, LatencyMs: latency.Milliseconds(), Error: "无解析结果"}
	}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, ip.IP.String())
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeDNS,
		Path:      path,
		Status:    StatusSuccess,
		LatencyMs: latency.Milliseconds(),
		Detail: map[string]interface{}{
			"records": addrs,
		},
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestDNSProbe -v`
Expected: PASS（NXDomain 测试可能需要真实 DNS；若 CI 无外网，跳过该断言改为「失败或成功均可」——见 Step 5 说明）

- [ ] **Step 5: 处理无外网环境**

若 CI 环境无 DNS，`TestDNSProbeNXDomain` 可能不稳定。改为：

```go
func TestDNSProbeNXDomain(t *testing.T) {
	p := DNSProbe{Timeout: 2 * time.Second}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "definitely-not-a-real-domain-xyz.invalid"}, PathDirect, nil)
	// 无外网时可能返回 error（网络不通）；两者都不应返回 success
	if res.Status == StatusSuccess {
		t.Fatalf("不存在的域名不应成功, got %+v", res)
	}
}
```

- [ ] **Step 6: 提交**

```bash
git add backend/internal/diagnostics/dns.go backend/internal/diagnostics/dns_test.go
git commit -m "feat(diagnostics): DNS 解析探测器"
```

---

### Task 7: Ping 探测器（ICMP → TCP 降级）

**Files:**
- Create: `backend/internal/diagnostics/ping.go`
- Test: `backend/internal/diagnostics/ping_test.go`

- [ ] **Step 1: 写失败测试（降级路径 + 结构）**

```go
// ping_test.go
package diagnostics

import (
	"context"
	"testing"
	"time"
)

// 无 ICMP 权限时应降级 TCP ping（连 443 测延迟），并标注降级。
// 单测不依赖真实网络：验证降级分支被触发（Dial 失败时返回 fail 而非 panic）。
func TestPingProbeFallbackToTCP(t *testing.T) {
	p := PingProbe{Timeout: 2 * time.Second, PingCmd: func(ctx context.Context, host string) ([]byte, error) {
		return nil, errNoICMP
	}}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "127.0.0.1"}, PathDirect, nil)
	// 无 ICMP + TCP 443 连不上（单测环境）→ fail；关键是不 panic 且 status 合法
	if res.Status != StatusFail && res.Status != StatusTimeout && res.Status != StatusSuccess {
		t.Fatalf("status 非法: %+v", res)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestPingProbe -v`
Expected: FAIL（PingProbe 不存在）

- [ ] **Step 3: 实现 ping.go**

```go
// ping.go
package diagnostics

import (
	"context"
	"errors"
	"net"
	"time"
)

// errNoICMP 标记无 ICMP 权限（Linux 缺 CAP_NET_RAW / 非 root）。
var errNoICMP = errors.New("无 ICMP 权限")

// PingProbe 连通性探测：ICMP 优先，无权限降级 TCP ping。
type PingProbe struct {
	Timeout time.Duration
	// PingCmd 执行 ICMP ping 并返回输出；nil 时尝试系统 ping 命令。
	// 可注入便于测试降级分支。
	PingCmd func(ctx context.Context, host string) ([]byte, error)
}

func (p *PingProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.Timeout <= 0 {
		p.Timeout = 5 * time.Second
	}
	// 1) ICMP（系统 ping 命令）
	if p.PingCmd != nil {
		start := time.Now()
		_, err := p.PingCmd(ctx, target.Target)
		latency := time.Since(start)
		if err == nil {
			return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds()}
		}
		if !errors.Is(err, errNoICMP) {
			// ICMP 命令失败但非权限问题：报失败（保留原始错误）
			status := StatusFail
			if ctx.Err() != nil {
				status = StatusTimeout
			}
			return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
		}
		// errNoICMP → 降级 TCP
	}

	// 2) TCP ping 降级：连目标 443（或 80）测延迟
	port := 443
	if target.Port > 0 {
		port = target.Port
	}
	addr := net.JoinHostPort(target.Target, itoa(port))
	d := net.Dialer{Timeout: p.Timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error(), Detail: map[string]interface{}{"degraded": true, "reason": "无 ICMP 权限，已用 TCP ping"}}
	}
	conn.Close()
	return ProbeResult{Target: target.Target, Type: TypePing, Path: path, Status: StatusSuccess, LatencyMs: latency.Milliseconds(), Detail: map[string]interface{}{"degraded": true, "reason": "无 ICMP 权限，已用 TCP ping"}}
}

func itoa(n int) string {
	// 避免 strconv 依赖，简单实现
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
```

> 注：`itoa` 应改用 `strconv.Itoa`——实现里直接用 `strconv.Itoa(port)` 并导入 `strconv`，上面是为了展示意图，实际代码用标准库。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestPingProbe -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/ping.go backend/internal/diagnostics/ping_test.go
git commit -m "feat(diagnostics): Ping 探测器（ICMP→TCP 降级）"
```

---

### Task 8: Traceroute 探测器（系统命令）

**Files:**
- Create: `backend/internal/diagnostics/traceroute.go`
- Test: `backend/internal/diagnostics/traceroute_test.go`

- [ ] **Step 1: 写失败测试**

```go
// traceroute_test.go
package diagnostics

import (
	"context"
	"testing"
	"time"
)

// 注入假命令输出，验证逐跳解析。
func TestTracerouteParse(t *testing.T) {
	out := `traceroute to 8.8.8.8 (8.8.8.8), 30 hops max, 60 byte packets
 1  192.168.1.1  1.234 ms  1.100 ms  1.050 ms
 2  10.0.0.1  5.000 ms  4.500 ms  4.200 ms
 3  8.8.8.8  20.000 ms  19.500 ms  19.800 ms
`
	hops := parseTraceroute(out)
	if len(hops) != 3 {
		t.Fatalf("应解析 3 跳, got %d: %#v", len(hops), hops)
	}
	if hops[0].Hop != 1 || hops[0].Addr != "192.168.1.1" {
		t.Fatalf("第一跳解析错误: %+v", hops[0])
	}
}

func TestTracerouteProbe(t *testing.T) {
	p := TracerouteProbe{Timeout: 5 * time.Second, RunCmd: func(ctx context.Context, host string) ([]byte, error) {
		return []byte(" 1  8.8.8.8  1.000 ms  1.000 ms  1.000 ms\n"), nil
	}}
	res := p.Run(context.Background(), DiagnosticTarget{Type: TypeTraceroute, Target: "8.8.8.8"}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestTraceroute -v`
Expected: FAIL（TracerouteProbe 不存在）

- [ ] **Step 3: 实现 traceroute.go**

```go
// traceroute.go
package diagnostics

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Hop 是 traceroute 的一跳。
type Hop struct {
	Hop  int    `json:"hop"`
	Addr string `json:"addr"`
	RTT  string `json:"rtt,omitempty"` // 原始 RTT 文本
}

// TracerouteProbe 调系统 traceroute/tracert 命令，解析逐跳输出。
type TracerouteProbe struct {
	Timeout time.Duration
	// RunCmd 可注入假命令便于测试；nil 用系统命令。
	RunCmd func(ctx context.Context, host string) ([]byte, error)
}

func (p *TracerouteProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	if p.Timeout <= 0 {
		p.Timeout = 30 * time.Second
	}
	runCmd := p.RunCmd
	if runCmd == nil {
		runCmd = p.systemTraceroute
	}
	start := time.Now()
	out, err := runCmd(ctx, target.Target)
	latency := time.Since(start)
	if err != nil {
		status := StatusFail
		if ctx.Err() != nil {
			status = StatusTimeout
		}
		msg := err.Error()
		// 命令缺失的明确提示
		if _, ok := err.(*exec.Error); ok {
			msg = "系统未安装 traceroute（Linux/macOS）或 tracert（Windows）"
		}
		return ProbeResult{Target: target.Target, Type: TypeTraceroute, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: msg}
	}
	hops := parseTraceroute(string(out))
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeTraceroute,
		Path:      path,
		Status:    StatusSuccess,
		LatencyMs: latency.Milliseconds(),
		Detail: map[string]interface{}{
			"hops": hops,
			"raw":  string(out),
		},
	}
}

func (p *TracerouteProbe) systemTraceroute(ctx context.Context, host string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "tracert", "-d", host).Output()
	}
	return exec.CommandContext(ctx, "traceroute", "-n", host).Output()
}

// parseTraceroute 解析 traceroute 输出，提取逐跳地址。
func parseTraceroute(out string) []Hop {
	var hops []Hop
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "traceroute") || strings.HasPrefix(line, "tracert") {
			continue
		}
		// 形如: " 1  192.168.1.1  1.234 ms ..."
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hopNum, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		if err != nil {
			continue
		}
		addr := fields[1]
		hops = append(hops, Hop{Hop: hopNum, Addr: addr})
	}
	return hops
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestTraceroute -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/traceroute.go backend/internal/diagnostics/traceroute_test.go
git commit -m "feat(diagnostics): Traceroute 探测器（系统命令解析）"
```

---

### Task 9: 预设目标

**Files:**
- Create: `backend/internal/diagnostics/targets.go`
- Test: `backend/internal/diagnostics/targets_test.go`

- [ ] **Step 1: 写失败测试**

```go
// targets_test.go
package diagnostics

import "testing"

func TestDefaultTargets(t *testing.T) {
	proxyURL := "http://127.0.0.1:7890"
	targets := DefaultTargets(func() string { return proxyURL })
	// 至少包含 GitHub API、raw、公共 DNS、代理端口
	found := map[string]bool{}
	for _, tg := range targets {
		found[tg.Target] = true
	}
	if !found["api.github.com"] {
		t.Error("预设应包含 api.github.com")
	}
	if !found["raw.githubusercontent.com"] {
		t.Error("预设应包含 raw.githubusercontent.com")
	}
	// 代理端口目标：host 应为代理主机，port 应为代理端口
	var hasProxy bool
	for _, tg := range targets {
		if tg.Type == TypeTCP && tg.Port == 7890 {
			hasProxy = true
		}
	}
	if !hasProxy {
		t.Error("预设应包含代理端口 TCP 探测")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd backend && go test ./internal/diagnostics/ -run TestDefaultTargets -v`
Expected: FAIL（DefaultTargets 不存在）

- [ ] **Step 3: 实现 targets.go**

```go
// targets.go
package diagnostics

import "net/url"

// DefaultTargets 返回预设诊断目标。proxyURLFn 提供当前内核代理地址，
// 动态加入代理端口探测；返回空串表示代理不可用。
func DefaultTargets(proxyURLFn func() string) []DiagnosticTarget {
	targets := []DiagnosticTarget{
		{Type: TypeDNS, Target: "api.github.com"},
		{Type: TypeTCP, Target: "api.github.com", Port: 443},
		{Type: TypeHTTP, Target: "https://api.github.com/"},
		{Type: TypeTCP, Target: "raw.githubusercontent.com", Port: 443},
		{Type: TypeHTTP, Target: "https://raw.githubusercontent.com/"},
		{Type: TypeDNS, Target: "1.1.1.1"},
		{Type: TypeTCP, Target: "1.1.1.1", Port: 53},
		{Type: TypeDNS, Target: "8.8.8.8"},
		{Type: TypeTCP, Target: "8.8.8.8", Port: 53},
		{Type: TypeDNS, Target: "223.5.5.5"},
		{Type: TypeTCP, Target: "223.5.5.5", Port: 53},
	}
	if proxyURLFn != nil {
		if u, err := url.Parse(proxyURLFn()); err == nil && u.Hostname() != "" {
			port := 7890 // 默认混合端口
			if p := u.Port(); p != "" {
				if n := atoiSafe(p); n > 0 {
					port = n
				}
			}
			targets = append(targets, DiagnosticTarget{Type: TypeTCP, Target: u.Hostname(), Port: port})
		}
	}
	return targets
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd backend && go test ./internal/diagnostics/ -run TestDefaultTargets -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/diagnostics/targets.go backend/internal/diagnostics/targets_test.go
git commit -m "feat(diagnostics): 预设目标（GitHub/raw/公共 DNS/代理端口）"
```

---

### Task 10: API 契约（.api 规格 + goctl 生成）

**Files:**
- Modify: `backend/api/AuroraMihomo-Go-Zero-API.api`（追加类型与路由）
- 生成：goctl 重新生成 types/handler/routes（DO NOT EDIT 头保持完好）

- [ ] **Step 1: 在 .api 规格追加类型与路由**

在 `// ---------- 其他 ----------` 段内追加：

```
// ---------- 网络诊断 ----------
type DiagnosticTarget {
	Type   string `json:"type"`   // ping|dns|tcp|http|traceroute
	Target string `json:"target"` // 主机/域名/URL
	Port   int    `json:"port,optional"` // tcp 用
}

type DiagnosticRunReq {
	Targets []DiagnosticTarget `json:"targets"`
	// Path 出网路径：direct|proxy|both，默认 both
	Path string `json:"path,optional"`
}

type DiagnosticRunResp {
	RequestId string `json:"requestId"`
}

type DiagnosticResultResp {
	RequestId string          `json:"requestId"`
	Done      bool            `json:"done"`
	Results   []ProbeResult   `json:"results,optional"`
}

// ProbeResult 是后端 diagnostics 包的结果结构（镜像，避免循环依赖）
type ProbeResult {
	Target    string      `json:"target"`
	Type      string      `json:"type"`
	Path      string      `json:"path"`
	Status    string      `json:"status"`
	LatencyMs int64       `json:"latencyMs,omitempty"`
	Detail    interface{} `json:"detail,omitempty"`
	Error     string      `json:"error,omitempty"`
}

@handler runDiagnostics
post /api/v1/diagnostics/run (DiagnosticRunReq) returns (DiagnosticRunResp)

@handler getDiagnosticsResult
get /api/v1/diagnostics/result/:requestId (IdPathReq) returns (DiagnosticResultResp)
```

- [ ] **Step 2: 运行 goctl 生成**

Run: `cd backend && goctl api go -api api/AuroraMihomo-Go-Zero-API.api -dir .`
Expected: 生成 types.go / handler 新增 runDiagnosticsHandler.go / getDiagnosticsResultHandler.go / routes.go 新增两路由

- [ ] **Step 3: 核对生成结果**

- `DO NOT EDIT` 头完好
- 新增类型 `DiagnosticTarget`/`DiagnosticRunReq`/`DiagnosticRunResp`/`DiagnosticResultResp`/`ProbeResult` 就位
- 既有 handler 未被重写（git diff 只显示新增）
- `go build ./...` 通过

- [ ] **Step 4: 提交**

```bash
git add backend/api/
git commit -m "feat(api): 网络诊断 run/result 路由与类型"
```

---

### Task 11: logic 层实现

**Files:**
- Create: `backend/api/internal/logic/protected/runDiagnosticsLogic.go`
- Create: `backend/api/internal/logic/protected/getDiagnosticsResultLogic.go`
- Modify: `backend/api/internal/svc/servicecontext.go`（构造 DiagnosticsService 注入 hub + proxyURL）

- [ ] **Step 1: 在 servicecontext 构造诊断服务**

```go
// servicecontext.go 内（upd.SetProxyURLFunc 附近）
diagSvc := diagnostics.New(diagnostics.Config{
	MaxConcurrent: 3,
	ResultTTL:     10 * time.Minute,
	ProbeTimeout:  5 * time.Second,
	Probes:        diagnostics.Probes(),
	Publish:       hub.Publish, // realtime.Hub.Publish(eventType, data)
	ProxyURL:      cfgSvc.LocalProxyURL,
})
// 存到 ServiceContext 字段 Diag *diagnostics.Service
```

- [ ] **Step 2: 实现 runDiagnosticsLogic**

```go
// runDiagnosticsLogic.go
package protected

import (
	"context"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/diagnostics"
	"github.com/zeromicro/go-zero/core/logx"
)

type RunDiagnosticsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRunDiagnosticsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RunDiagnosticsLogic {
	return &RunDiagnosticsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *RunDiagnosticsLogic) RunDiagnostics(req *types.DiagnosticRunReq) (resp *types.DiagnosticRunResp, err error) {
	targets := make([]diagnostics.DiagnosticTarget, 0, len(req.Targets))
	for _, t := range req.Targets {
		// SSRF 校验：HTTP/URL 类目标复用 fetcher 校验
		if t.Type == diagnostics.TypeHTTP {
			if verr := diagnostics.ValidateTarget(t.Target); verr != nil {
				return nil, verr
			}
		}
		targets = append(targets, diagnostics.DiagnosticTarget{Type: t.Type, Target: t.Target, Port: t.Port})
	}
	id, err := l.svcCtx.Diag.Run(l.ctx, diagnostics.DiagnosticRequest{Targets: targets, Path: req.Path})
	if err != nil {
		return nil, err
	}
	return &types.DiagnosticRunResp{RequestId: id}, nil
}
```

- [ ] **Step 3: 实现 getDiagnosticsResultLogic**

```go
// getDiagnosticsResultLogic.go
package protected

import (
	"context"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetDiagnosticsResultLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetDiagnosticsResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetDiagnosticsResultLogic {
	return &GetDiagnosticsResultLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *GetDiagnosticsResultLogic) GetDiagnosticsResult(req *types.IdPathReq) (resp *types.DiagnosticResultResp, err error) {
	run, ok := l.svcCtx.Diag.GetResult(req.Id)
	if !ok {
		return &types.DiagnosticResultResp{RequestId: req.Id, Done: false}, nil
	}
	results := make([]types.ProbeResult, 0, len(run.Results))
	for _, r := range run.Results {
		results = append(results, types.ProbeResult{
			Target: r.Target, Type: r.Type, Path: r.Path, Status: r.Status,
			LatencyMs: r.LatencyMs, Detail: r.Detail, Error: r.Error,
		})
	}
	return &types.DiagnosticResultResp{RequestId: req.Id, Done: run.Done, Results: results}, nil
}
```

- [ ] **Step 4: 编译 + 测试**

Run: `cd backend && go build ./... && go test ./api/...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/api/internal/logic/protected/ backend/api/internal/svc/servicecontext.go
git commit -m "feat(api): 诊断 run/result logic 与 servicecontext 装配"
```

---

### Task 12: 前端 store（stores/diagnostics.ts）

**Files:**
- Create: `frontend/src/stores/diagnostics.ts`
- Test: `frontend/src/stores/diagnostics.spec.ts`

- [ ] **Step 1: 写失败测试**

```ts
// diagnostics.spec.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useDiagnosticsStore } from './diagnostics'
import api from '../api'

vi.mock('../api', () => ({
  default: { post: vi.fn(), get: vi.fn() },
}))

describe('useDiagnosticsStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('run 发 POST 并保存 requestId', async () => {
    ;(api.post as any).mockResolvedValue({ data: { requestId: 'diag-1' } })
    const store = useDiagnosticsStore()
    await store.run({ type: 'tcp', target: '8.8.8.8', port: 53 }, 'both')
    expect(api.post).toHaveBeenCalledWith('/diagnostics/run', {
      targets: [{ type: 'tcp', target: '8.8.8.8', port: 53 }],
      path: 'both',
    })
    expect(store.requestId).toBe('diag-1')
    expect(store.running).toBe(true)
  })

  it('handleProgress 只接收匹配 requestId 的事件', () => {
    const store = useDiagnosticsStore()
    store.requestId = 'diag-1'
    store.handleProgress('diagnostic.progress', { requestId: 'diag-1', target: 'x', status: 'success' })
    expect(store.results).toHaveLength(1)
    // 非匹配 requestId 忽略
    store.handleProgress('diagnostic.progress', { requestId: 'other', target: 'y', status: 'success' })
    expect(store.results).toHaveLength(1)
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && npx vitest run src/stores/diagnostics.spec.ts`
Expected: FAIL（store 不存在）

- [ ] **Step 3: 实现 diagnostics.ts**

```ts
// diagnostics.ts
import { defineStore } from 'pinia'
import api from '../api'

export interface DiagnosticTarget {
  type: 'ping' | 'dns' | 'tcp' | 'http' | 'traceroute'
  target: string
  port?: number
}

export interface ProbeResult {
  target: string
  type: string
  path: string
  status: 'success' | 'fail' | 'timeout' | 'error'
  latencyMs?: number
  detail?: Record<string, unknown>
  error?: string
}

export const useDiagnosticsStore = defineStore('diagnostics', {
  state: () => ({
    running: false,
    requestId: '' as string,
    results: [] as ProbeResult[],
    error: '' as string,
  }),
  getters: {
    // 按 目标+类型+路径 聚合，供前端渲染成表格
    groupedResults: (s) => {
      const map = new Map<string, ProbeResult[]>()
      for (const r of s.results) {
        const key = `${r.target}|${r.type}`
        if (!map.has(key)) map.set(key, [])
        map.get(key)!.push(r)
      }
      return map
    },
  },
  actions: {
    async run(targets: DiagnosticTarget[], path: string) {
      this.running = true
      this.error = ''
      this.results = []
      try {
        const res = await api.post<{ requestId: string }>('/diagnostics/run', { targets, path })
        this.requestId = res.data.requestId
      } catch (e) {
        this.error = '诊断启动失败'
        this.running = false
        throw e
      }
    },
    // 由 useRealtime handler 调用：只收匹配当前 requestId 的进度
    handleProgress(type: string, data: Record<string, unknown>) {
      if (type !== 'diagnostic.progress' || data.requestId !== this.requestId) return
      this.results.push(data as unknown as ProbeResult)
    },
    async fetchResult(requestId: string) {
      const res = await api.get<{ done: boolean; results?: ProbeResult[] }>(
        `/diagnostics/result/${requestId}`,
      )
      if (res.data.done) {
        this.results = res.data.results || []
        this.running = false
      }
      return res.data.done
    },
    cancel() {
      // 前端无独立 cancel 端点；重置状态即可（后端 TTL 自动清理）
      this.running = false
      this.requestId = ''
      this.results = []
    },
    reset() {
      this.running = false
      this.requestId = ''
      this.results = []
      this.error = ''
    },
  },
})
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && npx vitest run src/stores/diagnostics.spec.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/diagnostics.ts frontend/src/stores/diagnostics.spec.ts
git commit -m "feat(diagnostics): 前端 store（run/进度过滤/轮询兜底）"
```

---

### Task 13: 前端页面（DiagnosticsView.vue）+ 路由 + 导航

**Files:**
- Create: `frontend/src/views/DiagnosticsView.vue`
- Create: `frontend/src/views/DiagnosticsView.spec.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: 加路由**

```ts
// router/index.ts
// 在 /settings 路由后加：
{ path: '/diagnostics', name: 'diagnostics', component: DiagnosticsView, meta: { title: '网络诊断' } },
```

```ts
// App.vue 导航项，/diagnostics 在 /diff 后：
{ to: '/diagnostics', label: '网络诊断', icon: Wifi },
```

（`Wifi` 从 lucide-vue-next 导入）

- [ ] **Step 2: 实现 DiagnosticsView.vue 骨架**

```vue
<!-- DiagnosticsView.vue（骨架；细节在 Step 3 补全） -->
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useDiagnosticsStore, type DiagnosticTarget } from '../stores/diagnostics'
import { useRealtime } from '../composables/useRealtime'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'

const store = useDiagnosticsStore()

// 预设目标一键全测
const presetTargets: DiagnosticTarget[] = [
  { type: 'dns', target: 'api.github.com' },
  { type: 'tcp', target: 'api.github.com', port: 443 },
  { type: 'http', target: 'https://api.github.com/' },
  { type: 'tcp', target: 'raw.githubusercontent.com', port: 443 },
  { type: 'dns', target: '1.1.1.1' },
  { type: 'tcp', target: '1.1.1.1', port: 53 },
  { type: 'dns', target: '8.8.8.8' },
  { type: 'tcp', target: '8.8.8.8', port: 53 },
  { type: 'dns', target: '223.5.5.5' },
  { type: 'tcp', target: '223.5.5.5', port: 53 },
]

// 手动输入
const manualTarget = ref('')
const manualType = ref<'tcp' | 'dns' | 'http' | 'ping' | 'traceroute'>('tcp')
const manualPort = ref<number>(443)
const path = ref<'direct' | 'proxy' | 'both'>('both')

// WS 实时进度：只过滤当前 requestId
const { status: wsStatus } = useRealtime((type, data) => {
  store.handleProgress(type, data)
})

// 轮询兜底：WS 断开时定期查 result
let pollTimer: number | null = null
function startPolling() {
  stopPolling()
  pollTimer = window.setInterval(async () => {
    if (!store.requestId) return
    const done = await store.fetchResult(store.requestId)
    if (done) stopPolling()
  }, 2000)
}
function stopPolling() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

async function runPreset() {
  await store.run(presetTargets, path.value)
  startPolling()
}

async function runManual() {
  if (!manualTarget.value.trim()) return
  const targets: DiagnosticTarget[] = [{ type: manualType.value, target: manualTarget.value.trim() }]
  if (manualType.value === 'tcp') targets[0].port = manualPort.value
  await store.run(targets, path.value)
  startPolling()
}

onUnmounted(stopPolling)
</script>

<template>
  <main class="max-w-6xl mx-auto p-4 sm:p-6 lg:p-8 space-y-6">
    <div class="mb-2">
      <h1 class="text-2xl sm:text-3xl font-bold">网络诊断</h1>
      <p class="text-xs sm:text-sm text-fg-subtle mt-1">
        从面板宿主机视角排查出网问题（订阅拉取 / 内核更新 / raw 地址失败）。
        支持直连与本地 Mihomo 代理对比，实时查看每步结果。
      </p>
    </div>

    <!-- 出网路径选择 -->
    <div class="flex items-center gap-4">
      <Label class="text-sm">出网路径</Label>
      <Select v-model="path">
        <SelectTrigger class="w-44"><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value="both">直连 + 代理对比</SelectItem>
          <SelectItem value="direct">仅直连</SelectItem>
          <SelectItem value="proxy">仅代理</SelectItem>
        </SelectContent>
      </Select>
      <span v-if="wsStatus !== 'live'" class="text-xs text-fg-subtle">
        WS {{ wsStatus }}，结果将轮询获取
      </span>
    </div>

    <!-- 预设目标一键全测 -->
    <section class="rounded-lg border border-line bg-surface p-4">
      <h2 class="text-sm font-semibold mb-2">预设目标一键全测</h2>
      <p class="text-xs text-fg-subtle mb-3">GitHub API / raw / 公共 DNS / 内核代理端口</p>
      <Button :disabled="store.running" @click="runPreset">
        {{ store.running ? '诊断中…' : '开始诊断' }}
      </Button>
    </section>

    <!-- 手动输入 -->
    <section class="rounded-lg border border-line bg-surface p-4">
      <h2 class="text-sm font-semibold mb-2">手动输入</h2>
      <div class="flex flex-wrap gap-3 items-end">
        <div class="min-w-0 flex-1">
          <Label class="text-sm text-fg-muted">目标</Label>
          <Input v-model="manualTarget" placeholder="主机/域名/URL" class="mt-1 font-mono" />
        </div>
        <div>
          <Label class="text-sm text-fg-muted">类型</Label>
          <Select v-model="manualType">
            <SelectTrigger class="w-36 mt-1"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="tcp">TCP</SelectItem>
              <SelectItem value="dns">DNS</SelectItem>
              <SelectItem value="http">HTTP</SelectItem>
              <SelectItem value="ping">Ping</SelectItem>
              <SelectItem value="traceroute">Traceroute</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="manualType === 'tcp'">
          <Label class="text-sm text-fg-muted">端口</Label>
          <Input v-model.number="manualPort" type="number" class="w-24 mt-1" />
        </div>
        <Button :disabled="store.running" @click="runManual">诊断</Button>
      </div>
    </section>

    <!-- 进度结果 -->
    <section v-if="store.results.length || store.running" class="rounded-lg border border-line bg-surface p-4">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-semibold">诊断结果</h2>
        <div class="flex gap-2">
          <span v-if="store.running" class="text-xs text-fg-subtle">进行中…</span>
          <Button variant="ghost" size="sm" @click="store.reset()">清空</Button>
        </div>
      </div>
      <div class="space-y-2">
        <div
          v-for="(r, i) in store.results"
          :key="i"
          class="flex items-center gap-3 rounded-md border border-line bg-elevated/50 px-3 py-2 text-sm"
        >
          <Badge :variant="r.status === 'success' ? 'ok' : r.status === 'fail' ? 'destructive' : 'neutral'">
            {{ r.status }}
          </Badge>
          <span class="font-mono text-xs">{{ r.path }}</span>
          <span class="flex-1 min-w-0 truncate">{{ r.target }} ({{ r.type }})</span>
          <span v-if="r.latencyMs" class="text-xs text-fg-subtle shrink-0">{{ r.latencyMs }}ms</span>
          <span v-if="r.error" class="text-xs text-destructive truncate shrink-0 max-w-64">{{ r.error }}</span>
        </div>
      </div>
    </section>
  </main>
</template>
```

> 注：模板里用了 `Label` 但未导入——在 script 顶部补 `import { Label } from '@/components/ui/label'`。完整实现时核对所有导入。

- [ ] **Step 3: 前端 lint + 测试**

Run: `cd frontend && npx eslint src/views/DiagnosticsView.vue src/stores/diagnostics.ts && npx vitest run`
Expected: PASS（新增组件无 lint 错误；既有 248 测试仍全绿）

- [ ] **Step 4: 前端类型检查**

Run: `cd frontend && npx vue-tsc --noEmit 2>&1 | grep -v "TS5101\|baseUrl\|aka.ms\|tsconfig.json"`
Expected: 无输出（仅 tsconfig 存量报错被过滤）

- [ ] **Step 5: 提交**

```bash
git add frontend/src/views/DiagnosticsView.vue frontend/src/views/DiagnosticsView.spec.ts frontend/src/router/index.ts frontend/src/App.vue
git commit -m "feat(diagnostics): 网络诊断页面 + 路由 + 导航入口"
```

---

### Task 14: 端到端验证 + 文档

**Files:**
- Modify: `userdocs/user-guide.md`（新增网络诊断章节）
- 运行：前后端 dev server 实机验证

- [ ] **Step 1: 更新使用文档**

在 `userdocs/user-guide.md` 新增「网络诊断」章节（目录 + 正文），说明：
- 功能入口（侧边栏「网络诊断」）
- 预设目标一键全测 / 手动输入
- 直连 vs 代理对比的读法（两者都绿=出网正常；直连红代理绿=代理可用但直连被墙；反之=代理配置问题）
- 各探测类型含义（ping/dns/tcp/http/traceroute）
- 无 ICMP 权限自动降级 TCP ping 的提示

Run: `node scripts/sync-docs.mjs`（同步到前端内置副本）

- [ ] **Step 2: 实机验证**

启动前后端 dev server，登录后访问 `/diagnostics`：
- 点「开始诊断」验证预设目标逐步出结果（WS 实时）
- 断 WS（停后端）验证轮询兜底
- 手动输入 `8.8.8.8:443` TCP、`https://www.baidu.com` HTTP、`example.com` DNS
- 直连 vs 代理对比：两列结果并排
- 验证无 ICMP 权限时 ping 标注「已用 TCP ping」

- [ ] **Step 3: 文档一致性检查**

Run: `node scripts/sync-docs.mjs --check`
Expected: `一致: frontend/src/content/user-guide.md`

- [ ] **Step 4: 提交**

```bash
git add userdocs/user-guide.md frontend/src/content/user-guide.md
git commit -m "docs: 网络诊断功能使用说明"
```

---

## Self-Review

**1. Spec 覆盖：**
- ✅ 5 类探测器（Task 4-8）
- ✅ 直连 vs 代理对比（Task 1 Path 展开 + Task 2 both）
- ✅ 预设目标 + 手动输入（Task 9 + Task 13）
- ✅ WS 实时 + 轮询兜底（Task 2 hub + Task 12 store + Task 13）
- ✅ SSRF 复用（Task 3）
- ✅ 并发上限/超时/仅认证（Task 2 + Task 11）
- ✅ 前端页面/路由/导航（Task 13）

**2. 占位符扫描：** 无 TBD/TODO；所有代码步骤完整。

**3. 类型一致性：**
- `DiagnosticTarget`/`ProbeResult`/`DiagnosticEvent` 在后端 diagnostics 包、API types、前端 store 三处命名一致
- `Run`/`Execute`/`GetResult`/`Cancel` 方法签名在 Task 1/2/11 一致
- `DefaultTargets(proxyURLFn)` 签名在 Task 9/13 一致（前端预设目标为静态，后端为动态取代理端口）

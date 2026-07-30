package mihomo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Status struct {
	IsRunning bool
	Version   string
	PID       int
	Error     string
}

type LogLine struct {
	Time    time.Time `json:"time"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
}

type LogListener func(LogLine)

type Manager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	// Reload 等价于 Restart（进程重启），会断开所有现有连接
	Reload(ctx context.Context) error
	// ReloadConfig 通过 mihomo 的 external-controller RESTful API 热重载配置，
	// 不重启进程、不断开现有连接。controller/secret 为空或请求失败时
	// 回退为 Reload（即 Restart）。
	ReloadConfig(ctx context.Context, controller, secret, configPath string) error
	Status() Status
	ValidateConfig(ctx context.Context, configPath string) error
	Version(ctx context.Context) (string, error)
	Logs(limit int) []LogLine
	SubscribeLogs(fn LogListener) (unsubscribe func())
}

type Config struct {
	BinaryPath string
	ConfigDir  string
}

type ProcessManager struct {
	// opMu 串行化 Start/Stop/Restart/Reload 等生命周期操作，
	// mu 只保护结构体字段的读写，两者职责分离避免长时间持锁
	opMu sync.Mutex

	config  Config
	cmd     *exec.Cmd
	version string
	// exited 在进程回收完成时关闭，供 Stop 等待，
	// 避免对同一子进程重复调用 Wait
	exited chan struct{}
	mu     sync.RWMutex

	logs     []LogLine
	logLimit int
	subsMu   sync.RWMutex
	subs     map[int]LogListener
	subSeq   int
}

func NewManager(cfg Config) *ProcessManager {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "mihomo"
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = "./data"
	}
	return &ProcessManager{
		config:   cfg,
		logLimit: 1000,
		subs:     map[int]LogListener{},
	}
}

func (m *ProcessManager) isProcessAliveLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return m.cmd.ProcessState == nil || !m.cmd.ProcessState.Exited()
	}
	return m.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (m *ProcessManager) appendLog(stream, msg string) {
	line := LogLine{Time: time.Now(), Stream: stream, Message: msg}
	m.mu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > m.logLimit {
		m.logs = m.logs[len(m.logs)-m.logLimit:]
	}
	m.mu.Unlock()

	// 先拷出订阅者，回调期间不持锁。
	// Go 的 RWMutex 不可重入且写锁优先：在 RLock 内直接回调时，若回调又
	// 触发 appendLog、同时有 SubscribeLogs 在等写锁，嵌套的 RLock 会被
	// 永久饿死。当前订阅者只做 Hub 广播、不会重入，但这个隐患没有任何
	// 收益，写法上避开即可（applog.Buffer 同样处理，并有测试固定）。
	m.subsMu.RLock()
	fns := make([]LogListener, 0, len(m.subs))
	for _, fn := range m.subs {
		fns = append(fns, fn)
	}
	m.subsMu.RUnlock()

	for _, fn := range fns {
		fn(line)
	}
}

func (m *ProcessManager) pipeOutput(r io.Reader, stream string) {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		m.appendLog(stream, sc.Text())
	}
}

func (m *ProcessManager) Start(ctx context.Context) error {
	// opMu 串行化整个生命周期操作，避免 Start/Stop/Restart 交错
	// 导致重复拉起进程或产生孤儿进程
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.startLocked(ctx)
}

func (m *ProcessManager) startLocked(ctx context.Context) error {
	m.mu.Lock()
	if m.isProcessAliveLocked() {
		m.mu.Unlock()
		return errors.New("mihomo is already running")
	}
	m.mu.Unlock()

	if err := os.MkdirAll(m.config.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create config dir failed: %w", err)
	}

	m.mu.Lock()
	needVersion := m.version == ""
	m.mu.Unlock()
	if needVersion {
		if v, err := m.versionUnlocked(ctx); err == nil {
			m.mu.Lock()
			m.version = v
			m.mu.Unlock()
		}
	}

	// 这里刻意不用 CommandContext：mihomo 内核是常驻子进程，
	// 其生命周期由 Stop/Restart 显式管理，绝不能随发起启动的那次
	// 请求 ctx 取消而被杀掉。
	//nolint:noctx // 常驻进程不应绑定请求级 context
	cmd := exec.Command(m.config.BinaryPath, "-d", m.config.ConfigDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mihomo: %w", err)
	}

	// exited 用于让 Stop 等待进程回收，避免对同一进程二次 Wait
	exited := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.exited = exited
	m.mu.Unlock()

	pid := cmd.Process.Pid
	// 通知订阅者时不得持有 m.mu，appendLog 内部会自行加锁
	m.appendLog("system", fmt.Sprintf("mihomo started pid=%d", pid))

	go m.pipeOutput(stdout, "stdout")
	go m.pipeOutput(stderr, "stderr")
	go func(c *exec.Cmd, done chan struct{}) {
		waitErr := c.Wait()
		m.mu.Lock()
		if m.cmd == c {
			m.cmd = nil
			m.exited = nil
		}
		m.mu.Unlock()
		close(done)
		if waitErr != nil {
			m.appendLog("system", "mihomo exited: "+waitErr.Error())
		} else {
			m.appendLog("system", "mihomo exited")
		}
	}(cmd, exited)
	return nil
}

func (m *ProcessManager) Stop(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.stopLocked(ctx)
}

func (m *ProcessManager) stopLocked(ctx context.Context) error {
	m.mu.Lock()
	if m.cmd == nil || m.cmd.Process == nil {
		m.mu.Unlock()
		return nil
	}
	proc := m.cmd.Process
	exited := m.exited
	// 立即置空，使 Status/isProcessAliveLocked 不再把该进程视为运行中；
	// 真正的回收由下面的 exited 信号或 fallback Wait 完成
	m.cmd = nil
	m.exited = nil
	m.mu.Unlock()

	var stopErr error
	if runtime.GOOS == "windows" {
		stopErr = proc.Kill()
	} else {
		stopErr = proc.Signal(syscall.SIGTERM)
		if stopErr != nil {
			stopErr = proc.Kill()
		}
	}

	// 由 Start 中的 cmd.Wait() 负责回收，这里只等待其完成信号；
	// 若自行再 Wait 会与之竞争同一子进程并返回伪错误
	// 进程非本管理器启动时（如外部注入）没有 exited 信号，
	// 此时自行 Wait 回收，不存在与 Start 的竞争
	if exited == nil {
		fallback := make(chan struct{})
		go func() {
			_, _ = proc.Wait()
			close(fallback)
		}()
		exited = fallback
	}

	var retErr error
	select {
	case <-exited:
		retErr = stopErr
	case <-ctx.Done():
		_ = proc.Kill()
		retErr = ctx.Err()
	case <-time.After(8 * time.Second):
		_ = proc.Kill()
		retErr = errors.New("mihomo stop timed out")
	}

	if retErr == nil {
		m.appendLog("system", "mihomo stopped")
	} else {
		m.appendLog("system", "mihomo stop error: "+retErr.Error())
	}
	return retErr
}

func (m *ProcessManager) Restart(ctx context.Context) error {
	// 整个停止-启动过程持一把锁，避免与并发的 Start/Stop 交错
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if err := m.stopLocked(ctx); err != nil {
		// 停止失败仍尝试启动，但记录原因便于排查
		m.appendLog("system", "restart: stop failed, continue to start: "+err.Error())
	}
	return m.startLocked(ctx)
}

func (m *ProcessManager) Reload(ctx context.Context) error {
	return m.Restart(ctx)
}

// ReloadConfig 优先走 mihomo 官方的 external-controller RESTful API
// (PUT /configs?force=true) 做热重载：不重启进程、不断开代理连接。
// 该接口仅在进程已在运行且 controller 地址可达时才有意义，
// 其余情况（未运行 / 未配置 controller / 请求失败）一律回退到 Reload（即 Restart）。
func (m *ProcessManager) ReloadConfig(ctx context.Context, controller, secret, configPath string) error {
	if strings.TrimSpace(controller) == "" {
		m.appendLog("system", "external-controller 未配置，回退为重启进程")
		return m.Reload(ctx)
	}
	if !m.isProcessAliveLocked2() {
		return m.Reload(ctx)
	}

	if err := putConfigsAPI(ctx, controller, secret, configPath); err != nil {
		m.appendLog("system", "热重载 API 调用失败，回退为重启进程: "+err.Error())
		return m.Reload(ctx)
	}
	m.appendLog("system", "已通过 external-controller API 热重载配置")
	return nil
}

// isProcessAliveLocked2 是 isProcessAliveLocked 的加锁包装，
// 供不持有 m.mu 的外部方法（如 ReloadConfig）安全调用。
func (m *ProcessManager) isProcessAliveLocked2() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isProcessAliveLocked()
}

func (m *ProcessManager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := Status{IsRunning: false, Version: m.version}
	if st.Version == "" {
		st.Version = "unknown"
	}
	if m.isProcessAliveLocked() {
		st.IsRunning = true
		st.PID = m.cmd.Process.Pid
	}
	return st
}

func (m *ProcessManager) Version(ctx context.Context) (string, error) {
	m.mu.RLock()
	cached := m.version
	m.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}

	// 执行外部进程时绝不能持有 m.mu —— 它同时保护日志缓冲区，
	// 一旦 `mihomo -v` 卡住（Windows 上被安全软件扫描时很常见），
	// 会连带阻塞状态查询与 WebSocket 日志推送。
	// 另加独立兜底超时，避免调用方传入的 ctx 本身没有 deadline。
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	v, err := m.versionUnlocked(execCtx)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.version = v
	m.mu.Unlock()
	return v, nil
}

func (m *ProcessManager) versionUnlocked(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, m.config.BinaryPath, "-v")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	line := strings.TrimSpace(out.String())
	if i := strings.IndexByte(line, 10); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "unknown", nil
	}
	return line, nil
}

func (m *ProcessManager) ValidateConfig(ctx context.Context, configPath string) error {
	cmd := exec.CommandContext(ctx, m.config.BinaryPath, "-t", "-f", configPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("config validation failed: %s", msg)
	}
	return nil
}

func (m *ProcessManager) Logs(limit int) []LogLine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.logs) {
		limit = len(m.logs)
	}
	out := make([]LogLine, limit)
	copy(out, m.logs[len(m.logs)-limit:])
	return out
}

func (m *ProcessManager) SubscribeLogs(fn LogListener) func() {
	m.subsMu.Lock()
	m.subSeq++
	id := m.subSeq
	m.subs[id] = fn
	m.subsMu.Unlock()
	return func() {
		m.subsMu.Lock()
		delete(m.subs, id)
		m.subsMu.Unlock()
	}
}

// putConfigsAPI 调用 mihomo external-controller 的 PUT /configs?force=true，
// 令内核在不重启进程的前提下重新加载配置文件。
// path 必须是内核进程能访问到的绝对路径（与 configPath 所在文件系统一致）。
func putConfigsAPI(ctx context.Context, controller, secret, configPath string) error {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	body, err := json.Marshal(map[string]string{"path": absPath})
	if err != nil {
		return err
	}

	base := strings.TrimSpace(controller)
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	url := base + "/configs?force=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request external-controller: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("external-controller returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

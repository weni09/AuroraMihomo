package adguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config 控制 AdGuard Home 子进程的二进制路径与工作目录。
// WebAddr 仅作 Status 展示；yaml 绑定在后续任务处理。
type Config struct {
	BinaryPath string
	WorkDir    string // 通常为 data/adguardhome
	WebAddr    string // 如 127.0.0.1:3000，仅供 Status 回显
}

// Status 是 AdGuard 进程对外可见状态（JSON 字段供 API 直接序列化）。
type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	WorkDir   string `json:"workDir"`
	WebAddr   string `json:"webAddr"`
	LastError string `json:"lastError,omitempty"`
}

// Manager 管理 AdGuard Home 常驻子进程的启停，对标 mihomo.ProcessManager 的
// opMu/mu 分离与「不绑定请求 ctx」语义。
type Manager struct {
	// opMu 串行化 Start/Stop/Restart，避免交错产生孤儿进程
	opMu sync.Mutex
	// mu 只保护结构体字段读写
	mu sync.RWMutex

	cfg     Config
	cmd     *exec.Cmd
	exited  chan struct{} // 进程 Wait 完成时关闭，供 Stop 等待
	version string
	lastErr string

	// testForceRunning 仅供同包单测模拟 Running，不改变真实进程状态。
	testForceRunning bool
}

func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// SetWebAddr 更新 Status/反代回显用的 Web 地址（如 127.0.0.1:3000）。
func (m *Manager) SetWebAddr(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.WebAddr = strings.TrimSpace(addr)
}

func (m *Manager) isProcessAliveLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return m.cmd.ProcessState == nil || !m.cmd.ProcessState.Exited()
	}
	return m.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (m *Manager) setLastErr(msg string) {
	m.mu.Lock()
	m.lastErr = msg
	m.mu.Unlock()
}

// Start 拉起 AdGuardHome --work-dir <WorkDir>。
// 刻意不用 CommandContext：常驻子进程生命周期由 Stop/Restart 显式管理，
// 绝不能随发起启动的那次请求 ctx 取消而被杀掉。
func (m *Manager) Start(ctx context.Context) error {
	_ = ctx
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.startLocked()
}

func (m *Manager) startLocked() error {
	m.mu.RLock()
	alive := m.isProcessAliveLocked()
	m.mu.RUnlock()
	if alive {
		// 已在跑：对调用方是幂等成功语义更友好，不污染 lastErr
		return nil
	}

	if m.cfg.BinaryPath == "" {
		err := errors.New("adguard binary path is empty")
		m.setLastErr(err.Error())
		return err
	}
	if _, err := os.Stat(m.cfg.BinaryPath); err != nil {
		msg := fmt.Sprintf("adguard binary not found: %v", err)
		m.setLastErr(msg)
		return errors.New(msg)
	}

	if m.cfg.WorkDir != "" {
		if err := os.MkdirAll(m.cfg.WorkDir, 0o755); err != nil {
			msg := fmt.Sprintf("create work dir failed: %v", err)
			m.setLastErr(msg)
			return fmt.Errorf("create work dir failed: %w", err)
		}
	}

	// 绝对路径：openrc 下 cwd 虽是 /opt/auroramihomo，但相对 --work-dir 在
	// 其它启动方式下会找不到配置，AGH 日志会报 first time launched / 找不到文件。
	binPath, err := filepath.Abs(m.cfg.BinaryPath)
	if err != nil {
		binPath = m.cfg.BinaryPath
	}
	workDir := m.cfg.WorkDir
	if workDir != "" {
		if abs, err := filepath.Abs(workDir); err == nil {
			workDir = abs
		}
	}
	webAddr := strings.TrimSpace(m.cfg.WebAddr)
	if webAddr == "" {
		webAddr = fmt.Sprintf("%s:%d", localhostBind, defaultWebPort)
	}
	webAddr = strings.TrimPrefix(strings.TrimPrefix(webAddr, "http://"), "https://")

	// 启动前写入完整引导配置，跳过官方 install.html 向导。
	if workDir != "" {
		dnsPort := defaultDNSPort
		if p, err := ReadDNSPort(workDir); err == nil && p > 0 {
			dnsPort = p
		}
		if err := EnsureBootstrapConfig(workDir, webAddr, dnsPort, "admin", ""); err != nil {
			m.setLastErr("bootstrap config: " + err.Error())
			// 不直接 return：仍尝试启动，便于看 AGH 自身日志
		} else if err := EnsureBindLocalhost(workDir); err != nil {
			m.setLastErr("ensure bind localhost: " + err.Error())
		}
	}

	cfgFile := ""
	if workDir != "" {
		cfgFile = filepath.Join(workDir, aghConfigFile)
	}
	args := []string{}
	if workDir != "" {
		args = append(args, "--work-dir", workDir)
	}
	if cfgFile != "" {
		args = append(args, "--config", cfgFile)
	}
	args = append(args, "--web-addr", webAddr, "--no-check-update")

	//nolint:noctx // 常驻进程不应绑定请求级 context
	cmd := exec.Command(binPath, args...)
	// 工作目录固定到 work-dir，避免相对 data/ 路径歧义
	if workDir != "" {
		cmd.Dir = workDir
	}
	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("failed to start adguard: %v", err)
		m.setLastErr(msg)
		return fmt.Errorf("failed to start adguard: %w", err)
	}

	exited := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.exited = exited
	m.lastErr = ""
	m.mu.Unlock()

	go func(c *exec.Cmd, done chan struct{}) {
		waitErr := c.Wait()
		m.mu.Lock()
		if m.cmd == c {
			m.cmd = nil
			m.exited = nil
		}
		if waitErr != nil {
			m.lastErr = "adguard exited: " + waitErr.Error()
		}
		m.mu.Unlock()
		close(done)
	}(cmd, exited)

	return nil
}

// Stop 先优雅信号再超时强杀；未运行时幂等返回 nil。
func (m *Manager) Stop(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.stopLocked(ctx)
}

func (m *Manager) stopLocked(ctx context.Context) error {
	m.mu.Lock()
	if m.cmd == nil || m.cmd.Process == nil {
		m.mu.Unlock()
		return nil
	}
	proc := m.cmd.Process
	exited := m.exited
	// 立即置空，使 Status 不再把该进程视为运行中
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

	// 由 Start 中的 Wait 回收；这里只等 exited，避免对同一子进程二次 Wait
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
		retErr = errors.New("adguard stop timed out")
	}

	if retErr != nil {
		m.setLastErr(retErr.Error())
	}
	return retErr
}

// Restart 先 Stop 再 Start；停止失败仍尝试启动（与 mihomo 一致）。
func (m *Manager) Restart(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	_ = m.stopLocked(ctx)
	return m.startLocked()
}

// Status 返回安装/运行状态快照。
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st := Status{
		Version:   m.version,
		WorkDir:   m.cfg.WorkDir,
		WebAddr:   m.cfg.WebAddr,
		LastError: m.lastErr,
	}
	if m.cfg.BinaryPath != "" {
		if _, err := os.Stat(m.cfg.BinaryPath); err == nil {
			st.Installed = true
		}
	}
	if m.testForceRunning {
		st.Running = true
	} else if m.isProcessAliveLocked() {
		st.Running = true
		st.PID = m.cmd.Process.Pid
	}
	return st
}

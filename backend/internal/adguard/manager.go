package adguard

import (
	"context"
	"errors"
	"fmt"
	"net"
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
//
// 有两种运行形态：
//   - 服务模式（controller 非 nil）：进程由 systemd/OpenRC 看护，面板只做
//     控制面调用；面板升级/重启/崩溃期间 DNS 过滤不随面板进程中断。
//   - exec 模式（controller 为 nil，Windows 等）：面板 spawn 子进程并负责
//     生命周期，与历史行为一致。
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
	// controller 系统服务管理器（systemd/OpenRC）；nil 回落 exec 子进程。
	// 构造后经 SetController 注入一次，运行期不变。
	controller ServiceController

	// testForceRunning 仅供同包单测模拟 Running，不改变真实进程状态。
	testForceRunning bool
	// pendingInitialPass 首次引导生成的管理员明文；TakeInitialAdminPassword 取走后清空。
	pendingInitialPass string
}

func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// SetController 注入系统服务控制器（nil 清除）。服务模式下 Start/Stop/Restart
// 走系统服务管理器，进程存活与崩溃重启由 systemd/OpenRC 负责。
func (m *Manager) SetController(c ServiceController) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.controller = c
	m.mu.Unlock()
}

// Controller 返回当前服务控制器（nil 表示 exec 子进程模式）。
func (m *Manager) Controller() ServiceController {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.controller
}

// ServiceMode 是否处于服务模式（进程由系统服务管理器看护）。
func (m *Manager) ServiceMode() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.controller != nil
}

// ServiceEnabled 服务是否已注册开机自启（服务模式下读系统真实状态）。
func (m *Manager) ServiceEnabled(ctx context.Context) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	ctrl := m.controller
	m.mu.RUnlock()
	if ctrl == nil {
		return false
	}
	return ctrl.IsEnabled(ctx)
}

// ConfigFilePath 返回 AGH yaml 路径（work-dir 下固定名），供注册服务单元用。
func (m *Manager) ConfigFilePath() string {
	if m == nil || m.cfg.WorkDir == "" {
		return ""
	}
	return filepath.Join(m.cfg.WorkDir, aghConfigFile)
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
// 服务模式下委托系统服务管理器（systemctl start 对已在运行的服务是 no-op）。
func (m *Manager) Start(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.startLocked(ctx)
}

// webAddrForBootstrap 归一化 Web 地址（去 scheme、默认回环 3000），
// 供引导写 yaml http.address 使用；Status 回显仍用 cfg.WebAddr 原值。
func (m *Manager) webAddrForBootstrap() string {
	webAddr := strings.TrimSpace(m.cfg.WebAddr)
	if webAddr == "" {
		webAddr = fmt.Sprintf("%s:%d", localhostBind, defaultWebPort)
	}
	return strings.TrimPrefix(strings.TrimPrefix(webAddr, "http://"), "https://")
}

// prepareBootstrapLocked 启动前写入完整引导配置，跳过官方 install.html 向导。
// 服务模式与 exec 模式共用：即使进程由系统服务拉起，首次引导的随机口令生成、
// 回环绑定与防污染 DNS 清洗仍是面板的职责（口令需要落库免密）。
func (m *Manager) prepareBootstrapLocked() {
	workDir := m.cfg.WorkDir
	if workDir == "" {
		return
	}
	dnsPort := defaultDNSPort
	if p, err := ReadDNSPort(workDir); err == nil && p > 0 {
		dnsPort = p
	}
	initPass, err := EnsureBootstrapConfig(workDir, m.webAddrForBootstrap(), dnsPort, "admin", "")
	if err != nil {
		m.setLastErr("bootstrap config: " + err.Error())
		// 不直接 return：仍尝试启动，便于看 AGH 自身日志
		return
	}
	if initPass != "" {
		m.mu.Lock()
		m.pendingInitialPass = initPass
		m.mu.Unlock()
	}
	if err := EnsureBindLocalhost(workDir); err != nil {
		m.setLastErr("ensure bind localhost: " + err.Error())
	}
	// 存量 yaml 可能仍含裸 8.8.8.8 bootstrap
	_ = SanitizePollutionProneDNS(workDir)
}

func (m *Manager) startLocked(ctx context.Context) error {
	m.mu.RLock()
	ctrl := m.controller
	m.mu.RUnlock()
	if ctrl != nil {
		// 服务模式：不 spawn 进程、不做 3s 探活——进程存活性由
		// systemd Restart=always / supervise-daemon 负责，Start 返回即成功。
		// 注意绝不能在此直接 kill（Restart=always 会 3 秒复活）。
		m.prepareBootstrapLocked()
		if err := ctrl.Start(ctx); err != nil {
			m.setLastErr("service start: " + err.Error())
			return fmt.Errorf("service start: %w", err)
		}
		m.mu.Lock()
		m.lastErr = ""
		m.mu.Unlock()
		return nil
	}

	m.mu.RLock()
	alive := m.isProcessAliveLocked()
	m.mu.RUnlock()
	if alive {
		// 已在跑：对调用方是幂等成功语义更友好，不污染 lastErr
		return nil
	}
	// 面板重启后，若上次子进程被 init 收养或仍占着 Web 口，
	// 再 Start 会因端口冲突失败，Status 却一直 Running=false，界面显示「已停止」。
	// Web 口已可连时视为已在服务，幂等成功（Stop 仍只能管本 Manager 拉起的进程）。
	if m.webPortOpen() {
		m.mu.Lock()
		m.lastErr = ""
		m.mu.Unlock()
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

	// 启动前写入完整引导配置，跳过官方 install.html 向导。
	m.prepareBootstrapLocked()

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
	// 刻意不传 --web-addr：yaml 的 http.address 是唯一事实来源（见
	// svc_unit_templates.go 的 D2 设计），命令行覆盖会造成改端口后的双来源漂移。
	args = append(args, "--no-check-update")

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

	// Start 立刻返回时进程可能马上因端口冲突退出，日志却写「已启动」、
	// 界面随后变成「已停止」。短暂等待：进程仍存活或 Web 口已开再成功。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		alive := m.isProcessAliveLocked()
		m.mu.RUnlock()
		if !alive {
			m.mu.RLock()
			msg := m.lastErr
			m.mu.RUnlock()
			if msg == "" {
				msg = "adguard process exited immediately after start"
			}
			return errors.New(msg)
		}
		if m.webPortOpen() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	// 进程仍在、Web 稍慢：仍视为成功（部分环境监听略晚）
	m.mu.RLock()
	stillAlive := m.isProcessAliveLocked()
	m.mu.RUnlock()
	if !stillAlive {
		return errors.New("adguard process exited before web became ready")
	}
	return nil
}

// TakeInitialAdminPassword 取走首次引导生成的明文口令（一次性）。
func (m *Manager) TakeInitialAdminPassword() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pendingInitialPass
	m.pendingInitialPass = ""
	return p
}

// SetVersion 由服务层在 Install 记录 release tag 后同步到进程状态。
// 不在 Start 时 exec --version：Windows 上对测试用假二进制会锁文件导致 TempDir 清理失败。
func (m *Manager) SetVersion(v string) {
	if m == nil {
		return
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	m.mu.Lock()
	m.version = v
	m.mu.Unlock()
}

// Stop 先优雅信号再超时强杀；未运行时幂等返回 nil。
func (m *Manager) Stop(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.stopLocked(ctx)
}

func (m *Manager) stopLocked(ctx context.Context) error {
	m.mu.RLock()
	ctrl := m.controller
	m.mu.RUnlock()
	if ctrl != nil {
		// 服务模式：systemctl stop 不 disable——用户临时停 ≠ 取消开机自启。
		// 必须走服务管理器而不是杀 PID：Restart=always 会把 kill 变成复活。
		if err := ctrl.Stop(ctx); err != nil {
			m.setLastErr("service stop: " + err.Error())
			return fmt.Errorf("service stop: %w", err)
		}
		return nil
	}

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
	return m.startLocked(ctx)
}

// webPortOpen 探测 Web 管理口是否已在监听（默认 127.0.0.1:3000）。
// 用于：Status 反映真实可达性；Start 时避免对已在跑的实例二次拉起。
func (m *Manager) webPortOpen() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	addr := strings.TrimSpace(m.cfg.WebAddr)
	workDir := m.cfg.WorkDir
	m.mu.RUnlock()
	// 优先读 yaml 的 http.address：面板重启后 cfg.WebAddr 可能是旧值
	// （面板 down 期间用户在 AGH 侧改过端口），探测必须用真实端口。
	if workDir != "" {
		if port, err := ReadWebPort(workDir); err == nil && port > 0 {
			addr = fmt.Sprintf("%s:%d", localhostBind, port)
		}
	}
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", localhostBind, defaultWebPort)
	}
	// 只认回环，避免误把局域网其它服务当成 AGH
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		if host != "localhost" {
			return false
		}
	}
	// noctx 要求 DialContext 而非 DialTimeout（后者默认绑定 Background，
	// 无法感知上层取消）；这里本就无法用上层 ctx（Start/Status 路径都可能
	// 同步调用），显式 Background + 400ms 超时与旧行为等价。
	d := net.Dialer{Timeout: 400 * time.Millisecond}
	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Status 返回安装/运行状态快照。
func (m *Manager) Status() Status {
	m.mu.RLock()
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
	ctrl := m.controller
	m.mu.RUnlock()

	// 服务模式：进程由系统服务看护，Manager 无 cmd 句柄，
	// 以 systemctl is-active 为准（Active 的 exec 开销毫秒级，放锁外）。
	if !st.Running && ctrl != nil && ctrl.Active(context.Background()) {
		st.Running = true
	}

	// 端口探测不能持 mu（Dial 可能数百 ms）。子进程未登记但 Web 已监听时仍报运行中，
	// 避免界面误显示「已停止」而 :3000 其实可达。
	if !st.Running && m.webPortOpen() {
		st.Running = true
	}
	return st
}

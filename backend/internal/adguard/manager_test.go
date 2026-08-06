package adguard

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildSleepHelper 编译一个忽略参数、收到信号前一直 sleep 的假二进制，
// 用于跨平台验证 Start/Stop 生命周期（Windows 没有可用的 sleep 可执行文件路径）。
func buildSleepHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main
import (
	"os"
	"os/signal"
	"syscall"
	"time"
)
func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	select {
	case <-c:
	case <-time.After(60 * time.Second):
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	out := filepath.Join(dir, "fake-adguard")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = os.Environ()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, b)
	}
	return out
}

func TestStart_MissingBinary(t *testing.T) {
	work := t.TempDir()
	mgr := NewManager(Config{
		BinaryPath: filepath.Join(work, "no-such-AdGuardHome"),
		WorkDir:    filepath.Join(work, "data"),
		WebAddr:    "127.0.0.1:3000",
	})
	err := mgr.Start(context.Background())
	if err == nil {
		t.Fatal("缺失二进制时应返回 error")
	}
	st := mgr.Status()
	if st.Installed {
		t.Fatal("Installed 应为 false")
	}
	if st.Running {
		t.Fatal("Running 应为 false")
	}
	if st.WebAddr != "127.0.0.1:3000" {
		t.Fatalf("WebAddr=%q", st.WebAddr)
	}
	if st.LastError == "" {
		t.Fatal("LastError 应记录失败原因")
	}
}

func TestStartStop_FakeBinary(t *testing.T) {
	bin := buildSleepHelper(t)
	work := t.TempDir()
	mgr := NewManager(Config{
		BinaryPath: bin,
		WorkDir:    filepath.Join(work, "adguardhome"),
		WebAddr:    "127.0.0.1:3000",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := mgr.Status()
	if !st.Installed {
		t.Fatal("Installed 应为 true")
	}
	if !st.Running {
		t.Fatal("Start 后 Running 应为 true")
	}
	if st.PID <= 0 {
		t.Fatalf("PID 应 > 0, got %d", st.PID)
	}
	if st.WorkDir != filepath.Join(work, "adguardhome") {
		t.Fatalf("WorkDir=%q", st.WorkDir)
	}
	// WorkDir 应被创建
	if _, err := os.Stat(st.WorkDir); err != nil {
		t.Fatalf("WorkDir 应存在: %v", err)
	}

	// 重复 Start 幂等成功
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("已运行时 Start 应幂等成功: %v", err)
	}

	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = mgr.Status()
	if st.Running {
		t.Fatal("Stop 后 Running 应为 false")
	}

	// 幂等 Stop
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("幂等 Stop 不应报错: %v", err)
	}
}

func TestRestart_FakeBinary(t *testing.T) {
	bin := buildSleepHelper(t)
	work := t.TempDir()
	mgr := NewManager(Config{
		BinaryPath: bin,
		WorkDir:    filepath.Join(work, "adguardhome"),
		WebAddr:    "127.0.0.1:3000",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid1 := mgr.Status().PID
	if err := mgr.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	st := mgr.Status()
	if !st.Running {
		t.Fatal("Restart 后应 Running")
	}
	if st.PID <= 0 {
		t.Fatalf("PID=%d", st.PID)
	}
	// PID 可能碰巧相同（极少），但不强制；至少进程要活着
	_ = pid1
	_ = mgr.Stop(ctx)
}

// fakeController 记录调用并可按需返回状态，供服务模式单测（不触碰真实系统服务）。
type fakeController struct {
	enabled bool
	active  bool
	started bool
	stopped bool
	err     error
}

func (f *fakeController) Install(context.Context, string, string, string) error { return f.err }
func (f *fakeController) Uninstall(context.Context) error                       { return f.err }
func (f *fakeController) Start(context.Context) error {
	f.started = true
	return f.err
}
func (f *fakeController) Stop(context.Context) error {
	f.stopped = true
	return f.err
}
func (f *fakeController) Restart(context.Context) error {
	f.started = true
	f.stopped = true
	return f.err
}
func (f *fakeController) SetBootEnabled(_ context.Context, enabled bool) error {
	f.enabled = enabled
	return f.err
}
func (f *fakeController) IsEnabled(context.Context) bool { return f.enabled }
func (f *fakeController) Active(context.Context) bool    { return f.active }

// TestStartStop_ServiceMode 服务模式下 Start/Stop/Restart 全部委托控制器，
// 不 spawn 进程、不检查二进制存在（安装时已校验）；Running 以 controller 为准。
func TestStartStop_ServiceMode(t *testing.T) {
	ctrl := &fakeController{}
	work := t.TempDir()
	mgr := NewManager(Config{
		BinaryPath: filepath.Join(work, "no-such-binary"),
		WorkDir:    filepath.Join(work, "adguardhome"),
		WebAddr:    "127.0.0.1:3000",
	})
	mgr.SetController(ctrl)
	ctx := context.Background()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !ctrl.started {
		t.Fatal("服务模式 Start 应调用 controller.Start")
	}
	if st := mgr.Status(); st.Running {
		t.Fatal("controller Active=false 时 Running 应为 false")
	}

	// Active=true 时 Status 反映运行中（崩溃重启间隙 Active 短暂为 false，
	// Status 依赖 controller 而非端口探测，避免误报）
	ctrl.active = true
	if st := mgr.Status(); !st.Running {
		t.Fatal("controller Active=true 时应 Running")
	}

	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !ctrl.stopped {
		t.Fatal("服务模式 Stop 应调用 controller.Stop")
	}
	ctrl.active = false
	if st := mgr.Status(); st.Running {
		t.Fatal("Stop 后 Running 应为 false")
	}

	if err := mgr.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !ctrl.started || !ctrl.stopped {
		t.Fatal("服务模式 Restart 应分解为 controller.Stop + Start")
	}
}

// TestStart_ServiceModeError 控制器失败时 Start 返回错误并记录 lastErr。
func TestStart_ServiceModeError(t *testing.T) {
	ctrl := &fakeController{err: fmt.Errorf("systemctl failed")}
	mgr := NewManager(Config{BinaryPath: "/nonexistent", WorkDir: t.TempDir()})
	mgr.SetController(ctrl)
	if err := mgr.Start(context.Background()); err == nil {
		t.Fatal("控制器失败时 Start 应返回 error")
	}
	if st := mgr.Status(); st.LastError == "" {
		t.Fatal("LastError 应记录失败原因")
	}
}

// TestWebPortOpen_ReadsYAML yaml 的 http.address 优先于 cfg.WebAddr：
// 面板 down 期间用户在 AGH 侧改过端口时，探测必须用真实端口。
func TestWebPortOpen_ReadsYAML(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	work := t.TempDir()
	cfg := fmt.Sprintf("http:\n  address: 127.0.0.1:%d\n", port)
	if err := os.WriteFile(filepath.Join(work, aghConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(Config{
		BinaryPath: "/nonexistent",
		WorkDir:    work,
		WebAddr:    "127.0.0.1:39999", // 故意与 yaml 不一致
	})
	if !mgr.webPortOpen() {
		t.Fatal("应探测到 yaml 中的真实端口而非 cfg.WebAddr")
	}
}

// buildArgsDumpHelper 编译一个把 os.Args 写入 ARGS_DUMP_FILE 的假二进制，
// 用于断言 exec 路径的启动参数（如不传 --web-addr）。
func buildArgsDumpHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main
import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)
func main() {
	if f := os.Getenv("ARGS_DUMP_FILE"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(os.Args, "\n")), 0o644)
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	select {
	case <-c:
	case <-time.After(60 * time.Second):
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	out := filepath.Join(dir, "fake-adguard")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = os.Environ()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, b)
	}
	return out
}

// TestStart_ExecModeNoWebAddrArg exec 路径不传 --web-addr（yaml 是端口唯一
// 事实来源）；若命令行与 yaml 同时出现，改端口后重启会用旧参数覆盖 yaml。
func TestStart_ExecModeNoWebAddrArg(t *testing.T) {
	bin := buildArgsDumpHelper(t)
	work := t.TempDir()
	dumpFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("ARGS_DUMP_FILE", dumpFile)
	mgr := NewManager(Config{
		BinaryPath: bin,
		WorkDir:    filepath.Join(work, "adguardhome"),
		WebAddr:    "127.0.0.1:3000",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(ctx)

	// Start 返回前经过 3s 探活，args 必然已落盘
	b, err := os.ReadFile(dumpFile)
	if err != nil {
		t.Fatalf("read args dump: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "--web-addr") {
			t.Fatalf("exec 路径不应传 --web-addr: %s", line)
		}
	}
	if !strings.Contains(string(b), "--no-check-update") {
		t.Fatalf("应传 --no-check-update:\n%s", b)
	}
}

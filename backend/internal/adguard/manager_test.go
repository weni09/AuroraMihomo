package adguard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/repository"
)

// fakeMihomoManager 实现 mihomo.Manager，可编程控制 running 状态与 Start 行为。
type fakeMihomoManager struct {
	mu         sync.Mutex
	running    bool
	startCalls int
	startErr   error
}

func (f *fakeMihomoManager) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	if f.startErr != nil {
		return f.startErr
	}
	f.running = true
	return nil
}
func (f *fakeMihomoManager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	return nil
}
func (f *fakeMihomoManager) Restart(ctx context.Context) error { return f.Start(ctx) }
func (f *fakeMihomoManager) Reload(ctx context.Context) error  { return f.Restart(ctx) }
func (f *fakeMihomoManager) ReloadConfig(ctx context.Context, controller, secret, configPath string) error {
	return nil
}
func (f *fakeMihomoManager) AttachExternal(pid int, version string) (bool, error) { return false, nil }
func (f *fakeMihomoManager) DiscoverAndAttach() (bool, error)                     { return false, nil }
func (f *fakeMihomoManager) Status() mihomo.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mihomo.Status{IsRunning: f.running}
}
func (f *fakeMihomoManager) ValidateConfig(ctx context.Context, configPath string) error { return nil }
func (f *fakeMihomoManager) Version(ctx context.Context) (string, error)                 { return "fake", nil }
func (f *fakeMihomoManager) Logs(limit int, level mihomo.Level) []mihomo.LogLine         { return nil }
func (f *fakeMihomoManager) SubscribeLogs(fn mihomo.LogListener) func()                  { return func() {} }

// 统计 Start 调用次数
func (f *fakeMihomoManager) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCalls
}
func (f *fakeMihomoManager) setRunning(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = b
}
func (f *fakeMihomoManager) setStartErr(e error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startErr = e
}

func newGuard(t *testing.T) (*MihomoGuard, *fakeMihomoManager) {
	t.Helper()
	db, err := repository.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fake := &fakeMihomoManager{}
	return NewMihomoGuard(db, fake), fake
}

// 未设置时期望运行默认 true（守护默认开，保持「面板启动自动拉内核」）。
func TestMihomoGuardDesiredDefaultTrue(t *testing.T) {
	g, _ := newGuard(t)
	if !g.DesiredRunning() {
		t.Fatal("未设置期望键时 DesiredRunning 应为 true")
	}
}

// SetDesiredRunning 持久化：false 后读回 false，true 后读回 true。
func TestMihomoGuardSetDesired(t *testing.T) {
	g, _ := newGuard(t)
	if err := g.SetDesiredRunning(false); err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	if g.DesiredRunning() {
		t.Fatal("关闭后 DesiredRunning 应为 false")
	}
	if err := g.SetDesiredRunning(true); err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	if !g.DesiredRunning() {
		t.Fatal("开启后 DesiredRunning 应为 true")
	}
}

// 未武装时 Guard 不动作：即使期望运行且内核已停，也不自动拉起。
func TestMihomoGuardNotArmedNoAction(t *testing.T) {
	g, fake := newGuard(t)
	fake.setRunning(false) // 内核已停
	g.Guard(context.Background())
	if fake.starts() != 0 {
		t.Fatalf("未武装时不应自动拉起，实际 startCalls=%d", fake.starts())
	}
}

// 已武装且期望运行、内核停止 → 自动拉起。
func TestMihomoGuardStartsWhenStoppedAndDesired(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	fake.setRunning(false)
	g.Guard(context.Background())
	if fake.starts() != 1 {
		t.Fatalf("内核停止且期望运行时应自动拉起，实际 startCalls=%d", fake.starts())
	}
	if !fake.Status().IsRunning {
		t.Fatal("Start 后 fake 内核应运行")
	}
}

// 内核正常运行时不重复拉起，且清零尝试计数。
func TestMihomoGuardNoStartWhenRunning(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	fake.setRunning(true)
	g.Guard(context.Background())
	if fake.starts() != 0 {
		t.Fatalf("运行中不应自动拉起，实际 startCalls=%d", fake.starts())
	}
}

// 期望运行关闭时（手动停止）不自动拉起，即使已武装。
func TestMihomoGuardRespectsManualStop(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	if err := g.SetDesiredRunning(false); err != nil {
		t.Fatal(err)
	}
	fake.setRunning(false)
	g.Guard(context.Background())
	if fake.starts() != 0 {
		t.Fatalf("手动停止后不应自动拉起，实际 startCalls=%d", fake.starts())
	}
}

// 限次窗口：窗口内多次失败尝试不超过 guardMaxAttempts。
func TestMihomoGuardRateLimit(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	fake.setRunning(false)
	fake.setStartErr(errors.New("fake start failure")) // Start 失败，内核保持 stopped

	for i := 0; i < guardMaxAttempts+2; i++ {
		g.Guard(context.Background())
	}
	if fake.starts() > guardMaxAttempts {
		t.Fatalf("窗口内尝试次数 %d 应不超过 %d", fake.starts(), guardMaxAttempts)
	}
}

// 内核恢复后重置尝试计数：恢复后下一次停止仍能拉起。
func TestMihomoGuardResetAfterSuccess(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	fake.setRunning(false)
	fake.setStartErr(errors.New("fake start failure"))

	// 连续失败到限次
	for i := 0; i < guardMaxAttempts; i++ {
		g.Guard(context.Background())
	}
	if fake.starts() != guardMaxAttempts {
		t.Fatalf("应有 %d 次尝试，实际 %d", guardMaxAttempts, fake.starts())
	}
	// 第 4 次应被限次拦截
	g.Guard(context.Background())
	if fake.starts() != guardMaxAttempts {
		t.Fatalf("限次窗口内不应再尝试，实际 %d", fake.starts())
	}

	// 内核恢复（running）→ 清零尝试计数
	fake.setStartErr(nil)
	fake.setRunning(true)
	g.Guard(context.Background())
	// 恢复后再次停止，应能立刻再拉
	fake.setRunning(false)
	g.Guard(context.Background())
	if fake.starts() != guardMaxAttempts+1 {
		t.Fatalf("恢复后应能再次拉起，实际 starts=%d", fake.starts())
	}
}

// SetDesiredRunning 后可读回；guard 的 DesiredRunning 与 settings 一致。
func TestMihomoGuardDesiredPersisted(t *testing.T) {
	g, _ := newGuard(t)
	if err := g.SetDesiredRunning(true); err != nil {
		t.Fatal(err)
	}
	if !g.DesiredRunning() {
		t.Fatal("期望应持久化为 true")
	}
}

// 期望关闭状态下调用 Guard 会清空尝试计数（重新开启后不被旧失败挡住）。
func TestMihomoGuardClearsAttemptsWhenNotDesired(t *testing.T) {
	g, fake := newGuard(t)
	g.SetArmed(true)
	fake.setRunning(false)
	fake.setStartErr(errors.New("fail"))

	// 失败到限次
	for i := 0; i < guardMaxAttempts; i++ {
		g.Guard(context.Background())
	}
	if fake.starts() != guardMaxAttempts {
		t.Fatalf("应有 %d 次尝试", guardMaxAttempts)
	}

	// 手动关闭期望（模拟用户 Stop）
	_ = g.SetDesiredRunning(false)
	g.Guard(context.Background()) // 期望关 → 清计数

	// 重新开启期望后应能立即再拉（不被旧失败挡住）
	_ = g.SetDesiredRunning(true)
	g.Guard(context.Background())
	if fake.starts() != guardMaxAttempts+1 {
		t.Fatalf("重新开启后应能再拉，实际 starts=%d", fake.starts())
	}
}

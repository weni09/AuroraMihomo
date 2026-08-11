package protected

import (
	"context"
	"path/filepath"
	"testing"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/service"
)

// logicMihomoFake 是 mihomo.Manager 的最小实现，供 logic 层测试断言启停路径。
type logicMihomoFake struct {
	running bool
}

func (f *logicMihomoFake) Start(ctx context.Context) error { f.running = true; return nil }
func (f *logicMihomoFake) Stop(ctx context.Context) error  { f.running = false; return nil }
func (f *logicMihomoFake) Restart(ctx context.Context) error {
	f.running = true
	return nil
}
func (f *logicMihomoFake) Reload(ctx context.Context) error { return f.Restart(ctx) }
func (f *logicMihomoFake) ReloadConfig(ctx context.Context, controller, secret, configPath string) error {
	return nil
}
func (f *logicMihomoFake) AttachExternal(pid int, version string) (bool, error) { return false, nil }
func (f *logicMihomoFake) Status() mihomo.Status {
	return mihomo.Status{IsRunning: f.running}
}
func (f *logicMihomoFake) ValidateConfig(ctx context.Context, configPath string) error { return nil }
func (f *logicMihomoFake) Version(ctx context.Context) (string, error)                 { return "fake", nil }
func (f *logicMihomoFake) Logs(limit int, level mihomo.Level) []mihomo.LogLine         { return nil }
func (f *logicMihomoFake) SubscribeLogs(fn mihomo.LogListener) func()                  { return func() {} }

// newMihomoLogicSvc 构造最小 ServiceContext：真实临时 db + 守护 + fake manager。
func newMihomoLogicSvc(t *testing.T) (*svc.ServiceContext, *logicMihomoFake, *service.MihomoGuard) {
	t.Helper()
	db, err := repository.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fake := &logicMihomoFake{}
	guard := service.NewMihomoGuard(db, fake)
	return &svc.ServiceContext{Database: db, MihomoManager: fake, MihomoGuard: guard}, fake, guard
}

// 手动停止内核会清掉期望运行（守护与面板重启都不再自动拉）。
func TestMihomoStopLogicClearsDesiredRunning(t *testing.T) {
	svcCtx, fake, guard := newMihomoLogicSvc(t)
	// 预置期望运行
	if err := guard.SetDesiredRunning(true); err != nil {
		t.Fatal(err)
	}

	l := NewMihomoStopLogic(context.Background(), svcCtx)
	resp, err := l.MihomoStop()
	if err != nil {
		t.Fatalf("stop 失败: %v", err)
	}
	if !resp.Success {
		t.Fatalf("stop 应成功: %+v", resp)
	}
	if fake.running {
		t.Fatal("fake 内核应已停止")
	}
	if guard.DesiredRunning() {
		t.Fatal("手动停止后期望运行应被清掉")
	}
}

// 手动启动内核会置位期望运行。
func TestMihomoStartLogicSetsDesiredRunning(t *testing.T) {
	svcCtx, fake, guard := newMihomoLogicSvc(t)
	// 预置已手动停止（期望关）
	if err := guard.SetDesiredRunning(false); err != nil {
		t.Fatal(err)
	}

	l := NewMihomoStartLogic(context.Background(), svcCtx)
	resp, err := l.MihomoStart()
	if err != nil {
		t.Fatalf("start 失败: %v", err)
	}
	if !resp.Success {
		t.Fatalf("start 应成功: %+v", resp)
	}
	if !fake.running {
		t.Fatal("fake 内核应已启动")
	}
	if !guard.DesiredRunning() {
		t.Fatal("手动启动后期望运行应置为 true")
	}
}

// 手动重启内核会置位期望运行（即使此前被手动停过）。
func TestMihomoRestartLogicSetsDesiredRunning(t *testing.T) {
	svcCtx, fake, guard := newMihomoLogicSvc(t)
	if err := guard.SetDesiredRunning(false); err != nil {
		t.Fatal(err)
	}

	l := NewMihomoRestartLogic(context.Background(), svcCtx)
	resp, err := l.MihomoRestart()
	if err != nil {
		t.Fatalf("restart 失败: %v", err)
	}
	if !resp.Success {
		t.Fatalf("restart 应成功: %+v", resp)
	}
	if !fake.running {
		t.Fatal("fake 内核应已运行")
	}
	if !guard.DesiredRunning() {
		t.Fatal("手动重启后期望运行应置为 true")
	}
}

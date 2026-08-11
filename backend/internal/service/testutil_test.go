package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"auroramihomo/backend/internal/engine"
	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/repository"
)

// mockMihomo 是 mihomo.Manager 的可编程假实现，
// 用于在不启动真实内核进程的情况下验证 ConfigService 的调用行为。
type mockMihomo struct {
	mu sync.Mutex

	validateErr error // ValidateConfig 返回的错误
	reloadErr   error // ReloadConfig/Reload 返回的错误

	validateCalls     int
	reloadConfigCalls int
	lastController    string
	lastSecret        string
	lastReloadPath    string
	restartCalls      int
}

func (m *mockMihomo) Start(ctx context.Context) error { return nil }
func (m *mockMihomo) Stop(ctx context.Context) error  { return nil }
func (m *mockMihomo) Restart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartCalls++
	return nil
}
func (m *mockMihomo) Reload(ctx context.Context) error { return m.Restart(ctx) }

func (m *mockMihomo) ReloadConfig(ctx context.Context, controller, secret, configPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadConfigCalls++
	m.lastController = controller
	m.lastSecret = secret
	m.lastReloadPath = configPath
	return m.reloadErr
}

func (m *mockMihomo) AttachExternal(pid int, version string) (bool, error) {
	return false, nil
}

func (m *mockMihomo) Status() mihomo.Status { return mihomo.Status{} }

func (m *mockMihomo) ValidateConfig(ctx context.Context, configPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validateCalls++
	return m.validateErr
}

func (m *mockMihomo) Version(ctx context.Context) (string, error) { return "mock", nil }
func (m *mockMihomo) Logs(int, mihomo.Level) []mihomo.LogLine     { return nil }
func (m *mockMihomo) SubscribeLogs(fn mihomo.LogListener) func()  { return func() {} }

func (m *mockMihomo) snapshot() (reloadConfigCalls, restartCalls int, controller, secret string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reloadConfigCalls, m.restartCalls, m.lastController, m.lastSecret
}

// mockSubStore 是 substore.Manager 的兜底假实现，
// 测试中优先命中 Go 原生引擎路径，此 mock 仅用于兜底分支不触发网络请求。
type mockSubStore struct{}

func (m *mockSubStore) FetchAndConvert(ctx context.Context, subscriptionURL string) ([]byte, error) {
	return nil, errMockNotConfigured
}

var errMockNotConfigured = &mockErr{"mockSubStore 未配置实际内容"}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }

// newTestConfigService 构造一个使用真实临时 SQLite 与 mock 内核管理器的 ConfigService，
// 供核心合并/恢复/定时更新分支的测试使用。
func newTestConfigService(t *testing.T) (*ConfigService, *repository.Database, *mockMihomo) {
	t.Helper()
	dir := t.TempDir()
	db, err := repository.NewDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mgr := &mockMihomo{}
	svc := NewConfigService(db, engine.NewMergeEngine(), mgr, &mockSubStore{}, dir)
	return svc, db, mgr
}

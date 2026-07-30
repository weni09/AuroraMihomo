package service

import (
	"errors"
	"sync"
	"time"
)

// ReloadManager 承担两件事：进程内热重载，以及「请求进程退出」的信号中转。
//
// 为什么需要它：
//
// 改动设置（Cron 表达式、CDN 源、合并策略等）此前要重启进程才生效，而本项目
// 在 Windows 上重启一次的代价很高——go-zero 的优雅关停在该平台是空实现，
// 关停流程会关掉数据库却停不掉监听，进程残留、端口被占，后续请求持续报
// "sql: database is closed"（详见 backend/api/aurora.go 的关停注释）。
// 优雅关停已单独修好，但「能不重启就不重启」依然是更稳的做法：
// 绝大多数设置变更只需重装 Cron 与刷新内存里的运行时设置，
// 不涉及监听端口，没有任何理由中断在途请求。
//
// 少数改动（监听地址/端口、JWT 密钥、数据库路径）确实必须重建进程，
// 那种情况下 RequestQuit 让调用方触发一次完整的优雅关停，
// 由外部进程管理器（systemd / docker restart / NSSM）负责拉起。
// 进程自己 fork 重启在 Windows 上无法可靠实现（没有 fork，
// 监听 socket 也无法继承），交给进程管理器是更诚实的选择。
type ReloadManager struct {
	mu sync.Mutex

	// reloaders 是热重载时要依次执行的动作，按注册顺序调用。
	// 用切片而非固定字段：调用方（main）最清楚有哪些东西需要重装，
	// 这里不该硬编码对 SettingsService / Scheduler 的依赖。
	reloaders []namedReloader

	lastReload time.Time
	reloadCnt  int

	quitOnce sync.Once
	quitCh   chan struct{}

	// quitReason 记录请求退出的原因，供日志与接口返回
	quitReason string
}

type namedReloader struct {
	name string
	fn   func() error
}

func NewReloadManager() *ReloadManager {
	return &ReloadManager{quitCh: make(chan struct{})}
}

// Register 添加一个热重载动作。name 只用于日志与错误信息定位。
func (m *ReloadManager) Register(name string, fn func() error) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloaders = append(m.reloaders, namedReloader{name: name, fn: fn})
}

// Reload 依次执行所有已注册的重载动作。
//
// 出错不中断后续动作：各动作之间相互独立（重装自动更新 Cron 失败
// 不该导致远程拉取 Cron 也不刷新），全部执行完再把错误合并返回，
// 这样调用方能一次看到所有问题，而不是修一个才发现下一个。
func (m *ReloadManager) Reload() error {
	m.mu.Lock()
	actions := make([]namedReloader, len(m.reloaders))
	copy(actions, m.reloaders)
	m.mu.Unlock()

	var errs []error
	for _, a := range actions {
		if err := a.fn(); err != nil {
			errs = append(errs, &reloadError{name: a.name, err: err})
		}
	}

	m.mu.Lock()
	m.lastReload = time.Now()
	m.reloadCnt++
	m.mu.Unlock()

	return errors.Join(errs...)
}

// Stats 返回热重载的执行情况，供接口与日志展示。
func (m *ReloadManager) Stats() (count int, last time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reloadCnt, m.lastReload
}

// RequestQuit 请求进程走完整的优雅关停流程。
//
// 用 sync.Once 保证只触发一次：重复请求（用户连点两次「重启」）不该
// 让关停流程跑两遍，否则会出现重复关库等重入问题。
func (m *ReloadManager) RequestQuit(reason string) {
	m.quitOnce.Do(func() {
		m.mu.Lock()
		m.quitReason = reason
		m.mu.Unlock()
		close(m.quitCh)
	})
}

// QuitRequested 返回一个在 RequestQuit 被调用后关闭的通道，
// 供 main 的关停 goroutine 与系统信号一起 select。
func (m *ReloadManager) QuitRequested() <-chan struct{} {
	return m.quitCh
}

// QuitReason 返回请求退出的原因，未请求过时为空。
func (m *ReloadManager) QuitReason() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.quitReason
}

type reloadError struct {
	name string
	err  error
}

func (e *reloadError) Error() string { return e.name + ": " + e.err.Error() }
func (e *reloadError) Unwrap() error { return e.err }

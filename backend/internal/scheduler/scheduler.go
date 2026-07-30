package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/logx"
)

// 可在运行期替换调度的任务名。
//
// 用具名常量而非散落的字符串：这些名字会出现在日志里，
// 也是 named 表的键，拼错会导致"看似注册成功却换错了任务"。
const (
	JobAutoUpdate = "auto-update"
	JobRemotePull = "remote config pull"
	JobLogCleanup = "applog cleanup"
)

// Scheduler wraps robfig/cron for background job execution.
type Scheduler struct {
	cron *cron.Cron
	mu   sync.Mutex

	// named 记录可重装任务当前的 entry id。
	//
	// 此前每类任务各有一对 xxxID/hasXxx 字段与一个几乎相同的
	// SetXxxJob 方法，加一类就要复制一遍（现已有三类）。
	// 改成按名字索引后，新增任务只是多一个常量。
	named map[string]cron.EntryID
}

func NewScheduler() *Scheduler {
	// SkipIfStillRunning：上一轮还没跑完就跳过本轮。
	// 合并配置可能耗时数十秒（多机场回源），若不抑制重叠，
	// 任务会在 applyMu 上无上限堆积，锁释放后连环执行形成雪崩。
	c := cron.New(
		cron.WithSeconds(),
		cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)),
	)
	return &Scheduler{cron: c, named: map[string]cron.EntryID{}}
}

func (s *Scheduler) AddJob(spec string, cmd func()) (cron.EntryID, error) {
	return s.cron.AddFunc(spec, cmd)
}

// SetJob 按名字替换一个可重装任务，供运行期改动 Cron 后即时生效。
//
// enabled=false 时移除既有任务并返回 nil——"关掉定时"是正常状态，不是错误。
//
// spec 非法时保持"已移除旧任务、未装新任务"的状态并返回错误：
// 让一个用户明确改坏了的表达式继续按旧调度跑，会造成"界面显示新值、
// 实际按旧值执行"的错觉，比停掉更难排查。调用方（SettingsService）
// 在落库前已用 NormalizeCron 校验过，走到这里出错说明是内部默认值有问题。
func (s *Scheduler) SetJob(name string, enabled bool, spec string, cmd func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.named[name]; ok {
		s.cron.Remove(id)
		delete(s.named, name)
	}
	if !enabled {
		logx.Infof("%s job disabled", name)
		return nil
	}
	id, err := s.cron.AddFunc(spec, cmd)
	if err != nil {
		return err
	}
	s.named[name] = id
	logx.Infof("%s job enabled, cron=%s", name, spec)
	return nil
}

// SetAutoUpdateJob replaces the previous auto-update job (if any).
// If enabled=false, existing auto-update job is removed.
func (s *Scheduler) SetAutoUpdateJob(enabled bool, spec string, cmd func()) error {
	return s.SetJob(JobAutoUpdate, enabled, spec, cmd)
}

// SetRemotePullJob 替换远程配置拉取任务。
// enabled=false 时移除既有任务（此时只能手动拉取）。
func (s *Scheduler) SetRemotePullJob(enabled bool, spec string, cmd func()) error {
	return s.SetJob(JobRemotePull, enabled, spec, cmd)
}

// SetLogCleanupJob 替换应用日志归档的清理任务。
func (s *Scheduler) SetLogCleanupJob(enabled bool, spec string, cmd func()) error {
	return s.SetJob(JobLogCleanup, enabled, spec, cmd)
}

func (s *Scheduler) Start(ctx context.Context) {
	logx.Info("Starting background scheduler...")
	s.cron.Start()
	go func() {
		<-ctx.Done()
		logx.Info("Stopping background scheduler...")
		s.cron.Stop()
	}()
}

// StopAndWait 停止调度并等待正在执行的任务跑完（受 timeout 约束）。
// 退出时必须先调用它再关闭数据库，否则正在进行的合并会撞上
// "database is closed"，留下磁盘配置与数据库状态不一致的残局。
func (s *Scheduler) StopAndWait(timeout time.Duration) {
	logx.Info("Stopping background scheduler and waiting for running jobs...")
	stopped := s.cron.Stop() // 返回的 ctx 在所有运行中的 job 结束后 Done
	select {
	case <-stopped.Done():
		logx.Info("All scheduled jobs finished")
	case <-time.After(timeout):
		logx.Error("等待定时任务收尾超时，仍有任务在运行")
	}
}

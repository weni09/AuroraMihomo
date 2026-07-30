package scheduler

import (
	"testing"
)

// 可重装任务此前每类都有一对 xxxID/hasXxx 字段与一个几乎相同的
// SetXxxJob 方法，加一类就复制一遍。收敛成按名字索引后，
// 这组测试保证替换、禁用、多任务隔离这三条语义没有走样。

func TestSetJobReplacesPrevious(t *testing.T) {
	s := NewScheduler()

	if err := s.SetJob("t", true, "0 0 1 * * *", func() {}); err != nil {
		t.Fatalf("首次装载失败: %v", err)
	}
	first := s.entryCount()

	if err := s.SetJob("t", true, "0 0 2 * * *", func() {}); err != nil {
		t.Fatalf("重装失败: %v", err)
	}
	if got := s.entryCount(); got != first {
		t.Errorf("重装应替换而非追加，条目数 %d -> %d", first, got)
	}
}

func TestSetJobDisableRemoves(t *testing.T) {
	s := NewScheduler()
	if err := s.SetJob("t", true, "0 0 1 * * *", func() {}); err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	if err := s.SetJob("t", false, "", func() {}); err != nil {
		t.Errorf("禁用不应报错——关掉定时是正常状态: %v", err)
	}
	if got := s.entryCount(); got != 0 {
		t.Errorf("禁用后应无任何条目，实际 %d", got)
	}
}

// 非法表达式必须报错，且保持"旧任务已移除、新任务未装"的状态。
// 让改坏的表达式继续按旧调度跑，会造成"界面显示新值、实际按旧值执行"的错觉。
func TestSetJobInvalidSpecRemovesOld(t *testing.T) {
	s := NewScheduler()
	if err := s.SetJob("t", true, "0 0 1 * * *", func() {}); err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	if err := s.SetJob("t", true, "这不是 cron", func() {}); err == nil {
		t.Fatal("非法表达式应返回错误")
	}
	if got := s.entryCount(); got != 0 {
		t.Errorf("非法表达式后不应残留旧任务，实际 %d 条", got)
	}
	// 恢复合法值后应能重新装上
	if err := s.SetJob("t", true, "0 0 3 * * *", func() {}); err != nil {
		t.Fatalf("恢复装载失败: %v", err)
	}
	if got := s.entryCount(); got != 1 {
		t.Errorf("应恢复为 1 条，实际 %d", got)
	}
}

// 不同名字的任务互不干扰——三类任务共用一张表，串了名字就会互相顶掉
func TestSetJobNamesAreIsolated(t *testing.T) {
	s := NewScheduler()
	for _, name := range []string{JobAutoUpdate, JobRemotePull, JobLogCleanup} {
		if err := s.SetJob(name, true, "0 0 1 * * *", func() {}); err != nil {
			t.Fatalf("%s 装载失败: %v", name, err)
		}
	}
	if got := s.entryCount(); got != 3 {
		t.Fatalf("三类任务应各占一条，实际 %d", got)
	}

	// 换掉其中一类，另外两类必须还在
	if err := s.SetJob(JobLogCleanup, true, "0 30 3 * * *", func() {}); err != nil {
		t.Fatalf("重装失败: %v", err)
	}
	if got := s.entryCount(); got != 3 {
		t.Errorf("重装一类不应影响其它类，实际 %d 条", got)
	}

	// 禁用其中一类，只减一条
	if err := s.SetJob(JobRemotePull, false, "", func() {}); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if got := s.entryCount(); got != 2 {
		t.Errorf("禁用一类应剩 2 条，实际 %d", got)
	}
}

// 三个具名封装必须落到各自的槽位，不能串
func TestNamedWrappersUseDistinctSlots(t *testing.T) {
	s := NewScheduler()
	if err := s.SetAutoUpdateJob(true, "0 0 4 * * *", func() {}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRemotePullJob(true, "0 0 5 * * *", func() {}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLogCleanupJob(true, "0 30 3 * * *", func() {}); err != nil {
		t.Fatal(err)
	}
	if got := s.entryCount(); got != 3 {
		t.Errorf("三个封装应各占一条，实际 %d —— 少于 3 说明串了槽位", got)
	}
}

func TestSetJobDisableWhenNeverEnabled(t *testing.T) {
	s := NewScheduler()
	// 从未装载过就直接禁用，不得 panic
	if err := s.SetJob("never", false, "", func() {}); err != nil {
		t.Errorf("不应报错: %v", err)
	}
}

// entryCount 返回当前 cron 中的条目数，仅供测试断言。
func (s *Scheduler) entryCount() int {
	return len(s.cron.Entries())
}

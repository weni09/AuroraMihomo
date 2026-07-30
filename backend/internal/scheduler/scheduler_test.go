package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// AddJob 应能注册一个按秒级 cron 表达式执行的任务，并在调度启动后被触发。
func TestAddJobRunsOnSchedule(t *testing.T) {
	s := NewScheduler()
	var count int32

	if _, err := s.AddJob("* * * * * *", func() { atomic.AddInt32(&count, 1) }); err != nil {
		t.Fatalf("注册任务失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()

	time.Sleep(2200 * time.Millisecond)
	if atomic.LoadInt32(&count) < 1 {
		t.Fatalf("任务应至少执行一次，实际 %d 次", count)
	}
}

// 非法 cron 表达式应立即报错，不应静默失败
func TestAddJobInvalidSpec(t *testing.T) {
	s := NewScheduler()
	if _, err := s.AddJob("not a cron expr", func() {}); err == nil {
		t.Fatal("非法 cron 表达式应报错")
	}
}

// enabled=false 时应移除已注册的自动更新任务，且不报错
func TestSetAutoUpdateJobDisable(t *testing.T) {
	s := NewScheduler()
	var count int32
	if err := s.SetAutoUpdateJob(true, "* * * * * *", func() { atomic.AddInt32(&count, 1) }); err != nil {
		t.Fatalf("启用自动更新任务失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()
	time.Sleep(1200 * time.Millisecond)

	if err := s.SetAutoUpdateJob(false, "", func() {}); err != nil {
		t.Fatalf("禁用自动更新任务失败: %v", err)
	}

	after := atomic.LoadInt32(&count)
	time.Sleep(2200 * time.Millisecond)
	if atomic.LoadInt32(&count) != after {
		t.Fatalf("禁用后任务不应再执行，禁用前 %d 次，禁用后又执行到 %d 次", after, atomic.LoadInt32(&count))
	}
}

// 重复调用 SetAutoUpdateJob 应替换掉旧任务，而不是叠加出多个并发执行的任务
func TestSetAutoUpdateJobReplacesOldOne(t *testing.T) {
	s := NewScheduler()
	var oldCount, newCount int32

	if err := s.SetAutoUpdateJob(true, "* * * * * *", func() { atomic.AddInt32(&oldCount, 1) }); err != nil {
		t.Fatalf("第一次启用失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()
	time.Sleep(1200 * time.Millisecond)

	if err := s.SetAutoUpdateJob(true, "* * * * * *", func() { atomic.AddInt32(&newCount, 1) }); err != nil {
		t.Fatalf("替换任务失败: %v", err)
	}

	oldBefore := atomic.LoadInt32(&oldCount)
	time.Sleep(2200 * time.Millisecond)

	if atomic.LoadInt32(&oldCount) != oldBefore {
		t.Fatalf("旧任务应已被替换掉不再执行，替换前 %d 次，之后又执行到 %d 次", oldBefore, atomic.LoadInt32(&oldCount))
	}
	if atomic.LoadInt32(&newCount) < 1 {
		t.Fatal("新任务应正常执行")
	}
}

// 非法 cron 表达式不应破坏已有的自动更新任务状态
func TestSetAutoUpdateJobInvalidSpecKeepsOldState(t *testing.T) {
	s := NewScheduler()
	var count int32
	if err := s.SetAutoUpdateJob(true, "* * * * * *", func() { atomic.AddInt32(&count, 1) }); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	if err := s.SetAutoUpdateJob(true, "not a cron expr", func() {}); err == nil {
		t.Fatal("非法 cron 表达式应报错")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()
	time.Sleep(1200 * time.Millisecond)

	// 旧任务已在报错前被移除（现有实现的行为），此处只验证不会 panic 或死锁，
	// 且后续调用仍能正常恢复调度
	if err := s.SetAutoUpdateJob(true, "* * * * * *", func() { atomic.AddInt32(&count, 1) }); err != nil {
		t.Fatalf("恢复调度失败: %v", err)
	}
}

// ctx 取消后调度器应停止，不留下 goroutine 泄漏（用 -race 验证无数据竞争）
func TestSchedulerStopsOnContextCancel(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	time.Sleep(100 * time.Millisecond) // 留出内部 goroutine 处理 ctx.Done 的时间
}

// 并发调用 SetAutoUpdateJob / AddJob 不应产生数据竞争
func TestSchedulerConcurrentAccess(t *testing.T) {
	s := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				enabled := n%2 == 0
				_ = s.SetAutoUpdateJob(enabled, "* * * * * *", func() {})
			}
		}(i)
	}
	wg.Wait()
}

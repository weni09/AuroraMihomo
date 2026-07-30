package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/scheduler"
)

// 定时拉取必须真的按 Cron 触发。
//
// 用秒级表达式驱动真实调度器，观察远程来源是否被反复拉取。
// 这条覆盖的是「Cron 表达式 → 调度器 → 拉取远程」的完整链路，
// 而非只验证表达式解析。
func TestCronScheduleActuallyTriggersPull(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@t.example.com:8388#Ticked"))
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	sch := scheduler.NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 每秒触发一次
	if err := sch.SetRemotePullJob(true, "* * * * * *", func() {
		jobCtx, jobCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer jobCancel()
		if err := svc.RunScheduledPull(jobCtx); err != nil {
			t.Logf("定时拉取报错（不影响计数断言）: %v", err)
		}
	}); err != nil {
		t.Fatalf("装载定时任务失败: %v", err)
	}
	sch.Start(ctx)
	defer sch.StopAndWait(5 * time.Second)

	// 等到至少触发两次，证明是「周期性」而非「一次性」。
	// 注意调度器启用了 SkipIfStillRunning，单次拉取偏慢时
	// 触发次数会少于经过的秒数，故给足余量。
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&hits) >= 2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Fatalf("Cron 应周期性触发拉取，15 秒内实际仅 %d 次", got)
	}
}

// 关闭定时拉取后不得再触发。
func TestCronScheduleDisabledStopsPull(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@t.example.com:8388#Ticked"))
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	sch := scheduler.NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := func() {
		jobCtx, jobCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer jobCancel()
		_ = svc.RunScheduledPull(jobCtx)
	}
	if err := sch.SetRemotePullJob(true, "* * * * * *", job); err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	sch.Start(ctx)
	defer sch.StopAndWait(5 * time.Second)

	// 等到至少触发一次
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&hits) == 0 {
		time.Sleep(200 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("前置条件不成立：任务未触发过")
	}

	// 关闭后计数应停止增长
	if err := sch.SetRemotePullJob(false, "* * * * * *", job); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	// 等在途任务收尾
	time.Sleep(2 * time.Second)
	before := atomic.LoadInt32(&hits)
	time.Sleep(3 * time.Second)
	if after := atomic.LoadInt32(&hits); after != before {
		t.Errorf("关闭定时拉取后不应再触发，计数由 %d 变为 %d", before, after)
	}
}

// 替换调度表达式不应留下重复任务。
// 若旧任务未被移除，改一次 Cron 就会多一个拉取源，频率翻倍。
func TestSetRemotePullJobReplacesPrevious(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@t.example.com:8388#Ticked"))
	}))
	defer srv.Close()

	sch := scheduler.NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int32
	job := func() { atomic.AddInt32(&calls, 1) }

	// 连续装载三次，只有最后一次应生效
	for i := 0; i < 3; i++ {
		if err := sch.SetRemotePullJob(true, "* * * * * *", job); err != nil {
			t.Fatalf("第 %d 次装载失败: %v", i+1, err)
		}
	}
	sch.Start(ctx)
	defer sch.StopAndWait(5 * time.Second)

	// 观察约 3 秒。若三个任务并存，计数会接近 9；只有一个时约为 3。
	time.Sleep(3500 * time.Millisecond)
	got := atomic.LoadInt32(&calls)
	if got == 0 {
		t.Fatal("任务未触发")
	}
	if got > 6 {
		t.Errorf("重复装载应替换旧任务，3.5 秒内触发 %d 次（疑似多个任务并存）", got)
	}
}

// 非法 Cron 装载时应报错，而不是静默挂一个永不触发的任务。
func TestSetRemotePullJobRejectsInvalidCron(t *testing.T) {
	sch := scheduler.NewScheduler()
	if err := sch.SetRemotePullJob(true, "not a cron", func() {}); err == nil {
		t.Error("非法 Cron 应报错")
	}
}

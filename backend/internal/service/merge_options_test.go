package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// 保存本地配置不得触发远程拉取。
//
// 这是本次需求的核心：用户改的是自己的 mihomo 配置，
// 不该因此去打上游机场。此前每次合并都无条件重建远程层，
// 保存一次配置就是一轮网络请求。
func TestApplyLocalOnlyDoesNotHitUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#Remote"))
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	// 保存本地配置：不应产生任何上游请求
	if _, err := svc.ApplyLocalOnly(context.Background()); err != nil {
		t.Fatalf("本地合并失败: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("保存本地配置不应拉取远程，实际请求 %d 次", got)
	}
}

// 手动拉取必须真的回源。
func TestPullAndMergeHitsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#Remote"))
	}))
	defer srv.Close()

	svc, db, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	if _, err := svc.PullAndMerge(context.Background()); err != nil {
		t.Fatalf("手动拉取失败: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got == 0 {
		t.Fatal("手动拉取必须回源")
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取远程层失败: %v", err)
	}
	if !strings.Contains(merged.Content, "Remote") {
		t.Errorf("远程层应含拉取到的节点，实际:\n%s", merged.Content)
	}
}

// 保存本地配置应复用上一次拉取的远程层，而不是把它丢掉。
// 若 RefreshRemote=false 被误实现为「跳过远程层」，
// 用户保存一次本地配置就会让订阅节点全部消失。
func TestApplyLocalOnlyReusesCachedRemoteLayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#CachedNode"))
	}))
	defer srv.Close()

	svc, db, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	// 先拉一次，让远程层有内容
	if _, err := svc.PullAndMerge(context.Background()); err != nil {
		t.Fatalf("首次拉取失败: %v", err)
	}
	// 关掉上游，证明后续不依赖网络
	srv.Close()

	if _, err := svc.ApplyLocalOnly(context.Background()); err != nil {
		t.Fatalf("上游不可用时本地合并仍应成功: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取远程层失败: %v", err)
	}
	if !strings.Contains(merged.Content, "CachedNode") {
		t.Errorf("保存本地配置不应丢弃已缓存的远程层，实际:\n%s", merged.Content)
	}
}

// 未配置远程来源时，手动拉取也不该产生网络请求。
func TestPullAndMergeWithNoneSourceMakesNoRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	// 默认即 none
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}
	if _, err := svc.PullAndMerge(context.Background()); err != nil {
		t.Fatalf("none 来源下拉取应成功（无操作）: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("none 来源不应产生请求，实际 %d 次", got)
	}
}

// Cron 驱动的定时拉取：到点即拉，不再自行判断间隔。
// 「何时触发」由调度器按 Cron 决定，RunScheduledPull 只负责执行。
func TestRunScheduledPullAlwaysPullsWhenSourceSet(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#Cron"))
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	// 连续两次都应真的拉取：节流由 Cron 表达式本身表达
	for i := 1; i <= 2; i++ {
		if err := svc.RunScheduledPull(context.Background()); err != nil {
			t.Fatalf("第 %d 次定时拉取失败: %v", i, err)
		}
		if got := atomic.LoadInt32(&hits); int(got) < i {
			t.Fatalf("第 %d 次应产生请求，实际累计 %d 次", i, got)
		}
	}
}

// none 来源下定时拉取不应产生任何请求。
func TestRunScheduledPullSkipsNoneSource(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	// 默认即 none
	if err := svc.RunScheduledPull(context.Background()); err != nil {
		t.Fatalf("none 来源下应无操作: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("none 来源不应产生请求，实际 %d 次", got)
	}
}

// 各类来源都走同一条定时拉取路径。此前订阅类来源另有一条「按各订阅
// interval 逐条到期刷新」的轮询，两套调度并存导致关掉定时拉取后后台仍在
// 回源，已移除。这里确认订阅类来源同样只由 Cron 驱动。
func TestRunScheduledPullHandlesSubscriptionSource(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@y.example.com:8388#Sub"))
	}))
	defer srv.Close()

	svc, db, _ := newTestConfigService(t)
	sub := &model.Subscription{Name: "s1", URL: srv.URL, Enabled: 1}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: sub.ID}
	})
	if err := svc.UpdateBaseConfig("mode: rule\nproxies: []\n"); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	if err := svc.RunScheduledPull(context.Background()); err != nil {
		t.Fatalf("订阅类来源的定时拉取失败: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got == 0 {
		t.Error("订阅类来源应由 Cron 驱动回源，实际未产生请求")
	}
}

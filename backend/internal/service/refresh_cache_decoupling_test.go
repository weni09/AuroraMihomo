package service

import (
	"context"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// 订阅管理（substore）与配置中心是两套独立功能：订阅的节点缓存服务于
// 分享/预览链路，最终配置的生成是另一件事。这组测试锁定「刷新缓存」
// 不得依赖配置中心的远程来源设置。
//
// 此前二者耦合（走 MergeAndApply）造成两个真实故障：
//  1. 远程来源指向已删订阅时，刷新任意一条订阅都报
//     「指定作为远程来源的订阅(N)不存在或已禁用」；
//  2. 远程来源指向订阅B时，对订阅A刷新会被 onlySubID 静默过滤，
//     界面提示成功而缓存一字未写。

func newRefreshTestSubs(t *testing.T, db interface {
	CreateSubscription(*model.Subscription) error
}) (*model.Subscription, *model.Subscription) {
	t.Helper()
	subA := &model.Subscription{
		Name: "airport-A", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzc0E=@a.example.com:8388#NodeA",
		ShareToken: "tok-refresh-A",
	}
	subB := &model.Subscription{
		Name: "airport-B", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzc0I=@b.example.com:8388#NodeB",
		ShareToken: "tok-refresh-B",
	}
	if err := db.CreateSubscription(subA); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSubscription(subB); err != nil {
		t.Fatal(err)
	}
	return subA, subB
}

// TestRefreshCacheWorksWhenRemoteSourceBroken 远程来源指向不存在的订阅时，
// 刷新缓存必须照常成功——这跟远程来源没有任何关系。
func TestRefreshCacheWorksWhenRemoteSourceBroken(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	subA, _ := newRefreshTestSubs(t, db)

	// 远程来源指向一个不存在的订阅（用户实际遇到的状态）
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: 99999}
	})

	if err := svc.RefreshSubscriptionCache(context.Background(), subA.ID); err != nil {
		t.Fatalf("远程来源失效不应影响刷新订阅缓存，却报错: %v", err)
	}
	got, err := db.GetSubscription(subA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CachedNodes == "" {
		t.Error("刷新后 cached_nodes 仍为空")
	}
}

// TestRefreshCacheNotSkippedWhenRemoteSourceIsOtherSub 远程来源指向订阅B时，
// 对订阅A刷新必须真的刷到 A，不能被静默跳过。
func TestRefreshCacheNotSkippedWhenRemoteSourceIsOtherSub(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	subA, subB := newRefreshTestSubs(t, db)

	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: subB.ID}
	})

	if err := svc.RefreshSubscriptionCache(context.Background(), subA.ID); err != nil {
		t.Fatalf("刷新订阅A失败: %v", err)
	}
	gotA, err := db.GetSubscription(subA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.CachedNodes == "" {
		t.Fatal("对A刷新缓存却没写入A的cached_nodes——又被静默跳过了")
	}
	// 刷新 A 不应连带改动 B
	gotB, err := db.GetSubscription(subB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotB.CachedNodes != "" {
		t.Errorf("只刷新A，B的缓存不该被写入: %q", gotB.CachedNodes)
	}
}

// TestRefreshCacheDoesNotTouchFinalConfig 刷新缓存不应生成/改写最终配置。
// 这是解耦的核心断言：它只是 substore 的操作。
func TestRefreshCacheDoesNotTouchFinalConfig(t *testing.T) {
	svc, db, mock := newTestConfigService(t)
	subA, _ := newRefreshTestSubs(t, db)

	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceNone}
	})

	beforeReloads := mock.reloadConfigCalls
	if err := svc.RefreshSubscriptionCache(context.Background(), subA.ID); err != nil {
		t.Fatal(err)
	}
	// 不应触发内核重载
	if mock.reloadConfigCalls != beforeReloads {
		t.Errorf("刷新缓存不应重载内核，reloadConfigCalls 从 %d 变为 %d", beforeReloads, mock.reloadConfigCalls)
	}
	// 不应生成远程聚合配置
	if cfg, err := db.GetRemoteMergedConfig(); err == nil && cfg != nil && cfg.Content != "" {
		t.Errorf("刷新缓存不应写入远程聚合配置: %q", cfg.Content)
	}
}

// TestRefreshCacheReportsRealFailure 订阅本身有问题时必须如实报错，
// 不能因为解耦了就把失败也一起吞掉。
func TestRefreshCacheReportsRealFailure(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	bad := &model.Subscription{
		Name: "broken", Enabled: 1, Type: "mihomo",
		Content:    "这不是任何合法的订阅内容",
		ShareToken: "tok-broken",
	}
	if err := db.CreateSubscription(bad); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshSubscriptionCache(context.Background(), bad.ID); err == nil {
		t.Error("内容非法的订阅刷新应报错")
	}
	// 失败状态应落库，供界面展示
	got, err := db.GetSubscription(bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "error" {
		t.Errorf("刷新失败后状态应为 error，实际 %q", got.Status)
	}
}

// TestRefreshCacheOnMissingSubscription 不存在的订阅应报错而非静默成功。
func TestRefreshCacheOnMissingSubscription(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	if err := svc.RefreshSubscriptionCache(context.Background(), 123456); err == nil {
		t.Error("刷新不存在的订阅应报错")
	}
}

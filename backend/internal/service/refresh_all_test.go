package service

import (
	"context"
	"slices"
	"testing"

	"auroramihomo/backend/internal/model"
)

// 一键刷新全部订阅缓存与单条「刷新缓存」共享 RefreshSubscriptionCache：
// 逐条回源、单条失败只标记自身状态、绝不中断整体。这组测试锁定批量路径
// 的计数与失败隔离语义。

// TestRefreshAllWorks 全部订阅都合法时，Total/Success 与各订阅缓存一致。
func TestRefreshAllWorks(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	for _, c := range []string{
		"ss://YWVzLTI1Ni1nY206cGFzc0E=@a.example.com:8388#NodeA",
		"ss://YWVzLTI1Ni1nY206cGFzc0I=@b.example.com:8388#NodeB",
	} {
		if err := db.CreateSubscription(&model.Subscription{
			Name: "airport", Enabled: 1, Type: "mihomo", Content: c,
		}); err != nil {
			t.Fatal(err)
		}
	}

	res, err := svc.RefreshAllSubscriptionCaches(context.Background())
	if err != nil {
		t.Fatalf("一键刷新不应报顶层错误: %v", err)
	}
	if res.Total != 2 || res.Success != 2 || res.Failed != 0 {
		t.Errorf("全部成功时的计数应为 Total=2 Success=2 Failed=0，实际 %+v", res)
	}

	subs, err := db.GetSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range subs {
		if sub.CachedNodes == "" {
			t.Errorf("订阅 %s 刷新后 cached_nodes 仍为空", sub.Name)
		}
		if sub.Status != "ok" {
			t.Errorf("订阅 %s 刷新后状态应为 ok，实际 %q", sub.Name, sub.Status)
		}
	}
}

// TestRefreshAllPartialFailure 合法与非法订阅并存时，非法者计入失败名单
// 且状态落为 error，合法者缓存照常写入，整体不中断。
func TestRefreshAllPartialFailure(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	good := []string{
		"ss://YWVzLTI1Ni1nY206cGFzc0E=@a.example.com:8388#NodeA",
		"ss://YWVzLTI1Ni1nY206cGFzc0I=@b.example.com:8388#NodeB",
	}
	for _, c := range good {
		if err := db.CreateSubscription(&model.Subscription{
			Name: "good", Enabled: 1, Type: "mihomo", Content: c,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateSubscription(&model.Subscription{
		Name: "broken", Enabled: 1, Type: "mihomo",
		Content: "这不是任何合法的订阅内容",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.RefreshAllSubscriptionCaches(context.Background())
	if err != nil {
		t.Fatalf("单条失败不应中断整体: %v", err)
	}
	if res.Total != 3 || res.Success != 2 || res.Failed != 1 {
		t.Errorf("计数应为 Total=3 Success=2 Failed=1，实际 %+v", res)
	}
	if !slices.Contains(res.FailedNames, "broken") || len(res.FailedNames) != 1 {
		t.Errorf("失败名单应为 [broken]，实际 %v", res.FailedNames)
	}

	subs, err := db.GetSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	var goodCached, brokenStatus int
	for _, sub := range subs {
		switch sub.Name {
		case "good":
			if sub.CachedNodes == "" {
				t.Errorf("合法订阅 %s 的缓存未写入", sub.Name)
			}
			if sub.Status != "ok" {
				t.Errorf("合法订阅 %s 状态应为 ok，实际 %q", sub.Name, sub.Status)
			}
			goodCached++
		case "broken":
			if sub.Status != "error" {
				t.Errorf("非法订阅状态应为 error，实际 %q", sub.Status)
			}
			brokenStatus++
		}
	}
	if goodCached != 2 || brokenStatus != 1 {
		t.Errorf("合法缓存应写入 2 条、非法状态应落库 1 条，实际 %d/%d", goodCached, brokenStatus)
	}
}

// TestRefreshAllIncludesDisabled 一键刷新应覆盖被禁用订阅——与单条
// 「刷新缓存」行为一致，禁用只是不再参与配置合并，缓存仍应保鲜。
func TestRefreshAllIncludesDisabled(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	if err := db.CreateSubscription(&model.Subscription{
		Name: "off", Enabled: 0, Type: "mihomo",
		Content: "ss://YWVzLTI1Ni1nY206cGFzc0E=@a.example.com:8388#NodeA",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.RefreshAllSubscriptionCaches(context.Background())
	if err != nil {
		t.Fatalf("一键刷新不应报顶层错误: %v", err)
	}
	if res.Total != 1 || res.Success != 1 || res.Failed != 0 {
		t.Errorf("禁用订阅也应被刷新，计数应为 Total=1 Success=1，实际 %+v", res)
	}
	got, err := db.GetSubscription(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.CachedNodes == "" {
		t.Error("禁用订阅的缓存未被写入")
	}
}

package repository

import (
	"path/filepath"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
)

// 远程聚合层必须按 name 定位（RemoteMergedConfigName），
// 而不能按 type="remote" 取最近一条——后者会命中每条订阅的
// 原始快照 remote-<id>，退化成「只用某一条订阅」。
// 这条测试固化该约束，因为来源选择功能完全依赖它取对内容。
func TestGetRemoteMergedConfigIgnoresPerSubscriptionSnapshots(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "rm.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := int(time.Now().Unix())
	// 先写聚合结果
	if err := db.SaveConfig(&model.Config{
		Name: RemoteMergedConfigName, Type: "remote",
		Content: "proxies:\n  - name: Aggregated\n", Version: now,
	}); err != nil {
		t.Fatalf("保存聚合配置失败: %v", err)
	}
	// 再写一条更晚的单订阅快照
	if err := db.SaveConfig(&model.Config{
		Name: "remote-1", Type: "remote",
		Content: "proxies:\n  - name: SingleSnapshot\n", Version: now + 10,
	}); err != nil {
		t.Fatalf("保存快照失败: %v", err)
	}

	got, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取聚合配置失败: %v", err)
	}
	if got.Name != RemoteMergedConfigName {
		t.Errorf("应按 name 取聚合行，实际取到 %q", got.Name)
	}
	if got.Content != "proxies:\n  - name: Aggregated\n" {
		t.Errorf("聚合内容被单订阅快照覆盖了:\n%s", got.Content)
	}
}

// 分享的名称与有效期需能独立更新，且不得连带清空其它字段。
// 用 Updates+map 而非 Save 就是为了避免结构体零值把
// cached_nodes 等字段写空。
func TestUpdateShareKeepsOtherFields(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "share.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sub := &model.Subscription{
		Name: "keeper", Enabled: 1, Type: "mihomo",
		URL:         "https://example.com/sub",
		CachedNodes: `[{"name":"cached"}]`,
		UserAgent:   "clash",
		ShareToken:  "tok",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if err := db.UpdateSubscriptionShare(sub.ID, "我的分享", exp); err != nil {
		t.Fatalf("更新分享失败: %v", err)
	}

	got, err := db.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("读取订阅失败: %v", err)
	}
	if got.ShareName != "我的分享" {
		t.Errorf("分享名未保存，实际 %q", got.ShareName)
	}
	if got.ShareExpiresAt.Unix() != exp.Unix() {
		t.Errorf("有效期未保存，want %v got %v", exp, got.ShareExpiresAt)
	}
	// 关键：其它字段不得被清空
	if got.CachedNodes == "" {
		t.Error("更新分享不应清空 cached_nodes")
	}
	if got.URL == "" {
		t.Error("更新分享不应清空 url")
	}
	if got.UserAgent == "" {
		t.Error("更新分享不应清空 user_agent")
	}
	if got.ShareToken != "tok" {
		t.Errorf("更新分享不应改动 token，实际 %q", got.ShareToken)
	}
	if got.Enabled != 1 {
		t.Error("更新分享不应改动启用状态")
	}
}

// 清空有效期（传零值）应能把已设的过期时间取消掉，
// 对应界面上的「永不过期」按钮。
func TestUpdateShareCanClearExpiry(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "clear.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := &model.SubCollection{Name: "col", Enabled: 1, ShareToken: "ctok"}
	if err := db.CreateCollection(c); err != nil {
		t.Fatalf("创建组合失败: %v", err)
	}
	if err := db.UpdateCollectionShare(c.ID, "n", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("设置有效期失败: %v", err)
	}
	if err := db.UpdateCollectionShare(c.ID, "n", time.Time{}); err != nil {
		t.Fatalf("清除有效期失败: %v", err)
	}
	got, err := db.GetCollection(c.ID)
	if err != nil {
		t.Fatalf("读取组合失败: %v", err)
	}
	if !got.ShareExpiresAt.IsZero() {
		t.Errorf("有效期应被清空，实际 %v", got.ShareExpiresAt)
	}
}

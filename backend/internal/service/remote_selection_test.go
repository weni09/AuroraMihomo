package service

import (
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
)

// buildRemoteConfig 会把每条订阅的原始内容以 name="remote-<id>" 落库，
// 同时把聚合结果以 name="remote-merged" 落库，两者 type 都是 "remote"。
// 合并阶段必须取聚合结果；若按 type 取"最近一条"，就可能取到某条单独订阅，
// 导致其它订阅的节点全部从最终配置中消失。
func TestGetRemoteMergedConfigIgnoresPerSubscriptionRows(t *testing.T) {
	_, db, _ := newTestConfigService(t)

	now := time.Now()
	// 聚合结果先写入
	if err := db.SaveConfig(&model.Config{
		Name:      "remote-merged",
		Type:      "remote",
		Content:   "proxies:\n  - {name: FROM_MERGED, type: ss, server: m.com, port: 1}\n",
		Version:   1,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("写入 remote-merged 失败: %v", err)
	}
	// 单条订阅快照后写入，且时间戳更新
	if err := db.SaveConfig(&model.Config{
		Name:      "remote-7",
		Type:      "remote",
		Content:   "proxies:\n  - {name: FROM_SINGLE_SUB, type: ss, server: s.com, port: 2}\n",
		Version:   2,
		CreatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("写入 remote-7 失败: %v", err)
	}

	cfg, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("GetRemoteMergedConfig 失败: %v", err)
	}
	if cfg == nil {
		t.Fatal("应取到 remote-merged 配置")
	}
	if cfg.Name != "remote-merged" {
		t.Fatalf("应取到聚合配置 remote-merged，实际取到 %q", cfg.Name)
	}
	if !strings.Contains(cfg.Content, "FROM_MERGED") {
		t.Errorf("取到的内容不是聚合结果: %s", cfg.Content)
	}
	if strings.Contains(cfg.Content, "FROM_SINGLE_SUB") {
		t.Errorf("误取到单条订阅快照: %s", cfg.Content)
	}
}

// 同名的聚合配置写入多次时应取最新那条。
func TestGetRemoteMergedConfigReturnsLatest(t *testing.T) {
	_, db, _ := newTestConfigService(t)

	for i, content := range []string{"proxies: []\n", "proxies:\n  - {name: NEWEST, type: ss, server: n.com, port: 3}\n"} {
		if err := db.SaveConfig(&model.Config{
			Name:    "remote-merged",
			Type:    "remote",
			Content: content,
			Version: i + 1,
		}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	cfg, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("GetRemoteMergedConfig 失败: %v", err)
	}
	if !strings.Contains(cfg.Content, "NEWEST") {
		t.Errorf("应取到最新的聚合配置，实际: %s", cfg.Content)
	}
}

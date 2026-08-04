package repository

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
)

func newTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 删除订阅时必须清理其在组合中的关联项，否则留下孤儿记录，
// 导致组合渲染时按不存在的 id 查询，节点静默丢失。
func TestDeleteSubscriptionCleansCollectionItems(t *testing.T) {
	db := newTestDB(t)

	s1 := &model.Subscription{Name: "A", URL: "http://a", Enabled: 1}
	s2 := &model.Subscription{Name: "B", URL: "http://b", Enabled: 1}
	for _, s := range []*model.Subscription{s1, s2} {
		if err := db.CreateSubscription(s); err != nil {
			t.Fatalf("建订阅失败: %v", err)
		}
	}

	c := &model.SubCollection{Name: "C", Enabled: 1, ShareToken: "tok1"}
	if err := db.CreateCollection(c); err != nil {
		t.Fatalf("建组合失败: %v", err)
	}
	if err := db.ReplaceCollectionItems(c.ID, []int64{s1.ID, s2.ID}); err != nil {
		t.Fatalf("写关联失败: %v", err)
	}

	if err := db.DeleteSubscription(s1.ID); err != nil {
		t.Fatalf("删订阅失败: %v", err)
	}

	items, err := db.ListCollectionItems(c.ID)
	if err != nil {
		t.Fatalf("查关联失败: %v", err)
	}
	for _, it := range items {
		if it.SubscriptionID == s1.ID {
			t.Fatalf("订阅已删除但组合仍持有其关联项（孤儿记录）: %+v", items)
		}
	}
	if len(items) != 1 || items[0].SubscriptionID != s2.ID {
		t.Fatalf("应只剩订阅 B 的关联，实际 %+v", items)
	}
}

// 组合关联项的优先级顺序必须稳定保留
func TestReplaceCollectionItemsKeepsOrder(t *testing.T) {
	db := newTestDB(t)
	c := &model.SubCollection{Name: "C", Enabled: 1, ShareToken: "tok2"}
	if err := db.CreateCollection(c); err != nil {
		t.Fatal(err)
	}
	want := []int64{30, 10, 20}
	if err := db.ReplaceCollectionItems(c.ID, want); err != nil {
		t.Fatal(err)
	}
	items, _ := db.ListCollectionItems(c.ID)
	if len(items) != 3 {
		t.Fatalf("期望 3 条关联，实际 %d", len(items))
	}
	for i, it := range items {
		if it.SubscriptionID != want[i] {
			t.Fatalf("第 %d 项顺序错误：期望 %d，实际 %d（全部：%+v）", i, want[i], it.SubscriptionID, items)
		}
	}
}

// 文件按 ID 更新时改名撞车必须报错，不能覆盖另一条记录
func TestSaveFileRenameConflict(t *testing.T) {
	db := newTestDB(t)
	a := &model.SubFile{Name: "a.yaml", Content: "AAA"}
	b := &model.SubFile{Name: "b.yaml", Content: "BBB"}
	for _, f := range []*model.SubFile{a, b} {
		if err := db.SaveFile(f); err != nil {
			t.Fatal(err)
		}
	}

	a.Name = "b.yaml" // 改名撞上 b
	if err := db.SaveFile(a); err == nil {
		t.Fatal("改名撞车应报错，实际成功了")
	}

	got, err := db.GetFileByName("b.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "BBB" {
		t.Fatalf("文件 B 的内容被污染：%q", got.Content)
	}
}

// 组合删除后其关联项也应清空
func TestDeleteCollectionCascade(t *testing.T) {
	db := newTestDB(t)
	c := &model.SubCollection{Name: "C", Enabled: 1, ShareToken: "tok3"}
	if err := db.CreateCollection(c); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceCollectionItems(c.ID, []int64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCollection(c.ID); err != nil {
		t.Fatal(err)
	}
	items, _ := db.ListCollectionItems(c.ID)
	if len(items) != 0 {
		t.Fatalf("组合已删除但关联项残留 %d 条", len(items))
	}
}

// 设置项的写入应为覆盖而非重复插入
func TestSettingUpsert(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetSetting("k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("k", "v2"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSetting("k")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v2" {
		t.Fatalf("设置项未被覆盖，实际 %q", got)
	}
}

// 配置快照与版本记录必须有保留上限，否则每分钟一次合并会撑爆磁盘
func TestConfigRetention(t *testing.T) {
	db := newTestDB(t)

	for i := 0; i < maxConfigsPerName+8; i++ {
		if err := db.SaveConfig(&model.Config{Name: "merged", Type: "merged", Content: "x", Version: i}); err != nil {
			t.Fatal(err)
		}
	}
	var cnt int64
	db.DB.Model(&model.Config{}).Where("name = ?", "merged").Count(&cnt)
	if cnt > int64(maxConfigsPerName) {
		t.Fatalf("同名配置未被裁剪，剩余 %d 条（上限 %d）", cnt, maxConfigsPerName)
	}

	// 不同 name 之间互不影响
	if err := db.SaveConfig(&model.Config{Name: "base", Type: "base", Content: "y"}); err != nil {
		t.Fatal(err)
	}
	db.DB.Model(&model.Config{}).Where("name = ?", "base").Count(&cnt)
	if cnt != 1 {
		t.Fatalf("其它名称的配置被误删，剩余 %d 条", cnt)
	}

	for i := 0; i < maxConfigVersions+5; i++ {
		if err := db.SaveConfigVersion(&model.ConfigVersion{Hash: "h", Content: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	db.DB.Model(&model.ConfigVersion{}).Count(&cnt)
	if cnt > int64(maxConfigVersions) {
		t.Fatalf("版本记录未被裁剪，剩余 %d 条（上限 %d）", cnt, maxConfigVersions)
	}
}

// WAL / busy_timeout 必须逐个补齐，而不是"DSN 里已有 ? 就整段跳过"。
// 否则用户一旦自带任何查询参数，WAL 与 busy_timeout 静默失效，
// 定时任务与请求争锁时会直接报 database is locked。
func TestEnsureSQLiteParams(t *testing.T) {
	// 与生产代码一致：modernc 驱动用 _pragma=name(value) 形式，
	// 且 _pragma 会重复出现多次
	want := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_time_format=sqlite",
	}

	cases := []struct {
		name        string
		in          string
		mustHave    []string
		mustNotHave []string
	}{
		{
			name: "无查询参数时补齐全部",
			in:   "./data/aurora.db",
			// 多个 _pragma 必须都在：按 key 去重的实现只会补上第一个，
			// 那样 busy_timeout 静默失效
			mustHave: []string{
				"_pragma=journal_mode(WAL)",
				"_pragma=busy_timeout(5000)",
				"_time_format=sqlite",
			},
		},
		{
			name: "已有其他参数时追加而非跳过",
			in:   "./data/aurora.db?cache=shared",
			mustHave: []string{
				"cache=shared",
				"_pragma=journal_mode(WAL)",
				"_pragma=busy_timeout(5000)",
			},
		},
		{
			name: "用户显式指定的 pragma 不被覆盖",
			in:   "file:x.db?_pragma=journal_mode(DELETE)",
			mustHave: []string{
				"_pragma=journal_mode(DELETE)",
				// 同为 _pragma 但名字不同，仍应补上
				"_pragma=busy_timeout(5000)",
			},
			mustNotHave: []string{"_pragma=journal_mode(WAL)"},
		},
		{
			name:        "用户指定的普通参数不被覆盖",
			in:          "file:x.db?_time_format=datetime",
			mustHave:    []string{"_time_format=datetime"},
			mustNotHave: []string{"_time_format=sqlite"},
		},
	}

	for _, c := range cases {
		got := ensureSQLiteParams(c.in, want)
		for _, sub := range c.mustHave {
			if !strings.Contains(got, sub) {
				t.Errorf("[%s] 结果应包含 %q，实际 %q", c.name, sub, got)
			}
		}
		for _, sub := range c.mustNotHave {
			if strings.Contains(got, sub) {
				t.Errorf("[%s] 结果不应包含 %q，实际 %q", c.name, sub, got)
			}
		}
	}
}

// UpdateSubscription 不得用全字段 Save：后台刷新会并发写 cached_nodes
// 与流量统计，全字段覆盖会把它们回滚成读取时的旧值。
func TestUpdateSubscriptionDoesNotClobberBackgroundFields(t *testing.T) {
	db := newTestDB(t)

	sub := &model.Subscription{Name: "orig", URL: "http://a", Enabled: 1}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}

	// 用户读取记录（模拟进入编辑界面）
	loaded, err := db.GetSubscription(sub.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 期间后台刷新落库了流量信息与节点缓存
	if err := db.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"upload":   1111,
			"download": 2222,
			"total":    3333,
		}).Error; err != nil {
		t.Fatal(err)
	}

	// 用户提交改名
	loaded.Name = "renamed"
	if err := db.UpdateSubscription(loaded); err != nil {
		t.Fatal(err)
	}

	after, err := db.GetSubscription(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "renamed" {
		t.Errorf("用户的改名应生效，实际 %q", after.Name)
	}
	if after.Upload != 1111 || after.Download != 2222 || after.Total != 3333 {
		t.Errorf("后台写入的流量统计不应被用户编辑覆盖，实际 up=%d down=%d total=%d",
			after.Upload, after.Download, after.Total)
	}
}

// MarkTaskRun 对不存在的任务名应建账本行（settings 驱动的 auto_update 等）
func TestMarkTaskRunCreatesLedgerWhenMissing(t *testing.T) {
	db := newTestDB(t)
	if err := db.MarkTaskRun("auto_update", "ok", "done", time.Time{}); err != nil {
		t.Fatalf("MarkTaskRun: %v", err)
	}
	rows, err := db.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	var found *model.Task
	for i := range rows {
		if rows[i].Name == "auto_update" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatal("应补建 auto_update 账本行")
	}
	if found.Status != "ok" || found.LastRun.IsZero() {
		t.Fatalf("账本字段异常: %+v", found)
	}
	// 二次写入应更新而非再建
	if err := db.MarkTaskRun("auto_update", "error", "fail", time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows, _ = db.ListTasks()
	n := 0
	for _, r := range rows {
		if r.Name == "auto_update" {
			n++
			if r.Status != "error" || r.Message != "fail" {
				t.Fatalf("二次写入未覆盖: %+v", r)
			}
		}
	}
	if n != 1 {
		t.Fatalf("应只有 1 条 auto_update，实际 %d", n)
	}
}

// PromoteTaskLedger 把旧 version_check 的 LastRun 迁到 auto_update
func TestPromoteTaskLedgerFromVersionCheck(t *testing.T) {
	db := newTestDB(t)
	old := time.Date(2026, 8, 4, 4, 0, 11, 0, time.Local)
	if err := db.DB.Create(&model.Task{
		Name: "version_check", Enabled: 0, LastRun: old, Status: "ok",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.PromoteTaskLedger("version_check", "auto_update"); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.ListTasks()
	var au *model.Task
	for i := range rows {
		if rows[i].Name == "auto_update" {
			au = &rows[i]
		}
	}
	if au == nil {
		t.Fatal("应创建 auto_update 账本")
	}
	if !au.LastRun.Equal(old) {
		t.Fatalf("LastRun 未迁移: got %v want %v", au.LastRun, old)
	}
	// to 已有更新记录时不覆盖
	newer := old.Add(time.Hour)
	_ = db.DB.Model(au).Updates(map[string]interface{}{"last_run": newer, "status": "ok"})
	if err := db.PromoteTaskLedger("version_check", "auto_update"); err != nil {
		t.Fatal(err)
	}
	rows, _ = db.ListTasks()
	for _, r := range rows {
		if r.Name == "auto_update" && !r.LastRun.Equal(newer) {
			t.Fatalf("不应被旧 version_check 覆盖: %v", r.LastRun)
		}
	}
}

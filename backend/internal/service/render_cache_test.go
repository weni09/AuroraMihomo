package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/substore"
)

func newTestRenderService(t *testing.T) (*RenderService, *repository.Database) {
	t.Helper()
	db, err := repository.NewDatabase(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRenderService(db, substore.NewEngine()), db
}

// 分享端点无需鉴权，重复请求应命中缓存而不是每次都重跑管道
func TestRenderCacheHitsOnRepeatedRequest(t *testing.T) {
	svc, _ := newTestRenderService(t)
	calls := 0
	fn := func() (*ShareResult, error) {
		calls++
		return &ShareResult{Body: "x", Target: "clash"}, nil
	}

	for i := 0; i < 5; i++ {
		if _, err := svc.cachedRender(context.Background(), "k1", fn); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("重复请求应只实际渲染一次，实际 %d 次", calls)
	}
}

// 缓存键不同（如 target/filter 不同）必须分别渲染，不能串味
func TestRenderCacheSeparatesDifferentKeys(t *testing.T) {
	svc, _ := newTestRenderService(t)
	mk := func(body string) func() (*ShareResult, error) {
		return func() (*ShareResult, error) { return &ShareResult{Body: body}, nil }
	}
	a, _ := svc.cachedRender(context.Background(), "tok|clash|", mk("A"))
	b, _ := svc.cachedRender(context.Background(), "tok|surge|", mk("B"))
	if a.Body != "A" || b.Body != "B" {
		t.Fatalf("不同参数应返回各自结果，实际 a=%q b=%q", a.Body, b.Body)
	}
}

// 订阅被修改后，缓存键中的数据版本应变化，从而返回最新内容
func TestRenderCacheInvalidatesOnDataChange(t *testing.T) {
	svc, db := newTestRenderService(t)

	sub := &model.Subscription{Name: "s", ShareToken: "tokx", Enabled: 1,
		Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#N1\n"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}

	v1 := svc.dataVersion("tokx")
	if v1 == "" {
		t.Fatal("应能取到订阅的数据版本")
	}

	// 修改订阅（updated_at 变化）
	time.Sleep(5 * time.Millisecond)
	if err := db.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Update("updated_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}

	v2 := svc.dataVersion("tokx")
	if v1 == v2 {
		t.Fatalf("订阅修改后数据版本应变化，两次均为 %q", v1)
	}
}

// 文件直链的缓存键必须跟随来源组合的变更：组合的管道/成员变了，
// 文件渲染内容会变，但文件自身 updated_at 不变——若键只含文件时间戳，
// 直链会一直返回陈旧内容。与 RenderByToken 的 dataVersion 同一模式。
func TestFileDataVersionTracksSourceCollectionChange(t *testing.T) {
	svc, db := newTestRenderService(t)

	sub := &model.Subscription{Name: "s", Enabled: 1,
		Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#N1\n"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	coll := &model.SubCollection{Name: "c", Enabled: 1}
	if err := db.CreateCollection(coll); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceCollectionItems(coll.ID, []int64{sub.ID}); err != nil {
		t.Fatal(err)
	}
	f := &model.SubFile{
		Name:         "tpl",
		ConfigType:   model.FileConfigTypeMihomo,
		SourceType:   model.SourceTypeCollection,
		SourceID:     coll.ID,
		TemplateLang: model.TemplateLangGo,
		Content:      "proxies:\n{{ range .Nodes }}  - name: \"{{ .Name }}\"\n    server: {{ .Server }}\n{{ end }}",
	}
	if err := db.SaveFile(f); err != nil {
		t.Fatal(err)
	}

	v1 := svc.fileDataVersion(f)
	if v1 == "" {
		t.Fatal("应能取到文件的数据版本")
	}

	// 组合变更（如加 flag 算子）→ 文件缓存键必须变化
	time.Sleep(5 * time.Millisecond)
	if err := db.DB.Model(&model.SubCollection{}).Where("id = ?", coll.ID).
		Update("updated_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}

	v2 := svc.fileDataVersion(f)
	if v1 == v2 {
		t.Fatalf("来源组合变更后文件数据版本应变化，两次均为 %q", v1)
	}
}

// 并发请求下闸门不应死锁，且结果都能正常返回
func TestRenderCacheConcurrentNoDeadlock(t *testing.T) {
	svc, _ := newTestRenderService(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "k" + string(rune('a'+n%4))
			_, err := svc.cachedRender(context.Background(), key, func() (*ShareResult, error) {
				time.Sleep(2 * time.Millisecond)
				return &ShareResult{Body: "ok"}, nil
			})
			if err != nil {
				t.Errorf("并发渲染不应报错: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

// 渲染失败不应被缓存，否则一次偶发失败会被固化 30 秒
func TestRenderCacheDoesNotCacheErrors(t *testing.T) {
	svc, _ := newTestRenderService(t)
	calls := 0
	fn := func() (*ShareResult, error) {
		calls++
		return nil, context.DeadlineExceeded
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.cachedRender(context.Background(), "kerr", fn); err == nil {
			t.Fatal("应返回错误")
		}
	}
	if calls != 3 {
		t.Fatalf("失败结果不应被缓存，应每次重试，实际只调用 %d 次", calls)
	}
}

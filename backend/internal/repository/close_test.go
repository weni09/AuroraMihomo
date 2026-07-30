package repository

import (
	"path/filepath"
	"strings"
	"testing"
)

// Close 之后任何查询都会返回 "sql: database is closed"。
// 这条测试固化该行为，作为下面两条测试的前提说明：
// 一旦进程在关库后还继续服务请求，用户看到的就是这个错误。
func TestQueryAfterCloseReturnsDatabaseClosed(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	_, err = db.GetSubscriptions()
	if err == nil {
		t.Fatal("关库后查询应报错")
	}
	if !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("期望 database is closed，实际: %v", err)
	}
}

// Close 必须幂等：关停路径可能因超时兜底等原因被走到两次，
// 第二次不应 panic 或报错，否则会掩盖真正的关停失败原因。
func TestCloseIsIdempotent(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "idem.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("首次关闭应成功: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("重复关闭应无错误，实际: %v", err)
	}
}

// nil 接收者与零值不应 panic：关停路径在初始化失败的分支上
// 也可能拿到未完全构造的 Database。
func TestCloseOnNilIsSafe(t *testing.T) {
	var db *Database
	if err := db.Close(); err != nil {
		t.Fatalf("nil 上调用 Close 应返回 nil，实际: %v", err)
	}
	empty := &Database{}
	if err := empty.Close(); err != nil {
		t.Fatalf("零值 Database 上调用 Close 应返回 nil，实际: %v", err)
	}
}

// Healthy 用于健康检查判定数据库可用性，关库后必须返回 false，
// 这样编排系统能把实例摘除，而不是让请求持续撞上 database is closed。
func TestHealthyReflectsClosedState(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "healthy.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if !db.Healthy() {
		t.Fatal("正常打开的数据库应为健康")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if db.Healthy() {
		t.Fatal("关闭后应判定为不健康")
	}

	var nilDB *Database
	if nilDB.Healthy() {
		t.Fatal("nil Database 应判定为不健康")
	}
}

// Closed 让关停期间仍在运行的长任务能在写库前停手。
//
// 背景：合并流程会先把 config.yaml 落盘再写数据库，且多处用 `_ =` 丢弃
// 写入错误。连接池关闭后这些写入静默失败，留下「磁盘是新配置、数据库
// 记录是旧的」的不一致。有了这个查询，调用方能提前发现并留下日志。
func TestClosedReflectsPoolState(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if db.Closed() {
		t.Error("刚初始化时不应报告已关闭")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if !db.Closed() {
		t.Error("Close 后必须报告已关闭，否则调用方会继续发注定失败的写入")
	}
	// 重复关闭后仍然是已关闭
	_ = db.Close()
	if !db.Closed() {
		t.Error("重复 Close 后仍应报告已关闭")
	}
}

// nil 接收者视为已关闭：调用方常在关停末期才拿到 Database，
// 这里返回 true 比 panic 更安全。
func TestClosedOnNilReceiver(t *testing.T) {
	var db *Database
	if !db.Closed() {
		t.Error("nil 的 Database 应报告已关闭")
	}
}

// Closed 与 Healthy 分工不同：前者只读本进程状态、不产生 IO，
// 可在热路径频繁调用；后者发 Ping 探测真实连通性。
func TestClosedAndHealthyAgreeAfterClose(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "agree.db"))
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if !db.Healthy() || db.Closed() {
		t.Fatalf("初始状态应为健康且未关闭，实际 healthy=%v closed=%v", db.Healthy(), db.Closed())
	}
	_ = db.Close()
	if db.Healthy() || !db.Closed() {
		t.Errorf("关闭后应为不健康且已关闭，实际 healthy=%v closed=%v", db.Healthy(), db.Closed())
	}
}

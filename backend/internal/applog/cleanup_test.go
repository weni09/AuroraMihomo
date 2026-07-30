package applog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 造一个归档文件并把修改时间设成 n 天前
func makeArchive(t *testing.T, path string, ageDays int, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
	mt := time.Now().AddDate(0, 0, -ageDays)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("设置时间失败: %v", err)
	}
}

func TestCleanupRemovesExpiredArchives(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")

	// 当前文件 + 3 个归档，其中 2 个已过期
	if err := os.WriteFile(logPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeArchive(t, logPath+".20260701-100000", 10, 1000)
	makeArchive(t, logPath+".20260705-100000", 8, 2000)
	makeArchive(t, logPath+".20260728-100000", 1, 3000)

	res, err := CleanupArchives(logPath, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Removed != 2 {
		t.Errorf("应删除 2 个过期归档，实际 %d", res.Removed)
	}
	if res.Bytes != 3000 {
		t.Errorf("应释放 3000 字节，实际 %d", res.Bytes)
	}

	// 未过期的归档必须留下
	if _, err := os.Stat(logPath + ".20260728-100000"); err != nil {
		t.Errorf("未过期归档被误删: %v", err)
	}
}

// 当前正在写的文件绝不能被按时间删掉——它由大小轮转机制管理，
// 一个长期运行且日志稀少的实例，其 aurora.log 的 mtime 可能很旧。
func TestCleanupNeverRemovesCurrentFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")

	// 当前文件的修改时间设成很久以前
	if err := os.WriteFile(logPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -100)
	if err := os.Chtimes(logPath, old, old); err != nil {
		t.Fatal(err)
	}

	res, err := CleanupArchives(logPath, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("不应删除任何文件，实际 %d", res.Removed)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatal("当前日志文件被删除了，这会让正在运行的实例丢失日志")
	}
}

func TestCleanupWithNoArchives(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")
	res, err := CleanupArchives(logPath, 7)
	if err != nil {
		t.Errorf("无归档时不应报错: %v", err)
	}
	if res.Removed != 0 {
		t.Errorf("无归档时不应删除任何东西，实际 %d", res.Removed)
	}
}

func TestCleanupEmptyPathIsNoop(t *testing.T) {
	res, err := CleanupArchives("", 7)
	if err != nil || res.Removed != 0 {
		t.Errorf("空路径应为空操作，实际 err=%v removed=%d", err, res.Removed)
	}
}

// 保留天数被夹到下限，避免传 0 或负数导致把所有归档删光
func TestCleanupClampsDaysToMinimum(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")
	// 刚刚归档的文件（几秒前）
	makeArchive(t, logPath+".20260730-120000", 0, 100)

	res, err := CleanupArchives(logPath, 0)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Removed != 0 {
		t.Error("days=0 应被夹到 1 天，刚归档的文件不该被删")
	}
}

func TestCleanupSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")
	// 一个命中通配符的目录
	sub := logPath + ".subdir"
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(sub, old, old)

	res, err := CleanupArchives(logPath, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Removed != 0 {
		t.Error("目录不应被当成归档删除")
	}
	if _, err := os.Stat(sub); err != nil {
		t.Errorf("目录被删了: %v", err)
	}
}

func TestSetRetentionDaysClamps(t *testing.T) {
	// 保存原值，避免影响其它测试（这是包级状态）
	orig := RetentionDays()
	defer SetRetentionDays(orig)

	cases := []struct{ in, want int }{
		{7, 7},
		{0, MinRetentionDays},
		{-5, MinRetentionDays},
		{9999, MaxRetentionDays},
		{365, 365},
		{1, 1},
	}
	for _, c := range cases {
		got := SetRetentionDays(c.in)
		if got != c.want {
			t.Errorf("SetRetentionDays(%d) 应为 %d，实际 %d", c.in, c.want, got)
		}
		if RetentionDays() != c.want {
			t.Errorf("RetentionDays() 应为 %d，实际 %d", c.want, RetentionDays())
		}
	}
}

func TestDefaultRetentionIsSevenDays(t *testing.T) {
	if DefaultRetentionDays != 7 {
		t.Errorf("默认保留天数应为 7，实际 %d", DefaultRetentionDays)
	}
}

// 清理不得因单个文件删除失败而中止：某个文件被占用时，
// 其余过期文件仍应被清掉。
func TestCleanupContinuesAfterSingleFailure(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "aurora.log")

	// 一个过期目录（Remove 对非空目录会失败）夹在两个过期文件之间
	blocker := logPath + ".20260702-000000"
	if err := os.Mkdir(blocker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -30)
	_ = os.Chtimes(blocker, old, old)

	makeArchive(t, logPath+".20260701-000000", 30, 500)
	makeArchive(t, logPath+".20260703-000000", 30, 700)

	res, err := CleanupArchives(logPath, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Removed != 2 {
		t.Errorf("应删除 2 个可删文件，实际 %d", res.Removed)
	}
	if res.Bytes != 1200 {
		t.Errorf("应释放 1200 字节，实际 %d", res.Bytes)
	}
}

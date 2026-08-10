package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/model"
)

// TestBackupTo 验证备份文件可被当作独立数据库打开且数据完整。
func TestBackupToCreatesRestorableBackup(t *testing.T) {
	db := newTestDB(t)

	if err := db.CreateSubscription(&model.Subscription{Name: "A", URL: "http://a", Enabled: 1}); err != nil {
		t.Fatalf("建订阅失败: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "backups")
	path, err := db.BackupTo(dir, 0)
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("备份文件不存在: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(path), backupPrefix) || !strings.HasSuffix(path, ".db") {
		t.Fatalf("备份文件名不符合约定: %s", filepath.Base(path))
	}

	// 备份是独立数据库：用 NewDatabase 打开验证数据与结构完整。
	restored, err := NewDatabase(path)
	if err != nil {
		t.Fatalf("打开备份失败（备份不可恢复）: %v", err)
	}
	t.Cleanup(func() { _ = restored.Close() })

	subs, err := restored.GetSubscriptions()
	if err != nil {
		t.Fatalf("从备份读订阅失败: %v", err)
	}
	if len(subs) != 1 || subs[0].Name != "A" {
		t.Fatalf("备份数据与源不一致: %+v", subs)
	}
}

// TestBackupToKeepsOnlyMaxKeep 验证保留策略：超过 maxKeep 的旧备份被清理。
func TestBackupToPrunesOldBackups(t *testing.T) {
	db := newTestDB(t)
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建备份目录失败: %v", err)
	}

	// 预置 5 份旧备份（文件名按时间排序）。
	old := []string{"aurora-20260101-000000.db", "aurora-20260102-000000.db",
		"aurora-20260103-000000.db", "aurora-20260104-000000.db", "aurora-20260105-000000.db"}
	for _, n := range old {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("写旧备份失败: %v", err)
		}
	}
	// 预置一个非备份命名文件，不应被清理。
	other := filepath.Join(dir, "keepme.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatalf("写普通文件失败: %v", err)
	}

	// maxKeep=3：本次备份 + 最新的 2 份旧备份保留，其余 3 份删除。
	if _, err := db.BackupTo(dir, 3); err != nil {
		t.Fatalf("备份失败: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	var dbs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), backupPrefix) && strings.HasSuffix(e.Name(), ".db") {
			dbs = append(dbs, e.Name())
		}
	}
	if len(dbs) != 3 {
		t.Fatalf("应保留 3 份备份，实际 %d: %v", len(dbs), dbs)
	}
	for _, n := range dbs {
		if n == "aurora-20260101-000000.db" || n == "aurora-20260102-000000.db" || n == "aurora-20260103-000000.db" {
			t.Fatalf("最旧的备份未被清理: %s", n)
		}
	}
	// 非备份文件不受影响。
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("非备份文件被误删")
	}
}

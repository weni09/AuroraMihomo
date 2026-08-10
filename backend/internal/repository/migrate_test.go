package repository

import (
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// migrateTestDB 新建一个空白测试库并返回其 gorm 句柄。
func migrateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.DB
}

// 版本化迁移的两条合成迁移：验证按序执行、事务提交与版本前进。
func TestRunMigrationsAppliesInOrderAndIdempotent(t *testing.T) {
	db := migrateTestDB(t)

	list := []migration{
		{1, "add-col-a", func(tx *gorm.DB) error {
			return tx.Exec("ALTER TABLE subscriptions ADD COLUMN upgrade_mig_a TEXT").Error
		}},
		{2, "add-col-b", func(tx *gorm.DB) error {
			return tx.Exec("ALTER TABLE subscriptions ADD COLUMN upgrade_mig_b TEXT").Error
		}},
	}

	if err := runMigrations(db, list); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	v, err := readSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if v != 2 {
		t.Fatalf("迁移后版本应为 2，实际 %d", v)
	}

	// 幂等：再次执行同一组迁移不应报错、不应推进版本。
	if err := runMigrations(db, list); err != nil {
		t.Fatalf("重复迁移不应失败: %v", err)
	}
	v, _ = readSchemaVersion(db)
	if v != 2 {
		t.Fatalf("重复迁移后版本应保持 2，实际 %d", v)
	}

	// 验证列真实存在（迁移确实生效，不只是版本号前进）。
	var hasCol int
	if err := db.Raw("SELECT count(*) FROM pragma_table_info('subscriptions') WHERE name = 'upgrade_mig_a'").Scan(&hasCol).Error; err != nil {
		t.Fatalf("查询列失败: %v", err)
	}
	if hasCol != 1 {
		t.Fatalf("upgrade_mig_a 列应存在")
	}
}

// 迁移失败时该步的事务整体回滚：结构变更与版本号都不应留下。
func TestRunMigrationsRollsBackOnFailure(t *testing.T) {
	db := migrateTestDB(t)

	list := []migration{
		{1, "add-col-ok", func(tx *gorm.DB) error {
			return tx.Exec("ALTER TABLE subscriptions ADD COLUMN upgrade_mig_ok TEXT").Error
		}},
		{2, "boom", func(tx *gorm.DB) error {
			return tx.Exec("ALTER TABLE no_such_table ADD COLUMN x TEXT").Error
		}},
	}

	err := runMigrations(db, list)
	if err == nil {
		t.Fatalf("含失败步骤的迁移应当报错")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误信息应指明失败迁移: %v", err)
	}

	v, _ := readSchemaVersion(db)
	if v != 1 {
		t.Fatalf("失败步骤回滚后版本应停在 1，实际 %d", v)
	}

	// 第 1 步已提交的列应保留。
	var hasOK int
	if err := db.Raw("SELECT count(*) FROM pragma_table_info('subscriptions') WHERE name = 'upgrade_mig_ok'").Scan(&hasOK).Error; err != nil {
		t.Fatalf("查询列失败: %v", err)
	}
	if hasOK != 1 {
		t.Fatalf("第 1 步的列应保留")
	}
	// 第 2 步的失败不应留下任何列。
	var hasBoom int
	if err := db.Raw("SELECT count(*) FROM pragma_table_info('subscriptions') WHERE name = 'upgrade_mig_boom'").Scan(&hasBoom).Error; err != nil {
		t.Fatalf("查询列失败: %v", err)
	}
	if hasBoom != 0 {
		t.Fatalf("失败步骤的变更应被回滚")
	}
}

// 数据库版本高于当前二进制支持的版本时，NewDatabase 必须拒绝启动。
func TestNewDatabaseRejectsNewerSchema(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDatabase(dsn)
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	// 模拟数据库被更新版本的二进制迁移到 99。
	if err := writeSchemaVersion(db.DB, 99); err != nil {
		t.Fatalf("写入测试版本失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if _, err := NewDatabase(dsn); err == nil {
		t.Fatalf("旧二进制加载新版本数据库应当报错")
	}
}

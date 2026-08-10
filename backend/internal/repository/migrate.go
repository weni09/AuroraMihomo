package repository

import (
	"fmt"

	"gorm.io/gorm"
)

// migration 描述一个数据库 Schema 版本迁移步骤。
//
// version 是到达该步骤后的 Schema 版本号（严格递增，从 1 开始）；
// up 在事务内执行该版本的实际结构变更或数据转换。SQLite 的
// PRAGMA user_version 参与事务，因此 up 内写入的 user_version
// 会随同步骤一起提交或回滚，不会出现"变更回滚了、版本号却前进"的假象。
type migration struct {
	version int
	name    string
	up      func(tx *gorm.DB) error
}

// migrations 按版本升序排列的全部 Schema 迁移。
//
// 新增迁移的固定流程：
//  1. 在表尾追加一个 version = 上一个条目 + 1 的条目；
//  2. 变更 backend/internal/model 里的模型定义；
//  3. 若变更让旧结构无法由 AutoMigrate 兜底（删列/改型/数据转换），
//     在 up 里完成，并更新既有数据；
//  4. 为新迁移补测试（幂等 + 失败回滚）。
//
// 当前为空表：建表全部由 AutoMigrate 管理，迁移体系是渐进引入的，
// 空表时 migrateSchema 是纯空操作，行为与引入前完全一致。
//
// 示例（下一次破坏性迁移怎么加，勿直接取消注释提交）：
//
//	var migrations = []migration{
//		{
//			version: 1,
//			name:    "drop-legacy-sub-rules",
//			up: func(tx *gorm.DB) error {
//				// 仅示例：真实迁移请同步改 models.go，并补 migrate_test。
//				return tx.Exec("DROP TABLE IF EXISTS sub_rules").Error
//			},
//		},
//	}
var migrations = []migration{}

// migrateSchema 执行数据库 Schema 版本迁移。在 NewDatabase 中位于
// AutoMigrate 之前调用：AutoMigrate 只会增量加列，破坏性变更
// （删列/改型/数据转换）只能由这里完成，迁移后的结构再交给
// AutoMigrate 兜底对齐新增字段。
func migrateSchema(db *gorm.DB) error {
	return runMigrations(db, migrations)
}

// runMigrations 把 list 中尚未应用的迁移依序执行，每步一个事务。
//
// 抽出 list 参数便于测试注入合成迁移表（包级 migrations 为空时
// 也能验证迁移机制本身）。
func runMigrations(db *gorm.DB, list []migration) error {
	current, err := readSchemaVersion(db)
	if err != nil {
		return err
	}

	latest := 0
	if len(list) > 0 {
		latest = list[len(list)-1].version
	}

	// 护栏：数据库版本高于当前二进制内置版本，说明库被更新版本的
	// 二进制创建/迁移过。旧二进制继续读写可能损坏新结构，直接报错
	// 让升级方换回新二进制，而不是带病运行。
	if current > latest {
		return fmt.Errorf("数据库 Schema 版本(%d)高于当前二进制支持的版本(%d)，请使用更新的 AuroraMihomo 二进制启动", current, latest)
	}

	for _, m := range list {
		if m.version <= current {
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := m.up(tx); err != nil {
				return err
			}
			return writeSchemaVersion(tx, m.version)
		}); err != nil {
			// 错误信息以中文开头，规避 staticcheck ST1005 对 ASCII 大写的检查
			return fmt.Errorf("迁移 %s(%d) 失败: %w", m.name, m.version, err)
		}
	}
	return nil
}

// readSchemaVersion 读取数据库当前的 Schema 版本。
func readSchemaVersion(db *gorm.DB) (int, error) {
	var v int
	if err := db.Raw("PRAGMA user_version").Scan(&v).Error; err != nil {
		return 0, fmt.Errorf("读取 Schema 版本失败: %w", err)
	}
	return v, nil
}

// writeSchemaVersion 在事务内写入 Schema 版本号。
//
// PRAGMA user_version 的赋值不支持参数占位符（modernc 驱动下
// "PRAGMA user_version = ?" 会报错），版本号来自本包代码里的 int，
// 用 Sprintf 构造字面量没有注入面。
func writeSchemaVersion(tx *gorm.DB, version int) error {
	if err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)).Error; err != nil {
		return fmt.Errorf("写入 Schema 版本(%d)失败: %w", version, err)
	}
	return nil
}

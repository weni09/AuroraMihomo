package repository

// SQLite 驱动用 github.com/libtnb/sqlite（gorm dialector）→
// modernc.org/sqlite（把 SQLite 的 C 代码转译成 Go 的纯 Go 实现）。
//
// 选它而不是官方 gorm.io/driver/sqlite 的原因：后者底层是
// mattn/go-sqlite3，必须开 CGO。关掉 CGO 虽然能编译通过，但运行时会直接
// 报 "requires cgo to work. This is a stub"。而 CGO 意味着交叉编译要为
// 每个目标平台准备 C 工具链（Alpine 还要区分 musl），本项目要覆盖
// linux/darwin/windows × amd64/arm64，代价很高。
// 换成纯 Go 后 CGO_ENABLED=0 即可全平台交叉编译，产物是静态二进制，
// Alpine 与 Debian/Ubuntu 通用。
//
// 维护注意：modernc.org/libc 必须与 modernc.org/sqlite 的 go.mod 里声明的
// 版本严格一致（上游明确要求，见其 issue #177）。单独 go get -u 升 libc
// 会导致运行期异常，升级 sqlite 时要一起对齐两者。

import (
	"auroramihomo/backend/internal/model"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	sqlite "github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	DB *gorm.DB

	// closed 标记连接池是否已关闭。用 atomic 而非互斥锁：
	// 关停期间的长任务会在每次写库前查询它，必须无锁且廉价。
	closed atomic.Bool
}

func NewDatabase(dsn string) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil && filepath.Dir(dsn) != "." {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// WAL 提升读写并发，busy_timeout 避免定时任务与请求争锁时
	// 直接抛出 "database is locked"。
	// 逐个补齐缺失参数：此前是"dsn 里只要已有 ? 就整段跳过"，
	// 用户一旦自带任何查询参数，WAL 与 busy_timeout 会静默失效。
	//
	// 参数写法跟随 modernc.org/sqlite（纯 Go 驱动，不需要 CGO）：
	//   - 用 `_pragma=name(value)` 而非 mattn 的 `_journal_mode` 等简写键。
	//     简写键在 modernc v1.55.0 才被支持，更早的版本会静默忽略，
	//     用 _pragma 形式则各版本行为一致。
	//   - `_time_format=sqlite` 让驱动按 mattn 的时间格式写入，
	//     保证与既有数据的字符串形态一致（gorm 层实际用 RFC3339，
	//     但直接走 db.Exec 写 time.Time 时由驱动格式化）。
	//   - 不设 `_timezone`：它会把时间转成目标时区再格式化，
	//     导致落盘偏移量与既有数据不一致。
	dsn = ensureSQLiteParams(dsn, []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
		"_time_format=sqlite",
	})

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite database: %w", err)
	}

	// Schema 版本迁移必须跑在 AutoMigrate 之前：AutoMigrate 只会增量加列，
	// 删列/改型/数据转换这类破坏性变更只能由 migrateSchema 完成；迁移后的
	// 结构再交给 AutoMigrate 兜底对齐新增字段。迁移列表为空时是纯空操作。
	// 两个失败路径都要关掉已建立的连接：migrateSchema 的"数据库版本高于
	// 当前二进制"就是拒绝启动的场景，不关连接会一直锁着 SQLite 文件，
	// 用户换回新二进制后仍可能撞上 "database is locked"。
	if err := migrateSchema(db); err != nil {
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := db.AutoMigrate(
		&model.Subscription{},
		&model.Config{},
		&model.Setting{},
		&model.Conflict{},
		&model.ConfigVersion{},
		&model.SubCollection{},
		&model.SubCollectionItem{},
		// SubRule（全局改写规则）已移除：改写需求由各订阅/组合自身的
		// 处理管道承担，全局规则是隐式的跨订阅副作用，难以排查。
		// 遗留的 sub_rules 表不再迁移，也不再被读写。
		// SubTemplate（自定义输出模板）已移除：组合的输出格式模板下拉已下线，
		// 该子系统本就没有创建入口，是彻底的死代码。
		// 遗留的 sub_templates 表不再迁移，也不再被读写。
		&model.SubFile{},
		&model.Task{},
		&model.MihomoState{},
	); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate database: %w", err)
	}

	// SQLite 单文件写入不支持真正并发，连接数放开反而加剧锁竞争。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(4)
		sqlDB.SetMaxIdleConns(2)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	r := &Database{DB: db}
	// 历史存量数据不会被写入时的裁剪覆盖，启动时先清一次
	r.pruneOnStartup()
	return r, nil
}

// pruneOnStartup 清理历史遗留的超量记录
func (r *Database) pruneOnStartup() {
	var names []string
	if err := r.DB.Model(&model.Config{}).Distinct().Pluck("name", &names).Error; err == nil {
		for _, n := range names {
			r.pruneConfigs(n)
		}
	}

	var keepIDs []int64
	if err := r.DB.Model(&model.ConfigVersion{}).
		Order("id desc").Limit(maxConfigVersions).
		Pluck("id", &keepIDs).Error; err == nil && len(keepIDs) > 0 {
		_ = r.DB.Where("id NOT IN ?", keepIDs).Delete(&model.ConfigVersion{}).Error
	}
}

func (r *Database) GetSubscriptions() ([]model.Subscription, error) {
	var subs []model.Subscription
	err := r.DB.Order("id asc").Find(&subs).Error
	return subs, err
}

func (r *Database) GetSubscription(id int64) (*model.Subscription, error) {
	var sub model.Subscription
	if err := r.DB.First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *Database) GetSubscriptionsByIDs(ids []int64) ([]model.Subscription, error) {
	var subs []model.Subscription
	if len(ids) == 0 {
		return subs, nil
	}
	err := r.DB.Where("id IN ?", ids).Find(&subs).Error
	return subs, err
}

func (r *Database) CreateSubscription(sub *model.Subscription) error {
	return r.DB.Create(sub).Error
}

// UpdateSubscription 只更新用户可编辑的列。
// 不能用 Save(全字段)：后台刷新会并发写 cached_nodes 与流量统计，
// 全字段覆盖会把它们回滚成本次读取时的旧值（lost update）。
func (r *Database) UpdateSubscription(sub *model.Subscription) error {
	return r.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]interface{}{
			"name":         sub.Name,
			"url":          sub.URL,
			"content":      sub.Content,
			"type":         sub.Type,
			"operators":    sub.Operators,
			"share_token":  sub.ShareToken,
			"enabled":      sub.Enabled,
			"interval":     sub.Interval,
			"user_agent":   sub.UserAgent,
			"cached_nodes": sub.CachedNodes,
			"updated_at":   sub.UpdatedAt,
		}).Error
}

// DeleteSubscription 删除订阅并清理其在各组合中的关联项，
// 否则会留下孤儿记录，组合渲染时按不存在的 id 查询导致节点静默丢失。
func (r *Database) DeleteSubscription(id int64) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("subscription_id = ?", id).Delete(&model.SubCollectionItem{}).Error; err != nil {
			return err
		}
		if err := clearRemoteSourceRef(tx, remoteSourceTypeSubscription, id); err != nil {
			return err
		}
		return tx.Delete(&model.Subscription{}, id).Error
	})
}

// 远程来源设置的键与类型值。
// 与 service 层的 settingRemoteSource* / domain.RemoteSource* 一致；
// 此处重复声明是为了避免 repository 反向依赖 service。
const (
	settingKeyRemoteSourceType = "remote.source.type"
	settingKeyRemoteSourceID   = "remote.source.id"
	settingKeyRemoteSourceURL  = "remote.source.url"

	remoteSourceTypeNone         = "none"
	remoteSourceTypeSubscription = "subscription"
	remoteSourceTypeCollection   = "collection"
	remoteSourceTypeFile         = "file"
)

// clearRemoteSourceRef 在实体被删除时，把指向它的「远程配置来源」设置
// 回落为 none。
//
// 不这么做的后果是留下悬空引用：设置里仍存着已删实体的 id，而
// buildRemoteConfig 找不到它就报「指定作为远程来源的订阅(N)不存在或已禁用」，
// 「拉取并合并」从此永久失败；同时界面下拉框的候选项里没有该 id，
// 显示为空白，用户根本看不出是哪儿出了问题。
//
// 只在类型与 id 都匹配时才清，避免误伤指向其它实体的设置。
func clearRemoteSourceRef(tx *gorm.DB, sourceType string, id int64) error {
	var typeSetting model.Setting
	if err := tx.First(&typeSetting, "key = ?", settingKeyRemoteSourceType).Error; err != nil {
		// 没有这条设置说明用户从未选过远程来源，无需清理
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(typeSetting.Value) != sourceType {
		return nil
	}
	var idSetting model.Setting
	if err := tx.First(&idSetting, "key = ?", settingKeyRemoteSourceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	refID, convErr := strconv.ParseInt(strings.TrimSpace(idSetting.Value), 10, 64)
	if convErr != nil || refID != id {
		return nil
	}

	for key, value := range map[string]string{
		settingKeyRemoteSourceType: remoteSourceTypeNone,
		settingKeyRemoteSourceID:   "0",
		settingKeyRemoteSourceURL:  "",
	} {
		if err := tx.Save(&model.Setting{Key: key, Value: value}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Database) MarkSubscriptionStatus(id int64, status, errMsg string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":        status,
		"error_message": errMsg,
		"updated_at":    now,
		// 无论成败都推进尝试时间，供失败退避判断使用
		"last_attempt": now,
	}
	if status == "ok" {
		updates["last_update"] = now
	}
	return r.DB.Model(&model.Subscription{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Database) GetConfigByType(configType string) (*model.Config, error) {
	var cfg model.Config
	// 按 id 兜底排序：created_at 只到秒，同一秒内写入多条时排序不稳定
	err := r.DB.Where("type = ?", configType).Order("created_at desc, id desc").First(&cfg).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// RemoteMergedConfigName 是所有启用订阅聚合后配置的固定名称。
// buildRemoteConfig 同时会为每条订阅单独存一份 name="remote-<id>" 的快照，
// 它们的 type 同样是 "remote"。
const RemoteMergedConfigName = "remote-merged"

// GetRemoteMergedConfig 取多订阅聚合后的远程配置。
//
// 必须按 name 而非 type 定位：type="remote" 下同时存在每条订阅的原始快照
// （remote-<id>），而这些快照通常比聚合结果写入得更晚。若按 type 取最近一条，
// 会取到某条单独订阅，导致其余订阅的节点全部从最终配置中消失。
func (r *Database) GetRemoteMergedConfig() (*model.Config, error) {
	var cfg model.Config
	err := r.DB.Where("name = ?", RemoteMergedConfigName).
		Order("created_at desc, id desc").First(&cfg).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// 每次合并都会写入多条配置快照与版本记录，若不清理，
// 按每分钟一次、每条数百 KB 计，单日可增长数 GB。
const (
	maxConfigsPerName = 5
	maxConfigVersions = 50
)

func (r *Database) SaveConfig(cfg *model.Config) error {
	if err := r.DB.Create(cfg).Error; err != nil {
		return err
	}
	r.pruneConfigs(cfg.Name)
	return nil
}

// pruneConfigs 同名配置只保留最近若干条
func (r *Database) pruneConfigs(name string) {
	if name == "" {
		return
	}
	var keepIDs []int64
	if err := r.DB.Model(&model.Config{}).
		Where("name = ?", name).
		Order("id desc").Limit(maxConfigsPerName).
		Pluck("id", &keepIDs).Error; err != nil || len(keepIDs) == 0 {
		return
	}
	_ = r.DB.Where("name = ? AND id NOT IN ?", name, keepIDs).Delete(&model.Config{}).Error
}

func (r *Database) GetSetting(key string) (string, error) {
	var s model.Setting
	err := r.DB.First(&s, "key = ?", key).Error
	if err != nil {
		return "", err
	}
	return s.Value, nil
}

func (r *Database) SetSetting(key, value string) error {
	s := model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return r.DB.Save(&s).Error
}

func (r *Database) GetSettings(prefix string) (map[string]string, error) {
	var rows []model.Setting
	q := r.DB.Model(&model.Setting{})
	if prefix != "" {
		q = q.Where("key LIKE ?", prefix+"%")
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

func (r *Database) ListConflicts(onlyUnresolved bool) ([]model.Conflict, error) {
	var rows []model.Conflict
	q := r.DB.Model(&model.Conflict{}).Order("id desc")
	if onlyUnresolved {
		q = q.Where("resolved = 0")
	}
	err := q.Find(&rows).Error
	return rows, err
}

func (r *Database) GetConflict(id int64) (*model.Conflict, error) {
	var c model.Conflict
	if err := r.DB.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Database) UpsertConflicts(rows []model.Conflict) error {
	for i := range rows {
		var existing model.Conflict
		err := r.DB.Where("key = ?", rows[i].Key).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rows[i].CreatedAt = time.Now()
			rows[i].UpdatedAt = time.Now()
			if err := r.DB.Create(&rows[i]).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		rows[i].ID = existing.ID
		if existing.Resolved == 1 {
			rows[i].Resolved = 1
			rows[i].Resolution = existing.Resolution
			rows[i].ManualValue = existing.ManualValue
		}
		rows[i].CreatedAt = existing.CreatedAt
		rows[i].UpdatedAt = time.Now()
		if err := r.DB.Save(&rows[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Database) ResolveConflict(id int64, resolution, manual string) error {
	return r.DB.Model(&model.Conflict{}).Where("id = ?", id).Updates(map[string]interface{}{
		"resolution":   resolution,
		"manual_value": manual,
		"resolved":     1,
		"updated_at":   time.Now(),
	}).Error
}

func (r *Database) ListResolvedConflicts() ([]model.Conflict, error) {
	var rows []model.Conflict
	err := r.DB.Where("resolved = 1").Find(&rows).Error
	return rows, err
}

func (r *Database) SaveConfigVersion(v *model.ConfigVersion) error {
	if err := r.DB.Create(v).Error; err != nil {
		return err
	}
	var keepIDs []int64
	if err := r.DB.Model(&model.ConfigVersion{}).
		Order("id desc").Limit(maxConfigVersions).
		Pluck("id", &keepIDs).Error; err != nil || len(keepIDs) == 0 {
		return nil
	}
	_ = r.DB.Where("id NOT IN ?", keepIDs).Delete(&model.ConfigVersion{}).Error
	return nil
}

func (r *Database) ListConfigVersions(limit int) ([]model.ConfigVersion, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []model.ConfigVersion
	err := r.DB.Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *Database) GetConfigVersion(id int64) (*model.ConfigVersion, error) {
	var v model.ConfigVersion
	if err := r.DB.First(&v, id).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Database) ListCollections() ([]model.SubCollection, error) {
	var rows []model.SubCollection
	err := r.DB.Order("id asc").Find(&rows).Error
	return rows, err
}

func (r *Database) GetCollection(id int64) (*model.SubCollection, error) {
	var c model.SubCollection
	if err := r.DB.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Database) GetCollectionByToken(token string) (*model.SubCollection, error) {
	// 必须拒绝空 token：分享被撤销后 share_token 为空串，
	// 若放行空值会让 /api/v1/share/ 这类空 token 请求匹配到任意一条
	// 已撤销的记录，等于撤销失效。
	if strings.TrimSpace(token) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var c model.SubCollection
	if err := r.DB.Where("share_token = ?", token).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Database) CreateCollection(c *model.SubCollection) error {
	return r.DB.Create(c).Error
}

func (r *Database) UpdateCollection(c *model.SubCollection) error {
	return r.DB.Save(c).Error
}

// DeleteCollection 在同一事务中删除组合及其关联项，
// 避免中途失败留下半删状态。
func (r *Database) DeleteCollection(id int64) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("collection_id = ?", id).Delete(&model.SubCollectionItem{}).Error; err != nil {
			return err
		}
		if err := clearRemoteSourceRef(tx, remoteSourceTypeCollection, id); err != nil {
			return err
		}
		return tx.Delete(&model.SubCollection{}, id).Error
	})
}

func (r *Database) ReplaceCollectionItems(collectionID int64, subIDs []int64) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("collection_id = ?", collectionID).Delete(&model.SubCollectionItem{}).Error; err != nil {
			return err
		}
		for i, sid := range subIDs {
			item := model.SubCollectionItem{CollectionID: collectionID, SubscriptionID: sid, Priority: i}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Database) ListCollectionItems(collectionID int64) ([]model.SubCollectionItem, error) {
	var rows []model.SubCollectionItem
	err := r.DB.Where("collection_id = ?", collectionID).Order("priority asc, id asc").Find(&rows).Error
	return rows, err
}

// Files
func (r *Database) ListFiles() ([]model.SubFile, error) {
	var rows []model.SubFile
	err := r.DB.Order("id asc").Find(&rows).Error
	return rows, err
}

func (r *Database) GetFile(id int64) (*model.SubFile, error) {
	var f model.SubFile
	if err := r.DB.First(&f, id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Database) GetFileByName(name string) (*model.SubFile, error) {
	var f model.SubFile
	if err := r.DB.Where("name = ?", name).First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFileByToken 按直链 token 查文件。token 为随机串，不可枚举，
// 因此该查询可安全地暴露在未鉴权的公开路由上。
func (r *Database) GetFileByToken(token string) (*model.SubFile, error) {
	if strings.TrimSpace(token) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var f model.SubFile
	if err := r.DB.Where("share_token = ?", token).First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// BackfillFileShareTokens 为历史文件补齐直链 token。
// 老版本的 SubFile 没有该字段，升级后若不补齐，这些文件的直链将永久失效。
func (r *Database) BackfillFileShareTokens(gen func() string) error {
	var rows []model.SubFile
	// 排除已被用户主动撤销的分享，否则每次重启都会把它们重新激活
	if err := r.DB.Where("(share_token IS NULL OR share_token = '') AND share_revoked = 0").
		Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if err := r.DB.Model(&model.SubFile{}).Where("id = ?", rows[i].ID).
			Update("share_token", gen()).Error; err != nil {
			return err
		}
	}
	return nil
}

// SaveFile 新建或更新文件。
// 已带 ID 的按主键更新，避免改名时误判为「同名文件」而覆盖另一条记录；
// 无 ID 时按 name 做 upsert，供导入等场景使用。
func (r *Database) SaveFile(f *model.SubFile) error {
	if f.ID > 0 {
		var existing model.SubFile
		if err := r.DB.First(&existing, f.ID).Error; err != nil {
			return err
		}
		// 改名冲突需显式报错，不能静默覆盖同名的另一条记录
		var dup model.SubFile
		if err := r.DB.Where("name = ? AND id <> ?", f.Name, f.ID).First(&dup).Error; err == nil {
			return fmt.Errorf("文件名 %s 已被占用", f.Name)
		}
		f.CreatedAt = existing.CreatedAt
		f.UpdatedAt = time.Now()
		return r.DB.Save(f).Error
	}

	var existing model.SubFile
	if err := r.DB.Where("name = ?", f.Name).First(&existing).Error; err == nil {
		f.ID = existing.ID
		f.CreatedAt = existing.CreatedAt
		f.UpdatedAt = time.Now()
		return r.DB.Save(f).Error
	}
	f.CreatedAt = time.Now()
	f.UpdatedAt = time.Now()
	return r.DB.Create(f).Error
}

func (r *Database) DeleteFile(id int64) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := clearRemoteSourceRef(tx, remoteSourceTypeFile, id); err != nil {
			return err
		}
		return tx.Delete(&model.SubFile{}, id).Error
	})
}

// ---- Tasks (设计 §5) ----

func (r *Database) ListTasks() ([]model.Task, error) {
	var rows []model.Task
	err := r.DB.Order("id asc").Find(&rows).Error
	return rows, err
}

// UpsertTask 按任务名写入或更新调度记录
func (r *Database) UpsertTask(name, cron string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	var existing model.Task
	err := r.DB.Where("name = ?", name).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.DB.Create(&model.Task{Name: name, Cron: cron, Enabled: en, Status: "idle"}).Error
	}
	if err != nil {
		return err
	}
	return r.DB.Model(&model.Task{}).Where("id = ?", existing.ID).
		Updates(map[string]interface{}{"cron": cron, "enabled": en}).Error
}

// DeleteTask 按名称删除调度记录，用于清理已下线的任务。
// 记录不存在不视为错误：清理动作是幂等的。
func (r *Database) DeleteTask(name string) error {
	return r.DB.Where("name = ?", name).Delete(&model.Task{}).Error
}

// MarkTaskRun 记录任务执行结果。
// 行不存在时补建一条账本记录：settings 驱动的调度任务（auto_update 等）
// 不在启动时 Upsert 进表，但仍需 LastRun 供控制台展示。
func (r *Database) MarkTaskRun(name, status, message string, nextRun time.Time) error {
	now := time.Now()
	updates := map[string]interface{}{
		"last_run": now,
		"status":   status,
		"message":  message,
	}
	if !nextRun.IsZero() {
		updates["next_run"] = nextRun
	}
	res := r.DB.Model(&model.Task{}).Where("name = ?", name).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	row := model.Task{
		Name:    name,
		Cron:    "",
		Enabled: 0, // 账本行不表示开关；注意 Task.Enabled 带 gorm default:1，
		// Create 时显式 0 仍会按默认值落 1。该字段无人读取（列表已过滤
		// 账本行，虚拟项的 Enabled 取自 settings），语义由注释言明即可。
		LastRun: now,
		Status:  status,
		Message: message,
	}
	if !nextRun.IsZero() {
		row.NextRun = nextRun
	}
	return r.DB.Create(&row).Error
}

// PromoteTaskLedger 把 fromName 的最近执行痕迹迁到 toName（仅当 from 有 LastRun，
// 且 to 没有更新的记录时）。用于下线旧任务名时保留控制台「上次执行」展示。
// 用 Find 而非 First：空结果不记 gorm「record not found」噪音日志。
func (r *Database) PromoteTaskLedger(fromName, toName string) error {
	if fromName == "" || toName == "" || fromName == toName {
		return nil
	}
	var fromRows []model.Task
	if err := r.DB.Where("name = ?", fromName).Limit(1).Find(&fromRows).Error; err != nil {
		return err
	}
	if len(fromRows) == 0 || fromRows[0].LastRun.IsZero() {
		return nil
	}
	from := fromRows[0]

	var toRows []model.Task
	if err := r.DB.Where("name = ?", toName).Limit(1).Find(&toRows).Error; err != nil {
		return err
	}
	if len(toRows) == 0 {
		return r.DB.Create(&model.Task{
			Name:    toName,
			Enabled: 0,
			LastRun: from.LastRun,
			Status:  from.Status,
			Message: from.Message,
		}).Error
	}
	to := toRows[0]
	// to 已有不早于 from 的记录：保留 to，避免用旧痕迹覆盖
	if !to.LastRun.IsZero() && !to.LastRun.Before(from.LastRun) {
		return nil
	}
	return r.DB.Model(&model.Task{}).Where("id = ?", to.ID).Updates(map[string]interface{}{
		"last_run": from.LastRun,
		"status":   from.Status,
		"message":  from.Message,
	}).Error
}

// ---- Mihomo State (设计 §6) ----

func (r *Database) SaveMihomoState(version string, pid int, status string, startedAt time.Time) error {
	var existing model.MihomoState
	err := r.DB.First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.DB.Create(&model.MihomoState{
			Version: version, PID: pid, Status: status,
			StartedAt: startedAt, UpdatedAt: time.Now(),
		}).Error
	}
	if err != nil {
		return err
	}
	return r.DB.Model(&model.MihomoState{}).Where("id = ?", existing.ID).
		Updates(map[string]interface{}{
			"version": version, "pid": pid, "status": status,
			"started_at": startedAt, "updated_at": time.Now(),
		}).Error
}

func (r *Database) GetMihomoState() (*model.MihomoState, error) {
	var st model.MihomoState
	if err := r.DB.First(&st).Error; err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *Database) GetSubscriptionByToken(token string) (*model.Subscription, error) {
	// 同 GetCollectionByToken：空 token 一律视为不存在
	if strings.TrimSpace(token) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var s model.Subscription
	if err := r.DB.Where("share_token = ?", token).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// Close 释放底层数据库连接。
// SQLite 在 Windows 上不关闭句柄会导致文件无法删除，
// 进程退出前应显式调用。
// Close 释放底层连接池。
//
// 幂等：关停路径可能因超时兜底等原因重复调用，重复关闭 *sql.DB 本身是
// 安全的（返回 nil），这里额外容忍 nil 接收者与未完成构造的零值，
// 以便初始化失败的分支也能无条件调用。
// ---- 分享管理（订阅 / 组合 / 文件三类分享的统一维护） ----

// UpdateSubscriptionShare 修改订阅分享的名称与有效期。
// expiresAt 为零值表示永不过期；用 Updates + map 而非 Save，
// 避免把结构体零值写回覆盖其它字段（如清空 cached_nodes）。
func (r *Database) UpdateSubscriptionShare(id int64, name string, expiresAt time.Time) error {
	return r.DB.Model(&model.Subscription{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"share_name":       name,
			"share_expires_at": expiresAt,
		}).Error
}

func (r *Database) UpdateCollectionShare(id int64, name string, expiresAt time.Time) error {
	return r.DB.Model(&model.SubCollection{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"share_name":       name,
			"share_expires_at": expiresAt,
		}).Error
}

func (r *Database) UpdateFileShare(id int64, name string, expiresAt time.Time) error {
	return r.DB.Model(&model.SubFile{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"share_name":       name,
			"share_expires_at": expiresAt,
		}).Error
}

// ResetSubscriptionShareToken 重置分享凭据。
// 旧链接随即失效，用于凭据外泄后的补救。
func (r *Database) ResetSubscriptionShareToken(id int64, token string) error {
	return r.DB.Model(&model.Subscription{}).Where("id = ?", id).
		Update("share_token", token).Error
}

func (r *Database) ResetCollectionShareToken(id int64, token string) error {
	return r.DB.Model(&model.SubCollection{}).Where("id = ?", id).
		Update("share_token", token).Error
}

// ResetFileShareToken 重置文件分享凭据，同时清除撤销标记
// （重新发凭据即意味着重新启用该分享）。
func (r *Database) ResetFileShareToken(id int64, token string) error {
	return r.DB.Model(&model.SubFile{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"share_token":   token,
			"share_revoked": 0,
		}).Error
}

// ClearSubscriptionShareToken 撤销分享：清空 token 后该分享无法再被访问。
// 保留实体本身，只是不再对外暴露。
func (r *Database) ClearSubscriptionShareToken(id int64) error {
	return r.DB.Model(&model.Subscription{}).Where("id = ?", id).
		Update("share_token", "").Error
}

func (r *Database) ClearCollectionShareToken(id int64) error {
	return r.DB.Model(&model.SubCollection{}).Where("id = ?", id).
		Update("share_token", "").Error
}

// ClearFileShareToken 撤销文件分享，并打上撤销标记，
// 避免启动时的 token 补发逻辑把它重新激活。
func (r *Database) ClearFileShareToken(id int64) error {
	return r.DB.Model(&model.SubFile{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"share_token":   "",
			"share_revoked": 1,
		}).Error
}

func (r *Database) Close() error {
	if r == nil || r.DB == nil {
		return nil
	}
	r.closed.Store(true)
	sqlDB, err := r.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Closed 报告连接池是否已被关闭。
//
// 供关停期间仍在运行的任务自查：本项目有多处 `_ = db.SaveXxx(...)`
// 丢弃错误的写入（合并流程尤其多），连接池关闭后这些写入会静默失败，
// 留下"config.yaml 已是新内容、数据库记录仍是旧的"这类不一致。
// 有了这个查询，长任务能在写库前判断是否已进入关停并提前停手，
// 而不是写一堆注定失败的语句。
//
// 与 Healthy 的区别：Healthy 发 Ping 探测真实连通性（供健康检查用），
// 这里只读本进程的关停状态，不产生 IO，可在热路径上频繁调用。
func (r *Database) Closed() bool {
	if r == nil {
		return true
	}
	return r.closed.Load()
}

// Healthy 报告数据库连接是否可用。
//
// 供健康检查使用：连接池被关闭后（进程处于关停中或已关库但仍在监听），
// 必须如实返回 false，让编排系统摘除流量。否则请求会持续撞上
// "sql: database is closed"，而调用方只看到一个没有上下文的 SQL 错误。
func (r *Database) Healthy() bool {
	if r == nil || r.DB == nil {
		return false
	}
	sqlDB, err := r.DB.DB()
	if err != nil {
		return false
	}
	return sqlDB.Ping() == nil
}

// ensureSQLiteParams 为 DSN 补齐缺失的查询参数，已显式指定的保持用户设置。
//
// 去重键的取法要区分两类参数：
//   - 普通参数（_time_format 等）按 key 去重
//   - `_pragma` 是可重复的多值键（_pragma=journal_mode(WAL) 与
//     _pragma=busy_timeout(5000) 必须共存），按 key 去重会导致只补上第一个。
//     因此对它取 "_pragma:<pragma 名>" 作为去重键。
//
// want 用有序切片而非 map：`_pragma` 会重复出现多次，map 的唯一键表达不了，
// 且切片顺序固定，DSN 稳定可复现。
func ensureSQLiteParams(dsn string, want []string) string {
	base, query := dsn, ""
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		base, query = dsn[:i], dsn[i+1:]
	}

	existing := map[string]bool{}
	parts := []string{}
	for _, kv := range strings.Split(query, "&") {
		if kv == "" {
			continue
		}
		parts = append(parts, kv)
		existing[dedupKey(kv)] = true
	}

	for _, kv := range want {
		if !existing[dedupKey(kv)] {
			parts = append(parts, kv)
			// 记入 existing，避免 want 内部有重复项时补两次
			existing[dedupKey(kv)] = true
		}
	}

	if len(parts) == 0 {
		return base
	}
	return base + "?" + strings.Join(parts, "&")
}

// dedupKey 取 DSN 参数的去重键。
//
// `_pragma=journal_mode(WAL)` 与 `_pragma=busy_timeout(5000)` 是两个不同的
// 设置项却共用 `_pragma` 这个 key，只按 key 去重会让第二个被误判为"已存在"。
// 这里对 _pragma 取到 pragma 名一级。
func dedupKey(kv string) string {
	k, v := kv, ""
	if j := strings.IndexByte(kv, '='); j >= 0 {
		k, v = kv[:j], kv[j+1:]
	}
	if k != "_pragma" {
		return k
	}
	name := v
	if i := strings.IndexByte(v, '('); i >= 0 {
		name = v[:i]
	}
	return "_pragma:" + strings.TrimSpace(strings.ToLower(name))
}

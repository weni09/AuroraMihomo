package model

import "time"

type Subscription struct {
	ID   int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"not null" json:"name"`
	// URL 与 Content 二选一：远程订阅填 URL，手动粘贴节点填 Content
	URL     string `gorm:"index" json:"url"`
	Content string `gorm:"type:text" json:"content"`
	Type    string `gorm:"default:'mihomo'" json:"type"`
	// Operators 为单条订阅独立的处理管道（JSON 数组），
	// 与组合上的管道构成 Sub-Store 的两级流水线
	Operators  string `gorm:"type:text" json:"operators"`
	ShareToken string `gorm:"size:64;index" json:"share_token"`
	// ShareName 分享的展示名称，留空时界面回落到订阅名
	ShareName string `json:"share_name"`
	// ShareExpiresAt 分享有效期，零值表示永不过期
	ShareExpiresAt time.Time `json:"share_expires_at"`
	Enabled        int       `gorm:"default:1" json:"enabled"`
	Interval       int       `gorm:"default:3600" json:"interval"`
	UserAgent      string    `json:"user_agent"`
	CachedNodes    string    `gorm:"type:text" json:"cached_nodes"`
	LastUpdate     time.Time `json:"last_update"`
	// LastAttempt 记录最近一次刷新尝试（无论成败）的时间。
	// 失败时 LastUpdate 不推进，若只看它会导致坏订阅每轮都判定为"到期"，
	// 进而每分钟全量重拉所有订阅并热重载内核。
	LastAttempt  time.Time `json:"last_attempt"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message"`
	// 机场下发的流量信息（来自 subscription-userinfo 响应头）
	Upload    int64     `json:"upload"`
	Download  int64     `json:"download"`
	Total     int64     `json:"total"`
	Expire    int64     `json:"expire"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Config struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `json:"name"`
	Type      string    `gorm:"index" json:"type"` // base|remote|override|merged
	Content   string    `json:"content"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type Conflict struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Key         string    `gorm:"size:191;index" json:"key"` // stable hash key
	Type        string    `gorm:"index" json:"type"`
	Path        string    `json:"path"`
	LocalValue  string    `gorm:"type:text" json:"local_value"`
	RemoteValue string    `gorm:"type:text" json:"remote_value"`
	ManualValue string    `gorm:"type:text" json:"manual_value"`
	Resolution  string    `json:"resolution"`
	Resolved    int       `gorm:"default:0;index" json:"resolved"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ConfigVersion struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Hash      string    `gorm:"size:64;index" json:"hash"`
	Content   string    `gorm:"type:text" json:"content"`
	FilePath  string    `json:"file_path"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type SubCollection struct {
	ID      int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name    string `gorm:"not null" json:"name"`
	Enabled int    `gorm:"default:1" json:"enabled"`
	// Template 字段（组合的输出格式模板）已移除：组合统一按 mihomo-yaml 渲染，
	// 想要其它格式请通过分享链接的 ?target= 临时指定（RenderService.ResolveTarget）。
	// 存量数据库里可能仍有 template 列，AutoMigrate 不会主动删列，留作死数据即可。
	Operators  string `gorm:"type:text" json:"operators"` // JSON array of PipelineOperator
	ShareToken string `gorm:"size:64;uniqueIndex" json:"share_token"`
	// ShareName 分享的展示名称，留空时界面回落到组合名
	ShareName string `json:"share_name"`
	// ShareExpiresAt 分享有效期，零值表示永不过期
	ShareExpiresAt time.Time `json:"share_expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SubCollectionItem struct {
	ID             int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	CollectionID   int64 `gorm:"index;not null" json:"collection_id"`
	SubscriptionID int64 `gorm:"index;not null" json:"subscription_id"`
	Priority       int   `gorm:"default:0" json:"priority"`
}

// 文件的配置类型：决定分享该文件时如何产出内容。
const (
	// FileConfigTypeFile 原样输出文件内容（规则片段、Surge 模块等）
	FileConfigTypeFile = "file"
	// FileConfigTypeMihomo 把文件内容当作模板，套用订阅来源的节点渲染成 mihomo 配置
	FileConfigTypeMihomo = "mihomo"
)

// Mihomo 配置模板的书写语言，仅在 ConfigType=mihomo 时生效。
// 对齐官方 Sub-Store 的"覆写"概念：模板不是从零生成整份配置，
// 而是对系统自动生成的基础配置（proxies/proxy-groups/rules）做增量修改。
const (
	// TemplateLangGo 原有的 Go text/template 语法，可用 {{ range .Nodes }} 遍历节点。
	// 默认值，保持存量文件的行为不变。
	TemplateLangGo = "gotemplate"
	// TemplateLangYAML 正文是 YAML 片段，与基础配置深度合并（对齐官方 YAML 覆写）
	TemplateLangYAML = "yaml"
	// TemplateLangJS 正文是 function main(config){...return config}，
	// 用 goja 执行后拿返回值作为最终配置（对齐官方 JavaScript 覆写）
	TemplateLangJS = "javascript"
)

// 文件模板的节点来源类型。
const (
	SourceTypeSubscription = "subscription"
	SourceTypeCollection   = "collection"
)

// 文件正文的来源方式，与官方 Sub-Store 的 source 字段对齐。
const (
	// FileSourceLocal 正文即用户在编辑器里写的内容
	FileSourceLocal = "local"
	// FileSourceRemote 正文从 SyncURL 列出的地址拉取（可多行、多个地址）
	FileSourceRemote = "remote"
)

// 本地与远程正文的合并方式，对齐官方 mergeSources。
const (
	// FileMergeNone 不合并：按 SourceMode 只取一侧
	FileMergeNone = ""
	// FileMergeLocalFirst 本地内容在前，远程内容依次拼在后面
	FileMergeLocalFirst = "localFirst"
	// FileMergeRemoteFirst 远程内容在前，本地内容拼在最后
	FileMergeRemoteFirst = "remoteFirst"
)

// 远程地址拉取失败时的处理策略，对齐官方 ignoreFailedRemoteFile。
const (
	// FileFailStrict 任一地址失败即整体报错（默认）。
	// 默认从严：静默产出缺内容的文件会让客户端拿到不完整配置而不自知。
	FileFailStrict = ""
	// FileFailSkip 跳过失败的地址，并在结果中给出提示
	FileFailSkip = "enabled"
	// FileFailQuiet 静默跳过失败的地址
	FileFailQuiet = "quiet"
)

type SubFile struct {
	ID      int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name    string `gorm:"uniqueIndex;not null" json:"name"`
	Content string `gorm:"type:text" json:"content"`
	Type    string `gorm:"default:'raw'" json:"type"`
	// ShareToken 是文件直链的凭据。文件名由用户自定义、可被枚举，
	// 因此直链不能用文件名寻址，否则未认证者可遍历读取全部文件内容
	// （这些文件常含订阅模板乃至机场凭据）。改用随机 token 寻址。
	ShareToken string `gorm:"size:64;index" json:"share_token"`
	// SyncURL 为远程正文地址，支持多行、每行一个，按顺序拉取后拼接。
	// 历史数据只有单个地址，按单行解析结果一致，无需迁移。
	SyncURL string `json:"sync_url"`

	// SourceMode 决定正文取自本地编辑器还是远程地址。
	// 空串按 local 解释，保持存量文件（只有 content）的行为不变。
	SourceMode string `gorm:"default:'local'" json:"source_mode"`
	// MergeSources 为空表示不合并，只取 SourceMode 指定的一侧；
	// localFirst / remoteFirst 则两侧都取，仅先后顺序不同。
	MergeSources string `json:"merge_sources"`
	// IgnoreFailedRemote 决定某个远程地址拉取失败时的处理：
	// 空串=整体报错，enabled=跳过并提示，quiet=静默跳过。
	IgnoreFailedRemote string `json:"ignore_failed_remote"`
	// UserAgent 为拉取远程正文时使用的 UA，留空则用默认值。
	// 部分机场按 UA 返回不同格式，需要能指定。
	UserAgent string `json:"user_agent"`

	// ConfigType 决定分享输出：file=原样输出，mihomo=作为模板渲染节点。
	// 默认 file，保持既有文件的行为不变。
	ConfigType string `gorm:"default:'file'" json:"config_type"`
	// SourceType/SourceID 指定 mihomo 类型下节点从哪来（单条订阅或组合）。
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
	// TemplateLang 决定 ConfigType=mihomo 时正文按什么语言解释：
	// gotemplate（默认，兼容存量）/ yaml（覆写深合并）/ javascript（脚本覆写）。
	TemplateLang string `gorm:"default:'gotemplate'" json:"template_lang"`
	// TrafficURL 为流量显示链接：客户端读取 subscription-userinfo 时，
	// 由该地址代为提供机场流量信息（文件模板本身没有流量概念）。
	TrafficURL string `json:"traffic_url"`

	// ShareName 为分享的展示名称，便于在分享管理里区分用途；
	// 留空时界面回落到文件名。
	ShareName string `json:"share_name"`
	// ShareExpiresAt 为分享有效期，零值表示永不过期。
	ShareExpiresAt time.Time `json:"share_expires_at"`
	// ShareRevoked 标记分享被用户主动撤销。
	// 必须与「token 为空」区分开：启动时的 BackfillFileShareTokens 会给
	// 所有空 token 的历史文件补发凭据，若不加该标记，用户撤销的分享
	// 会在下次重启后被静默重新激活。
	ShareRevoked int `gorm:"default:0" json:"share_revoked"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Task 设计 §5：后台任务调度记录
// 任务名：subscription_update / config_merge / mihomo_reload / version_check
type Task struct {
	ID      int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name    string    `gorm:"index" json:"name"`
	Cron    string    `json:"cron"`
	Enabled int       `gorm:"default:1;index" json:"enabled"`
	LastRun time.Time `json:"last_run"`
	NextRun time.Time `json:"next_run"`
	Status  string    `json:"status"`
	Message string    `gorm:"type:text" json:"message"`
}

// MihomoState 设计 §6：内核运行状态持久化
type MihomoState struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Version   string    `json:"version"`
	PID       int       `gorm:"column:pid" json:"pid"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

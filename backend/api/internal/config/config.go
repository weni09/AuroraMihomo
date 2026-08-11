package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	rest.RestConf
	Auth struct {
		AccessSecret string `json:",optional"`
		AccessExpire int64  `json:",default=86400"` // 1 day
	}

	// Server 为原生 http.Server 层面的超时设置。
	// go-zero 的 Timeout 只约束单请求的处理时长，不覆盖读写报文阶段，
	// 缺少这些值时慢速连接（Slowloris）可长期占用连接不被回收。
	Server struct {
		// ReadHeaderTimeoutSec 是抵御 Slowloris 的关键项：限制读完请求头的时间
		ReadHeaderTimeoutSec int `json:",default=10"`
		ReadTimeoutSec       int `json:",default=60"`
		// WriteTimeoutSec 必须大于上面的 Timeout（默认 300s），否则合并配置、
		// 下载内核这类长耗时请求会在写响应阶段被连接层掐断。设为 0 表示不限制。
		WriteTimeoutSec int `json:",default=360"`
		IdleTimeoutSec  int `json:",default=120"`
		MaxHeaderBytes  int `json:",default=1048576"` // 1 MiB
	}

	// TrustedProxies 为可信反向代理的 IP/CIDR 白名单。
	// 仅当请求直连来源命中白名单时，才采信 X-Forwarded-For / X-Real-IP。
	// 为空表示完全不信任这些头部，一律使用 RemoteAddr——否则任何人
	// 都能通过伪造头部绕过登录失败限流。
	TrustedProxies []string `json:",optional"`

	DataSource string `json:",default=./data/aurora.db"`
	Mihomo     struct {
		BinaryPath string `json:",default="`
		ConfigDir  string `json:",default=./data"`
	}
	SubStore struct {
		NodePath       string `json:",default=node"`
		SubStoreScript string `json:",default=./data/substore/substore.js"`
	}
	Bootstrap struct {
		EnsureOnStart     bool `json:",default=true"`
		FailOnEnsureError bool `json:",default=false"`
	}

	// AppLog 控制本项目自身运行日志的采集（不影响 mihomo 内核日志）。
	// go-zero 的 Log 段只管控制台/文件输出格式，这里管的是
	// "把日志留一份在内存与文件里，供界面查看与事后回溯"。
	AppLog struct {
		// MemoryLimit 内存中保留的条数，供界面实时查看
		MemoryLimit int `json:",default=1000"`
		// ToFile 是否落盘。开启后可回溯重启前的故障现场——
		// 崩溃或异常退出时控制台输出往往已经丢失
		ToFile bool `json:",default=true"`
		// FilePath 为空时取 <Mihomo.ConfigDir>/logs/aurora.log
		FilePath string `json:",optional"`
		// MaxFileMB 单文件大小上限，超过即归档轮转
		MaxFileMB int `json:",default=8"`
		// MaxBackups 保留的归档份数，与 MaxFileMB 共同决定磁盘上界
		MaxBackups int `json:",default=5"`
		// IncludeAccessLog 是否收录 HTTP 访问日志与框架统计。
		// 默认关闭：go-zero 对每个请求写一条，而前端自身的轮询与每 5 秒
		// 状态推送就在不断产生请求，收录后会把业务日志冲走。
		IncludeAccessLog bool `json:",default=false"`
	}
	AutoUpdate struct {
		Enabled bool   `json:",default=false"`
		Cron    string `json:",optional"` // 为空时由 updater 兜底为 0 0 4 * * *
	}
	// Backup 控制数据库在线备份的落盘位置与保留份数。
	Backup struct {
		// Dir 为备份文件目录；为空时取 <Mihomo.ConfigDir>/backups
		//（容器镜像已预建 /data/backups，与 ConfigDir=/data 对齐）。
		Dir string `json:",optional"`
		// MaxKeep 保留的最近备份份数，超过即按时间清理最旧的。
		MaxKeep int `json:",default=7"`
	}
	Updater struct {
		MihomoRepo    string `json:",default=MetaCubeX/mihomo"`
		ZashboardRepo string `json:",default=Zephyruso/zashboard"`
		GitHubAPI     string `json:",default=https://api.github.com"`
		TimeoutSec    int    `json:",default=180"`
		// CDNProviders 为 GitHub Release 资产（内核/面板二进制）的下载源。
		// 只有这一类需要镜像：API 查询不走镜像（无镜像支持 REST API）。
		CDNProviders []string `json:",optional"`
		// UseMihomoProxy 下载与版本查询是否优先经由本地 mihomo 代理。
		// 默认开启：内核跑起来后走它出网通常比第三方镜像更快也更可靠。
		UseMihomoProxy bool `json:",default=true"`
		// SelfRepo 为主程序（AuroraMihomo 自身）的 GitHub 仓库，形如
		// "owner/AuroraMihomo"。留空时由 updater 兜底为默认
		// "weni09/AuroraMihomo"；运行期可在「系统设置 · 下载与更新出网」
		// 修改（存库），此配置仅作启动默认。设置页清空保存即停用自升级。
		SelfRepo string `json:",optional"`
	}
}

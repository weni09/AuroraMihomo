package svc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/internal/applog"
	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/engine"
	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/netcheck"
	"auroramihomo/backend/internal/realtime"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/scheduler"
	"auroramihomo/backend/internal/service"
	"auroramihomo/backend/internal/substore"
	"auroramihomo/backend/internal/updater"

	"log"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config          config.Config
	Database        *repository.Database
	MihomoManager   mihomo.Manager
	MergeEngine     *engine.MergeEngine
	Scheduler       *scheduler.Scheduler
	SubStore        substore.Manager
	ConfigService   *service.ConfigService
	Updater         *updater.Manager
	SettingsService *service.SettingsService
	RenderService   *service.RenderService
	// TransparentService 管理透明代理开关。开启会改动内核配置（TUN 模式）
	// 或宿主的防火墙与策略路由（TProxy 模式），因此带强制确认与自动回滚。
	TransparentService *service.TransparentService
	LoginLimiter       *auth.LoginLimiter
	Hub                *realtime.Hub
	// AppLog 缓存本项目自身的运行日志（logx 的输出），供界面查看。
	// 与 MihomoManager 的内核日志分开：一个是"本程序说的"、
	// 一个是"内核说的"，混成一条流反而更难排查。
	AppLog *applog.Buffer
	// AppLogPath 是应用日志的落盘路径，未启用落盘时为空串。
	// 供定时清理任务定位归档文件。
	AppLogPath string
}

func NewServiceContext(c config.Config) *ServiceContext {
	db, err := repository.NewDatabase(c.DataSource)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// JWT 密钥必须跨重启保持一致，否则已签发的令牌全部失效、用户被强制登出。
	// 优先取配置/环境变量，其次复用库中已持久化的密钥，最后才新生成并落库。
	if c.Auth.AccessSecret == "" {
		saved, err := db.GetSetting("jwt_access_secret")
		if err == nil && strings.TrimSpace(saved) != "" {
			c.Auth.AccessSecret = saved
		} else {
			secret, err := auth.GenerateSecret(32)
			if err != nil {
				log.Fatalf("Failed to generate JWT secret: %v", err)
			}
			if err := db.SetSetting("jwt_access_secret", secret); err != nil {
				log.Fatalf("Failed to persist JWT secret: %v", err)
			}
			c.Auth.AccessSecret = secret
		}
	}

	dataDir := c.Mihomo.ConfigDir
	if dataDir == "" {
		dataDir = "./data"
	}
	binName := "mihomo"
	if runtime.GOOS == "windows" {
		binName = "mihomo.exe"
	}
	mihomoPath := c.Mihomo.BinaryPath
	if mihomoPath == "" {
		mihomoPath = filepath.Join(dataDir, "bin", binName)
	}

	upd := updater.New(updater.Config{
		DataDir:            dataDir,
		MihomoBinaryPath:   mihomoPath,
		ZashboardDir:       filepath.Join(dataDir, "zashboard"),
		AutoUpdateEnabled:  c.AutoUpdate.Enabled,
		AutoUpdateCron:     c.AutoUpdate.Cron,
		MihomoRepo:         c.Updater.MihomoRepo,
		ZashboardRepo:      c.Updater.ZashboardRepo,
		GitHubAPI:          c.Updater.GitHubAPI,
		HTTPTimeoutSeconds: c.Updater.TimeoutSec,
		CDNProviders:       c.Updater.CDNProviders,
		UseMihomoProxy:     c.Updater.UseMihomoProxy,
	})

	hub := realtime.NewHub()
	mgrCfg := mihomo.Config{BinaryPath: mihomoPath, ConfigDir: dataDir}
	ssCfg := substore.Config{NodePath: c.SubStore.NodePath, SubStoreScript: c.SubStore.SubStoreScript}
	mergeEngine := engine.NewMergeEngine()
	mihomoManager := mihomo.NewManager(mgrCfg)

	mihomoManager.SubscribeLogs(func(line mihomo.LogLine) {
		hub.Publish("log.message", line)
	})

	// 接入应用自身日志。放在这里而不是 main：必须晚于 rest.MustNewServer
	// （它内部走 logx.SetUp，会覆盖 writer），而 NewServiceContext 正是在其后调用。
	appLogBuf, appLogPath := setupAppLog(c, dataDir, hub)

	subStoreManager := substore.NewManager(ssCfg)
	settingsService := service.NewSettingsService(db, upd)
	// 组件状态需要显示内核版本。版本由 mihomo -v 探测并缓存在进程管理器里，
	// 用 Status() 而非 Version()：后者可能启动子进程，不适合放在设置读取路径上。
	settingsService.SetMihomoVersionFunc(func() string {
		v := mihomoManager.Status().Version
		if v == "unknown" {
			// 尚未探测到时返回空串，由前端决定如何呈现「未知」，
			// 避免把内部占位串直接显示给用户
			return ""
		}
		return v
	})

	// 首次启动生成管理员密码：以哈希形式入库，明文仅在本次启动时展示一次
	pwd, err := db.GetSetting("admin_password")
	if err != nil || strings.TrimSpace(pwd) == "" {
		newPwd, err := auth.GenerateSecret(16) // 32 位十六进制，128 bit 熵
		if err != nil {
			log.Fatalf("Failed to generate initial password: %v", err)
		}
		hashed, err := auth.HashPassword(newPwd)
		if err != nil {
			log.Fatalf("Failed to hash initial password: %v", err)
		}
		if err := db.SetSetting("admin_password", hashed); err != nil {
			log.Fatalf("Failed to persist initial password: %v", err)
		}

		pwdFilePath := filepath.Join(dataDir, "initial_password.txt")
		if err := os.WriteFile(pwdFilePath, []byte(newPwd+"\n"), 0o600); err != nil {
			logx.Errorf("写入初始密码文件失败: %v", err)
		}

		fmt.Println("=========================================================")
		fmt.Printf("初始管理员密码：%s\n", newPwd)
		fmt.Printf("同时已写入文件：%s\n", pwdFilePath)
		fmt.Println("该密码仅此次显示，请登录后尽快修改。")
		fmt.Println("=========================================================")
	}

	cfgSvc := service.NewConfigService(db, mergeEngine, mihomoManager, subStoreManager, dataDir)
	// 设计 §16：合并时读取用户配置的策略
	cfgSvc.SetPolicyProvider(settingsService.GetMergePolicy)
	// 合并时读取用户选定的远程来源（单条订阅 / 组合 / 文件模板）
	cfgSvc.SetRemoteSourceProvider(settingsService.GetRemoteSource)

	// 更新器出网优先走本地内核的代理。端口取决于当前生效的 config.yaml，
	// 由 ConfigService 解析，这里只做注入，避免 updater 反向依赖配置层。
	upd.SetProxyURLFunc(cfgSvc.LocalProxyURL)

	// RenderService 依赖 cfgSvc 的 substore 引擎，只能在其后构造；
	// 而 cfgSvc 又需要它来渲染组合/文件模板来源，故用 setter 回注，
	// 避免两者构造期的循环依赖。
	renderSvc := service.NewRenderService(db, cfgSvc.SubStoreEngine())
	cfgSvc.SetRenderers(
		func(ctx context.Context, id int64) (string, error) {
			// 强制以 mihomo-yaml 渲染：远程配置层必须是可合并的 mihomo 配置，
			// 不能沿用组合自身可能设成 base64/明文链接的输出模板。
			return renderSvc.RenderCollection(ctx, id, "mihomo-yaml")
		},
		renderSvc.RenderFileTemplate,
	)

	// 透明代理。reloadFn 走一次完整的合并下发，使开关立即作用到内核配置上
	// （注入逻辑在 ConfigService 的合并流程里，见 netcheck.Inject 的调用点）。
	//
	// Applier 只在 Linux 上构造：TProxy 依赖 nftables 与策略路由，其它平台
	// 给 nil，service 层据此拒绝启用该模式而不是运行到一半才失败。
	var tpApplier *netcheck.Applier
	if runtime.GOOS == "linux" {
		tpApplier = &netcheck.Applier{
			// 快照与配置备份分开存：前者是宿主的网络状态，
			// 与 mihomo 配置的备份不是一类东西
			SnapshotDir: filepath.Join(dataDir, "netbackup"),
			Runner:      netcheck.NewExecRunner(),
			Logf:        logx.Infof,
		}
	}
	transparentSvc := service.NewTransparentService(
		db,
		tpApplier,
		logx.WithContext(context.Background()),
		func(ctx context.Context) error {
			_, err := cfgSvc.MergeAndApplyDetailed(ctx, service.MergeWithRefresh(0))
			return err
		},
		// 新增：读取 base.yaml
		func() (string, error) {
			return cfgSvc.GetBaseConfig()
		},
		// 新增：更新 base.yaml
		func(content string) error {
			return cfgSvc.UpdateBaseConfig(content)
		},
	)
	// 环境准备（装依赖、写 sysctl）同样只在 Linux 上构造，理由同 Applier。
	// Root 用默认的 "/"：写的是真实的 /etc/sysctl.d/，测试才会指向临时目录。
	if runtime.GOOS == "linux" {
		transparentSvc.SetProvisioner(&netcheck.Provisioner{
			Runner: netcheck.NewExecRunner(),
			Logf:   logx.Infof,
		})
	}
	// TProxy 规则必须放行这两个管理端口，否则规则生效瞬间就失去了关掉开关的
	// 通道。两者都是可配置的，所以取实际值而不是硬编码：面板端口来自
	// aurora-api.yaml（进程内不变），内核 API 端口来自 config.yaml
	// （用户随时可改，因此传函数每次现取）。
	transparentSvc.SetManagementPorts(c.Port, func() int {
		target, err := cfgSvc.KernelAPITarget()
		if err != nil {
			// 配置还没生成、或没启用 external-controller。返回 0 表示
			// 不放行该项，而不是猜一个默认端口——猜错等于没放行，
			// 却让人以为已经放行了。
			logx.Errorf("解析内核 API 端口失败，透明代理规则将不放行该端口: %v", err)
			return 0
		}
		port, err := strconv.Atoi(target.Port)
		if err != nil {
			logx.Errorf("内核 API 端口 %q 无法解析为数字，透明代理规则将不放行该端口", target.Port)
			return 0
		}
		return port
	})

	// 老版本 SubFile 无 share_token，升级后需补齐，否则这些文件的直链失效
	if err := db.BackfillFileShareTokens(func() string { return mustRandomToken(16) }); err != nil {
		logx.Errorf("补齐文件直链 token 失败: %v", err)
	}

	// 设计 §5：登记后台任务，便于查询调度状态。
	// subscription_update（每分钟按订阅 interval 轮询）已随定时拉取的统一而移除，
	// 清掉存量记录，否则任务列表里会一直显示一个永不执行的任务。
	_ = db.DeleteTask("subscription_update")
	_ = db.UpsertTask("config_merge", "on-demand", true)
	_ = db.UpsertTask("mihomo_reload", "on-demand", true)
	_ = db.UpsertTask("version_check", c.AutoUpdate.Cron, c.AutoUpdate.Enabled)

	return &ServiceContext{
		Config:             c,
		Database:           db,
		MihomoManager:      mihomoManager,
		MergeEngine:        mergeEngine,
		Scheduler:          scheduler.NewScheduler(),
		SubStore:           subStoreManager,
		ConfigService:      cfgSvc,
		Updater:            upd,
		SettingsService:    settingsService,
		RenderService:      renderSvc,
		TransparentService: transparentSvc,
		// 5 分钟内失败 5 次锁定 15 分钟，抵御口令爆破
		LoginLimiter: auth.NewLoginLimiter(5, 5*time.Minute, 15*time.Minute),
		Hub:          hub,
		AppLog:       appLogBuf,
		AppLogPath:   appLogPath,
	}
}

// setupAppLog 把应用自身日志接入内存缓冲、文件与实时推送。
//
// 返回内存缓冲供接口查询。任何环节失败都只降级、不中断启动：
// 日志查看是辅助功能，不该因为磁盘不可写就让服务起不来。
func setupAppLog(c config.Config, dataDir string, hub *realtime.Hub) (*applog.Buffer, string) {
	path := c.AppLog.FilePath
	if path == "" && c.AppLog.ToFile {
		path = filepath.Join(dataDir, "logs", "aurora.log")
	}

	w, err := applog.New(applog.Options{
		Limit:            c.AppLog.MemoryLimit,
		FilePath:         path,
		MaxFileBytes:     int64(c.AppLog.MaxFileMB) << 20,
		MaxBackups:       c.AppLog.MaxBackups,
		IncludeAccessLog: c.AppLog.IncludeAccessLog,
	})
	if err != nil {
		// 此时 writer 尚未注册，可以安全地用 logx 报告这个错误
		logx.Errorf("应用日志落盘不可用，仅保留内存缓冲: %v", err)
	}

	buf := w.Buffer()

	// 桥接到实时通道。
	//
	// 这个回调绝对不能打日志：它由日志写入触发，一旦自己再写日志
	// 就是无限递归。Hub.Publish 内部无日志调用（realtime/hub.go），
	// 慢订阅者走 select-default 丢弃而非阻塞，因此这里是安全的。
	// 后续若要在此处加错误处理，也只能写 stderr，不能调 logx。
	buf.Subscribe(func(e applog.Entry) {
		hub.Publish("applog.message", e)
	})

	// AddWriter 而非 SetWriter：保留 go-zero 已装好的控制台 writer，
	// 让开发时终端照常有输出，本 writer 只是多收一份。
	logx.AddWriter(w)
	return buf, w.FilePath()
}

// mustRandomToken 生成 n 字节的随机十六进制 token，用于文件直链等公开凭据。
// 随机源失败属于不可恢复的环境异常，此处直接终止而非退化为可预测值。
func mustRandomToken(n int) string {
	t, err := auth.GenerateSecret(n)
	if err != nil {
		log.Fatalf("Failed to generate random token: %v", err)
	}
	return t
}

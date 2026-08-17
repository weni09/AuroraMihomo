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
	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/applog"
	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/diagnostics"
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
	Config        config.Config
	Database      *repository.Database
	MihomoManager mihomo.Manager
	// MihomoGuard 内核「期望运行」守护：检测到停止且期望运行则自动拉起
	//（限次）。手动启停 logic 通过它持久化期望态。
	MihomoGuard     *service.MihomoGuard
	MergeEngine     *engine.MergeEngine
	Scheduler       *scheduler.Scheduler
	SubStore        substore.Manager
	ConfigService   *service.ConfigService
	Updater         *updater.Manager
	SettingsService *service.SettingsService
	RenderService   *service.RenderService
	// MonitorService 采集宿主资源（CPU/内存/网络/磁盘/运行时长）并计算
	// 网络速率。单例：速率差分需要跨请求保留上次采样基线。
	MonitorService *service.MonitorService
	// TransparentService 管理透明代理开关。开启会改动内核配置（TUN 模式）
	// 或宿主的防火墙与策略路由（TProxy 模式），因此带强制确认与自动回滚。
	TransparentService *service.TransparentService
	// AdGuardService 编排可选的 AdGuard Home 子进程与 DNS 一键对接。
	// 未安装时主路径不依赖它；API logic 层通过本字段调用。
	AdGuardService *service.AdGuardService
	// AdGuardManager 供 /adguard-ui 反代读取运行状态与 web 上游。
	AdGuardManager *adguard.Manager
	// AdGuardSSO 在 Aurora 已登录时代持 AGH agh_session，实现 iframe 免密。
	// 仅内存、不落盘；登出时 Clear。
	AdGuardSSO   *adguard.SessionBridge
	LoginLimiter *auth.LoginLimiter
	// PasswordVer 记录管理员口令版本（改密次数）。登录签发的 JWT 携带
	// 当前值，改密 +1 后旧令牌在三处验签路径（API 闸门 / WS / AdGuard
	// 反代）被拒绝，实现无状态 JWT 的改密吊销。
	PasswordVer *auth.PasswordVer
	Hub         *realtime.Hub
	// Diag 网络诊断服务：接收 run/result 请求，后台执行探测并缓存结果，
	// 进度事件经 Hub 实时推送。
	Diag *diagnostics.Service
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
		SelfRepo:           c.Updater.SelfRepo,
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

		pwdFilePath := filepath.Join(dataDir, InitialPasswordFileName)
		if err := os.WriteFile(pwdFilePath, []byte(newPwd+"\n"), 0o600); err != nil {
			logx.Errorf("写入初始密码文件失败: %v", err)
		}

		fmt.Println("=========================================================")
		fmt.Printf("初始管理员密码：%s\n", newPwd)
		fmt.Printf("同时已写入文件：%s\n", pwdFilePath)
		fmt.Println("该密码仅此次显示；成功登录或改密后会自动删除上述文件。")
		fmt.Println("=========================================================")
	}

	cfgSvc := service.NewConfigService(db, mergeEngine, mihomoManager, subStoreManager, dataDir)
	// 新装库无 base 时写入开箱默认（从真实部署提炼、已去个人数据）
	if err := cfgSvc.EnsureDefaultBase(); err != nil {
		log.Printf("写入开箱默认基础配置失败: %v", err)
	}
	// 设计 §16：合并时读取用户配置的策略
	cfgSvc.SetPolicyProvider(settingsService.GetMergePolicy)
	// 合并时读取用户选定的远程来源（单条订阅 / 组合 / 文件模板）
	cfgSvc.SetRemoteSourceProvider(settingsService.GetRemoteSource)

	// 更新器出网优先走本地内核的代理。端口取决于当前生效的 config.yaml，
	// 由 ConfigService 解析，这里只做注入，避免 updater 反向依赖配置层。
	upd.SetProxyURLFunc(cfgSvc.LocalProxyURL)

	// 透明代理服务在下方构造，但诊断服务的标注回调需要引用它。先声明变量
	// 再于下方赋值：闭包延迟到标注时求值，透明代理开关在运行期变化也会即时
	// 反映；声明在诊断服务之前以满足 Go 的文本顺序要求。
	var transparentSvc *service.TransparentService

	// 网络诊断服务：并发上限 3、结果保留 10 分钟、单探测超时 5 秒。
	// Publish 对接实时通道推单步进度（EventTypeProgress）；ProxyURL 供
	// proxy 路径探测取本地代理地址，与 updater 同一来源（cfgSvc.LocalProxyURL）。
	// CapNetAdminFn 供直连路径标注透明代理接管提示：TPROXY 下缺 CAP_NET_ADMIN
	// 无法打 PanelMark 绕开自身规则，直连探测实际被 TPROXY 接管。Detect() 带
	// 3s 缓存 + 单飞，高频调用无开销。
	// TransparentStatusFn 以透明代理开启状态为标注总开关：TUN 下 mihomo 自管
	// 路由、无 PanelMark 豁免，direct 直连流量必被接管（即使持有 CAP_NET_ADMIN
	// 原判据也不触发）；TPROXY 下按 CapNetAdminFn 区分可能接管/已绕开。
	// transparentSvc 构造在前（见下方），此处直接引用其 Status()——闭包延迟
	// 到标注时求值，透明代理开关在运行期变化也会即时反映。
	diagSvc := diagnostics.New(diagnostics.Config{
		MaxConcurrent: 3,
		ResultTTL:     10 * time.Minute,
		ProbeTimeout:  5 * time.Second,
		Probes:        diagnostics.Probes(),
		Publish:       hub.Publish,
		ProxyURL:      cfgSvc.LocalProxyURL,
		CapNetAdminFn: func() bool { return netcheck.Detect().CapNetAdmin },
		TransparentStatusFn: func() (bool, string) {
			st, _ := transparentSvc.Status()
			return st.Enabled, st.Mode
		},
	})

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

	// raw 拉取（模板/订阅远程源）与 Release 下载共用同一份 CDN 配置：
	// fetcher 通过查询函数现查 updater 的优先化列表，last 变化即时生效，
	// 无需在设置变更时推送。镜像成功也记入同一 last 优先序。
	cfgSvc.SetRawCDNProviderFunc(upd.PrioritizedCDNProviders)
	renderSvc.SetRawCDNProviderFunc(upd.PrioritizedCDNProviders)
	// raw 官方链接拉取优先经本地 mihomo 代理直取官方（与发布包下载一致）。
	// 端口由 ConfigService 解析，这里只注入查询回调。
	cfgSvc.SetRawProxyURLFunc(cfgSvc.LocalProxyURL)
	renderSvc.SetRawProxyURLFunc(cfgSvc.LocalProxyURL)
	// raw 镜像成功时记入全局 last 优先序（落库，重启后仍优先该镜像）
	cfgSvc.SetRawSuccessCallback(upd.RememberCDNSuccess)

	// 透明代理。reloadFn 走一次完整的合并下发，使开关立即作用到内核配置上
	// （注入逻辑在 ConfigService 的合并流程里，见 netcheck.Inject 的调用点）。
	//
	// Applier 只在 Linux 上构造：TProxy 依赖 nftables 与策略路由，其它平台
	// 给 nil，service 层据此拒绝启用该模式而不是运行到一半才失败。
	//
	// 关键：变量类型必须是接口而不是 *netcheck.Applier。
	// 把一个值为 nil 的 *Applier 赋给接口参数，得到的接口不等于 nil
	//（它带着类型信息），service 层的 `applier == nil` 守卫会失效，
	// 方法照常被调用并在解引用字段时崩掉——非 Linux 上启动即 panic。
	// 声明为接口类型，未赋值时它就是真正的 nil 接口。
	var tpApplier service.TransparentApplier
	var dnsRedirApplier *netcheck.Applier
	if runtime.GOOS == "linux" {
		dnsRedirApplier = &netcheck.Applier{
			// 快照与配置备份分开存：前者是宿主的网络状态，
			// 与 mihomo 配置的备份不是一类东西
			SnapshotDir: filepath.Join(dataDir, "netbackup"),
			Runner:      netcheck.NewExecRunner(),
			Logf:        logx.Infof,
		}
		tpApplier = dnsRedirApplier
	}
	transparentSvc = service.NewTransparentService(
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
	// 开机持久化（Linux）：把已确认的 TProxy 规则写入宿主开机链路，
	// 宿主重启后自动恢复。只在 Linux 构造——写入 /etc 需要 root 且
	// 依赖 init 系统（systemd/OpenRC）；其它平台保持 nil，行为与旧版一致。
	if runtime.GOOS == "linux" {
		transparentSvc.SetBootPersist(&netcheck.BootPersist{
			Root:   "/",
			Init:   netcheck.DetectInitSystem(),
			Runner: netcheck.NewExecRunner(),
			Logf:   logx.Infof,
		})
	}
	// 合并流程要知道"TProxy 规则是不是面板下发的"才能决定是否注入 routing-mark：
	// 只配了 tproxy-port 不构成启用 TProxy（把流量引过去的规则不在配置里）。
	//
	// 两个服务因此互相需要，用注入打破：TransparentService 借 cfgSvc 读写
	// base.yaml，cfgSvc 借这个方法值读托管标记。传方法值而非取一次布尔——
	// 标记会随用户开关变化，取值一次会让此后所有合并都用启动时的旧状态。
	cfgSvc.SetTProxyManagedProvider(transparentSvc.TProxyManaged)

	// 配置生效后让防火墙规则跟上：规则里烧进了 tproxy-port / DNS 端口 /
	// 内核 API 端口，用户在配置中心改完这些，只有 config.yaml 会变。
	// Resync 自身带指纹比对，配置没漂移时是空操作，因此每次合并都调是安全的。
	cfgSvc.SetTransparentResyncFn(transparentSvc.Resync)

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
	// DNS 重定向目标：取 config.yaml 里 mihomo 实际的 dns.listen 端口。
	// 送到 tproxy-port 是不行的——TPROXY 保留原始目的端口，mihomo 会把它当作
	// 普通流量而不按 DNS 应答，域名解析就始终不被接管（真机实测过的现象）。
	transparentSvc.SetDNSPortFn(func() int {
		port, err := cfgSvc.KernelDNSPort()
		if err != nil {
			// 解析失败时返回 0，由 service 回落到默认端口并留痕。
			// 不猜一个值：猜错会让 DNS 规则指向没人监听的端口。
			logx.Errorf("解析 mihomo DNS 监听端口失败，透明代理将使用默认端口: %v", err)
			return 0
		}
		return port
	})
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

	// AdGuard Home：可选子进程。默认不下载；仅用户安装后才会 Installed。
	// work-dir 与二进制分开放：yaml/统计在 data/adguardhome，可执行文件在 data/bin。
	aghWorkDir := filepath.Join(dataDir, "adguardhome")
	if abs, err := filepath.Abs(aghWorkDir); err == nil {
		aghWorkDir = abs
	}
	aghWebAddr := "127.0.0.1:3000"
	if v, err := db.GetSetting("adguard.web_addr"); err == nil && strings.TrimSpace(v) != "" {
		aghWebAddr = strings.TrimSpace(v)
	}
	aghMgr := adguard.NewManager(adguard.Config{
		BinaryPath: upd.AdGuardBinaryPath(),
		WorkDir:    aghWorkDir,
		WebAddr:    aghWebAddr,
	})
	// 服务化：Linux 上探测 systemd/OpenRC 并注入控制器，AGH 由系统服务
	// 看护（面板升级/重启期间 DNS 不中断）；探测不到（Windows 等）为 nil，
	// Manager 回落 exec 子进程托管。
	aghMgr.SetController(adguard.NewServiceController())
	aghSvc := service.NewAdGuardService(db, upd, aghMgr, transparentSvc, cfgSvc, aghWorkDir, aghWebAddr)
	// 免密桥：与反代共用 web 地址解析；用户名从 settings/yaml 读取。
	aghSSO := adguard.NewSessionBridge(func() string {
		// SSO 登录请求与反代同源：0.0.0.0 → 127.0.0.1，单网卡 IP 则连该 IP
		if host, port, err := adguard.ReadWebListen(aghWorkDir); err == nil && port > 0 {
			return adguard.LocalProxyUpstream(host, port)
		}
		if a := strings.TrimSpace(aghMgr.Status().WebAddr); a != "" {
			return a
		}
		return aghWebAddr
	})
	// 永久接管：AGH 口令 AES-GCM 加密落 settings，主密钥用 JWT AccessSecret
	credStore := service.NewAGHCredStore(db, c.Auth.AccessSecret)
	aghSSO.SetCredStore(credStore)
	if err := aghSSO.HydrateFromStore(); err != nil {
		logx.Errorf("加载 AdGuard 免密凭据失败: %v", err)
	}
	aghSSO.SetUsername(aghSvc.AdminUsername())
	// 存量：首次安装写了 initial_admin_password.txt 但未落 sso_password_enc 时，
	// 反代无法免密，iframe 一直停在 AGH 登录页，用户会以为「没启动」。
	// 仅当库中尚无密文时从该文件补写，避免覆盖用户后来在设置里改过的口令。
	if _, encPass, lErr := credStore.Load(); lErr == nil && strings.TrimSpace(encPass) == "" {
		passFile := filepath.Join(dataDir, "adguardhome", "initial_admin_password.txt")
		if b, rErr := os.ReadFile(passFile); rErr == nil {
			if p := strings.TrimSpace(string(b)); p != "" {
				user := aghSvc.AdminUsername()
				if user == "" {
					user = "admin"
				}
				if err := aghSSO.PersistCredentials(user, p); err != nil {
					logx.Errorf("从 initial_admin_password 补写 AGH 免密凭据失败: %v", err)
				} else {
					logx.Info("已从 initial_admin_password.txt 补写 AdGuard 免密凭据")
				}
			}
		}
	}
	aghSvc.SetSSO(aghSSO)
	// 无 TProxy 时模式 2 使用独立 DNS 重定向表；与透明代理共用 Linux 上的 Applier。
	if dnsRedirApplier != nil {
		aghSvc.SetDNSRedirectApplier(dnsRedirApplier)
	}
	// 若上次退出时 wiring=on，恢复 TProxy DNS 覆盖到 AGH 端口，
	// 否则 Resync/重启后规则会悄悄指回 mihomo，对接名存实亡。
	aghSvc.RestoreWiringOverrideOnBoot()
	// 服务模式：存量升级补写 unit，并按 settings 期望运行决定是否 enable+start。
	// 必须在 ShouldStartAtBoot 分支之前——否则已装用户升级后 AGH 既无 unit
	// 也不走面板自启，等于停机。
	if aghMgr.ServiceMode() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			aghSvc.EnsureServiceUnitOnBoot(ctx)
		}()
	} else if aghSvc.ShouldStartAtBoot() {
		// exec 模式：enabled_at_boot 且二进制在盘 → 后台拉起，不阻塞面板启动。
		// 失败只记日志——AGH 是可选组件，起不来不能拖垮主服务。
		// StartWithBootRetry：先让出 ~800ms，失败则有限次指数退避重试。
		go func() {
			// 总预算覆盖：初始等待 + 最多 3 次 Start + 2s/4s 退避，留余量给慢盘。
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := aghSvc.StartWithBootRetry(ctx); err != nil {
				logx.Errorf("AdGuard Home 开机自启最终失败: %v", err)
			} else {
				logx.Info("AdGuard Home 已按 enabled_at_boot 启动")
			}
		}()
	} else {
		logx.Infof("AdGuard Home 跳过开机自启（component=%v desired=%v installed=%v serviceMode=%v）",
			aghSvc.ComponentEnabled(), aghSvc.DesiredRunning(), aghMgr.Status().Installed, aghMgr.ServiceMode())
	}

	// 老版本 SubFile 无 share_token，升级后需补齐，否则这些文件的直链失效
	if err := db.BackfillFileShareTokens(func() string { return mustRandomToken(16) }); err != nil {
		logx.Errorf("补齐文件直链 token 失败: %v", err)
	}

	// 设计 §5：登记后台任务，便于查询调度状态。
	// subscription_update（每分钟按订阅 interval 轮询）已随定时拉取的统一而移除，
	// 清掉存量记录，否则任务列表里会一直显示一个永不执行的任务。
	_ = db.DeleteTask("subscription_update")
	// version_check 是旧名：启动时用 yaml AutoUpdate.Enabled 登记，与系统设置里的
	// auto_update 双轨，常出现「版本检查已关闭、组件自动更新已开启」。真实调度
	// 只走 Settings + Scheduler 的 auto_update；成功执行改记 auto_update 账本。
	// 先把旧行的 LastRun 迁到 auto_update，再删，避免控制台丢「上次执行」痕迹。
	_ = db.PromoteTaskLedger("version_check", "auto_update")
	_ = db.DeleteTask("version_check")
	_ = db.UpsertTask("config_merge", "on-demand", true)
	// 宿主资源监控。磁盘合计常规文件系统；数据目录仅作分区枚举失败时的回退。
	// 转绝对路径：Windows 上相对路径无法定位分区。
	absDataDir, _ := filepath.Abs(dataDir)
	monitorSvc := service.NewMonitorService(absDataDir)

	mihomoGuard := service.NewMihomoGuard(db, mihomoManager)
	return &ServiceContext{
		Config:             c,
		Database:           db,
		MihomoManager:      mihomoManager,
		MihomoGuard:        mihomoGuard,
		MergeEngine:        mergeEngine,
		Scheduler:          scheduler.NewScheduler(),
		SubStore:           subStoreManager,
		ConfigService:      cfgSvc,
		Updater:            upd,
		SettingsService:    settingsService,
		RenderService:      renderSvc,
		MonitorService:     monitorSvc,
		TransparentService: transparentSvc,
		AdGuardService:     aghSvc,
		AdGuardManager:     aghMgr,
		AdGuardSSO:         aghSSO,
		// 5 分钟内失败 5 次锁定 15 分钟，抵御口令爆破
		LoginLimiter: auth.NewLoginLimiter(5, 5*time.Minute, 15*time.Minute),
		// 口令版本：登录签发进 JWT，改密 +1 后旧令牌立即失效。
		// 从 settings 恢复（见 initPasswordVer），进程内并发安全。
		PasswordVer: initPasswordVer(db),
		Hub:         hub,
		Diag:        diagSvc,
		AppLog:      appLogBuf,
		AppLogPath:  appLogPath,
	}
}

// initPasswordVer 从 settings 表恢复口令版本计数。
// 缺失或解析失败按 0 处理：未改密过的存量部署平滑升级，
// 旧令牌（无 ver 声明）不会被误踢下线。
func initPasswordVer(db *repository.Database) *auth.PasswordVer {
	ver := int64(0)
	if v, err := db.GetSetting("admin_password_ver"); err == nil && strings.TrimSpace(v) != "" {
		if parsed, perr := strconv.ParseInt(strings.TrimSpace(v), 10, 64); perr == nil {
			ver = parsed
		} else {
			logx.Errorf("口令版本设置无法解析 %q: %v", v, perr)
		}
	}
	return auth.NewPasswordVer(ver)
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

// InitialPasswordFileName 首次启动写入的明文初始密码文件名。
// 登录成功或改密后应删除，避免明文长期留在数据卷。
const InitialPasswordFileName = "initial_password.txt"

// RemoveInitialPasswordFile 删除 dataDir 下的初始密码明文文件。
// 文件不存在时静默忽略；其它错误只记日志，不让登录/改密失败。
func RemoveInitialPasswordFile(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = "./data"
	}
	path := filepath.Join(dataDir, InitialPasswordFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logx.Errorf("删除初始密码文件失败 path=%s: %v", path, err)
	}
}

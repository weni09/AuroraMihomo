package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/handler"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/service"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/aurora-api.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	// 环境变量优先，便于容器部署时从外部注入密钥等敏感配置
	c.ApplyEnvOverrides()

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	svcCtx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, svcCtx)

	// 静态资源（Zashboard 内嵌面板 + 本项目前端）挂到原生 mux，
	// go-zero 路由无法匹配任意深度路径，这里在进入路由前直接分流。
	// getWebFS 作为 provider 按请求求值：磁盘 public/ 与二进制内嵌
	// 资源随时切换，删掉磁盘目录不需要重启也能回退到内嵌。
	staticMux := http.NewServeMux()
	mountStatic(staticMux, "/ui", func() http.FileSystem {
		return http.Dir(filepath.Join(c.Mihomo.ConfigDir, "zashboard"))
	})
	mountStatic(staticMux, "/", getWebFS)
	// staticMux 在 server.StartWithOpts 中包装到最外层 Handler
	registerWebSocket(server, svcCtx)
	registerHealthz(server, svcCtx)
	applyAuthHardening(server, svcCtx)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 热重载与「请求退出」的中转。注册的动作在收到 SIGHUP 或调用
	// POST /api/v1/system/reload 时依次执行，无需重启进程。
	reloadMgr := service.NewReloadManager()

	svcCtx.SettingsService.SetReloadFunc(func(enabled bool, cronExpr string) error {
		return svcCtx.Scheduler.SetAutoUpdateJob(enabled, cronExpr, func() {
			// 派生自 rootCtx 而非 context.Background()：后者让关停时的 cancel()
			// 完全够不到这个任务，下载会一直跑到 5 分钟超时，而关停只等 30 秒，
			// 之后数据库就被关掉了。挂到 rootCtx 上，收到退出信号即中断。
			ctx, cancel := context.WithTimeout(rootCtx, 5*time.Minute)
			defer cancel()
			if err := svcCtx.Updater.UpdateMihomo(ctx); err != nil {
				logx.Errorf("auto update mihomo failed: %v", err)
			} else if rootCtx.Err() == nil {
				// 关停已开始时不要重启内核：此时 MihomoManager.Stop 可能已经执行过，
				// 再 Restart 会拉起一个不受本进程管理的孤儿内核，占着代理端口
				// 直到用户手动杀掉。
				_ = svcCtx.MihomoManager.Restart(rootCtx)
			}
			if err := svcCtx.Updater.UpdateZashboard(ctx); err != nil {
				logx.Errorf("auto update zashboard failed: %v", err)
			}
			if rootCtx.Err() != nil {
				// 数据库可能已关闭，此时写库只会拿到 "database is closed"，
				// 且这次任务并未真正跑完，不该标记成功
				return
			}
			_ = svcCtx.Database.MarkTaskRun("version_check", "ok", "", time.Time{})
			svcCtx.Hub.Publish("task.progress", map[string]any{"name": "auto_update", "percent": 100})
		})
	})

	if err := svcCtx.SettingsService.LoadAndApply(); err != nil {
		logx.Errorf("load settings failed: %v", err)
	}

	// AdGuard 独立自动更新：与 mihomo/zashboard 的 JobAutoUpdate 分离。
	// 组件关闭或开关关闭时 SetAdGuardAutoUpdateJob(enabled=false)。
	if svcCtx.AdGuardService != nil {
		applyAdGuardSchedule := func() error {
			enabled := svcCtx.AdGuardService.ShouldRunAutoUpdate()
			cronExpr := svcCtx.AdGuardService.AutoUpdateCron()
			return svcCtx.Scheduler.SetAdGuardAutoUpdateJob(enabled, cronExpr, func() {
				ctx, cancel := context.WithTimeout(rootCtx, 5*time.Minute)
				defer cancel()
				// 以「期望运行」为准：更新途中进程可能已停，不能只看当前 Running
				wantRunning := svcCtx.AdGuardService.DesiredRunning()
				wasRunning := false
				if st, stErr := svcCtx.AdGuardService.Status(ctx); stErr == nil && st != nil {
					wasRunning = st.Running
				}
				if wasRunning {
					// 勿用 Stop：会清掉 enabled_at_boot，更新失败后面板重启不再自启
					if err := svcCtx.AdGuardService.StopProcess(ctx); err != nil {
						logx.Errorf("auto update adguard: stop failed: %v", err)
					}
				}
				if err := svcCtx.AdGuardService.Install(ctx); err != nil {
					logx.Errorf("auto update adguard failed: %v", err)
					if rootCtx.Err() == nil {
						svcCtx.Hub.Publish("task.progress", map[string]any{
							"name": "adguard_auto_update", "error": err.Error(),
						})
					}
					if (wantRunning || wasRunning) && rootCtx.Err() == nil {
						_ = svcCtx.AdGuardService.Start(rootCtx)
					}
					return
				}
				if (wantRunning || wasRunning) && rootCtx.Err() == nil {
					if err := svcCtx.AdGuardService.Start(rootCtx); err != nil {
						logx.Errorf("auto update adguard: restart failed: %v", err)
					}
				}
				if rootCtx.Err() != nil {
					return
				}
				_ = svcCtx.Database.MarkTaskRun("adguard_auto_update", "ok", "", time.Time{})
				svcCtx.Hub.Publish("task.progress", map[string]any{
					"name": "adguard_auto_update", "percent": 100,
				})
			})
		}
		svcCtx.AdGuardService.SetScheduleReloadFunc(applyAdGuardSchedule)
		if err := applyAdGuardSchedule(); err != nil {
			logx.Errorf("register adguard auto-update scheduler failed: %v", err)
		}
		reloadMgr.Register("adguard-auto-update-schedule", applyAdGuardSchedule)
	}

	if c.Bootstrap.EnsureOnStart {
		ensureCtx, ensureCancel := context.WithTimeout(rootCtx, 3*time.Minute)
		err := svcCtx.Updater.EnsureComponents(ensureCtx)
		ensureCancel()
		if err != nil {
			if c.Bootstrap.FailOnEnsureError {
				logx.Must(err)
			}
			logx.Errorf("bootstrap ensure failed: %v", err)
		}
	}

	// 远程配置拉取：调度由用户配置的 Cron 决定，支持运行期改动后即时重装
	svcCtx.SettingsService.SetRemotePullReloadFunc(func(enabled bool, cronExpr string) error {
		return svcCtx.Scheduler.SetRemotePullJob(enabled, cronExpr, func() {
			// 与订阅轮询同样必须带 deadline：合并期间持有 applyMu，
			// 上游卡死会让后续所有手动操作挂住。
			// 派生自 rootCtx，使关停时能中断——挂在 context.Background() 上
			// 的话，10 分钟的拉取会在数据库关闭后继续写库。
			ctx, cancel := context.WithTimeout(rootCtx, 10*time.Minute)
			defer cancel()
			if err := svcCtx.ConfigService.RunScheduledPull(ctx); err != nil {
				logx.Errorf("scheduled remote pull failed: %v", err)
				if rootCtx.Err() != nil {
					return // 关停中，数据库可能已关闭
				}
				_ = svcCtx.Database.MarkTaskRun("config_merge", "error", err.Error(), time.Time{})
				svcCtx.Hub.Publish("task.progress", map[string]any{"name": "remote_pull", "error": err.Error()})
				return
			}
			if rootCtx.Err() != nil {
				return
			}
			_ = svcCtx.Database.MarkTaskRun("config_merge", "ok", "", time.Time{})
			svcCtx.Hub.Publish("config.updated", map[string]any{"ok": true, "source": "scheduled_pull"})
		})
	})
	if err := svcCtx.SettingsService.ApplyRemotePullSchedule(); err != nil {
		logx.Errorf("register remote pull scheduler failed: %v", err)
	}

	// 注册热重载动作。
	//
	// 这两个函数正是启动时用的同一套入口：LoadAndApply 从数据库重读设置并
	// 重装自动更新 Cron，ApplyRemotePullSchedule 重装远程拉取 Cron。
	// 复用它们而不另写一份重载逻辑，避免「启动生效、重载不生效」这类分歧。
	// 端口、JWT 密钥、数据库路径来自配置文件且涉及监听/连接重建，
	// 不在热重载范围内——那些改动需要真正重启进程。
	reloadMgr.Register("settings", svcCtx.SettingsService.LoadAndApply)
	reloadMgr.Register("remote-pull-schedule", svcCtx.SettingsService.ApplyRemotePullSchedule)
	reloadMgr.Register("applog-cleanup-schedule", svcCtx.SettingsService.ApplyLogCleanupSchedule)
	registerSystemRoutes(server, svcCtx, reloadMgr)
	registerPublicAuthRoutes(server, svcCtx)
	registerSubscriptionProbeRoute(server, svcCtx)

	// 此处曾有一个「每分钟轮询、按各订阅自身 interval 逐条刷新」的任务，已移除。
	// 它与上面的远程拉取 Cron 构成两套并行调度：用户在配置中心关掉定时拉取后，
	// 后台仍会按订阅间隔回源并热重载内核，与界面呈现完全不符。
	// 现在订阅内容的刷新时机只由远程拉取 Cron 与手动「立即拉取并合并」决定。

	// publish mihomo status every 5s
	if _, err := svcCtx.Scheduler.AddJob("*/5 * * * * *", func() {
		// 关停中直接跳过：这个任务每 5 秒写一次库，正好落在
		// 「Scheduler 已停但仍有一轮在跑 / 数据库即将关闭」的窗口上
		if rootCtx.Err() != nil {
			return
		}
		st := svcCtx.MihomoManager.Status()
		status := "stopped"
		if st.IsRunning {
			status = "running"
		}
		// 设计 §6：持久化内核运行状态
		_ = svcCtx.Database.SaveMihomoState(st.Version, st.PID, status, time.Now())
		svcCtx.Hub.Publish("mihomo.status", map[string]any{
			"status":  status,
			"version": st.Version,
			"pid":     st.PID,
		})
	}); err != nil {
		logx.Errorf("register status publisher failed: %v", err)
	}

	// 日志归档清理：删除超过保留期的轮转归档。
	//
	// 与按大小轮转是两套互补的约束：轮转限制"单文件多大、留几份"，
	// 这里限制"留多久"。只有份数限制的话，日志量小的实例会一直留着
	// 几个月前的归档；只有时间限制的话，日志量大的实例可能在一天内
	// 就把磁盘写满。
	//
	// 调度可在系统设置里改，用与自动更新、远程拉取同一套重装机制，
	// 改完即时生效、无需重启进程。
	appLogPath := svcCtx.AppLogPath
	if appLogPath != "" {
		svcCtx.SettingsService.SetLogCleanupReloadFunc(func(enabled bool, cronExpr string) error {
			return svcCtx.Scheduler.SetLogCleanupJob(enabled, cronExpr, func() {
				if rootCtx.Err() != nil {
					return
				}
				runAppLogCleanup(appLogPath, "定时")
			})
		})
		if err := svcCtx.SettingsService.ApplyLogCleanupSchedule(); err != nil {
			logx.Errorf("register applog cleanup failed: %v", err)
		}

		// 启动时立即跑一次：进程可能停了很久，期间没有任何清理发生，
		// 等到下一个调度点才处理会让过期归档继续占着磁盘。
		go runAppLogCleanup(appLogPath, "启动时")
	}

	svcCtx.Scheduler.Start(rootCtx)

	// 透明代理的崩溃恢复：上次启用后若用户没来得及确认网络正常，进程就
	// 退出了（崩溃、宿主重启、手工 kill），数据库里会留下一条待确认记录。
	// 此时必须回到关闭状态——没有人确认过网络是通的，就不该让那套规则
	// 继续生效，而 TProxy 规则配错时用户可能连面板都访问不了。
	//
	// 必须在下面的合并之前：回滚会把开关关掉，之后的合并才会生成一份
	// 不带 tun / tproxy 的配置；反序的话内核会先带着旧开关状态起来。
	svcCtx.TransparentService.RecoverPending(rootCtx)

	// 补上 RecoverPending 覆盖不到的另一种不一致：TProxy 的规则不持久化
	// 到宿主重启，"已确认启用"的记录却会。ReconcileState 核实宿主上的
	// 规则是否还真实存在，不存在则回落为关闭，避免界面一直显示"已开启"
	// 而实际网络根本没被接管。同样要在合并之前，理由与上面一致。
	svcCtx.TransparentService.ReconcileState(rootCtx)

	// 部署设计 §7：启动流程应为 merge config → validate → start mihomo，
	// 此前直接跳过合并/校验直接起内核，若磁盘上的 config.yaml 因外部改动
	// 损坏或缺失，内核会带着坏配置启动且无自愈路径。
	// MergeAndApplyDetailed 内部已有"写入前备份 + 校验失败自动回滚"机制，
	// 因此这里合并失败也不阻断启动：回滚会保留磁盘上原有的可用配置，
	// 首次启动（配置文件不存在）失败则退化为无配置启动，交由用户手动处理。
	mergeCtx, mergeCancel := context.WithTimeout(rootCtx, 2*time.Minute)
	firstRun := !svcCtx.ConfigService.ConfigExists()
	// 启动时拉取一次远程：否则首次启动（或远程层缓存为空时）
	// 内核会带着一份没有远程内容的配置起来。
	// 未配置远程来源（none）时 buildRemoteConfig 会直接写空远程层，
	// 不会产生网络请求。
	if _, err := svcCtx.ConfigService.MergeAndApplyDetailed(mergeCtx, service.MergeWithRefresh(0)); err != nil {
		if firstRun {
			logx.Errorf("首次启动生成配置失败，将以无配置状态尝试启动内核: %v", err)
		} else {
			logx.Errorf("启动时刷新配置失败，将使用磁盘上现有的配置启动内核: %v", err)
		}
	}
	mergeCancel()

	if err := svcCtx.MihomoManager.Start(rootCtx); err != nil {
		logx.Errorf("mihomo start skipped: %v", err)
	}

	// httpSrv 由 StartWithOpts 的回调交出来，用于自己调用 Shutdown。
	//
	// 为什么不能依赖 go-zero 的优雅关停：它在 Windows 上是空实现。
	// core/proc/shutdown+polyfill.go 里 AddShutdownListener 直接把回调
	// 原样返回而不注册，SetTimeToForceQuit / Shutdown 都是空函数；
	// rest.Server.Stop() 也只调了 logx.Close()，并不停止监听。
	// 于是收到信号后本函数会关掉数据库，而 ListenAndServe 从没人叫停，
	// 监听继续存活、进程不退出，之后每个请求都撞
	// "sql: database is closed"，且端口一直被占用。
	//
	// 因此这里接管 http.Server 的生命周期：关停时自己调 Shutdown，
	// 让「停止接受新连接 → 等在途请求收尾 → 关数据库」的顺序真正成立。
	// 这条路径在 Linux/容器上同样有效（proc 的监听器只是多余地再关一次）。
	srvReady := make(chan *http.Server, 1)

	// shutdownDone 用于让 main 等待关停流程真正跑完，
	// 避免「监听已关、数据库已关、进程仍在」的中间态。
	shutdownDone := make(chan struct{})

	go func() {
		defer close(shutdownDone)

		quit := make(chan os.Signal, 1)
		// os.Interrupt 而非 syscall.SIGINT：Windows 上 Ctrl+C 递送的是前者。
		// 两者在 Unix 下等价，这样写可跨平台。
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

		select {
		case <-quit:
			logx.Info("收到退出信号，开始优雅关停")
		case <-reloadMgr.QuitRequested():
			logx.Info("收到重启请求，开始优雅关停")
		}
		cancel()

		// 关闭顺序：先停止接受新请求并等在途请求收尾，再停定时任务与内核，
		// 最后才关数据库。若先关 DB，正在执行的合并会在写完 config.yaml 后
		// 拿到 "database is closed"，导致磁盘配置与数据库记录不一致。
		var httpSrv *http.Server
		select {
		case httpSrv = <-srvReady:
		default: // 尚未启动完成，无需关闭
		}
		if httpSrv != nil {
			// 先让 WS 主动退出：Shutdown 不会关闭已劫持（hijacked）的连接，
			// 标准库在 hijack 时就把它们移出了 activeConn（net/http/server.go
			// 的 StateHijacked 分支），所以 WS 既不会拖慢 Shutdown，也不会被它
			// 唤醒——不主动通知的话那些读写 goroutine 会一直挂到进程退出。
			svcCtx.Hub.Close()

			shutCtx, shutCancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
			if err := httpSrv.Shutdown(shutCtx); err != nil {
				// 会吃满这个超时的是仍在处理的普通请求（合并配置、拉取订阅
				// 可长达数分钟），不是 WS。超时后继续关停，不能卡住整个流程，
				// 否则端口一直不释放。
				logx.Errorf("HTTP 优雅关停未在 %s 内完成: %v", httpShutdownTimeout, err)
			}
			shutCancel()
		}

		svcCtx.Scheduler.StopAndWait(schedulerStopTimeout)
		server.Stop() // 关闭 go-zero 的日志写入

		stopCtx, stopCancel := context.WithTimeout(context.Background(), kernelStopTimeout)
		defer stopCancel()
		_ = svcCtx.MihomoManager.Stop(stopCtx)
		// AdGuard 是可选常驻子进程，关停主进程时必须一并停掉，
		// 否则会留下占着 DNS/Web 端口的孤儿进程。
		if svcCtx.AdGuardManager != nil {
			_ = svcCtx.AdGuardManager.Stop(stopCtx)
		}

		// 释放 SQLite 句柄，避免文件被占用。
		// 此时 HTTP 服务已停、定时任务已收尾，不会再有人使用连接。
		if err := svcCtx.Database.Close(); err != nil {
			logx.Errorf("close database failed: %v", err)
		}
		logx.Info("shutdown complete")
	}()

	// SIGHUP 触发热重载，是 Unix 下重载配置的惯例（Windows 无此信号，
	// 该平台用 POST /api/v1/system/reload）。
	// 与关停 goroutine 分开：重载可以反复执行，不该与一次性的退出流程纠缠。
	watchReloadSignal(rootCtx, reloadMgr)

	fmt.Printf("Starting server at %s:%d\n", c.Host, c.Port)
	// 同源 /adguard-ui 反代：用登录 cookie 或 Bearer 鉴权，转发到 AdGuard Web UI。
	// 上游优先读 work-dir 里的 web 端口，且只允许回环地址，避免误反代到外网。
	aghProxy := adguard.NewProxyHandler(svcCtx.AdGuardManager, svcCtx.Config.Auth.AccessSecret, svcCtx.PasswordVer, func() string {
		if svcCtx.AdGuardManager == nil {
			return "127.0.0.1:3000"
		}
		st := svcCtx.AdGuardManager.Status()
		if st.WorkDir != "" {
			if port, err := adguard.ReadWebPort(st.WorkDir); err == nil && port > 0 {
				return fmt.Sprintf("127.0.0.1:%d", port)
			}
		}
		addr := strings.TrimSpace(st.WebAddr)
		if addr == "" {
			return "127.0.0.1:3000"
		}
		return addr
	}, svcCtx.AdGuardSSO)
	// 最外层包装静态资源分流：API/WS 走 go-zero，/adguard-ui 反代，其余走静态（含 SPA /adguard）文件（含 SPA 回退）
	server.StartWithOpts(func(svr *http.Server) {
		svr.Handler = staticFallback(svr.Handler, staticMux, aghProxy)
		applyServerTimeouts(svr, c)
		// 交给关停 goroutine，让它能自己调 Shutdown 真正停掉监听
		srvReady <- svr
	})

	// StartWithOpts 返回意味着监听已结束（Shutdown 已被调用）。
	// 等关停流程收尾后再退出，避免上面描述的中间态；设置上限以防某个
	// 环节卡死导致进程永不退出（那会一直占着端口）。
	select {
	case <-shutdownDone:
	case <-time.After(shutdownGraceTotal):
		logx.Error("等待关停流程超时，强制退出")
	}
}

func registerWebSocket(server *rest.Server, svcCtx *svc.ServiceContext) {
	// CheckOrigin 拒绝跨站浏览器连接，降低 token 泄露后被异源页面滥用的风险。
	// 空 Origin 放行：非浏览器客户端（部分探针/脚本）通常不带 Origin。
	// 令牌仍常走 query（浏览器 WS 难设 Authorization），Referer/日志泄露风险仍在，
	// 本批不改传输位置，避免牵动前端。
	upgrader := websocket.Upgrader{CheckOrigin: wsOriginAllowed}
	server.AddRoute(rest.Route{
		Method: http.MethodGet,
		Path:   "/ws",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			// 浏览器 WebSocket 无法自定义请求头，令牌通过 query 传递；
			// 未鉴权的连接可读取内核日志，必须拦截。
			if !verifyWSToken(r, svcCtx.Config.Auth.AccessSecret, svcCtx.PasswordVer) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()

			// http.Server 的 Read/WriteTimeout 会在连接被 Hijack 时残留为
			// 绝对 deadline，几十秒后就会掐断这条本应长活的日志推送连接。
			// 升级完成后清除，改由下方的读循环感知对端关闭。
			if nc := conn.NetConn(); nc != nil {
				_ = nc.SetReadDeadline(time.Time{})
				_ = nc.SetWriteDeadline(time.Time{})
			}

			// initial snapshot
			st := svcCtx.MihomoManager.Status()
			status := "stopped"
			if st.IsRunning {
				status = "running"
			}
			_ = conn.WriteJSON(map[string]any{
				"type": "mihomo.status",
				"data": map[string]any{"status": status, "version": st.Version, "pid": st.PID},
				"at":   time.Now(),
			})
			for _, line := range svcCtx.MihomoManager.Logs(50, "") {
				_ = conn.WriteJSON(map[string]any{"type": "log.message", "data": line, "at": time.Now()})
			}
			// 应用日志同样给一份初始快照，否则刚打开页面时列表是空的，
			// 用户要等到下一条日志产生才看得到内容
			for _, e := range svcCtx.AppLog.Snapshot(50, "") {
				_ = conn.WriteJSON(map[string]any{"type": "applog.message", "data": e, "at": time.Now()})
			}

			ch, unsub := svcCtx.Hub.Subscribe(64)
			defer unsub()

			// reader to detect close
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()

			for {
				select {
				case <-done:
					return
				case ev, ok := <-ch:
					if !ok {
						// Hub 已关闭，说明进程正在关停。
						//
						// 发一个正常的关闭帧再退出：不发的话前端只看到 TCP 断开，
						// 会按"网络异常"提示并立即重连，而此时服务端正在关停，
						// 重连必然失败、又触发下一轮退避，用户看到一串报错。
						// 明确的 CloseMessage 让前端能区分"服务端重启"这一情形。
						_ = conn.WriteControl(
							websocket.CloseMessage,
							websocket.FormatCloseMessage(websocket.CloseServiceRestart, "server restarting"),
							time.Now().Add(time.Second),
						)
						// 唤醒读 goroutine：它阻塞在 ReadMessage 上，且上面已把
						// read deadline 清零，只有置一个已过期的 deadline 才能让它
						// 立刻返回错误退出，否则会挂到进程结束。
						if nc := conn.NetConn(); nc != nil {
							_ = nc.SetReadDeadline(time.Now())
						}
						return
					}
					b, _ := json.Marshal(ev)
					if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
						return
					}
				}
			}
		},
	})
}

// staticFallback 包装在 go-zero handler 之外：
// API 走 go-zero，/adguard-ui 走 AGH 反代，/adguard 走 SPA，其余由静态文件处理。
// 这样可支持任意深度的静态资源路径与 SPA 客户端路由回退。
func staticFallback(apiHandler, static, adguardHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// /healthz 必须走 go-zero，否则会被静态服务返回 index.html，
		// 导致容器健康检查永远"通过"
		isAPI := strings.HasPrefix(path, "/api/") || path == "/ws" || path == "/healthz"

		// 这是唯一的最外层 Handler，安全响应头统一在此设置，
		// 避免各 handler 各写一份导致遗漏
		setSecurityHeaders(w, isAPI)

		if isAPI {
			apiHandler.ServeHTTP(w, r)
			return
		}

		// AdGuard Home Web UI 同源反代（iframe 嵌入管理端）
		// 仅 /adguard-ui 反代 AGH；/adguard 留给 Vue 路由（刷新仍留在 Aurora 壳内）
		if strings.HasPrefix(path, "/adguard-ui") {
			if path == "/adguard-ui" {
				http.Redirect(w, r, "/adguard-ui/", http.StatusMovedPermanently)
				return
			}
			if adguardHandler != nil {
				// AGH 前端资源同样压缩：反代响应与静态资源共享同源
				// 6 连接池，AGH 的 JS/CSS 裸传会拖慢 zashboard/管理端
				// 首屏（gzip.go 的流式安全同样适用：text/event-stream
				// 不在可压缩类型里，AGH 日志流不受影响）。
				staticGzipHandler(adguardHandler).ServeHTTP(w, r)
				return
			}
			http.Error(w, "adguard proxy unavailable", http.StatusServiceUnavailable)
			return
		}

		// AGH SPA 无 basename 时可能把 iframe 导航到站点根下的 /login.html、/control/*。
		// 已登录 Aurora 时把这些路径收回反代，避免 iframe 内嵌套整站 Aurora。
		if adguardHandler != nil && isLeakedAdGuardPath(path) {
			if c, err := r.Cookie("aurora_session"); err == nil && c != nil && strings.TrimSpace(c.Value) != "" {
				// 改写到 /adguard-ui 前缀再交给反代
				r2 := r.Clone(r.Context())
				if path == "/login.html" || path == "/login" {
					r2.URL.Path = "/adguard-ui" + path
				} else {
					r2.URL.Path = "/adguard-ui" + path
				}
				adguardHandler.ServeHTTP(w, r2)
				return
			}
		}

		// 静态资源（管理端 public/ 与 /ui zashboard）套 gzip：
		// 主 bundle 1.16MB 裸传是手机弱网首屏慢的主因（详见 gzip.go）。
		staticGzipHandler(static).ServeHTTP(w, r)
	})
}

// isLeakedAdGuardPath 判断是否为 AGH 前端从子路径逃逸到站点根的典型路径。
func isLeakedAdGuardPath(path string) bool {
	switch path {
	case "/login.html", "/login", "/install.html", "/install.html/":
		return true
	}
	if strings.HasPrefix(path, "/control/") || path == "/control" {
		return true
	}
	// AGH 静态资源（与 Aurora 的 /assets 可能冲突；仅在有 aurora_session 时才拦截）
	if strings.HasPrefix(path, "/assets/") && (strings.Contains(path, "apple-touch-icon") ||
		strings.Contains(path, "safari-pinned-tab") || strings.HasSuffix(path, "favicon.png")) {
		return true
	}
	return false
}

// setSecurityHeaders 设置基础安全响应头。
//
// 面板默认以明文 HTTP 暴露，因此不下发 HSTS（在无 TLS 的场景下 HSTS 会
// 把用户锁死在无法访问的 https:// 上）；由部署方在反向代理层按需添加。
func setSecurityHeaders(w http.ResponseWriter, isAPI bool) {
	h := w.Header()
	// 禁止 MIME 嗅探，避免用户上传的文本被当作可执行内容解释
	h.Set("X-Content-Type-Options", "nosniff")
	// 只允许同源内嵌：管理端要把挂在 /ui/ 的 zashboard 以 iframe 内嵌，
	// 用 DENY 会直接让内嵌页面白屏。SAMEORIGIN 仍能挡住第三方站点的
	// 点击劫持，因为 /ui/ 与管理端同源。
	h.Set("X-Frame-Options", "SAMEORIGIN")
	// 跨域跳转时不泄露完整 URL（可能含 share token 等凭据）
	h.Set("Referrer-Policy", "no-referrer")

	if isAPI {
		// API 响应不应被任何中间层或浏览器缓存：其中包含订阅内容、
		// 分享 token 等敏感数据
		h.Set("Cache-Control", "no-store")
		return
	}

	// 前端为自包含的 SPA，不依赖任何第三方源；frame-ancestors 与
	// X-Frame-Options 双重防护以兼容新旧浏览器。
	// 说明：Vue 运行时不需要 unsafe-eval，但内联样式仍需 unsafe-inline。
	h.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		// zashboard 的构建产物包含内联启动脚本与 eval 用法，
		// 它与管理端同源共用这份 CSP，因此必须放开这两项，
		// 否则内嵌面板会因 CSP 拦截而白屏。
		"script-src 'self' 'unsafe-inline' 'unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		// zashboard 需要直连内核的 external-controller（可能是任意
		// 主机/端口），故放开 http/https/ws/wss；管理端自身只用同源。
		"connect-src 'self' http: https: ws: wss:",
		// 允许把同源的 /ui/ 面板内嵌进管理端
		"frame-src 'self'",
		"frame-ancestors 'self'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; "))
}

// mountStatic 把静态目录挂到 go-zero 之外的原生 mux 上，
// 避免 go-zero 路由无法匹配任意深度路径的问题。
// fsysProvider 在每次请求时调用：管理端页面允许运行时在磁盘目录
// 与内嵌资源之间切换（见 getWebFS），/ui 的 zashboard 固定在磁盘。
func mountStatic(mux *http.ServeMux, routePrefix string, fsysProvider func() http.FileSystem) {
	if routePrefix == "/" {
		mux.Handle("/", spaFileSystemServer("", fsysProvider))
		return
	}
	prefix := strings.TrimSuffix(routePrefix, "/")
	handler := spaFileSystemServer(prefix, fsysProvider)
	mux.Handle(prefix+"/", handler)
	mux.Handle(prefix, http.RedirectHandler(prefix+"/", http.StatusMovedPermanently))
}

// registerHealthz 暴露无需鉴权的健康检查端点，
// 供容器 HEALTHCHECK、反向代理与编排系统的存活/就绪探测使用。
func registerHealthz(server *rest.Server, svcCtx *svc.ServiceContext) {
	server.AddRoute(rest.Route{
		Method: http.MethodGet,
		Path:   "/healthz",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			// 数据库不可用视为未就绪，让编排系统摘除流量。
			// 判定逻辑收敛在 repository.Healthy，避免两处各写一份。
			dbOK := svcCtx.Database.Healthy()

			st := svcCtx.MihomoManager.Status()
			body := map[string]any{
				"status":   "ok",
				"database": dbOK,
				// mihomo 未运行不影响本服务可用性，仅作信息展示
				"mihomo": map[string]any{
					"running": st.IsRunning,
					"version": st.Version,
					"pid":     st.PID,
				},
			}
			code := http.StatusOK
			if !dbOK {
				body["status"] = "degraded"
				code = http.StatusServiceUnavailable
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(body)
		},
	})
}

// wsOriginAllowed 限制浏览器跨站 WebSocket。
// 抽成独立函数便于单测，不依赖 gorilla 升级流程。
func wsOriginAllowed(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	// 只比 host（含端口），不比 scheme：面板可能 http 而 Origin 写 https（反代）。
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// 非法 Origin 一律拒绝，避免解析失败时放行
		return false
	}
	reqHost := r.Host
	if reqHost == "" {
		reqHost = r.Header.Get("Host")
	}
	return sameWSHost(u.Host, reqHost)
}

// sameWSHost 比较 Origin host 与请求 Host，忽略默认端口写法差异。
func sameWSHost(originHost, reqHost string) bool {
	normalize := func(h string) string {
		h = strings.ToLower(strings.TrimSpace(h))
		// 去掉方括号形式的 IPv6 以便比较时一致
		return h
	}
	o, rh := normalize(originHost), normalize(reqHost)
	if o == rh {
		return true
	}
	// example.com vs example.com:443 / :80 —— 仅当一侧无端口时做宽松匹配
	stripDefault := func(h string) (host string, port string, hasPort bool) {
		// 简易拆分：IPv6 [addr]:port 或 host:port
		if strings.HasPrefix(h, "[") {
			if i := strings.LastIndex(h, "]:"); i >= 0 {
				return h[:i+1], h[i+2:], true
			}
			return h, "", false
		}
		if i := strings.LastIndex(h, ":"); i >= 0 && strings.Count(h, ":") == 1 {
			return h[:i], h[i+1:], true
		}
		return h, "", false
	}
	oh, op, oHas := stripDefault(o)
	rh2, rp, rHas := stripDefault(rh)
	if oh != rh2 {
		return false
	}
	if !oHas && rHas && (rp == "80" || rp == "443") {
		return true
	}
	if !rHas && oHas && (op == "80" || op == "443") {
		return true
	}
	return false
}

// verifyWSToken 校验 WebSocket 连接携带的 JWT。
// 优先读 query token（浏览器 WS 惯例），其次 Authorization Bearer。
// ver 为口令版本闸门：改密后旧令牌即使签名有效也拒绝连接。
func verifyWSToken(r *http.Request, secret string, ver *auth.PasswordVer) bool {
	if secret == "" {
		return false
	}
	raw := strings.TrimSpace(r.URL.Query().Get("token"))
	if raw == "" {
		raw = auth.ExtractBearerToken(r)
	}
	if raw == "" {
		return false
	}
	claims, err := auth.ParseToken(raw, secret)
	if err != nil {
		return false
	}
	return auth.TokenVersionValid(claims, ver.Current())
}

// isPublicAPIPath 判断路径是否为无需 JWT 的公开 API 路由。
// 白名单必须与 docs/AuroraMihomo-Go-Zero-API.api 的 public 组保持一致
// （login / serveFile / shareByToken）：这些路径本就不带令牌鉴权，
// 带旧令牌请求也不应被口令版本闸门拦截——否则改密后用户无法重新登录。
// /ws 与 /healthz 不在列：前者令牌走 query 且由 verifyWSToken 独立校验，
// 后者无 Authorization 头，闸门对无头请求一律放行。
func isPublicAPIPath(p string) bool {
	return p == "/api/v1/auth/login" ||
		strings.HasPrefix(p, "/api/v1/file/") ||
		strings.HasPrefix(p, "/api/v1/share/")
}

// applyAuthHardening 在 go-zero rest.WithJwt 之外补一层口令版本闸门：
// 改密（admin_password_ver +1）后，携带旧版本 JWT 的请求立即 401，
// 实现无状态令牌的改密吊销。
//
// 设计约束：
//   - 只拦截「签名与有效期都通过」的令牌——签名错误/过期的请求仍由
//     rest.WithJwt 处理，401 行为与日志不变，本闸门不替换 go-zero 鉴权；
//   - 公开路径（登录/文件直链/分享）放行，避免前端对全部请求统一附加
//     Authorization 头（api.ts）时把改密后的登录请求误杀；
//   - 挂在 server.Use（最外层），对所有 go-zero 路由生效；
//     不依赖 routes.go（goctl 生成物），再生成不丢失。
func applyAuthHardening(server *rest.Server, svcCtx *svc.ServiceContext) {
	server.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isPublicAPIPath(r.URL.Path) {
				if raw := auth.ExtractBearerToken(r); raw != "" {
					if claims, err := auth.ParseToken(raw, svcCtx.Config.Auth.AccessSecret); err == nil &&
						!auth.TokenVersionValid(claims, svcCtx.PasswordVer.Current()) {
						http.Error(w, "token revoked", http.StatusUnauthorized)
						return
					}
				}
			}
			next(w, r)
		}
	})
}

// applyServerTimeouts 为原生 http.Server 补齐连接层超时。
//
// go-zero 的 RestConf.Timeout 只作用于单个请求的处理阶段（通过中间件实现），
// 不限制读取请求头/请求体与写响应的时长。缺少这些设置时，攻击者可以用极慢的
// 速度发送请求头持续占用连接（Slowloris），最终耗尽连接资源。
func applyServerTimeouts(svr *http.Server, c config.Config) {
	sec := func(v int) time.Duration { return time.Duration(v) * time.Second }

	if c.Server.ReadHeaderTimeoutSec > 0 {
		svr.ReadHeaderTimeout = sec(c.Server.ReadHeaderTimeoutSec)
	}
	if c.Server.ReadTimeoutSec > 0 {
		svr.ReadTimeout = sec(c.Server.ReadTimeoutSec)
	}
	if c.Server.WriteTimeoutSec > 0 {
		svr.WriteTimeout = sec(c.Server.WriteTimeoutSec)
	}
	if c.Server.IdleTimeoutSec > 0 {
		svr.IdleTimeout = sec(c.Server.IdleTimeoutSec)
	}
	if c.Server.MaxHeaderBytes > 0 {
		svr.MaxHeaderBytes = c.Server.MaxHeaderBytes
	}
}

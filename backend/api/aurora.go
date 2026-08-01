package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/handler"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/service"

	"github.com/golang-jwt/jwt/v4"
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
	staticMux := http.NewServeMux()
	mountStatic(staticMux, "/ui", filepath.Join(c.Mihomo.ConfigDir, "zashboard"))
	mountStatic(staticMux, "/", webRoot())
	// staticMux 在 server.StartWithOpts 中包装到最外层 Handler
	registerWebSocket(server, svcCtx)
	registerHealthz(server, svcCtx)

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
	// 最外层包装静态资源分流：API/WS 走 go-zero，其余走静态文件（含 SPA 回退）
	server.StartWithOpts(func(svr *http.Server) {
		svr.Handler = staticFallback(svr.Handler, staticMux)
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
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server.AddRoute(rest.Route{
		Method: http.MethodGet,
		Path:   "/ws",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			// 浏览器 WebSocket 无法自定义请求头，令牌通过 query 传递；
			// 未鉴权的连接可读取内核日志，必须拦截。
			if !verifyWSToken(r, svcCtx.Config.Auth.AccessSecret) {
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
// API / WebSocket 请求交给 go-zero 路由，其余全部由静态文件服务处理。
// 这样可支持任意深度的静态资源路径与 SPA 客户端路由回退。
func staticFallback(apiHandler http.Handler, static http.Handler) http.Handler {
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
		static.ServeHTTP(w, r)
	})
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

// webRoot 定位前端静态资源目录：
// 生产镜像使用 ./public，本地开发回退到 frontend/dist。
func webRoot() string {
	for _, dir := range []string{"./public", "./frontend/dist"} {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return dir
		}
	}
	return "./public"
}

func spaFileServer(routePrefix, dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, routePrefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = "index.html"
		}

		full := filepath.Join(dir, filepath.Clean("/"+rel))
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			// 命中真实文件，交给标准 FileServer（自动处理 MIME / Range / 缓存）
			http.StripPrefix(routePrefix, fileServer).ServeHTTP(w, r)
			return
		}

		// SPA 回退：把未知路径交给 index.html 由前端路由接管
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, index)
	})
}

// mountStatic 把静态目录挂到 go-zero 之外的原生 mux 上，
// 避免 go-zero 路由无法匹配任意深度路径的问题。
func mountStatic(mux *http.ServeMux, routePrefix, dir string) {
	if routePrefix == "/" {
		mux.Handle("/", spaFileServer("", dir))
		return
	}
	prefix := strings.TrimSuffix(routePrefix, "/")
	handler := spaFileServer(prefix, dir)
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

// verifyWSToken 校验 WebSocket 连接携带的 JWT。
// 优先读 Authorization 头（便于非浏览器客户端），其次读 token query 参数。
func verifyWSToken(r *http.Request, secret string) bool {
	if secret == "" {
		return false
	}
	raw := strings.TrimSpace(r.URL.Query().Get("token"))
	if raw == "" {
		if h := r.Header.Get("Authorization"); h != "" {
			raw = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if raw == "" {
		return false
	}
	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	return err == nil && token.Valid
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

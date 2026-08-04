package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/handler/protected"
	"auroramihomo/backend/api/internal/handler/public"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/applog"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// httpShutdownTimeout 是等待在途请求收尾的上限。
//
// 取 15s 是权衡：普通 API 请求远快于此；真正会拖长的是 /ws 长连接与
// 正在进行的订阅拉取，它们不该无限期阻塞关停。超时后仍继续后续步骤
// （停内核、关数据库），只是记一条日志——卡住不退比强制断连更糟，
// 那会让端口一直被占用。
const httpShutdownTimeout = 15 * time.Second

// 关停各阶段的上限。分别定义而非写死在调用处，是为了让下面的总预算
// 能由它们相加得出——曾经这里是 15+30+5=50s 的阶段预算配一个写死的
// 45s 总上限，最坏情况下 main 会在 Database.Close() 执行前就返回，
// 于是进程带着未释放的 SQLite 句柄退出，正是本次要修的那类问题。
const (
	schedulerStopTimeout = 30 * time.Second
	kernelStopTimeout    = 5 * time.Second

	// shutdownGraceTotal 必须严格大于各阶段之和，留出余量给阶段之间的
	// 日志与清理。改任一阶段超时都不需要再手工核对这个值。
	shutdownGraceTotal = httpShutdownTimeout + schedulerStopTimeout + kernelStopTimeout + 10*time.Second
)

// watchReloadSignal 监听 SIGHUP 并触发热重载。
//
// SIGHUP 在 Windows 上不存在，signal.Notify 对它是空操作，
// 因此该平台只能走 HTTP 接口——这也是 registerSystemRoutes 存在的原因之一。
// ctx 用于在关停时退出监听：Reload 会读数据库，若关停后仍响应信号，
// 就会撞上已关闭的连接池（"sql: database is closed"）。
func watchReloadSignal(ctx context.Context, mgr *service.ReloadManager) {
	ch := make(chan os.Signal, 1)
	notifyReloadSignal(ch)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				// 关停与信号可能同时到达，进入重载前再确认一次
				if ctx.Err() != nil {
					return
				}
				logx.Info("收到 SIGHUP，开始热重载配置")
				if err := mgr.Reload(); err != nil {
					logx.Errorf("热重载部分失败: %v", err)
				} else {
					logx.Info("热重载完成")
				}
			}
		}
	}()
}

// registerPublicAuthRoutes 注册无需 JWT 的鉴权辅助路由。
//
// logout 必须可在无 token / 过期 cookie 时调用：HttpOnly 的 aurora_session
// 只能由服务端 Set-Cookie 过期清掉，前端 JS 删不掉。
// 不改 goctl 生成的 routes.go，与 system 路由同模式手挂。
func registerPublicAuthRoutes(server *rest.Server, svcCtx *svc.ServiceContext) {
	server.AddRoutes([]rest.Route{
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/auth/logout",
			Handler: public.LogoutHandler(svcCtx),
		},
	})
}

// registerSystemRoutes 暴露热重载与重启接口。
//
// 挂在 /api/v1/system 下并复用 protected 的 JWT 中间件：这两个端点能改变
// 进程状态（重装定时任务、触发退出），必须鉴权，否则任何人都能让服务重启。
func registerSystemRoutes(server *rest.Server, svcCtx *svc.ServiceContext, mgr *service.ReloadManager) {
	authOpt := rest.WithJwt(svcCtx.Config.Auth.AccessSecret)

	server.AddRoutes([]rest.Route{
		// Bearer → aurora_session：给 /adguard-ui 等只认 cookie 的同源反代对齐会话
		{
			Method:  http.MethodPost,
			Path:    "/api/v1/auth/session",
			Handler: public.SyncSessionHandler(svcCtx),
		},
		{
			Method: http.MethodPost,
			Path:   "/api/v1/system/reload",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				err := mgr.Reload()
				count, last := mgr.Stats()
				body := map[string]any{
					"reloaded":     err == nil,
					"reload_count": count,
					"last_reload":  last.Format(time.RFC3339),
				}
				if err != nil {
					// 部分动作失败仍返回 200 之外的状态码，但要把细节带出来：
					// 调用方需要知道是哪一项没重装成功
					body["error"] = err.Error()
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(body)
					return
				}
				httpx.OkJson(w, body)
			},
		},
		{
			Method: http.MethodPost,
			Path:   "/api/v1/system/restart",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				// 先应答再触发退出：关停会关闭监听，若先触发，
				// 这条响应可能写不回去，调用方只看到连接被断开。
				httpx.OkJson(w, map[string]any{
					"restarting": true,
					"message":    "已开始优雅关停，需由进程管理器拉起（systemd / docker restart / NSSM）",
				})
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				// 异步触发，让当前请求正常结束后再进入关停
				go func() {
					time.Sleep(100 * time.Millisecond)
					mgr.RequestQuit("HTTP /api/v1/system/restart")
				}()
			},
		},
		{
			// 应用自身日志的历史查询。与内核日志的 /api/v1/mihomo/logs 对称，
			// 但支持 limit 与 level 查询参数——应用日志量更大且分级别，
			// 只看 error 是最常见的排查方式。
			Method: http.MethodGet,
			Path:   "/api/v1/system/logs",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				limit := parseLimit(r.URL.Query().Get("limit"), appLogDefaultLimit, appLogMaxLimit)
				level := applog.ParseLevel(r.URL.Query().Get("level"))

				entries := svcCtx.AppLog.Snapshot(limit, level)
				httpx.OkJson(w, map[string]any{
					"logs":  entries,
					"total": svcCtx.AppLog.Len(),
				})
			},
		},
		{
			// 清空内存缓冲。不动已落盘的文件——那份是事后回溯用的，
			// 界面上的"清空"只该影响当前查看的列表。
			Method: http.MethodDelete,
			Path:   "/api/v1/system/logs",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				svcCtx.AppLog.Clear()
				httpx.OkJson(w, map[string]any{"cleared": true})
			},
		},
	}, authOpt)
}

// registerSubscriptionProbeRoute 挂订阅流量参数探测接口。
//
// V2Board 类机场只在特定 flag 参数下下发 subscription-userinfo 头，
// 探测接口对订阅 URL 逐一尝试常见参数并返回「有流量信息且节点完整」
// 的组合，供订阅表单一键应用。走 JWT 鉴权（探测会真实拉取外部 URL，
// 不能开放给匿名调用）；字面路径 /subscriptions/probe 优先于 goctl 的
// /subscriptions/:id 参数路由，不冲突。
func registerSubscriptionProbeRoute(server *rest.Server, svcCtx *svc.ServiceContext) {
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/v1/subscriptions/probe",
		Handler: protected.ProbeSubscriptionHandler(svcCtx),
	}, rest.WithJwt(svcCtx.Config.Auth.AccessSecret))
}

const (
	appLogDefaultLimit = 200
	// appLogMaxLimit 与内存缓冲上限一致：请求更多也没有意义
	appLogMaxLimit = 1000
)

// runAppLogCleanup 执行一次日志归档清理并记录结果。
//
// reason 只用于日志措辞（"定时"/"启动时"），便于在日志里区分触发来源。
// 清理无删除时不打日志：这个任务每天都跑，绝大多数时候无事可做，
// 每次都记一条会把真正有信息量的日志冲淡。
func runAppLogCleanup(path, reason string) {
	days := applog.RetentionDays()
	res, err := applog.CleanupArchives(path, days)
	if err != nil {
		logx.Errorf("%s清理日志归档失败: %v", reason, err)
		return
	}
	if res.Removed > 0 {
		logx.Infof("%s清理日志归档：保留 %d 天，删除 %d 个文件、释放 %.1f MB",
			reason, days, res.Removed, float64(res.Bytes)/(1<<20))
	}
}

// parseLimit 解析 limit 参数，非法或缺省时用 def，并夹到 max 以内。
// 不返回错误：日志查询的容错应偏向"给点东西看"而不是报 400。
func parseLimit(raw string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// notifyReloadSignal 注册重载信号。信号常量随平台不同，
// 具体注册放在带 build tag 的文件里（signal_unix.go / signal_windows.go）。
var notifyReloadSignal = func(ch chan<- os.Signal) {
	if sig := reloadSignal(); sig != nil {
		signal.Notify(ch, sig)
	}
}

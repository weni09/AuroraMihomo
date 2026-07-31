# Go / go-zero 后端开发规范（backend）

本文件是 AuroraMihomo 后端（`backend/`）的开发规范，供 AI 编码代理与开发者共同遵循。跨前后端的项目级内容（整体架构、Makefile 命令、规范强制机制、Skills）见仓库根目录 `AGENTS.md`；前端规范见 `frontend/AGENTS.md`。

## 技术栈

| 层 | 选型 |
|---|---|
| Web 框架 | go-zero `rest`（v1.10.2），代码由 goctl 1.7.3 生成 |
| ORM | gorm + `github.com/libtnb/sqlite`（纯 Go 驱动，无需 CGO） |
| 数据库 | SQLite（`./data/aurora.db`，路径由 `DataSource` 配置） |
| 鉴权 | `golang-jwt/jwt/v4` |
| 实时推送 | `gorilla/websocket` |
| 定时任务 | `robfig/cron/v3` |
| 脚本执行 | `dop251/goja`（跑 Sub-Store JS） |

单体部署：一个 Go 二进制同时提供 HTTP API 与前端静态资源。**没有独立的 rpc/gRPC 服务。**

**全程 `CGO_ENABLED=0`。** SQLite 驱动是纯 Go 实现，因此五个目标平台
（linux/darwin/windows × amd64/arm64）都能直接交叉编译，产物为静态二进制，
Alpine 与 Debian/Ubuntu 通用。**不要引入需要 CGO 的依赖**——那会让发布流程
重新背上为每个平台准备 C 工具链的负担。新增依赖后请用
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./backend/api` 自查。

## 目录结构

```
backend/api/                  服务入口与 go-zero 生成物
  aurora.go                   main：加载配置、挂载静态资源、注册 WebSocket
  etc/aurora-api.yaml         全部可调参数
  internal/config/            配置结构体 + 环境变量覆盖
  internal/handler/           goctl 生成的路由与 handler（public/ 与 protected/ 分组）
  internal/logic/             业务逻辑，主要开发区
  internal/svc/               ServiceContext，唯一的依赖构造入口
  internal/types/             goctl 生成的请求/响应类型
backend/internal/             领域实现，被 logic 层调用
  auth/ domain/ engine/ fetcher/ mihomo/ model/ realtime/
  repository/ scheduler/ service/ substore/ updater/
docs/AuroraMihomo-Go-Zero-API.api   API 规格（对外契约）
migrations/                   手写 SQL 迁移（001_*.sql 起顺序执行）
```

## 代码生成流程

### API 修改流程（强制）

```
1. 先改 docs/AuroraMihomo-Go-Zero-API.api
       ↓
2. 执行 goctl 生成代码
       ↓
3. 核对生成结果（见下"生成后必须核对"）
       ↓
4. 继续后续改动（logic / service / 前端）
```

代理可以自行运行 goctl。但生成是**覆盖式**的，所以有两条硬要求：

- **生成前确认工作区干净**（至少 `backend/api/internal/` 下无未提交的手写改动），
  以便生成结果异常时能一键还原。
- **生成后必须核对**：`routes.go` 与 `types.go` 的 `DO NOT EDIT` 头仍在、
  新增路由与类型确实出现、既有 handler 未被意外重写、`go build ./backend/...`
  通过。goctl 对已存在的 handler/logic 文件默认跳过而不覆盖，
  但新增 handler 会生成空的 logic 骨架，需要自己填实现。

若改了 Request/Response 结构体名，必须在生成后同步修正 logic 层的出入参类型。

`--style goZero` 是本项目既有生成物的风格，重新生成时须保持一致。
本项目的生成命令（在仓库根目录执行）：

```bash
goctl api go -api docs/AuroraMihomo-Go-Zero-API.api -dir backend/api --style goZero
```

`internal/handler/routes.go` 与 `internal/types/types.go` 带 `DO NOT EDIT` 头，禁止手改。handler 文件除请求解析/响应写法需要调整外也不要手改。

### 数据库修改流程（强制）

```
1. 在 migrations/ 新增顺序编号的 SQL 文件
       ↓
2. 提交 SQL 等待用户确认
       ↓
3. 由用户执行迁移
       ↓
4. 确认完成后，方可继续
```

本项目用 gorm 手写 model（`backend/internal/model/`），**不使用 `goctl model` 生成**，也没有 `_gen.go` 文件。

## 分层约定

- **handler**：goctl 生成。只做解码、调 logic、写响应。
- **logic**：`backend/api/internal/logic/{public,protected}/`，每个用例一个文件（如 `listCollectionsLogic.go`）。持有 `logx.Logger`、`ctx`、`svcCtx`。
- **service / repository**：`backend/internal/`，领域逻辑与数据访问。SQL 只能出现在 repository 层。
- **ServiceContext**：唯一的依赖构造入口。不要在 `svc.NewServiceContext` 之外自行打开数据库或建客户端。

## 错误处理

- 不静默忽略错误。CI 启用了 `errcheck`，丢弃错误返回值会直接失败。
- logic 层直接返回 `error`，由 handler 的 `httpx.ErrorCtx` 统一响应。本项目**不使用** `errorx.NewCodeError`。
- 需要被调用方判别的错误定义为包级哨兵变量，形如 `var ErrShareExpired = errors.New("...")`，用 `errors.Is` 比较（CI 的 `errorlint` 会拦住 `==` 比较和错误包装误用）。
- 记录日志用 logic 自带的 `l.Error(...)` / `l.Info(...)`，而非全局 `logx`。

## 配置

所有可调参数放 `backend/api/etc/aurora-api.yaml`，不要硬编码。敏感项支持环境变量覆盖（见 `internal/config/env.go` 的 `ApplyEnvOverrides`）。

改动超时、白名单、更新源等行为时，在 yaml 里就近写注释说明**为什么**取这个值 —— 现有配置文件已是这个风格，请保持。

## 关停与热重载

go-zero 的优雅关停在 Windows 上是**空实现**（`core/proc/shutdown+polyfill.go` 里
`AddShutdownListener` 不注册回调，`rest.Server.Stop()` 只关日志）。因此
`backend/api/aurora.go` 自己持有 `*http.Server` 并调用 `Shutdown`，
关停顺序是「停止接受新连接 → 等在途请求 → 停调度 → 停内核 → 关数据库」。
**不要改成依赖 `server.Stop()`**：那会让数据库先关而监听仍在，
后续请求全部报 `sql: database is closed`，且端口不释放。

改设置优先走热重载，不要重启进程：

| 方式 | 平台 | 作用 |
|---|---|---|
| `SIGHUP` | Unix | 重装 Cron 与运行时设置，不断连接 |
| `POST /api/v1/system/reload` | 全平台 | 同上（Windows 无 SIGHUP，用这个） |
| `POST /api/v1/system/restart` | 全平台 | 优雅退出，等进程管理器拉起 |

两个端点都需 JWT。端口、JWT 密钥、数据库路径来自配置文件，涉及监听/连接重建，
不在热重载范围内。进程**不做 fork 自重启** —— Windows 没有 fork、
监听 socket 无法继承，自行重建端口会有双实例抢占窗口，拉起交由
systemd / docker restart / NSSM / `make run-supervised`。

WebSocket 需要单独处理：连接被 hijack 后标准库会把它从 `activeConn` 移除
（`net/http/server.go` 的 `StateHijacked` 分支），所以 `Shutdown` 既不等它、
也不会唤醒它。关停时先调 `Hub.Close()` 让写循环退出，发 close code 1012
（`CloseServiceRestart`）告知前端，再用 `SetReadDeadline(now)` 唤醒读 goroutine。
前端 `useRealtime` 识别 1012 后进入 `restarting` 状态、改用固定间隔重连，
不再按退避狂试。

### 长任务的取消语义

关停会 cancel 根 context，因此后台任务与 cron 闭包**必须派生自 `rootCtx`**，
不要用 `context.Background()` —— 否则 cancel 够不到，任务会在数据库关闭后继续
写库，而这类写入多被 `_ =` 丢弃，故障完全无声。

配置合并/恢复的取消检查点一律设在 `writeConfigAtomically` **之前**：

- 落盘前取消：什么都还没改，直接返回，无需回滚
- 落盘后不再检查：磁盘已是新配置，中止会留下「磁盘新、数据库旧」的不一致
  （界面读 `merged` 记录，会一直显示旧内容）

即**取消可以避免「白做一场」，但不制造「做一半」**。新增耗时步骤时遵循这条分界线。
订阅拉取是串行的 N×30 秒，是最需要检查点的一段。
`repository.Database.Closed()` 供关停期间的任务在写库前自查，避免发出注定失败的写入。

## 日志

两路日志，刻意分开：

| 来源 | 采集 | 实时事件 | 历史接口 |
|---|---|---|---|
| mihomo 内核 | `internal/mihomo` 读子进程 stdout/stderr | `log.message` | `GET /api/v1/mihomo/logs` |
| 本项目自身 | `internal/applog` 实现 `logx.Writer` | `applog.message` | `GET /api/v1/system/logs?limit=&level=` |

混成一条流会让人分不清"内核说的"和"本程序说的"，排查时反而更费劲。

接入方式是 `logx.AddWriter`（不是 `SetWriter`）——保留 go-zero 已装好的控制台
writer，本 writer 只多收一份。**必须在 `rest.MustNewServer` 之后注册**：
`logx.SetUp` 用 `setupOnce` 保护且会直接覆盖 writer，早于它注册会被丢掉。
当前注册点在 `svc.NewServiceContext`（它正好在 `MustNewServer` 之后调用）。

写日志路径上有两条硬约束：

- **`applog` 的写入链路里不得调用 logx**（含 `Buffer.Append` 的订阅者、
  `fileSink` 的错误处理）。日志写入 → 通知订阅者 → 订阅者打日志 → 再次写入
  就是无限递归。落盘失败一律静默丢弃，内存缓冲仍可用。
- **回调期间不持订阅者锁**。Go 的 `RWMutex` 不可重入且写锁优先：在 `RLock`
  内直接遍历回调时，若回调又触发写入、同时有 `Subscribe` 在等写锁，
  嵌套的 `RLock` 会被永久饿死。`applog.Buffer` 与 `mihomo.ProcessManager`
  都先把订阅者拷出再回调，并有测试固定
  （`TestNestedAppendNotStarvedBySubscribe`）。

默认过滤 HTTP 访问日志与框架 stat（`AppLog.IncludeAccessLog: false`）：
go-zero 对每个请求写一条，而前端的实时通道与状态推送本身就在产生请求，
收录后会把业务日志冲走。排查请求链路时再临时打开。

落盘在 `<ConfigDir>/logs/aurora.log`。磁盘占用由两套互补的约束共同兜住：

- **按大小轮转**（`AppLog.MaxFileMB` × `MaxBackups`，默认 8MB × 5 份）——
  限制"单文件多大、留几份"，防止日志量大的实例一天内写满磁盘
- **按时间清理**（系统设置里的「日志保留天数」，默认 7 天，范围 1~365）——
  限制"留多久"，防止日志量小的实例一直留着几个月前的归档

清理只删轮转归档，**不动当前正在写的 `aurora.log`**：一个长期运行且日志稀少
的实例，其当前文件的 mtime 可能很旧，按时间删它会丢掉正在用的日志。
判据用文件 mtime 而非文件名里的时间戳——两者等价（归档后不再写入），
但不必解析文件名，也不会因将来改命名格式而失效。

保留天数与清理调度都存在数据库 `settings` 表（`applog.retention_days`、
`applog.cleanup_cron`、`applog.cleanup_enabled`），经 `SettingsService` 读写，
改动即时生效且跨重启保持。清理任务默认每天凌晨 3:30 执行（避开整点，
那里通常已有别的任务），并在启动时补跑一次——进程可能停了很久，
等到下一个调度点才清会让过期归档继续占着磁盘。

### 可重装的定时任务

三类任务支持运行期换调度、无需重启：自动更新、远程配置拉取、日志清理。
它们共用 `Scheduler.SetJob(name, enabled, spec, cmd)`：

- 此前每类各有一对 `xxxID`/`hasXxx` 字段与一个几乎相同的 `SetXxxJob`，
  加一类就复制一遍。现在按名字索引（`JobAutoUpdate` 等常量），
  新增任务只是多一个常量 + 一个 `SettingsService.ApplyXxxSchedule`。
- `enabled=false` 返回 nil——"关掉定时"是正常状态而非错误。
- **spec 非法时保持"旧任务已移除、新任务未装"并返回错误。** 让改坏的表达式
  继续按旧调度跑，会造成"界面显示新值、实际按旧值执行"的错觉，比停掉更难查。
  调用方应在落库前用 `NormalizeCron` 校验（5 段会自动补秒成 6 段）。
- 数据库里的调度值被手工改坏时，读取侧回退默认值而不是让任务装载失败、
  清理彻底停摆。

新增这类任务时记得在 `aurora.go` 里 `reloadMgr.Register(...)` 注册对应的
`ApplyXxxSchedule`，否则 SIGHUP 与 `/api/v1/system/reload` 不会重装它。

界面的"清空"只清内存缓冲，不动文件——那份是事后回溯用的。

## 静态检查

CI（`.github/workflows/ci.yml`）会跑：`gofmt` 检查、`go vet`、`golangci-lint v2.6.2`、`go build`、`go test -race -cover`。

golangci-lint 启用：`errcheck` `govet` `ineffassign` `staticcheck` `unused` `bodyclose` `noctx` `errorlint` `misspell`。配置见 `.golangci.yml`。

`scripts/check-conventions.py` 中与后端相关的可机检规则（详见根目录 `AGENTS.md`「规范如何被强制」一节）：

| 规则 | 内容 |
|---|---|
| BE1 | SQL 只能出现在 `internal/repository` 与 `internal/model` |
| BE2 | goctl 生成物的 `DO NOT EDIT` 头必须完好（丢失即疑似手改） |
| BE3 | logic 层用 `l.Error` / `l.Info`，不直接调全局 `logx.Error` |

## 提交前必须通过的检查

```bash
make fmt-check   # gofmt 格式校验
make vet         # go vet
make test        # go test ./backend/...
make lint        # golangci-lint（未纳入默认 check，见下方说明）
```

或在仓库根一次跑完（含前端）：`make check` / `make check-all`（Windows 若只有 mingw32-make 则用 `mingw32-make`）。

`make lint` 未纳入默认 `check`：本地 golangci-lint 若由低版本 Go 构建会拒绝分析 go1.25 项目，CI 用 v2.6.2 无此问题。

## 命名

| 元素 | 约定 | 示例 |
|---|---|---|
| 包名 | 简短、小写、无下划线 | `mihomo`、`substore` |
| 接口 | 描述性名词或 `-er` 后缀 | `Fetcher` |
| 构造函数 | `NewXxx` / `MustNewXxx` | `NewServiceContext` |
| 错误变量 | `ErrXxx` | `ErrShareExpired` |
| logic 文件 | 小驼峰 + `Logic.go`（goctl 风格） | `listCollectionsLogic.go` |

## 测试

- 测试与源码同包，`foo_test.go`。
- 优先表驱动。
- 已有集成测试与安全测试在 `backend/api/`（`integration_test.go`、`security_test.go`、`securityheaders_test.go`），新增端到端行为时参考其写法。
- 新代码提交前跑 `make test-race`。

## 注释

业务注释使用中文，说明**意图、边界、失败路径与为什么这样做**，不复述代码。标识符仍用英文。

现有代码（如 `aurora.go` 的静态资源分流、`aurora-api.yaml` 的超时说明、`.golangci.yml` 的 errcheck 动机）已建立了"解释权衡与踩过的坑"的注释风格，新增代码请对齐这一密度与深度。

导出符号需有文档注释。注意 `.golangci.yml` 中 staticcheck 的 ST1000/ST1003/ST1020/ST1021/ST1022 相关设置。

## 类型去重

新增 API 类型前，先 grep 检索 `.api` 规格与 `internal/types`，避免重复定义造成冲突。

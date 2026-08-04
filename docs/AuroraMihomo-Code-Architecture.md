# AuroraMihomo Code Architecture

> 本文描述**当前实现**的代码结构。早期草案中的 `cmd/server`、`apps/api`、`pkg/` 等布局已废弃，以仓库实况与 `AGENTS.md` 为准。

## 1. Overview

AuroraMihomo 是 Mihomo（Clash.Meta）配置与运行时管理平台：

- 单体部署：一个 Go 二进制同时提供 HTTP API 与前端静态资源
- 无独立 rpc/gRPC 服务
- Go module：`auroramihomo`

设计目标：

- 模块边界清晰、可单测
- 适合长驻进程（内核托管、定时任务、透明代理）
- Sub-Store 算子在 Go 内通过 goja 执行，不依赖外置 Node 服务

------------------------------------------------------------------------

## 2. Repository Structure

```
AuroraMihomo/
├── backend/
│   ├── api/                      # go-zero rest 入口（main 在 aurora.go）
│   │   ├── aurora.go
│   │   ├── etc/aurora-api.yaml
│   │   └── internal/
│   │       ├── config/
│   │       ├── handler/          # goctl 生成 + 少量手写
│   │       ├── logic/            # 业务编排（public / protected）
│   │       ├── svc/              # ServiceContext 依赖注入
│   │       └── types/            # goctl 生成的 API 类型
│   └── internal/                 # 领域实现（与 api 分层）
│       ├── adguard/              # 可选 AdGuard Home 子进程与反代
│       ├── applog/
│       ├── auth/                 # 密码哈希、登录限流、JWT 辅助
│       ├── domain/               # 配置领域模型
│       ├── engine/               # MergeEngine（Base/Remote/Override）
│       ├── fetcher/              # 远程订阅拉取
│       ├── mihomo/               # 内核进程管理
│       ├── model/                # 持久化模型
│       ├── netcheck/             # 透明代理环境检测与规则
│       ├── realtime/             # WebSocket hub
│       ├── repository/           # SQLite / gorm，唯一允许写 SQL 的层
│       ├── scheduler/            # 后台定时任务
│       ├── service/              # 应用服务（配置、设置、渲染、透明代理…）
│       ├── substore/             # 订阅解析与管道算子（含 goja）
│       ├── updater/              # mihomo / Zashboard / AdGuard 更新
│       └── version/
├── frontend/                     # Vue 3 + Vite + Pinia + Tailwind
├── docs/                         # 设计与 API 规格（.api）
├── migrations/                   # 手写 SQL 迁移
├── public/                       # 前端构建产物（make build-frontend 同步）
├── scripts/                      # 规范机检等
└── userdocs/                     # 用户文档原件
```

入口二进制：`go build -o auroramihomo ./backend/api`

------------------------------------------------------------------------

## 3. Request Path

```
浏览器
  → backend/api（go-zero rest + 静态资源 / Zashboard /ui/）
      → logic（public 免登录 / protected JWT）
          → internal/service
              → internal/repository → SQLite
          → internal/mihomo（进程）
          → internal/substore（渲染分享、管道）
          → internal/fetcher（拉订阅）
```

公开端点（仅 token，无 JWT）：

- `GET /api/v1/share/:token` — 订阅/组合分享
- `GET /api/v1/file/:token` — 文件分享

公开渲染路径会剥离不安全算子（如任意 JS 模板），见 `substore.StripPublicUnsafeOps`。

------------------------------------------------------------------------

## 4. Module Responsibilities

| 包 | 职责 |
|---|---|
| `mihomo` | 内核启停、重载、日志、版本探测 |
| `engine` | 语义合并、冲突策略、产出 config.yaml |
| `substore` | 节点解析、管道算子、分享渲染、goja 脚本 |
| `fetcher` | http(s) 订阅下载；拒绝非 http 与云 metadata/link-local |
| `scheduler` | 订阅刷新、自动更新等定时任务 |
| `updater` | 下载并校验 mihomo / Zashboard / AdGuard 发布包 |
| `netcheck` | TUN/TProxy 环境检测、防火墙规则、面板出站 fwmark |
| `auth` | PBKDF2 口令、登录限流、TrustedProxies |
| `service` | 跨包用例编排，供 logic 调用 |
| `repository` | 唯一 SQL 边界（规范 BE1） |

------------------------------------------------------------------------

## 5. Dependency Direction

允许：

```
api/logic → service → repository → model
         ↘ engine / mihomo / substore / fetcher / updater …
```

避免：

- `engine` / `mihomo` / `substore` 依赖 `api`
- repository 以外的包写 SQL

依赖注入：`svc.NewServiceContext` 手工构造，配置来自 go-zero `config.Config`。

------------------------------------------------------------------------

## 6. Auth & Session

- 管理员单用户；口令 PBKDF2 存 SQLite `settings.admin_password`
- 首次启动生成随机口令，写入 `data/initial_password.txt` 并打印一次；**成功登录或改密后删除该文件**
- JWT HS256；`Authorization` 与 HttpOnly cookie `aurora_session` 双通道（后者供 AdGuard 反代等同源场景）
- 登录失败按来源 IP 限流；仅 TrustedProxies 命中时采信 XFF

------------------------------------------------------------------------

## 7. Logging & Realtime

- 应用日志：go-zero `logx` + `applog.Buffer`（界面可查）与可选落盘
- 内核日志：`mihomo.Manager` 订阅后经 `realtime.Hub` WebSocket 推送
- WS 连接校验 Origin（与 Host 同站），降低跨站滥用面

------------------------------------------------------------------------

## 8. Testing

- 单测：`go test ./backend/...`（MergeEngine、fetcher 协议/SSRF、substore 算子、auth…）
- 前端：`vitest` + `vue-tsc` + eslint
- 门禁：`make check`；CI 在 PR / main / release 调用

规范机检见 `scripts/check-conventions.py` 与 `AGENTS.md`。

------------------------------------------------------------------------

## 9. Related Docs

- 跨层规范：`AGENTS.md`、`backend/AGENTS.md`、`frontend/AGENTS.md`
- API 契约：`docs/AuroraMihomo-Go-Zero-API.api`
- 用户文档：`userdocs/user-guide.md`
- 透明代理：`docs/AuroraMihomo-Transparent-Proxy.md`

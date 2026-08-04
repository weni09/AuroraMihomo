# AGENTS.md

本文件为 AuroraMihomo 仓库的项目级开发规范，供 AI 编码代理与开发者共同遵循。后端专属规范见 `backend/AGENTS.md`，前端专属规范见 `frontend/AGENTS.md`；本文件只保留跨前后端的内容。

## 项目概述

AuroraMihomo 是 Mihomo（Clash.Meta）配置管理平台：管理订阅、合并配置、检测冲突、托管 Mihomo 内核与 Zashboard 面板。

单体部署：一个 Go 二进制同时提供 HTTP API 与前端静态资源。**没有独立的 rpc/gRPC 服务。**

```
浏览器 → backend/api（go-zero rest）→ internal/service → internal/repository → SQLite
                    ↓
         internal/mihomo（进程管理）→ mihomo 内核
         internal/substore（goja 执行 Sub-Store 脚本）
```

Go module 名：`auroramihomo`

## 技术栈概览

| 层 | 选型 | 详细规范 |
|---|---|---|
| 后端 | Go + go-zero `rest` + gorm + SQLite | `backend/AGENTS.md` |
| 前端 | Vue 3 + Vite + TypeScript + Tailwind 3 | `frontend/AGENTS.md` |

## 目录结构

```
backend/                      后端源码（go-zero 服务入口 + 领域实现），见 backend/AGENTS.md
frontend/                     Vue 3 前端源码，见 frontend/AGENTS.md
docs/AuroraMihomo-Go-Zero-API.api   API 规格（对外契约）
migrations/                   手写 SQL 迁移（001_*.sql 起顺序执行）
public/                       前端构建产物（由 make build-frontend 同步，不要手改）
scripts/                      开发规范机检脚本（check-conventions.py）
.agents/skills/                本仓库可用的 AI 编码代理技能，见下方「Skills」
```

## 开发命令

统一走 Makefile（Windows 下在 Git Bash 执行）：

```bash
make deps            # 安装前后端依赖
make dev             # 仅启动后端；前端另开 cd frontend && npm run dev
make build           # 前端构建 → 同步 public/ → 编译后端二进制
make run-supervised  # 常驻运行，进程退出后自动重启（配合 /system/restart）
make check           # 提交前跑这个：fmt-check vet test type-check lint-frontend test-frontend conventions
make check-all       # 再加上 golangci-lint
make test            # go test ./backend/...
make test-race       # 带竞态检测
make cover           # 各包覆盖率
make fmt             # gofmt -l -w ./backend
make type-check      # 前端 vue-tsc
make lint-frontend   # 前端 eslint（强制「禁止 any」等规则）
make test-frontend   # 前端 vitest（一次性）；watch 用 make test-frontend-watch
make conventions     # 校验本文档与 backend/frontend AGENTS.md 中可机检的规范条款
make docker          # 构建镜像
```

注意作用域是 `./backend/...` 而非 `./...`（仓库根有构建产物二进制，全局通配会误伤）。

Windows 若只有 `mingw32-make`，把上面的 `make` 换成 `mingw32-make`。`make lint` 未纳入默认 `check`：本地 golangci-lint 若由低版本 Go 构建会拒绝分析 go1.25 项目，CI 用 v2.6.2 无此问题。

## 规范如何被强制

文档写了不等于生效。可机检的条款已接入 CI 与 `make check`：

| 检查 | 覆盖的规范 | 存量处理 |
|---|---|---|
| `gofmt` / `go vet` / `golangci-lint` | 格式化、错误不得忽略（errcheck）、错误比较用法（errorlint）等 9 项 | 已全绿 |
| `npx eslint`（前端） | **禁止 `any`** 等 | 87 处存量冻结在 `frontend/eslint-suppressions.json`，新增会失败 |
| `vue-tsc` | 前端类型完整性 | 已全绿 |
| `vitest`（前端） | 数据丢失路径与弹窗/提示无障碍的回归 | 42 个测试，已全绿 |
| `scripts/check-conventions.py` | 见下表 | 30 处存量冻结在 `scripts/conventions-baseline.txt` |

`scripts/check-conventions.py` 的规则（后端规则详见 `backend/AGENTS.md`，前端规则详见 `frontend/AGENTS.md`）：

| 规则 | 内容 |
|---|---|
| BE1 | SQL 只能出现在 `internal/repository` 与 `internal/model` |
| BE2 | goctl 生成物的 `DO NOT EDIT` 头必须完好（丢失即疑似手改） |
| BE3 | logic 层用 `l.Error` / `l.Info`，不直接调全局 `logx.Error` |
| FE1 | 禁止硬编码中性色（slate/gray/zinc/neutral/stone），须用主题 token |
| FE2 | 前端引入未在 `tailwind.config.js` 中注册的 shadcn 语义 token（`bg-primary` 等）—— 写了不生效 |
| SK1 | `.agents/skills/` 文档不得含外部机器的绝对路径 |

主观条款（注释是否讲清了"为什么"、命名是否贴切、双主题视觉是否可辨）无法机检，仍靠评审。

两个基线文件都是**只减不增**的技术债账本：修好存量后同步下调数字（前端可用 `npx eslint . --prune-suppressions` 自动收紧），不要把新代码写进去。确需重置时用 `make conventions-baseline`，且应在提交说明里讲清原因。

## API / 数据库修改流程

改 API 契约时先改 `.api` 规格，再执行 goctl 生成，然后核对生成结果（`DO NOT EDIT` 头完好、新增路由与类型到位、既有 handler 未被重写、`go build` 通过）。代理可自行运行 goctl——它是覆盖式的，所以生成前要确认 `backend/api/internal/` 下没有未提交的手写改动，出问题才能一键还原。

数据库结构变更仍需先提交变更、等待确认，再执行迁移：迁移会动用户的真实数据，不可逆，与重新生成代码不是一个风险量级。

具体步骤见 `backend/AGENTS.md`「代码生成流程」一节。

## 静态检查

CI（`.github/workflows/ci.yml`）会跑：`gofmt` 检查、`go vet`、`golangci-lint v2.6.2`、`go build`、`go test -race -cover`，前端跑构建与类型检查。后端 lint 启用规则与关停/热重载注意事项详见 `backend/AGENTS.md`。

## Skills

`.agents/skills/` 下配置了若干 AI 编码代理技能。各技能 `SKILL.md` 头部若带 `⚠️` 提示，说明其在本仓库的可用性现状（多为依赖脚本/外部资料缺失），使用前先看该提示：

**Claude Code Desktop / CLI**：项目记忆入口是 `CLAUDE.md`（通过 `@AGENTS.md` 引入本文件）；MCP 在仓库根 `.mcp.json`；skill 需在 `.claude/skills/` 可见——clone 后执行 `bash scripts/setup-claude-desktop.sh` 创建指向 `.agents/skills/` 的本机联接。详见 `CLAUDE.md`。

| Skill | 用途 | 本仓库可用性 |
|---|---|---|
| `vue3` | Vue 3 官方指南与 API 参考 | 可用 |
| `web-tailwind` | Tailwind CSS 3.4 用法（本仓库实际版本） | 可用 |
| `ui-styling` | shadcn/ui + Tailwind + canvas 视觉设计 | 可用 |
| `ui-ux-pro-max` | UI/UX 设计规范库（样式/配色/字体/图表） | 可用 |
| `shadcn-vue` | shadcn-vue 组件管理（新增/搜索/组合） | 可用，前端现状说明见其 SKILL.md 顶部 |
| `slides` | HTML 演示文稿生成 | 部分可用：依赖的检索脚本在本仓库不存在，需直接阅读 `references/` 原文 |
| `design` | 品牌/Logo/CIP/图标/社媒图设计 | 部分可用，与日常业务开发关联不大 |
| `banner-design` | 多平台横幅设计 | 部分可用 |
| `brand` | 品牌语调与视觉资料管理 | 不可用：仓库未初始化品牌资料，相关脚本会直接报错退出 |

其余通用工程技能（`superpowers:*`、`document-skills:*`、`zcode-guide:*`、`skill-creator`、`find-skills`、`browser-use:*`）不针对本仓库业务，按其各自触发场景正常使用即可。

## 命名（跨层通用约定）

- 错误变量统一 `ErrXxx` 前缀，用 `errors.Is` 比较，不用 `==`（后端）。
- 具体到后端/前端各自的命名表，见 `backend/AGENTS.md` / `frontend/AGENTS.md`。

## 注释

业务注释使用中文，说明**意图、边界、失败路径与为什么这样做**，不复述代码。标识符仍用英文。这一密度与深度要求对前后端均适用，具体示例见 `backend/AGENTS.md`「注释」与 `frontend/AGENTS.md`「编码准则 · 注释规范」。

## 类型去重

新增 API 类型前，先 grep 检索 `.api` 规格与 `internal/types`，避免重复定义造成冲突（后端）。前端新增类型前同理检索 `src/types` 或既有 store/组件内的类型定义。

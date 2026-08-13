# 主程序升级体验优化设计

日期：2026-08-13
状态：已评审通过
范围：主程序（AuroraMihomo 自身）自升级的观测性与可读性

## 背景与问题

当前「主程序升级」链路（`POST /api/v1/system/self-update`）是同步长请求：下载主程序包（12MB+）→ sha256 校验 → 解压 → 试跑新二进制 → 自动备份 DB → 应答「即将重启生效」→ 触发优雅关停。存在三个体验痛点：

1. **升级过程零反馈**：请求同步阻塞可能数分钟，前端只有「下载中…」三字，用户不知道卡在下载、校验还是解压，也不知道是否还在进行。
2. **失败原因不透明**：前端失败仅 `console.error(e)`，无法区分「仓库未配置」「已在升级中」「下载源全失败」「sha256 校验不匹配」——这几类处理方式完全不同。
3. **无变更日志预览**：升级确认是原生 `confirm()`，用户看不到新版本改了什么就被要求确认。

## 目标

- 升级过程实时反馈：阶段 + 下载进度百分比，前端轮询展示。
- 失败原因可读化：结构化错误码，前端给出对应可操作提示。
- 变更日志预览：升级确认弹窗展示 release notes。

## 非目标

- 不改升级核心流程：下载源优先级（上次成功源排最前）、sha256 校验、升级前自动备份 DB、SwapSelfBinary 时序全部保持。
- 不改 `/system/restart` 与升级的互斥逻辑。
- 不做推送式实时通道（SSE / WebSocket 复用）：升级必然重启进程导致连接断开，轮询最稳且与现有 ServerStats 模式一致。

## 方案

### 1. 升级过程实时反馈（轮询状态接口）

**后端（backend/internal/updater）**

`githubRelease` 结构加 `Body string json:"body"`，release notes 随 `latestRelease` 一起解析，零额外请求。

新增升级状态机 `SelfUpdateStatus`：

```go
type SelfUpdateStatus struct {
    Running       bool              `json:"running"`        // 是否进行中
    Phase         string            `json:"phase"`          // idle|downloading|verifying|extracting|preparing|restarting|failed
    Percent       int               `json:"percent"`        // 下载进度 0-100，非下载阶段为 0
    Message       string            `json:"message"`        // 人类可读阶段说明
    TargetVersion string            `json:"targetVersion"`  // 正在升级到的版本
    Error         *SelfUpdateError  `json:"error,omitempty"` // 失败时结构化错误
    StartedAt     string            `json:"startedAt,omitempty"`
}
```

- `downloadFile` 加进度回调：`io.Copy` 改带计数 Writer，Manager 记已下载字节/总字节 → Percent。
- `UpdateSelf` 改异步：`StartSelfUpdate(ctx)` 起 goroutine 执行下载→校验→解压→试跑→暂存，尾部自动备份 DB + `RequestQuit`（逻辑与现在 POST handler 尾部一致）。
- 新增 `GET /api/v1/system/self-update/status` 返回 `SelfUpdateStatus`（`.api` 规格 + goctl 生成）。

**前端**

- `settings store` 加 `selfUpdateStatus` state + `pollSelfUpdate()`（setInterval，仅升级进行中轮询；升级完成/服务重启后停止）。
- 升级区块：升级中显示阶段徽标 + `Progress` 进度条 + 文案（「下载中 42%」「校验完整性…」「即将重启生效…」）；按钮禁用显示对应文案。

### 2. 失败原因可读化

**后端**：结构化错误码，`SelfUpdateError{ Code, Message }`：

| Code | 含义 | 前端提示 |
|---|---|---|
| `repo_not_configured` | 仓库未配置 | 引导去「下载与更新出网」填写 |
| `already_in_progress` | 已有升级在跑 | 正在升级，请稍候 |
| `check_failed` | 版本查询失败 | 网络/代理问题，稍后重试 |
| `download_failed` | 所有下载源失败 | 检查下载源与网络 |
| `checksum_mismatch` | sha256 校验失败 | 下载源可能被篡改/截断，重试或换源 |
| `extract_failed` | 解压失败 | 包损坏，重试或反馈仓库 |
| `verify_failed` | 新二进制试跑失败 | 包损坏，重试或反馈仓库 |

POST 错误响应与 status 接口的 `error.code` 一致，前端统一映射。

**前端**：错误提示从「下载失败」变为「下载失败：校验和不匹配，可能下载源被篡改，重试或更换下载源」，失败状态停留展示 + 重试按钮。

### 3. 变更日志预览

**后端**：`SelfCheck` 加 `ReleaseNotes string`（截断 ~4KB 防爆响应）。

**前端**：升级确认从原生 `confirm()` 改为 `ModalDialog`：
- 标题「升级到 v0.12.0」
- 当前版本 → 目标版本
- release notes（`selfUpdateInfo.releaseNotes`，纯文本渲染，超长折叠）
- 确认「升级并重启」/ 取消
- 拉不到 release notes 时降级为原确认文案

## 数据流

```
前端 [检查主程序更新] → GET /system/self-update/check
                        ← SelfCheck{ configured, currentVersion, latestVersion,
                                     updateAvailable, releaseNotes, message }

前端 [升级并重启] → 确认弹窗(变更日志) → POST /system/self-update (异步，立即返回)
                → 开始轮询 GET /system/self-update/status (1s 间隔)
                ← SelfUpdateStatus{ running, phase, percent, message, ... }

后端 goroutine: 下载(进度更新) → 校验 → 解压 → 试跑 → 暂存 .new
                → 自动备份 DB → RequestQuit("HTTP self-update")
                → 进程管理器拉起新版

前端 轮询到 running=false 或连接断开(服务重启) → 停止轮询
```

## 错误处理

- 下载失败：`download_failed`，保留各源失败明细在 message。
- 校验失败：`checksum_mismatch`，绝不写入 `.new`（现有行为保持）。
- 备份 DB 失败：只记录不阻断升级（现有行为保持），message 附注。
- 升级成功后进程重启：前端轮询连接断开是预期，停止轮询，不做错误提示。

## 测试

- 后端：
  - 状态机阶段推进：downloading → verifying → extracting → preparing → failed。
  - 错误码映射：每类错误 → 对应 Code。
  - 进度计数：模拟下载流，验证 Percent 递增。
  - status 接口 handler：进行中/完成/失败三种响应。
- 前端：
  - store 轮询逻辑：开始/停止/失败置错。
  - 确认弹窗渲染与按钮状态。
  - 错误文案映射。

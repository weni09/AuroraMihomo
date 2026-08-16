# 网络诊断功能设计

日期：2026-08-16
状态：已批准（用户确认方案 A）
范围：面板自身出网问题排查

## 背景

用户在面板上排查「订阅拉取失败 / 内核更新失败 / raw 地址拉不下来」这类出网问题时，缺乏一个系统的诊断工具。现有 `backend/internal/netcheck` 包只服务透明代理功能（环境探测、防火墙、DNS 重定向），没有通用的网络诊断能力。

已有一个相似功能 `probeSubscription`（订阅参数探测）可作为参考模式，但它是针对订阅 URL 的特化探测，不可复用为通用诊断。

## 目标与场景

**核心场景**：排查面板自身出网问题。从面板宿主机视角出发，帮助判断是 DNS 解析失败、网络连通性问题，还是上游不可达。

**诊断能力**（用户确认全选）：
- Ping（连通性/延迟，ICMP 优先，无权限降级 TCP）
- DNS 解析查询（A/AAAA 记录、耗时）
- TCP 端口连通性（host:port 建连测延迟）
- HTTP 请求探测（状态码、耗时、重定向链）
- Traceroute 路由追踪（逐跳，调用系统命令）

**出网路径**（用户确认）：直连 vs 本地 mihomo 代理对比。每个探测可选择路径，对比结果可判断「是面板直连不通还是代理不通」。

**目标来源**（用户确认）：预设目标 + 手动输入。预设覆盖 GitHub API、raw.githubusercontent、mihomo 代理端口、公共 DNS 等；同时支持用户手动输入任意主机/域名/URL。

**结果呈现**（用户确认）：WebSocket 实时流式，经现有 `/ws` + `realtime.Hub` 推送，前端 `useRealtime` 过滤。

## 方案选择

**方案 A（选定）：REST 发起 + hub 定向事件流**

- `POST /api/v1/diagnostics/run` 发起诊断，立即返回 `requestId`（异步执行）
- 进度经现有 `/ws` hub 推送 `diagnostic.progress` 事件（带 requestId），前端 `useRealtime` 过滤自己发起的 requestId
- `GET /api/v1/diagnostics/result/:requestId` 轮询兜底（WS 断开也能拿全量结果）

**选择理由**：复用全部现有设施（useRealtime、realtime.Hub、/ws），零新增基础设施；探测器独立包 `backend/internal/diagnostics`，每个探测器一个单元，可独立测试。

**未选方案**：
- 方案 B（专用诊断 WS 端点）：精准定向但新增一套 WS 基础设施 + 前端新 composable，与现有 hub 并存两套通道，复杂度高。
- 方案 C（全同步 REST）：traceroute 可能跑 30 秒，用户看着空白页面等，体验差。

## 架构

```
前端「网络诊断」页（新路由 /diagnostics）
  │ POST /api/v1/diagnostics/run {targets, path}
  ▼
DiagnosticsService（backend/internal/diagnostics 包）
  │ 生成 requestId，异步 goroutine 跑探测
  ▼
探测器框架（Probe 接口 + 并发信号量 + 超时）
  ├─ PingProbe      ICMP → TCP 降级
  ├─ DNSProbe       域名解析（A/AAAA/耗时）
  ├─ TCPProbe       端口连通性
  ├─ HTTPProbe      状态码/耗时/重定向
  └─ TracerouteProbe 系统命令（traceroute/tracert）解析
  │ 每步完成 → realtime.Hub.Publish("diagnostic.progress", {requestId, step})
  ▼
前端 useRealtime 过滤 requestId → 实时进度展示
  │ 兜底：GET /api/v1/diagnostics/result/:requestId（WS 断开也能拿全量结果）
```

## 后端组件

### 新包 `backend/internal/diagnostics/`

| 文件 | 职责 |
|---|---|
| `diagnostics.go` | `Service`：Run（异步+requestId）/ GetResult / Cancel；结果缓存（TTL 10 分钟）+ 并发上限 |
| `probe.go` | `Probe` 接口 + 运行框架（每探测超时、进度回调、统一结果结构） |
| `ping.go` | ICMP（`x/net/icmp`，Linux 需 CAP_NET_RAW）→ 无权限自动降级 TCP ping，标注「降级」 |
| `dns.go` | `net.Resolver` 查 A/AAAA，记录所用服务器与耗时 |
| `tcp.go` | 对 host:port 建连测延迟 |
| `http.go` | GET 目标，报状态码/总耗时/重定向链 |
| `traceroute.go` | 调 `traceroute`/`tracert` 系统命令，解析输出逐跳 |
| `targets.go` | 预设目标：GitHub API（api.github.com:443）、raw.githubusercontent（raw.githubusercontent.com:443）、mihomo 代理端口（来自注入的 proxyURLFn，动态取当前内核代理地址）、公共 DNS（固定列表 1.1.1.1 / 8.8.8.8 / 223.5.5.5） |

### 统一结果结构

```go
type ProbeResult struct {
  Target  string      // 目标描述
  Type    string      // ping|dns|tcp|http|traceroute
  Path    string      // direct|proxy
  Status  string      // success|fail|timeout|error
  Latency time.Duration
  Detail  any         // 各类型特有数据
  Error   string
}
```

### 服务生命周期

```go
type Service struct {
  // 注入：进度发布回调（hub.Publish）、代理地址查询、SSRF 校验器
  publish  func(ev DiagnosticEvent)
  proxyURL func() string
  // 运行态：并发信号量、结果缓存（map[requestId]RunResult + TTL）
  sem   chan struct{}
  cache sync.Map
}
```

- `Run(ctx, req) (requestId, error)`：校验输入 → 生成 requestId → 异步 goroutine 跑探测 → 每步完成发布进度事件 → 全部完成存结果
- `GetResult(requestId) (RunResult, bool)`：查缓存
- `Cancel(requestId)`：取消进行中的诊断（context cancel）
- 并发上限：全局信号量（如同时最多 3 个诊断运行），超出返回明确错误
- 结果缓存 TTL 10 分钟，到期清理

### 出网路径

- `direct`：用现有直连 Transport（复用 fetcher 的 `guardedDialContext`）
- `proxy`：经本地 mihomo 代理。复用 fetcher 的 `proxyClient()` 模式——诊断服务注入 `proxyURLFn`（与 updater/fetcher 同款注入，不硬依赖任何包）

### API 层

`.api` 规格新增 2 个 protected 路由：

```
type DiagnosticRunReq {
  Targets []DiagnosticTarget `json:"targets"`
  Path    string             `json:"path,optional"`  // direct|proxy|both，默认 both
}

type DiagnosticTarget {
  Type   string `json:"type"`              // ping|dns|tcp|http|traceroute
  Target string `json:"target"`            // 主机/域名/URL
  Port   int    `json:"port,optional"`     // tcp 用
}

@handler runDiagnostics
post /api/v1/diagnostics/run (DiagnosticRunReq) returns (DiagnosticRunResp)

type DiagnosticRunResp {
  RequestId string `json:"requestId"`
}

@handler getDiagnosticsResult
get /api/v1/diagnostics/result/:requestId returns (DiagnosticResultResp)
```

### 进度事件

`realtime.Hub.Publish("diagnostic.progress", data)`，data 结构：

```go
type DiagnosticEvent struct {
  RequestId string      `json:"requestId"`
  Target    string      `json:"target"`
  Type      string      `json:"type"`
  Path      string      `json:"path"`
  Status    string      `json:"status"`
  LatencyMs int64       `json:"latencyMs,omitempty"`
  Detail    interface{} `json:"detail,omitempty"`
  Error     string      `json:"error,omitempty"`
}
```

前端 `useRealtime` 的 handler 里过滤 `data.requestId === 当前请求的 requestId`。

## 安全

- **SSRF 防护**：目标输入复用 `fetcher` 的 `validateFetchURL`/`isBlockedMetadataIP`/`guardedDialContext`（含 DNS 复验）
- **并发限制**：全局信号量（同时最多 3 个诊断运行）
- **超时**：ping/tcp/dns 5s、http 10s、traceroute 30s；单次诊断总时限 60s
- **仅认证**：protected 路由，需登录
- **输入校验**：非法目标（含被拦地址）直接标 error，不阻塞其余目标

## 前端

### 新页面 `/diagnostics`

侧边栏加入口（App.vue 导航项，图标 `Wifi` 或 `Activity`），路由 `/diagnostics` 指向 `DiagnosticsView.vue`。

**页面结构**：
- **预设目标卡片**：一键全测（GitHub API / raw / 代理端口 / 公共 DNS）
- **手动输入区**：目标 + 探测类型（多选）+ 出网路径（直连/代理/两者对比）
- **实时进度区**：逐步渲染，成功绿/失败红/超时黄；每项可展开详情
- **直连 vs 代理对比**：两种路径结果并排展示，一眼看出哪条不通

**store**：`stores/diagnostics.ts`
- state：`running`、`requestId`、`results[]`
- actions：`run(targets, path)`（发 POST 拿 requestId）、`fetchResult(requestId)`（轮询兜底）、`cancel()`、`reset()`
- 与 `useRealtime` 集成：页面挂载时注册 handler 过滤 requestId，卸载时注销

**状态流转**：
- 发起 → `running=true`，显示进度
- 收到 progress 事件 → 更新对应 target 的结果
- 全部完成（result 接口 status=done）→ `running=false`
- WS 断开 → 轮询 result 接口兜底

## 错误处理

- 单探测失败不中断整体（每项独立结果）
- ICMP 无权限 → 降级 TCP ping 并标注「无 ICMP 权限，已用 TCP ping」
- traceroute 命令缺失 → 明确提示「系统未安装 traceroute」
- WS 断开 → 前端自动轮询 result 接口兜底
- 输入校验失败（非法目标）→ 该目标直接标 error，不阻塞其余

## 测试

### 后端单测

- **各探测器**：用本地 `httptest` 服务器验证
  - TCPProbe：起本地 TCP listener，验证延迟与不可达分支
  - HTTPProbe：httptest 返回状态码/重定向，验证状态码与重定向链解析
  - DNSProbe：注入 mock resolver（`net.Resolver` 可替换 `Dial`）
  - PingProbe：降级路径（无 ICMP 权限时走 TCP ping）
  - TracerouteProbe：注入假命令输出，验证逐跳解析
- **诊断服务**：run/result/cancel/并发上限/缓存 TTL

### API 集成测试

- run + result 全链路（mock 探测器注入）

### 前端组件测试

- 进度渲染、错误态、直连/代理对比布局
- 状态流转（发起 → 进度 → 完成 / 失败）

## 不做的事（YAGNI）

- 不做历史诊断记录持久化（结果缓存仅内存 TTL 10 分钟）
- 不做诊断结果的导出/分享
- 不做定时自动诊断
- 不做「诊断报告」生成
- 不做纯前端探测（浏览器受 CORS 限制，结果不可靠）

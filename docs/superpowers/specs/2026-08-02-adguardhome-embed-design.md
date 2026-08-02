# 内置 AdGuard Home 设计

日期：2026-08-02  
状态：待用户审阅  
范围：Aurora 按需下载并托管 AdGuard Home（AGH）；同源反代 + iframe 使用官方 UI；可选 DNS 一键对接透明代理。  
**不**自研 DNS 查询日志/拦截引擎。

## 0. 背景与决策摘要

### 原诉求

在现有 DNS 能力上实现接近 AdGuard Home 的：

- 查询日志（时间、域名、类型、客户端、应答细节、耗时等）
- 从日志一键拦截等

### 现状（代码库）

- DNS 是 **mihomo 内置 DNS**（配置中心 + TProxy 劫持到 `dns.listen` + TUN `dns-hijack`）。
- **无**查询日志表/API/UI；广告拦截主要靠规则 `REJECT`，不是 DNS 级过滤。
- mihomo EC 仅有即时 `/dns/query`，**无**查询历史 API。
- TUN 的 `dns-hijack` 进入 **内部 DNS 模块**，不经过 `dns.listen` 套接字。

### 为何不自研 Logger

为 C 档字段 + TUN 接近全量，需改 TUN 数据面、与 `auto-redirect`/`dns-hijack` 抢路径，复杂且风险高。  
业界稳态拆分：**DNS 过滤/日志 → AdGuard Home；代理/分流 → Mihomo**。

### 产品决策（已确认）

| 项 | 决定 |
|----|------|
| 能力来源 | 内置 AdGuard Home |
| 集成深度 | 托管进程 + **AGH 自带 UI**（不重做查询日志页） |
| DNS 对接 | 生命周期 + **可选一键对接**（默认不强制） |
| 二进制 | **按需下载**（对标更新 Mihomo） |
| 打开方式 | Aurora **反代** `/adguard/` + **iframe** |
| 实现方案 | **方案 1**：第二内核式托管（对标 mihomo Manager + updater） |

### 非目标（P1）

- Aurora 自绘查询日志/拦截 UI、过滤列表管理
- 编译期链接 AGH 源码进单二进制
- AGH 注册为 systemd/Windows 服务；面板退出后 DNS 仍常驻
- TUN 全自动抢光所有 DNS（默认不硬改 `dns-hijack`）
- Aurora ↔ AGH SSO；多 AGH 实例
- 过滤列表与 mihomo rules 双向同步
- Windows 透明 :53 劫持承诺

---

## 1. 进程、目录、端口、首次安装

### 1.1 角色

AGH 是 **可选** 第二子进程，与 mihomo 并列，不替代 mihomo。

| | Mihomo | AdGuard Home |
|--|--------|--------------|
| 职责 | 代理 / 分流 /（可选）内核 DNS | DNS 过滤、查询日志、拦截、官方 Web |
| 必须 | 核心 | 可选；未安装时引导安装 |
| 二进制 | `data/bin/mihomo` | `data/bin/AdGuardHome`（Windows 带 `.exe`） |
| 工作目录 | `data/`（`-d`） | `data/adguardhome/`（`--work-dir`） |

### 1.2 目录

```text
data/
  bin/AdGuardHome
  adguardhome/          # AGH 自管 yaml、统计、列表缓存
    AdGuardHome.yaml
```

- **不**使用 `AdGuardHome -s install` 系统服务；由 Aurora 子进程拉起。
- Aurora 进程退出时 **停止** AGH 子进程（P1；独立服务模式延后）。

### 1.3 默认端口

| 用途 | 默认 | 说明 |
|------|------|------|
| AGH Web | `127.0.0.1:3000` | 仅回环；外网只经 `/adguard/` |
| AGH DNS | 优先用户配置；常见 `0.0.0.0:1053` 或有权限时 `:53` | 与 mihomo `dns.listen` 冲突时见下 |
| 反代 | Aurora 同端口路径 `/adguard/` | |

**冲突：** 启动/安装时检测端口；不与 mihomo listen 静默双绑。冲突则改用空闲高位端口或提示，并在一键对接中使用实际 AGH DNS 端口。  
**权限：** P1 不自动 setcap；`:53` 绑定失败则报错并建议高位端口 + TProxy 劫持。

### 1.4 进程管理

包：`backend/internal/adguard`（名称实现期可微调）`Manager`：

- Install/Update 委托 updater；Start / Stop / Restart / Status / Version
- Status：`installed, running, pid, version, webAddr, dnsPort, workDir, wiring, lastError`
- stdout/stderr：**独立**缓冲，不混入 mihomo logs / applog

### 1.5 首次安装

1. 侧栏「AdGuard」→ 未安装则下载安装  
2. 解压到 `data/bin/`，工作目录 `data/adguardhome/`  
3. Start 后经 iframe 完成 **AGH 自带** 管理员与 DNS 向导  
4. 一键对接 dual 入口在工具条，**默认不自动改** mihomo/TProxy  

---

## 2. 下载 / 更新与设置

### 2.1 Updater

在 `backend/internal/updater` 增加与 Mihomo/Zashboard 平行能力：

| 项 | 设计 |
|----|------|
| 默认仓库 | `AdguardTeam/AdGuardHome`（可配置） |
| 资产 | 按 `GOOS`/`GOARCH` 匹配官方 release |
| 下载 | 现有 CDN 列表 +「经 mihomo 代理下载」 |
| 串行 | 避免与 mihomo/zashboard 更新并发踩 `data/bin` |
| 更新中 | Stop → 二进制 `.bak` → 替换 → Start；失败可提示用 bak 恢复 |
| 版本 | settings 记录 tag；`update/check` **增加 adguard 项** |

仅 stable release；不静默后台安装。

### 2.2 API（须先改 `.api` 再 goctl）

| 方法 | 路径 |
|------|------|
| GET | `/api/v1/adguard/status` |
| POST | `/api/v1/adguard/install` |
| POST | `/api/v1/update/adguard` |
| POST | `/api/v1/adguard/start` \| `stop` \| `restart` |
| GET | `/api/v1/adguard/wiring` |
| POST | `/api/v1/adguard/wiring/apply` |
| POST | `/api/v1/adguard/wiring/rollback` |

全部 **protected**（登录 JWT）。长时间操作与 `UpdateMihomo` 对齐（P1 不新造异步模型）。

### 2.3 Settings KV（无新业务表）

| Key | 含义 | 默认 |
|-----|------|------|
| `adguard.enabled_at_boot` | 面板启动时是否拉起 AGH | `false` |
| `adguard.web_addr` | Web 监听 | `127.0.0.1:3000` |
| `adguard.dns_port` | 面板记录的 DNS 端口（对接用） | 检测/约定值 |
| `adguard.repo` | 可选覆盖仓库 | 官方 |
| `adguard.version` | 已装 tag | 空 |
| `adguard.dns_wiring` | `off` / applied 元数据 | `off` |
| `adguard.dns_wiring_snapshot` | 对接回滚快照 JSON | 空 |

查询日志与过滤列表 **只**在 AGH work-dir。

### 2.4 前端

- **侧栏「AdGuard」主入口**：状态、安装/更新、启停、iframe、对接、说明  
- **设置 → 更新**：与 Mihomo/Zashboard 并列「更新 AdGuard Home」  
- **不**在配置中心 DNS 表单重做 AGH 规则 UI  

---

## 3. 反代与 iframe

### 3.1 路由顺序

```text
/api/|/ws|/healthz  → go-zero
/adguard/           → [鉴权] ReverseProxy → 127.0.0.1:webPort
/ui/                → zashboard 静态
其余                → SPA
```

`/adguard` → 301 → `/adguard/`。  
须 **优先于** SPA fallback，挂原生 mux（与 `/ui` 同类位置）。

### 3.2 反代行为

- Strip 前缀 `/adguard` 后 upstream  
- 支持 WebSocket Upgrade（若 AGH 需要）  
- `X-Forwarded-*`；AGH `trusted_proxies` 含 `127.0.0.1`  
- 改写 Location / cookie path，避免子路径资源 404（参考 AGH FAQ 的 nginx 写法）  
- 若上游返回 `X-Frame-Options: DENY`，代理侧改为可同源嵌入（或剥离），否则 iframe 白屏  
- AGH **subpath 非一等公民**：P1 钉 stable 手工验收；失败则 UI **降级「新标签打开 /adguard/」**，并保留说明；不把跨端口直连当默认主路径  

### 3.3 安全

| 要求 | 设计 |
|------|------|
| `/adguard/*` | 与 protected API **同级会话**；禁止匿名 |
| JWT 与 iframe | SPA 的 `Authorization` 头不会自动带给 iframe 子资源 → 登录后写 **SameSite 合适的会话 cookie**（HttpOnly）供同源 `/adguard` 校验；**不**在 URL 长期挂 token |
| AGH Web 绑定 | **仅 127.0.0.1**，防绕过 Aurora |
| AGH 管理员 | 保留双层登录；P1 无 SSO |
| DNS | UDP/TCP **不**走 HTTP 反代 |
| CSP | 现有 `frame-src 'self'` 覆盖同源 iframe |

### 3.4 前端页

对标 ZashboardView chrome（状态、新标签、避免双 header）：

- `status` 未运行则不挂 iframe  
- `iframe.src = "/adguard/"`  
- 加载失败展示 `lastError` + 重试 + 降级入口  

---

## 4. 可选 DNS 一键对接

### 4.1 目标拓扑（wiring 开启后）

```text
终端 DNS
  → TProxy :53 或用户把 DNS 指到面板
    → AGH（日志 / 拦截 / 列表）
      → 上游可选 127.0.0.1:<mihomo-dns-port>（保留 fake-ip / policy）
         或公共 DNS（仅过滤）
业务流量 → mihomo（mixed / TProxy / TUN）
```

默认 **wiring=off**，不改现网。

### 4.2 向导与快照

1. 预检：AGH running、端口、TProxy/TUN、mihomo dns  
2. 展示变更清单与勾选  
3. **先快照** → 应用 → TProxy 则 `Resync` → 失败 **自动回滚**  
4. 「解除对接」按快照恢复  

快照字段包括：原 DNS 重定向逻辑/端口、mihomo `dns.listen`、AGH upstream 相关原文、flags。只保留 **一份** 当前对接快照。

### 4.3 可勾选动作（P1）

| 动作 | 默认 | 行为 |
|------|------|------|
| A. TProxy DNS → AGH | TProxy 启用时勾选 | `dnsPortFn` 在 wiring=on 时返回 AGH DNS 端口 |
| B. 避免端口冲突 | 勾选 | mihomo `dns.listen` 与 AGH 冲突则改为 `127.0.0.1:<空闲>`，走配置合并/热重载 |
| C. AGH 上游 → mihomo DNS | `dns.enable` 时勾选 | 有限补丁 AGH yaml upstream（提示可能覆盖 bootstrap 上游） |
| D. TUN 弱化内部 hijack | **默认不勾** | 文案说明 `dns-hijack` 绕过 AGH；可选清空面板注入的 `any:53`（风险提示）。P1 不做额外 iptables 抢 TUN DNS |

手动代理：只说明把系统/DHCP DNS 指向面板；不改客户端。  
Windows：不承诺透明 :53；仅说明手动指 DNS。

### 4.4 与配置中心

wiring=on 时 AdGuard 页横幅提示：改 `dns.listen` 前宜先解除对接。  
**不**整表锁定 DNS 表单。

### 4.5 成功标准

- TProxy + apply 后，AGH 查询日志可见设备查询  
- 勾选 C 且 fake-ip 时，允许的域名仍可经 mihomo 返回假 IP 段  
- rollback 后劫持目标回到 mihomo  

---

## 5. 模块、权限、许可、生命周期

### 5.1 模块边界

```text
updater            AGH 资产下载/更新
internal/adguard   进程与有限 yaml 补丁
service            status/wiring 编排；TransparentService / ConfigService
api logic          薄封装（goctl handler → logic → service）
aurora mux         /adguard 反代 + 鉴权
frontend           AdGuardView + 设置更新入口
```

- 不在 logic 层直接 `exec` AGH。  
- P1 **无新 migration**（只用 settings KV）。若日后必须建表，按 AGENTS：**先提交迁移说明并等确认再执行**。  

### 5.2 许可

AdGuard Home 为 **GPL-3.0** 独立程序。Aurora **仅下载并 exec** 官方/可配置 release 二进制，**不**链接进 `auroramihomo`。  
用户文档与发行说明标注可选组件与来源；**默认不**把 AGH 打进 Aurora 发布包。

### 5.3 生命周期

| 事件 | 行为 |
|------|------|
| Aurora 启动 | 仅 `enabled_at_boot` 且二进制存在时 Start |
| Aurora 退出 | Stop AGH |
| 检查更新 | mihomo / zashboard / **adguard** 三项 |

---

## 6. 测试、风险、验收

### 6.1 测试

- 单元：资产匹配、wiring 计划、快照、路径 strip、无会话 401  
- 反代：可用 fake upstream 测前缀与鉴权  
- 手工：TProxy 对接后 AGH 见日志；rollback；iframe 双层登录；**未装 AGH 时主路径无回归**  
- `make check` 相关项保持绿  

### 6.2 风险摘要

| 风险 | 缓解 |
|------|------|
| subpath 与 AGH 前端 | 改写 + 降级 + 钉版本验收 |
| :53 权限 | 高位端口 + TProxy |
| 双 DNS 权威 | 向导默认 AGH 入口 + mihomo 上游 |
| TUN 绕过 | 默认不硬改 hijack；文档 |
| 更新换二进制 | stop + bak |
| 反代未鉴权 | 强制会话 + Web 回环 |
| 对接改坏网络 | 快照 + 失败回滚 + 常显解除 |

### 6.3 P1 验收清单

1. 可按需下载安装，status 正确  
2. 启停正常；同源 `/adguard/` iframe（或文档降级路径）可用  
3. 不点对接则不改 TProxy/mihomo DNS  
4. TProxy 下一键对接后 AGH 可见查询；可回滚  
5. 未安装 AGH 时代理/配置主路径无回归  

---

## 7. 实现顺序建议（供后续 plan，非本 spec 执行）

1. updater 资产选择 + install/update API  
2. adguard Manager 启停与 status  
3. `/adguard` 反代 + 会话 cookie + 前端 AdGuard 页 iframe  
4. wiring 预检/apply/rollback + TProxy `dnsPortFn` 分支  
5. 设置页更新入口、`update/check` 扩展、文档  
6. 测试与透明代理场景手工验收  

---

## 8. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-08-02 | 初稿：由自研 DNS 日志转向内置 AGH；完成 §1–§6 设计确认后落盘 |

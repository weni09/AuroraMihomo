# AdGuard Home 产品化管控设计

日期：2026-08-02  
状态：待用户审阅  
分支：`feature/adguardhome-embed`  
前置：`docs/superpowers/specs/2026-08-02-adguardhome-embed-design.md`（进程托管、反代、基础 wiring）  
范围：组件级开启/关闭/彻底卸载、条件侧栏、安装引导、设置弹窗（账号同步、运行、Web 端口、版本与更新、DNS 模式取代主路径 wiring）。

## 0. 背景与决策

### 问题

P1 嵌入已具备下载、启停、`/adguard/` iframe 与 wiring，但：

- 侧栏常驻，无法「不要这个组件」；
- 缺少彻底卸载；
- 管控分散，无统一设置面板；
- DNS 对接与日常设置心智不统一。

### 已确认产品决策

| 项 | 决定 |
|----|------|
| 集成骨架 | 方案 1：设置组件开关 + 条件侧栏 + 页内设置弹窗 |
| 组件关闭 | **隐藏菜单 + Stop 进程**，保留二进制与 work-dir |
| 彻底卸载 | 独立危险操作：停 + 删文件 + 清 settings，强确认 |
| DNS | **模式 0/1/2 取代**主路径 wiring 向导 |
| 密码 | 可勾选与 Aurora **持续自动同步** |
| 组件默认 | **`component_enabled=false`**（新装与升级默认关） |
| 下载出网 | **遵循**系统「下载与更新出网」，不另开开关 |
| 升级链接 | 可配多个，**顺序回落**（AdGuard 专用列表，空则用全局 CDN） |

### 非目标

- 重做 AGH 过滤列表/查询日志 UI（仍用官方 iframe）
- AGH SSO 免登录进 iframe（密码可与 Aurora 相同，仍可能看到 AGH 登录页）
- Windows 上承诺透明 :53 与 Linux TProxy 对等
- 关闭组件时删除文件（那是卸载）

---

## 1. 组件生命周期

### 1.1 三态

| 概念 | 含义 | 持久化 |
|------|------|--------|
| 组件开启 | 入口可见、可管 | `adguard.component_enabled`（默认 `false`） |
| 已安装 | 二进制存在 | `data/bin/AdGuardHome*` |
| 运行中 | 子进程存活 | Manager 运行时 |

### 1.2 系统设置

位置：系统设置 → **可选组件**（或现有「组件状态」下独立卡片）：

- 开关：**启用 AdGuard Home 组件**
- 说明：关闭后侧栏消失并停止进程；数据保留
- 按钮：**彻底卸载…**（已安装或 work-dir 存在时可用）

**关闭流程：**

1. 若 DNS 模式 ≠ 0 → 先退出模式并回滚劫持/listen；失败则 **中止关闭**，组件保持开启并报错  
2. Stop AGH  
3. `component_enabled=false`  
4. 前端刷新导航  

**开启流程：** 仅写 `component_enabled=true`，不自动安装、不自动启动。

**`enabled_at_boot`：** 仅当组件开启且已安装时，Aurora 启动才可拉起 AGH。

### 1.3 侧栏与路由

- 关闭：不渲染「AdGuard Home」；访问 `/adguard` → 提示并链到设置锚点  
- 开启：显示「AdGuard Home」

### 1.4 彻底卸载

二次确认（勾选「不可恢复」或输入确认语）：

1. DNS 模式 → 0 + 回滚  
2. Stop  
3. 删除 `data/bin/AdGuardHome*` 与 `.bak`  
4. 删除 `data/adguardhome/`  
5. 清除 adguard.* settings（含 version、dns mode 快照、sync 开关、专用 CDN 等）  
6. 强制 `component_enabled=false`  

优先保证 DNS 回滚成功再删文件。

### 1.5 API

```text
PUT  /api/v1/adguard/component   { "enabled": true|false }
POST /api/v1/adguard/uninstall   { "confirm": true }
GET  /api/v1/adguard/status      扩展 componentEnabled 等
```

均 JWT protected。

---

## 2. 安装引导与主页面

### 2.1 页面状态机

| 条件 | UI |
|------|-----|
| 组件关 | 引导去设置开启 |
| 组件开 · 未安装 | 安装引导（GPL/目录/下载按钮/错误重试） |
| 已装 · 未运行 | 状态 + 启动 + **设置** |
| 运行中 | 顶栏（状态、设置、新标签）+ iframe `/adguard/` |

安装成功不默认 Start，由用户点启动或在设置里操作。

### 2.2 文案

侧栏与标题使用 **AdGuard Home**。

---

## 3. 设置弹窗

主页 **「设置」** 打开 Dialog（窄屏可用全高 Sheet）。

### 3.1 账号

- 设置 AGH 管理员用户名/密码（经 AGH 支持的配置或 API 写入）  
- 勾选 **与 Aurora 管理员密码保持同步**（`adguard.sync_password`）  
  - 勾选保存时：用用户提供的当前口令或改密流程写入 AGH  
  - 之后 `ChangePassword` 成功且 sync 开启 → 自动更新 AGH 密码；失败：applog + 可在 status 带 `passwordSyncError`  
- **不**在 SQLite 存密码明文；只存同步开关与 AGH 用户名（若需要）

### 3.2 运行

运行 / 停止 / 重启；展示 running、PID。

### 3.3 网页管理端口

- 配置独立 Web 端口；`http.address` / bind 保持 **回环**（安全：外网只经 Aurora 反代，除非用户明确改——P1 仍强制 127.0.0.1）  
- 改端口后提示并支持自动 Restart  
- 反代上游随 `ReadWebPort` 更新  

### 3.4 版本与更新

- 显示本地 / 远程版本  
- 检查更新、立即更新（复用 UpdateAdGuard；运行中先停后更再按策略恢复）  
- **自动更新：** 系统设置自动更新区增加「包含 AdGuard Home」；与全局 cron 同一套，不另起 cron  
- **升级链接：** `adguard.cdn_providers` 字符串列表（多行 UI），**按序回落**；空则用全局 `CDNProviders`  
- 出网：`UseMihomoProxy` 等 **只读复用** 系统设置，弹窗内展示说明「遵循系统设置 → 下载与更新出网」

### 3.5 DNS 服务模式（主路径）

| 模式 | 值 | 行为 |
|------|-----|------|
| 未托管 | `0` | 不劫持；AGH DNS 可用高位端口 |
| 使用 53 端口 | `1` | AGH 听 :53；预检占用与权限；失败给出 OS 相关让路说明 |
| 重定向 53→AGH | `2` | AGH 高位端口；TProxy/规则重定向 53→该端口；默认保留上游指向 mihomo DNS（原 wiring C） |

- 切换：预检 → 确认 → 快照 → 应用 → 失败整单回滚  
- 组件关闭强制模式 0 + 回滚  
- 主 UI **移除**旧 wiring 向导；实现复用/演进现有 wiring 代码  
- 模式 2 在非 TProxy/不支持平台：预检 warning 或禁用并说明  

Settings 键示例：`adguard.dns_mode` = `0|1|2`，快照键演进自 `adguard.dns_wiring_snapshot`。

---

## 4. 数据、模块、测试

### 4.1 Settings 键（增量）

| Key | 含义 |
|-----|------|
| `adguard.component_enabled` | 组件开关，默认 false |
| `adguard.sync_password` | 是否与 Aurora 同步密码 |
| `adguard.username` | AGH 管理员用户名（可选） |
| `adguard.dns_mode` | `0\|1\|2` |
| `adguard.auto_update` | 是否参加系统自动更新 |
| `adguard.cdn_providers` | JSON 数组，升级回落源 |
| 既有 | `web_addr`/`dns_port`/`version`/`enabled_at_boot`/快照等 |

无新业务表（除非后续必须，再走 migration 确认流程）。

### 4.2 模块

- `AdGuardService`：component enable/disable、uninstall、dns mode、password sync hook  
- `updater`：AdGuard 下载使用 `adguard.cdn_providers` 覆盖或前缀全局 CDN  
- `ChangePassword` logic：成功后条件调用 AGH 设密  
- 前端：`SettingsView` 组件卡、`App.vue` 条件导航、`AdGuardView` 状态机 + 设置 Dialog、store 扩展  

### 4.3 测试要点

- 默认 component 关闭时导航不含 AdGuard  
- 关闭时 Stop 被调用；DNS mode 非 0 时先回滚  
- 卸载删除路径与 settings 清理（临时目录单测）  
- DNS 模式预检：53 占用 → 模式 1 拒绝  
- 密码同步开关 on 时改密触发 AGH 更新（mock）  
- 反代仍 401 无会话；登出清 cookie（已有能力保持）  
- 前端：组件关隐藏菜单；未安装引导；设置弹窗分区渲染  

### 4.4 验收清单

1. 默认无 AdGuard 菜单；设置开启后出现  
2. 关闭：菜单消失且进程停止；文件仍在  
3. 卸载：警告 → 文件与配置清除，菜单无  
4. 安装引导可下载；设置内可启停与改 Web 端口  
5. 密码同步勾选后改 Aurora 密，AGH 可同步（或明确失败提示）  
6. DNS 模式 1/2 预检与回滚；关闭组件不留劫持  
7. 更新走系统出网策略；自定义升级链接顺序回落  

### 4.5 实现顺序建议

1. `component_enabled` + 设置 UI + 条件导航 + 关闭停进程  
2. uninstall API + 确认 UI  
3. AdGuard 页状态机（安装引导）  
4. 设置弹窗：运行 + Web 端口 + 版本/更新/CDN  
5. DNS 模式 0/1/2（迁移 wiring）  
6. 账号设密 + ChangePassword 同步钩子  
7. 文档与回归  

---

## 5. 与前置 spec 的关系

| 前置能力 | 本设计 |
|----------|--------|
| 下载/启停/反代/iframe | **保留** |
| 主 UI wiring 向导 | **替换为 DNS 模式** |
| `enabled_at_boot` | 从属于 component_enabled |
| 登录 cookie / 登出 / 关机 Stop | **保留** |

---

## 6. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-08-02 | 初稿：产品化管控（开关/卸载/设置弹窗/DNS 模式/密码同步） |

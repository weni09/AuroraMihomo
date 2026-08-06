# AdGuard Home 服务化改造 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 AdGuard Home 从「面板托管子进程」改造为「独立系统服务」（systemd / OpenRC），面板只做控制面；面板升级/重启/崩溃期间 DNS 过滤常驻，AGH 崩溃由服务管理器自动拉起。

**Architecture:** 在 `backend/internal/adguard.Manager` 上引入服务控制器接口：Linux 实现为面板自写的 `aurora-adguardhome` 服务单元（systemd / OpenRC 分派，复用 `scripts/install.sh` 的既有先例），Windows 回落现有 exec 子进程路径。**核心设计决策：unit 只固化安装期不变的参数（二进制路径 / work-dir / config 路径 / `--no-check-update`），不固化任何端口参数；AGH yaml 是端口（Web/DNS）的唯一事实来源，改端口 = 改 yaml + `systemctl restart`，unit 写一次永不重写。**

**Tech Stack:** 现有 Go service + go-zero API、settings KV、既有 `backend/internal/adguard` 与 `AdGuardService`、systemd / OpenRC 服务管理。

**Spec:** `docs/superpowers/specs/2026-08-02-adguardhome-embed-design.md` §1.2「独立服务模式延后」项的落地；本计划为 P2a，P2b（TProxy 规则持久化）与 P2c（Windows 服务化）另行评估。

---

## 关键设计决策

### D1. 自写服务单元（方案 B），不用 AGH 官方 `-s install`

- 官方 `-s install` 把 `--web-addr` 等参数固化进官方 unit，改端口要重装服务，且无 `LimitNOFILE`、无法按需给 CAP；与「面板统一管二进制与配置」的现有哲学冲突。
- 面板自写 unit 内容固定、可控，可带 `CapabilityBoundingSet=CAP_NET_BIND_SERVICE`（`:53` 权限只给 AGH，不再要求整个面板带 CAP）。

### D2. unit 不固化端口参数，yaml 为唯一事实来源（本次改造核心优化）

```ini
# /etc/systemd/system/aurora-adguardhome.service —— 无任何端口参数
[Unit]
Description=Aurora AdGuard Home (managed by AuroraMihomo)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/auroramihomo/data/bin/AdGuardHome \
    --work-dir /opt/auroramihomo/data/adguardhome \
    --config /opt/auroramihomo/data/adguardhome/AdGuardHome.yaml \
    --no-check-update
Restart=always
RestartSec=3
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
```

- `EnsureBootstrapConfig`（`bootstrap.go:101`）与 `SetWebPort`（`adguard_settings.go:57`）已在维护 yaml 的 `http.address`，yaml 作为唯一来源的前提成立。
- 改 Web / DNS 端口 = 改 yaml + `systemctl restart`，unit 从安装写到卸载零次重写。
- unit 仅有的变量（二进制路径 / work-dir）在安装时一次定死；升级只替换二进制文件内容，路径不变。

### D3. 控制面统一走服务管理器，杜绝直接 kill

systemd `Restart=always` 会把「杀 PID」变成「杀死后 3 秒复活」。面板对 AGH 的 Stop/Start/Restart 一律走 `systemctl` / `rc-service`；仅非服务模式（Windows、无服务管理器）保留现有 exec 子进程路径。

### D4. 生命周期语义调整

| 事件 | 现状（子进程托管） | 改造后（服务模式） |
|---|---|---|
| 面板启动 | `StartWithBootRetry` 拉起 | 不再拉起；系统服务自己起来（`enable` 控制） |
| 面板退出 | `AdGuardManager.Stop`（aurora.go:376） | **删除该钩子**，AGH 常驻 |
| 用户点「停止」 | Stop 进程 + 清 `enabled_at_boot` | `systemctl stop`（不 disable） |
| 开机自启开关 | `enabled_at_boot` settings | 驱动 `systemctl enable/disable`（settings 保留作回显） |
| 运行中崩溃 | 只记 lastErr，不重启 | 服务管理器 `Restart=always` 拉起；Status 显示「重启中」 |
| 自动更新 | `StopProcess` → 换 bin → Start | `systemctl stop` → 换 bin（.bak 保留）→ `systemctl start` |
| 卸载 | Stop → 删 bin → 删 workdir → 清 settings | 先 `stop` + `disable` + 删 unit，再删 bin/workdir/settings（**必须先 disable，否则开机又装回来**） |

### D5. 边界（服务化 ≠ DNS 全套常驻）

- TProxy / iptables 规则是面板进程态（wiring 下发、面板重启 `Resync` 恢复）——面板 down 期间 AGH 过滤常驻，但「劫持入口」随面板消失；P2b 再评估规则持久化。
- SSO 免密不受影响：`agh_session` 存 AGH 侧；面板不在时经 SSH 端口转发直连 `127.0.0.1:3000`（Web 保持回环绑定）。
- 双管理者竞态：服务已 `enable` 时面板不再 Start（D4），无需依赖旧 `webPortOpen` 幂等兜底，但保留其作为异常兜底。

---

## File structure

| Path | Responsibility |
|------|----------------|
| `backend/internal/adguard/svc_controller.go` | 服务控制器接口 + systemd/OpenRC 实现 + 平台分派（新文件） |
| `backend/internal/adguard/svc_unit_templates.go` | systemd / OpenRC unit 模板（新文件，纯静态内容） |
| `backend/internal/adguard/svc_controller_test.go` | 模板断言、命令序列、分派测试（新文件） |
| `backend/internal/adguard/manager.go` | 接入 controller；去掉 `--web-addr` 命令行参数；`webPortOpen` 探测改读 yaml |
| `backend/internal/adguard/manager_test.go` | 端口唯一来源相关单测补充 |
| `backend/internal/service/adguard_service.go` | Install/Uninstall 注册注销服务；Stop/自启语义；自动更新链路走 controller |
| `backend/internal/service/adguard_settings.go` | `SetWebPort` / `SetDNSListenPort` 服务模式走 `controller.Restart` |
| `backend/internal/service/adguard_service_test.go` | controller mock 单测 |
| `backend/api/aurora.go` | 删除服务模式下的退出 Stop 钩子；boot 路径跳过 `StartWithBootRetry` |
| `backend/api/internal/svc/servicecontext.go` | 构造并注入 controller |
| `docs/AuroraMihomo-Go-Zero-API.api` + goctl | status DTO 增加运行形态字段（若前端需要） |
| `frontend/src/views/AdGuardView.vue` / `AdGuardSettingsDialog.vue` | 运行形态展示、自启开关文案 |
| `docs/AuroraMihomo-Deployment-Design.md` + `userdocs/user-guide.md` + `frontend/src/content/user-guide.md` | 服务化说明 |

---

### Task 1: 服务控制器接口与 systemd/OpenRC 实现

**Files:**
- Create: `backend/internal/adguard/svc_controller.go`
- Create: `backend/internal/adguard/svc_unit_templates.go`
- Create: `backend/internal/adguard/svc_controller_test.go`

- [x] **Step 1: 接口定义**

```go
// ServiceController 抽象 AGH 的系统服务管理；nil 表示无服务管理器（回落 exec 子进程）。
type ServiceController interface {
    Install(ctx context.Context, binPath, workDir, cfgFile string) error // 写 unit + daemon-reload + enable
    Uninstall(ctx context.Context) error                                  // stop + disable + 删 unit
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Restart(ctx context.Context) error
    IsEnabled() bool    // unit 存在且 enable
    Active() bool       // systemctl is-active / rc-service status
}
```

- [x] **Step 2: 平台分派** — 有 `systemctl` 走 systemd；有 `rc-update`/`rc-service` 走 OpenRC（与 `scripts/install.sh` 分派逻辑一致）；其余（含 Windows）返回 nil → Manager 回落 exec 路径。

- [x] **Step 3: unit 模板** — systemd + OpenRC 各一份，按 D2 静态内容；OpenRC 用 `supervise-daemon`（对标 `/etc/init.d/auroramihomo` 写法，`supervisor="supervise-daemon"`）。unit 写入 `/etc/systemd/system/aurora-adguardhome.service` / `/etc/init.d/aurora-adguardhome`，随后 `systemctl daemon-reload` / `chmod +x`。

- [x] **Step 4: 单测** — 模板渲染后**断言不含 `--web-addr` / `--dns` 等端口参数**（防回归 D2）；命令序列断言（mock exec）；分派在无 systemctl 时返回 nil。

```bash
go test ./backend/internal/adguard/ -run ServiceController -count=1
```

- [x] **Step 5: Commit** `feat(adguard): 服务控制器（systemd/OpenRC）`

---

### Task 2: Manager 接入控制器 + 端口唯一事实来源

**Files:**
- Modify: `backend/internal/adguard/manager.go`
- Modify: `backend/internal/adguard/manager_test.go`

- [x] **Step 1: `Manager` 增加 `controller ServiceController` 字段**（构造时可 nil）；`SetController` 注入。`controller != nil` 时 `Start/Stop/Restart` 全部走控制器；`Status().Running` 改为 `controller.Active() || webPortOpen()`。

- [x] **Step 2: 去掉 `--web-addr` 命令行参数**（manager.go:143-147、184）——exec 路径也统一去掉：yaml 已由 `EnsureBootstrapConfig`/`SetWebPort` 维护，命令行覆盖只会造成双来源漂移。`m.cfg.WebAddr` 降级为纯 Status/反代回显字段，`SetWebAddr` 保留。

- [x] **Step 3: `webPortOpen` 探测地址改为 `ReadWebPort(workDir)` 优先**（manager.go:345）——面板 down 期间用户在 AGH 侧改过端口的场景下，面板重启后探测/幂等判断使用真实端口；`cfg.WebAddr` 兜底。

- [x] **Step 4: 单测** — exec 路径参数不再含 `--web-addr`；`webPortOpen` 在 yaml 端口与 cfg 不一致时用 yaml 端口；controller 非 nil 时 Start 不 spawn 进程。

```bash
go test ./backend/internal/adguard/ -count=1
```

- [x] **Step 5: Commit** `feat(adguard): Manager 接入服务控制器，yaml 为端口唯一来源`

---

### Task 3: 服务编排语义（AdGuardService）

**Files:**
- Modify: `backend/internal/service/adguard_service.go`
- Modify: `backend/internal/service/adguard_settings.go`
- Modify: `backend/internal/service/adguard_service_test.go`

- [x] **Step 1: Install 注册服务** — `updater.UpdateAdGuard` 成功后调 `controller.Install(binPath, workDir, cfgFile)`（失败则提示但保留二进制，便于手工修复）；原有 `EnsureBindLocalhost` / `ReadDNSPort` 落库逻辑保留。

- [x] **Step 2: Uninstall 顺序调整**（adguard_service.go:573）— wiring rollback → `controller.Uninstall`（stop + disable + 删 unit）→ 删 bin/.bak → `RemoveAll workDir` → 清 settings。**disable 必须发生在删二进制之前**，否则开机后 unit 指向已删二进制。

- [x] **Step 3: 自启语义** — 服务模式下 `ShouldStartAtBoot()` 返回 `controller.IsEnabled()`（settings `enabled_at_boot` 保留为回显/兼容）；面板 boot 路径（`StartWithBootRetry`）在服务模式下跳过（见 Task 4）。用户「停止」只 `controller.Stop` 不 disable（adguard_service.go:336 改为不再清 `enabled_at_boot`——语义从「期望运行」变为「当前是否 enable」，由新开关 API 驱动）。

- [x] **Step 4: 自动更新链路** — `StopProcess` / `Start`（adguard_service.go:349、239）在服务模式下走 `controller.Stop/Start`；`Restart` 走 `controller.Restart`。.bak 机制不变。

- [x] **Step 5: 改端口** — `SetWebPort`（adguard_settings.go:53）与 `SetDNSListenPort`（:82）的 `Restart` 调用在服务模式下走 `controller.Restart`；**unit 零操作**（D2 的直接收益）。

- [x] **Step 6: 单测** — controller mock：Install 后注册、Uninstall 顺序（disable 先于删 bin）、Stop 不清 enable、自启语义切换、改端口走 controller.Restart 且不重写 unit。

```bash
go test ./backend/internal/service/ -run AdGuard -count=1
```

- [x] **Step 7: Commit** `feat(adguard): 服务化编排（注册/注销/自启/更新/改端口）`

---

### Task 4: 面板生命周期接入

**Files:**
- Modify: `backend/api/internal/svc/servicecontext.go`
- Modify: `backend/api/aurora.go`

- [x] **Step 1: servicecontext.go 构造 controller**（:297-312 附近）并 `mgr.SetController(...)`；按平台分派（Task 1）。

- [x] **Step 2: 删除服务模式下的退出 Stop 钩子**（aurora.go:376 `_ = svcCtx.AdGuardManager.Stop(stopCtx)`）——仅服务模式删除；exec 模式（controller nil）保留，避免 Windows 上进程泄漏。

- [x] **Step 3: boot 路径** — 服务模式下跳过 `StartWithBootRetry`（系统服务自己起来）；exec 模式原样。

- [x] **Step 4: 验证** — `go build ./backend/api/`；`make check` 相关项保持绿。

- [x] **Step 5: Commit** `feat(api): 面板生命周期接入服务化`

---

### Task 5: 前端展示（运行形态 / 自启文案）

**Files:**
- Modify: `docs/AuroraMihomo-Go-Zero-API.api` + goctl（若加字段）
- Modify: `frontend/src/views/AdGuardView.vue` / `AdGuardSettingsDialog.vue`

- [x] **Step 1: status DTO 增加 `managedBy`**（`"process" | "systemd" | "openrc"`）与 `bootEnabled`（服务模式读 `IsEnabled`）。走 `.api` → goctl 流程（核对 DO NOT EDIT 头与既有 handler 不被重写）。

- [x] **Step 2: 前端** — 设置弹窗「开机自启」开关在服务模式下文案改为「系统服务开机自启（面板退出后 DNS 仍常驻）」并调用 enable/disable API；状态区展示运行形态；面板 down 说明文案。

- [x] **Step 3: vitest** — 设置弹窗状态展示（若有 AdGuard 相关既有 spec 则扩展）。

- [x] **Step 4: Commit** `feat(frontend): AdGuard 服务化运行形态展示`

---

### Task 6: 文档 + 回归

- 更新 `docs/AuroraMihomo-Deployment-Design.md`：新增 `aurora-adguardhome` unit 说明（含 D2 的「unit 无端口参数」设计）、OpenRC 单元、面板退出后 DNS 常驻的边界（TProxy 规则随面板）。
- 更新双份 user-guide：服务模式说明、自启语义、面板 down 时的访问方式（SSH 转发 / 直连）。
- 回归：

```bash
go test ./backend/internal/adguard/ ./backend/internal/service/ ./backend/api/internal/... -count=1
cd frontend && npm run type-check && npm run lint -- --quiet && npm run test -- --run AdGuard
```

- [x] Commit `docs: AdGuard 服务化部署与用户说明`

---

### Task 7: 手工验收（执行者勾选）

- [ ] systemd 机：安装 → `systemctl status aurora-adguardhome` active；`systemctl list-unit-files` 已 enable
- [ ] 面板 `systemctl restart auroramihomo` → AGH 全程不中断（DNS 连续查询无断流）
- [ ] 改 Web 端口 → 只改 yaml + restart；`systemctl cat aurora-adguardhome` 内容与安装时一致（无重写）
- [ ] AGH 进程 kill -9 → 3 秒内被 systemd 拉起；面板 Status 不误报「已停止」
- [ ] 面板内点「停止」→ 进程停、unit 仍 enable（开机还会起）；点自启开关 → enable/disable 生效
- [ ] 自动更新 → 走 systemctl stop/start，无残留进程
- [ ] 卸载 → unit 删除 + disable，重启机器不再起 AGH；workdir/bin 清除
- [ ] OpenRC 机：同样走一遍（supervise-daemon 拉起、rc-update 状态）
- [ ] Windows（exec 模式）：现有行为无回归（面板退出仍停 AGH、改端口仍生效）

---

## Spec coverage

| Spec（embed 2026-08-02） | Tasks |
|------|-------|
| §1.2 独立服务模式（延后项） | 1–4 |
| §1.4 进程管理 | 1–2 |
| §2.1 更新链路服务化 | 3 |
| §5.3 生命周期（启动/退出/更新） | 3–4 |
| 部署文档 / user-guide | 6 |
| 手工验收 | 7 |

## Placeholder self-check

- 无 TBD。P2b（TProxy 规则持久化）与 P2c（Windows 服务化）不在本计划内，边界已在 D5 写明。
- 无新 migration（仅 settings KV，符合 AGENTS.md「数据库结构变更先提交确认」约束）。
- 系统级 unit 写入需要 root：面板在 systemd/OpenRC 部署下默认 root 运行（见部署文档），权限前提成立；非 root 部署时 Install 报错并提示。

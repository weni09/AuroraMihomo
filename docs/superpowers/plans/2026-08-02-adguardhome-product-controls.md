# AdGuard Home 产品化管控 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为已嵌入的 AdGuard Home 增加组件级开/关与彻底卸载、条件侧栏、安装引导，以及设置弹窗（运行/Web 端口/版本更新/DNS 模式/账号与 Aurora 密码同步）。

**Architecture:** 在 `feature/adguardhome-embed` 既有 Manager/反代/wiring 上扩展：`adguard.component_enabled` 控制导航与 API 门禁；关闭时强制 DNS 模式回 0 并 Stop；设置弹窗调专用 API；DNS 模式 0/1/2 演进自 wiring；ChangePassword 成功后条件同步 AGH 口令。

**Tech Stack:** 现有 Go service + go-zero API、Vue 3 Pinia、settings KV、既有 `backend/internal/adguard` 与 `AdGuardService`。

**Spec:** `docs/superpowers/specs/2026-08-02-adguardhome-product-controls-design.md`  
**Worktree:** `D:\goWork\AuroraMihomo\.worktrees\adguardhome-embed`  
**前置 embed spec:** `docs/superpowers/specs/2026-08-02-adguardhome-embed-design.md`

---

## File structure

| Path | Responsibility |
|------|----------------|
| `backend/internal/service/adguard_wiring.go` | 新增 settings 常量：`component_enabled`、`dns_mode`、`sync_password`、`auto_update`、`cdn_providers` |
| `backend/internal/service/adguard_service.go` | `SetComponentEnabled`、`Uninstall`、`SetDNSMode`、`SetWebPort`、`SetAdminPassword`、`SyncPasswordFromAurora` |
| `backend/internal/service/adguard_dns_mode.go` | 模式 0/1/2 预检/应用/回滚（从 wiring 演进） |
| `backend/internal/service/adguard_*_test.go` | 单测 |
| `backend/internal/adguard/users.go` | AGH 管理员密码写入（yaml 或官方方式） |
| `backend/internal/adguard/port_probe.go` | :53 占用探测 |
| `backend/internal/updater/updater.go` | UpdateAdGuard 使用 adguard CDN 列表回落 |
| `docs/AuroraMihomo-Go-Zero-API.api` + goctl | component/uninstall/settings/dnsMode 等 |
| `backend/api/internal/logic/protected/changePasswordLogic.go` | 同步钩子 |
| `frontend/src/stores/adguard.ts` / `settings.ts` | 状态 |
| `frontend/src/App.vue` | 条件导航 |
| `frontend/src/views/SettingsView.vue` | 组件开关 + 卸载 |
| `frontend/src/views/AdGuardView.vue` | 状态机 + 设置 Dialog |
| `frontend/src/components/AdGuardSettingsDialog.vue` | 设置弹窗 |
| `userdocs/user-guide.md` + `frontend/src/content/user-guide.md` | 文档 |

---

### Task 1: component_enabled 后端门禁

**Files:**
- Modify: `backend/internal/service/adguard_wiring.go`（常量）
- Modify: `backend/internal/service/adguard_service.go`
- Create/Modify: `backend/internal/service/adguard_component_test.go`
- Modify: `backend/api/...` status DTO 增加 `componentEnabled`

- [ ] **Step 1: 常量与读取**

```go
settingAdGuardComponent = "adguard.component_enabled" // "true" / "false"，默认 false

func (s *AdGuardService) ComponentEnabled() bool {
	v := strings.TrimSpace(s.getSetting(settingAdGuardComponent, "false"))
	return v == "1" || strings.EqualFold(v, "true") || v == "yes"
}
```

- [ ] **Step 2: SetComponentEnabled**

```go
// enabled=false: 若 dns mode!=0 先 ExitDNSMode；Stop；写 false。任一步失败返回 error 且不写 false。
// enabled=true: 只写 true。
func (s *AdGuardService) SetComponentEnabled(ctx context.Context, enabled bool) error
```

- [ ] **Step 3: Status 带 ComponentEnabled**；boot 自启增加 `ComponentEnabled() &&`

- [ ] **Step 4: 单测** 默认 false；enable/disable；disable 调用 stop（mock mgr）

```bash
go test ./backend/internal/service/ -run Component -count=1
```

- [ ] **Step 5: Commit** `feat(adguard): 组件开关 component_enabled`

---

### Task 2: Uninstall API

**Files:**
- Modify: `adguard_service.go` — `Uninstall(ctx, confirm bool) error`
- Test: 临时目录装假二进制 + workdir，uninstall 后不存在

```go
func (s *AdGuardService) Uninstall(ctx context.Context, confirm bool) error {
	if !confirm {
		return errors.New("请确认卸载（confirm=true）")
	}
	// 1 ExitDNSMode 2 Stop 3 remove binary+bak 4 RemoveAll workDir
	// 5 clear adguard.* settings 6 component_enabled=false
}
```

清除 settings 可用 `GetSettings("adguard.")` 若 repository 支持前缀；否则显式删已知 key 列表。

- [ ] 测试 + Commit `feat(adguard): 彻底卸载`

---

### Task 3: API — component / uninstall / status 字段

**Files:**
- `docs/AuroraMihomo-Go-Zero-API.api`
- goctl 生成
- logic 实现

```api
type AdGuardComponentReq {
	Enabled bool `json:"enabled"`
}
type AdGuardUninstallReq {
	Confirm bool `json:"confirm"`
}
// AdGuardStatusResp 增加:
// ComponentEnabled bool `json:"componentEnabled"`
// DnsMode int `json:"dnsMode"` // 后续任务填实，先 0

@handler adGuardSetComponent
put /api/v1/adguard/component (AdGuardComponentReq) returns (Result)

@handler adGuardUninstall
post /api/v1/adguard/uninstall (AdGuardUninstallReq) returns (Result)
```

```bash
goctl api go -api docs/AuroraMihomo-Go-Zero-API.api -dir backend/api --style goZero
go build ./backend/api/
```

- [ ] Commit `feat(api): AdGuard 组件开关与卸载接口`

---

### Task 4: 前端 — 设置开关 + 条件导航

**Files:**
- `frontend/src/stores/adguard.ts` — `componentEnabled`、`setComponent`、`uninstall`
- `frontend/src/views/SettingsView.vue` — 可选组件卡片
- `frontend/src/App.vue` — nav 仅当 enabled
- `frontend/src/router` — 可选 beforeEnter 检查（或页内提示）
- `AdGuardView.vue` — 组件关时提示

设置 UI：

```text
AdGuard Home
  [开关] 启用组件
  关闭将停止进程并隐藏菜单，文件保留。
  [彻底卸载] → Dialog 勾选确认 → POST uninstall
```

加载设置页时 `fetchStatus` 或 settings 接口带回 `adguardComponentEnabled`。  
也可仅用 `/adguard/status`（组件关时 status 仍可读 componentEnabled，**不要**因关组件对 status 返回 403，否则设置页无法展示开关状态）。

约定：

- `GET /adguard/status`：**始终允许**（返回 componentEnabled）
- start/install/iframe 等：组件关时 **403/错误「请先启用组件」**

- [ ] 单测：App 或 store 在 false 时过滤 nav（若可测）
- [ ] Commit `feat(frontend): AdGuard 组件开关与条件导航`

---

### Task 5: AdGuard 页安装引导状态机

**Files:**
- `frontend/src/views/AdGuardView.vue`
- `AdGuardView.spec.ts`

状态：

1. `!componentEnabled` → 去设置  
2. `!installed` → 安装引导 CTA  
3. `installed && !running` → 启动 + 设置按钮  
4. `running` → iframe + 设置  

去掉主路径旧 wiring 大入口（保留可进设置）。

- [ ] Commit `feat(frontend): AdGuard 安装引导状态机`

---

### Task 6: DNS 模式 0/1/2

**Files:**
- `backend/internal/adguard/port_probe.go` — `PortInUse(network, addr string) bool`
- `backend/internal/service/adguard_dns_mode.go`
- API: `GET/PUT /api/v1/adguard/dns-mode`
- 前端设置弹窗 E 区

```go
type DNSMode int // 0 none, 1 bind53, 2 redirect

func (s *AdGuardService) SetDNSMode(ctx context.Context, mode DNSMode, opts ...) error
```

- 模式 1：probe `:53`；占用则 error + 中文说明；改 AGH dns.port=53；可能需 root 提示  
- 模式 2：复用/调用现有 wiring 的 RedirectTProxy + ResolveConflict + PatchUpstream 默认组合  
- 模式 0：rollback 快照  
- `SetComponentEnabled(false)` 调用 `SetDNSMode(0)`  

迁移：若旧 `dns_wiring=on`，status 可读为 mode 2。

- [ ] 单测 build 计划与占用拒绝  
- [ ] Commit `feat(adguard): DNS 服务模式 0/1/2`

---

### Task 7: 设置弹窗 — 运行 / Web 端口 / 版本更新

**Files:**
- `frontend/src/components/AdGuardSettingsDialog.vue`
- API 若缺：`PUT /api/v1/adguard/web-port { port }`
- service: `SetWebPort` → yaml Ensure + Restart  
- settings 自动更新增加 `adguardAutoUpdate`  
- updater: 读 `adguard.cdn_providers` JSON，`buildCDNURLs` 时 AdGuard 专用列表优先  

弹窗分区 B/C/D 按 spec；出网说明只读展示系统 `useMihomoProxy`。

- [ ] Commit `feat(frontend): AdGuard 设置弹窗（运行/端口/更新）`

---

### Task 8: 账号密码 + Aurora 同步

**Files:**
- `backend/internal/adguard/users.go` — 写入 AGH 用户（查 AGH 配置 `users` 字段；bcrypt 与 AGH 兼容）
- `AdGuardService.SetCredentials(ctx, user, pass string)`
- `AdGuardService.SyncPasswordFromAurora(ctx, plainPassword string)` 若 sync 开  
- `changePasswordLogic.go` 在 SetSetting 成功后：

```go
if svcCtx.AdGuardService != nil && svcCtx.AdGuardService.PasswordSyncEnabled() {
    if err := svcCtx.AdGuardService.SyncPasswordFromAurora(l.ctx, req.NewPassword); err != nil {
        l.Errorf("同步 AdGuard 密码失败: %v", err)
        // Result message 可附加警告，仍 Success aurora 改密
    }
}
```

- API: `PUT /api/v1/adguard/credentials`  
- 弹窗 A 区 UI  

**注意：** AGH 密码哈希算法需与当前 AGH 版本一致；若只能调 HTTP API，则要求 AGH running 并用现会话——实现前用 yaml `users` 方案（官方安装向导同款）并在注释写明版本假设。

- [ ] 单测：sync flag off 不调用；on 时调用  
- [ ] Commit `feat(adguard): 管理员密码设置与 Aurora 同步`

---

### Task 9: 文档 + 回归

- 更新双份 user-guide：组件开关、卸载、设置弹窗、DNS 模式、密码同步  
- 删除/改写仅描述「常驻侧栏 wiring 按钮」的过时段落  
- `go test` 相关包；`npm run type-check`；AdGuard vitest  

```bash
go test ./backend/internal/adguard/ ./backend/internal/service/ ./backend/api/internal/logic/protected/ -count=1
cd frontend && npm run type-check && npm run test -- --run AdGuard
```

- [ ] Commit `docs: AdGuard 产品化管控说明`

---

### Task 10: 手工验收（执行者勾选）

- [ ] 默认无侧栏 AdGuard  
- [ ] 设置开启 → 菜单出现 → 安装引导  
- [ ] 关闭 → 停进程 + 菜单消失，bin 仍在  
- [ ] 卸载警告 → 文件与配置清除  
- [ ] 设置弹窗启停、改端口、检查更新  
- [ ] DNS 模式切换预检；关闭组件不留劫持  
- [ ] 勾选密码同步后改 Aurora 密，AGH 可登录（或失败提示）  

---

## Spec coverage

| Spec | Tasks |
|------|-------|
| §1 组件开/关/卸载 | 1–4 |
| §2 安装引导主页面 | 5 |
| §3 设置弹窗 | 6–8 |
| §4 测试验收 | 9–10 |
| 下载出网/CDN | 7 |
| 密码持续同步 | 8 |

## Placeholder self-check

无 TBD；DNS 模式复用 wiring 文件路径已标明；密码写入依赖 `users.go` 任务内实现并测。

---

**Plan complete and saved to `docs/superpowers/plans/2026-08-02-adguardhome-product-controls.md`.**

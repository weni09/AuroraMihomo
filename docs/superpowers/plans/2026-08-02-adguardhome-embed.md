# 内置 AdGuard Home Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按需下载并托管 AdGuard Home，经同源 `/adguard/` 反代 + iframe 使用官方 UI，并提供可选的 TProxy DNS 一键对接与回滚。

**Architecture:** AdGuard Home 作为与 mihomo 并列的可选子进程（`data/bin` + `data/adguardhome` work-dir）。下载走现有 `updater` CDN 管线；HTTP 反代挂在 go-zero 外的原生 mux 并强制 Aurora 会话；一键对接通过 settings 快照改 TProxy `DNSPort` / 可选 mihomo `dns.listen` / AGH upstream，失败回滚。

**Tech Stack:** Go + go-zero rest、现有 `updater`/`mihomo.ProcessManager` 模式、Vue 3 + Pinia、SQLite settings KV（无新 migration）。

**Spec:** `docs/superpowers/specs/2026-08-02-adguardhome-embed-design.md`

---

## File structure

| Path | Responsibility |
|------|----------------|
| `backend/internal/updater/adguard_asset.go` | `pickAdGuardAsset`、`adguardFileName` |
| `backend/internal/updater/adguard_asset_test.go` | 资产匹配单测 |
| `backend/internal/updater/updater.go` | `UpdateAdGuard`、`AdGuardBinaryPath`、`CheckLatest` 扩展、`AdGuardRepo` |
| `backend/internal/adguard/manager.go` | 进程 Start/Stop/Restart/Status（对标 mihomo） |
| `backend/internal/adguard/manager_test.go` | 假二进制启停测 |
| `backend/internal/adguard/yaml_patch.go` | 只读端口、有限补丁 upstream |
| `backend/internal/adguard/yaml_patch_test.go` | yaml 补丁单测 |
| `backend/internal/adguard/proxy.go` | `/adguard/` ReverseProxy（strip 前缀、WS、去 DENY frame） |
| `backend/internal/adguard/proxy_test.go` | 反代与鉴权单测 |
| `backend/internal/service/adguard_service.go` | 编排 install/status/wiring + settings |
| `backend/internal/service/adguard_wiring.go` | wiring 计划/apply/rollback |
| `backend/internal/service/adguard_wiring_test.go` | 计划计算与快照 |
| `backend/internal/service/transparent_service.go` | `dnsPortFn` 可被 wiring 覆盖 |
| `backend/api/internal/svc/servicecontext.go` | 注入 AdGuard manager/service、启动 boot |
| `backend/api/aurora.go` | 挂载 `/adguard/`、登录 Set-Cookie、staticFallback 顺序 |
| `backend/api/internal/logic/public/loginLogic.go` | 登录写会话 cookie |
| `docs/AuroraMihomo-Go-Zero-API.api` | 新类型与路由 |
| `backend/api/internal/handler|logic|types` | goctl 生成 + 手写 logic 实现 |
| `frontend/src/views/AdGuardView.vue` | 主页面 |
| `frontend/src/stores/adguard.ts` | 状态与动作 |
| `frontend/src/router/index.ts` / `App.vue` | 路由与侧栏 |
| `frontend/src/stores/settings.ts` / `SettingsView.vue` | 更新入口 |
| `frontend/src/views/LoginView.vue` | 若需配合 cookie（通常后端 Set-Cookie 即可） |
| `userdocs/user-guide.md` + `frontend/src/content/user-guide.md` | 用户文档 |

---

### Task 1: AdGuard release 资产选择

**Files:**
- Create: `backend/internal/updater/adguard_asset.go`
- Create: `backend/internal/updater/adguard_asset_test.go`
- Modify: `backend/internal/updater/updater.go`（`adguardFileName`、`AdGuardBinaryPath`、Config 字段可本任务先只加 path helper）

- [ ] **Step 1: 写失败单测**

官方资产名形如 `AdGuardHome_linux_amd64.tar.gz`、`AdGuardHome_windows_amd64.zip`（下划线、`GOOS_GOARCH`）。

```go
// backend/internal/updater/adguard_asset_test.go
package updater

import (
	"runtime"
	"strings"
	"testing"
)

func TestPickAdGuardAsset_CurrentPlatform(t *testing.T) {
	rel := &githubRelease{
		TagName: "v0.107.61",
		Assets: []githubAsset{
			{Name: "AdGuardHome_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/linux.tgz", Size: 10},
			{Name: "AdGuardHome_windows_amd64.zip", BrowserDownloadURL: "https://example/win.zip", Size: 11},
			{Name: "AdGuardHome_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example/mac.tgz", Size: 12},
			{Name: "AdGuardHome_linux_amd64.deb", BrowserDownloadURL: "https://example/deb", Size: 9},
		},
	}
	url, name, _, err := pickAdGuardAsset(rel)
	if err != nil {
		t.Fatalf("当前平台应匹配到资产: %v", err)
	}
	if strings.HasSuffix(strings.ToLower(name), ".deb") {
		t.Fatalf("不应选中 deb: %s", name)
	}
	_ = url
	// 文件名应含 adguardhome 与当前 goos 片段
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "adguardhome") {
		t.Fatalf("name=%s", name)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(lower, ".zip") {
		t.Fatalf("windows 期望 zip, got %s", name)
	}
	if runtime.GOOS != "windows" && !strings.Contains(lower, ".tar.gz") && !strings.HasSuffix(lower, ".gz") {
		// 官方多为 .tar.gz
		t.Logf("got archive %s (ok if tar.gz)", name)
	}
}

func TestPickAdGuardAsset_Unsupported(t *testing.T) {
	rel := &githubRelease{TagName: "v1", Assets: nil}
	// 通过临时把逻辑写成对空 assets 返回 error 即可；本测只断言无匹配时有 error
	_, _, _, err := pickAdGuardAsset(rel)
	if err == nil {
		t.Fatal("无资产应 error")
	}
}

func TestAdGuardFileName(t *testing.T) {
	n := adguardFileName()
	if runtime.GOOS == "windows" {
		if n != "AdGuardHome.exe" {
			t.Fatalf("got %s", n)
		}
	} else if n != "AdGuardHome" {
		t.Fatalf("got %s", n)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./backend/internal/updater/ -run 'TestPickAdGuardAsset|TestAdGuardFileName' -count=1
```

Expected: FAIL（函数未定义）

- [ ] **Step 3: 实现 `pickAdGuardAsset` 与 `adguardFileName`**

```go
// backend/internal/updater/adguard_asset.go
package updater

import (
	"fmt"
	"runtime"
	"strings"
)

func adguardFileName() string {
	if runtime.GOOS == "windows" {
		return "AdGuardHome.exe"
	}
	return "AdGuardHome"
}

// pickAdGuardAsset 从 AdGuardHome release 中选当前平台压缩包。
// 官方命名：AdGuardHome_<os>_<arch>.tar.gz | .zip；排除 .deb/.rpm 等。
func pickAdGuardAsset(rel *githubRelease) (url, name string, size int64, err error) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	// 官方 arch 用 amd64/arm64/386 等
	var needle string
	switch {
	case goos == "windows" && goarch == "amd64":
		needle = "windows_amd64"
	case goos == "windows" && goarch == "arm64":
		needle = "windows_arm64"
	case goos == "linux" && goarch == "amd64":
		needle = "linux_amd64"
	case goos == "linux" && goarch == "arm64":
		needle = "linux_arm64"
	case goos == "linux" && goarch == "arm" || goos == "linux" && goarch == "armv7":
		needle = "linux_armv7"
	case goos == "darwin" && goarch == "amd64":
		needle = "darwin_amd64"
	case goos == "darwin" && goarch == "arm64":
		needle = "darwin_arm64"
	default:
		return "", "", 0, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
	wantZip := goos == "windows"
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if isDistroPackage(lower) {
			continue
		}
		if !strings.Contains(lower, "adguardhome") {
			continue
		}
		if !strings.Contains(lower, needle) {
			continue
		}
		if wantZip {
			if !strings.HasSuffix(lower, ".zip") {
				continue
			}
		} else {
			if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
				continue
			}
		}
		return a.BrowserDownloadURL, a.Name, a.Size, nil
	}
	return "", "", 0, fmt.Errorf("no AdGuardHome asset matched for %s/%s in %s", goos, goarch, rel.TagName)
}
```

注意：`isDistroPackage` 已在 `updater.go` 同包；`githubRelease`/`githubAsset` 已存在。若 `armv7` case 语法在 go 里要用括号：

```go
case goos == "linux" && (goarch == "arm" || goarch == "armv7"):
```

- [ ] **Step 4: 测试通过**

```bash
go test ./backend/internal/updater/ -run 'TestPickAdGuardAsset|TestAdGuardFileName' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/updater/adguard_asset.go backend/internal/updater/adguard_asset_test.go
git commit -m "feat(updater): AdGuard Home release 资产选择"
```

---

### Task 2: UpdateAdGuard 下载安装

**Files:**
- Modify: `backend/internal/updater/updater.go`
- Modify: `backend/internal/updater/updater_test.go`（可 mock 有限测；下载可用现有 download 测试模式）
- Modify: `backend/api/internal/config/config.go`（可选 `AdGuardRepo` 默认）

- [ ] **Step 1: 扩展 Config 与路径**

在 `updater.Config` 增加：

```go
AdGuardRepo       string
AdGuardBinaryPath string // 空则 DataDir/bin/AdGuardHome[.exe]
```

`New` 里：

```go
if cfg.AdGuardRepo == "" {
    cfg.AdGuardRepo = "AdguardTeam/AdGuardHome"
}
if cfg.AdGuardBinaryPath == "" {
    cfg.AdGuardBinaryPath = filepath.Join(cfg.DataDir, "bin", adguardFileName())
}
```

```go
func (m *Manager) AdGuardBinaryPath() string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.cfg.AdGuardBinaryPath
}

func (m *Manager) repoAdGuard() string {
    return m.cfg.AdGuardRepo
}
```

- [ ] **Step 2: 实现 `UpdateAdGuard`**

对标 `UpdateMihomo`：

1. `updateMu.Lock`
2. `latestRelease(ctx, repoAdGuard())`
3. `pickAdGuardAsset`
4. `downloadWithCDN` 到临时目录
5. 解压：`.zip` → `unzip`；`.tar.gz` → 实现或复用 tar 解压（若项目无 tar 工具函数，本任务在 `updater.go` 增加 `untarGz`，单测用小 fixture）
6. `findExtractedBinary(extractDir, "AdGuardHome")`（Windows 注意 `.exe`）
7. 若目标已存在：`copyFile` 到 `target+".bak"` 再覆盖 target
8. 非 Windows `chmod 755`
9. 可选：`exec.Command(target, "--version")` 或 `-v` / `-h` 探测（以官方 CLI 为准；失败则仍保留文件但返回 error 让上层感知）
10. 若有 version persister 类似 zashboard，可 `SetAdGuardVersion`；P1 也可用 settings 在 service 层记 tag

**解压 tar.gz：** 若仓库没有现成函数，新增：

```go
func untarGz(src, destDir string) error
```

只处理常规文件与目录，防 `../` 路径穿越（zip 侧若已有同样检查则对齐）。

- [ ] **Step 3: 扩展 `CheckLatest`**

```go
func (m *Manager) CheckLatest(ctx context.Context, mihomoLocalVersion string) (mihomo, zashboard, adguard ComponentCheck)
```

这是 **破坏性签名变更**：同步改所有调用方（`updateCheckLogic`、测试）。

`ComponentCheck` 已有则复用；adguard 的 local version 从 settings/文件探测传入，或 `CheckLatest` 增加参数 `adguardLocal string`。

- [ ] **Step 4: 单测**

- `AdGuardBinaryPath` 默认路径  
- `untarGz` 路径穿越拒绝（若新建）  
- `CheckLatest` 三返回值编译通过  

```bash
go test ./backend/internal/updater/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/updater/ backend/api/internal/config/
git commit -m "feat(updater): 下载安装 AdGuard Home 二进制"
```

---

### Task 3: adguard 进程 Manager

**Files:**
- Create: `backend/internal/adguard/manager.go`
- Create: `backend/internal/adguard/manager_test.go`

- [ ] **Step 1: 定义 API**

```go
package adguard

type Config struct {
	BinaryPath string
	WorkDir    string // data/adguardhome
	WebAddr    string // 127.0.0.1:3000 — 通过启动参数或首次 yaml；P1 用官方默认 + 文档
}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	WorkDir   string `json:"workDir"`
	WebAddr   string `json:"webAddr"`
	LastError string `json:"lastError,omitempty"`
}

type Manager struct { /* opMu, mu, cmd, exited, cfg, lastErr */ }

func NewManager(cfg Config) *Manager
func (m *Manager) Start(ctx context.Context) error
func (m *Manager) Stop(ctx context.Context) error
func (m *Manager) Restart(ctx context.Context) error
func (m *Manager) Status() Status
```

启动命令（查官方 CLI，常见形态）：

```text
AdGuardHome -w <WorkDir> --no-etc-hosts
```

或：

```text
AdGuardHome --work-dir <WorkDir>
```

**实现前用本机 `AdGuardHome -h` 核对 flag**；计划默认：

```go
cmd := exec.Command(m.cfg.BinaryPath, "--work-dir", m.cfg.WorkDir)
// 不绑定请求 ctx（与 mihomo 相同常驻语义）
```

Web 只听 127.0.0.1：优先在 **首次生成/补丁 yaml** 的 `bind_host`（Task 4/5），而不是依赖可能不存在的 CLI。

- [ ] **Step 2: 测试用假二进制**

```go
// manager_test.go — 写一个小脚本/可执行到 t.TempDir
// Windows: .bat 或 go test 内 build 的 helper
// 策略：用 `os.Args` 写一个 wait-on-signal 的 go run 太重；
// 对标 mihomo manager_test：查找已有 helper 或 echo 循环 sleep
```

最小测：

1. 无二进制 → Start 返回 error，Status.Installed=false  
2. 假二进制（`#!/bin/sh\nsleep 60` 或 windows `ping -t`）→ Start 后 Running=true → Stop 后 Running=false  

- [ ] **Step 3: 实现 Manager**（对标 `mihomo/manager.go` 的 opMu、pipe 可简化只留 lastError）

- [ ] **Step 4: 测试通过 + Commit**

```bash
go test ./backend/internal/adguard/ -count=1
git add backend/internal/adguard/
git commit -m "feat(adguard): 进程启停 Manager"
```

---

### Task 4: yaml 只读端口与 upstream 有限补丁

**Files:**
- Create: `backend/internal/adguard/yaml_patch.go`
- Create: `backend/internal/adguard/yaml_patch_test.go`

- [ ] **Step 1: 失败单测**

```go
func TestReadDNSPort(t *testing.T) {
	dir := t.TempDir()
	content := []byte("dns:\n  port: 1053\nbind_host: 127.0.0.1\n")
	// 写入 AdGuardHome.yaml
	port, err := ReadDNSPort(dir)
	if err != nil || port != 1053 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestPatchUpstream_Insert(t *testing.T) {
	// 将 dns.upstream_dns 设为 ["127.0.0.1:1054"]，保留其它键
}
```

- [ ] **Step 2: 实现**

使用 `gopkg.in/yaml.v3`（项目已有依赖）解析为 `map[string]any` 或小结构体：

```go
func ReadDNSPort(workDir string) (int, error)
func ReadWebPort(workDir string) (int, error) // http.port 等字段以实际 yaml 为准
func EnsureBindLocalhost(workDir string) error  // bind_host: 127.0.0.1
func PatchUpstreamDNS(workDir string, upstreams []string) (previous []string, err error)
func RestoreUpstreamDNS(workDir string, previous []string) error
```

**禁止**整文件重写丢掉用户配置：load → 改字段 → marshal 写回（接受 key 顺序变化）。

- [ ] **Step 3: 测试通过 + Commit**

```bash
go test ./backend/internal/adguard/ -count=1
git commit -m "feat(adguard): yaml 端口读取与 upstream 补丁"
```

---

### Task 5: AdGuardService + settings + wiring 核心

**Files:**
- Create: `backend/internal/service/adguard_service.go`
- Create: `backend/internal/service/adguard_wiring.go`
- Create: `backend/internal/service/adguard_wiring_test.go`
- Modify: `backend/internal/service/transparent_service.go`（DNS 端口覆盖）
- Modify: `backend/internal/service/config_service.go`（若需改 dns.listen 辅助方法）

**Settings keys（常量）：**

```go
const (
	settingAdGuardBoot     = "adguard.enabled_at_boot"
	settingAdGuardWebAddr  = "adguard.web_addr"
	settingAdGuardDNSPort  = "adguard.dns_port"
	settingAdGuardVersion  = "adguard.version"
	settingAdGuardWiring   = "adguard.dns_wiring"           // "off" | "on"
	settingAdGuardSnapshot = "adguard.dns_wiring_snapshot" // JSON
)
```

- [ ] **Step 1: wiring 计划纯函数单测**

```go
type WiringOptions struct {
	RedirectTProxy bool
	ResolveConflict bool
	PatchUpstream  bool
	WeakenTUNHijack bool // 默认 false
}

type WiringPlan struct {
	Actions []string
	AGHDNSPort int
	MihomoDNSListen string // 变更后
	// ...
}

func buildWiringPlan(opts WiringOptions, cur currentDNSState) (WiringPlan, error)
```

测试用例：

1. TProxy on + 同端口 1053 → 计划含「改 mihomo listen 127.0.0.1:1054」「TProxy DNS→AGH」  
2. wiring 已 off + 无 TProxy → Redirect 动作跳过或标 warning  

- [ ] **Step 2: 实现 `AdGuardService`**

```go
type AdGuardService struct {
	db       *repository.Database // 或现有 settings 接口
	updater  *updater.Manager
	mgr      *adguard.Manager
	transp   *TransparentService
	cfgSvc   *ConfigService
	// ...
}

func (s *AdGuardService) Status(ctx context.Context) (*AdGuardStatusDTO, error)
func (s *AdGuardService) Install(ctx context.Context) error // UpdateAdGuard + 记 version
func (s *AdGuardService) Start/Stop/Restart(...)
func (s *AdGuardService) WiringPreview(ctx context.Context) (*WiringPlan, error)
func (s *AdGuardService) WiringApply(ctx context.Context, opts WiringOptions) error
func (s *AdGuardService) WiringRollback(ctx context.Context) error
```

**Transparent DNS 覆盖：**

```go
// TransparentService 增加：
dnsPortOverride func() int // 非 nil 且 >0 时优先于 dnsPortFn

func (s *TransparentService) SetDNSPortOverride(fn func() int) {
	s.dnsPortOverride = fn
}

func (s *TransparentService) dnsPort() int {
	if s.dnsPortOverride != nil {
		if p := s.dnsPortOverride(); p > 0 {
			return p
		}
	}
	// 原逻辑
}
```

在 `ServiceContext` 里：

```go
transp.SetDNSPortOverride(func() int {
	// 读 settings wiring==on 时返回 AGH dns port
})
```

**Apply 顺序：** 读状态 → 建 plan → 写 snapshot → 执行（改 listen / patch upstream / set wiring on / Resync）→ 失败 rollback。

**改 mihomo dns.listen：** 通过 ConfigService 读 base、改 Extra[`listen`]、保存并 reload（复用现有保存 base 路径；**不要**手写绕过 merge）。若只有 final 可改，优先改 base 中 dns 段以持久。

- [ ] **Step 3: 单测 buildWiringPlan + 快照 JSON roundtrip**

```bash
go test ./backend/internal/service/ -run AdGuard -count=1
```

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(service): AdGuard 编排与 DNS wiring 计划"
```

---

### Task 6: API 规格 + goctl + logic

**Files:**
- Modify: `docs/AuroraMihomo-Go-Zero-API.api`
- Regenerate: `backend/api/internal/handler|logic|types`（goctl）
- Implement logic bodies
- Modify: `backend/api/internal/svc/servicecontext.go`
- Modify: `updateCheckLogic` 返回 adguard 项

- [ ] **Step 1: 在 `.api` 增加类型与路由**

```api
type AdGuardStatusResp {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	WorkDir   string `json:"workDir"`
	WebAddr   string `json:"webAddr"`
	DNSPort   int    `json:"dnsPort"`
	Wiring    string `json:"wiring"`
	LastError string `json:"lastError,optional"`
	// EntryPath 同源反代路径，前端 iframe 用
	EntryPath string `json:"entryPath"`
}

type AdGuardWiringResp {
	Wiring  string   `json:"wiring"`
	Actions []string `json:"actions"`
	// 预检警告
	Warnings []string `json:"warnings,optional"`
}

type AdGuardWiringApplyReq {
	RedirectTProxy  bool `json:"redirectTProxy,optional"`
	ResolveConflict bool `json:"resolveConflict,optional"`
	PatchUpstream   bool `json:"patchUpstream,optional"`
	WeakenTUNHijack bool `json:"weakenTunHijack,optional"`
}

// 在 @server 已有 jwt 组内：
@handler adGuardStatus
get /api/v1/adguard/status returns (AdGuardStatusResp)

@handler adGuardInstall
post /api/v1/adguard/install returns (Result)

@handler adGuardStart
post /api/v1/adguard/start returns (Result)

@handler adGuardStop
post /api/v1/adguard/stop returns (Result)

@handler adGuardRestart
post /api/v1/adguard/restart returns (Result)

@handler adGuardWiring
get /api/v1/adguard/wiring returns (AdGuardWiringResp)

@handler adGuardWiringApply
post /api/v1/adguard/wiring/apply (AdGuardWiringApplyReq) returns (Result)

@handler adGuardWiringRollback
post /api/v1/adguard/wiring/rollback returns (Result)

@handler updateAdGuard
post /api/v1/update/adguard returns (Result)
```

扩展 `UpdateCheck` 相关 resp 增加 `AdGuard` 组件字段（与 mihomo/zashboard 对称）。

- [ ] **Step 2: goctl 生成**

```bash
# 生成前确认 backend/api/internal 无未提交手写脏改；按 backend/AGENTS.md 命令执行
goctl api go -api docs/AuroraMihomo-Go-Zero-API.api -dir backend/api --style goZero
```

核对：`DO NOT EDIT` 头在、新路由出现、旧 handler 未被误删。

- [ ] **Step 3: 实现 logic** → 调 `AdGuardService`；错误返回中文 message。

- [ ] **Step 4: ServiceContext 接线**

```go
aghMgr := adguard.NewManager(adguard.Config{
	BinaryPath: upd.AdGuardBinaryPath(),
	WorkDir:    filepath.Join(dataDir, "adguardhome"),
	WebAddr:    "127.0.0.1:3000",
})
aghSvc := service.NewAdGuardService(...)
// boot: if enabled_at_boot { go aghSvc.Start }
// shutdown hook: 若 aurora 有统一 Shutdown，注册 Stop
```

- [ ] **Step 5: `go build` / 相关 test**

```bash
go test ./backend/api/... -count=1
go build -o bin/auroramihomo ./backend/api
```

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(api): AdGuard 状态/启停/更新/wiring 接口"
```

---

### Task 7: 登录 Cookie + `/adguard/` 反代

**Files:**
- Create: `backend/internal/adguard/proxy.go`
- Create: `backend/internal/adguard/proxy_test.go`
- Modify: `backend/api/internal/logic/public/loginLogic.go`
- Modify: `backend/api/aurora.go`（`staticFallback` 前挂载）
- Modify: 登出逻辑（若有）清 cookie

**Cookie 名：** `aurora_session`  
**属性：** `Path=/adguard`（或 `/` 若其它也要用）、`HttpOnly`、`SameSite=Lax`、`Secure` 仅当请求为 HTTPS。  
**值：** 与 JWT 相同字符串（校验复用 `verifyWSToken` 同类 parse）。

- [ ] **Step 1: Login 成功 Set-Cookie**

在 `Login` 返回前无法直接写 header（logic 只回 body）—— **改 handler** 或 login 后在 custom middleware 写 cookie。

推荐：改 `loginHandler` 在写 JSON 前：

```go
http.SetCookie(w, &http.Cookie{
	Name:     "aurora_session",
	Value:    resp.Token,
	Path:     "/",
	HttpOnly: true,
	SameSite: http.SameSiteLaxMode,
	// MaxAge: 与 AccessExpire 一致
})
```

前端仍可继续用 `localStorage` 的 token 调 API；cookie 专供 `/adguard` 与可选用。

- [ ] **Step 2: 反代鉴权**

```go
func ServeAdGuardProxy(mgr *Manager /* web upstream */, secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, secret) { // cookie 或 Authorization Bearer
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !mgr.Status().Running {
			http.Error(w, "adguard not running", http.StatusServiceUnavailable)
			return
		}
		// ReverseProxy to http://127.0.0.1:webPort
		// Director: strip /adguard prefix
		// ModifyResponse: 去掉 X-Frame-Options DENY；必要时改 Location
	})
}
```

`authorized`：先 `Cookie aurora_session`，再 `Authorization: Bearer`。

- [ ] **Step 3: 挂到 aurora**

在 `staticFallback` 内：

```go
if strings.HasPrefix(path, "/adguard") {
	adguardHandler.ServeHTTP(w, r)
	return
}
```

或 `mux.Handle("/adguard/", ...)` 优先于 SPA。

- [ ] **Step 4: 单测**

- 无 cookie → 401  
- 有合法 cookie → 后端 fake upstream 收到 strip 后路径 `/`  
- 响应 `X-Frame-Options: DENY` 被删除或改为 `SAMEORIGIN`

```bash
go test ./backend/internal/adguard/ -run Proxy -count=1
go test ./backend/api/ -run Security -count=1
```

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(adguard): 会话 cookie 与 /adguard 同源反代"
```

---

### Task 8: 前端 AdGuard 页 + 设置入口

**Files:**
- Create: `frontend/src/stores/adguard.ts`
- Create: `frontend/src/views/AdGuardView.vue`
- Create: `frontend/src/views/AdGuardView.spec.ts`（基础渲染/未安装 CTA）
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/App.vue`（navItems）
- Modify: `frontend/src/stores/settings.ts`、`SettingsView.vue`

- [ ] **Step 1: store**

```ts
// adguard.ts — 禁止 any
interface AdGuardStatus {
  installed: boolean
  running: boolean
  version: string
  webAddr: string
  dnsPort: number
  wiring: string
  entryPath: string
  lastError?: string
}
// fetchStatus, install, start, stop, restart, wiringPreview, apply, rollback
```

- [ ] **Step 2: AdGuardView.vue**

布局对标 ZashboardView：

- 顶栏：状态 Badge、安装/更新、启停、对接 Dialog、新标签（`entryPath`）  
- 未安装：主 CTA  
- 运行中：`<iframe :src="status.entryPath" class="..." title="AdGuard Home" />`  
- 对接 Dialog：四个 checkbox 对应 API flags，默认值按 spec  
- `setPageChrome` 避免双 header  

主题色：用 token，禁止 slate/gray 硬编码（FE1）。

- [ ] **Step 3: 路由与侧栏**

```ts
{ path: '/adguard', name: 'adguard', component: AdGuardView, meta: { title: 'AdGuard' } }
```

`navItems` 加在 Zashboard 附近，icon 选现有 lucide 之一（如 `Shield`）。

- [ ] **Step 4: settings 更新**

`updateAdGuard()` → `POST /update/adguard`；`checkUpdate` 展示 adguard 版本行。

- [ ] **Step 5: 前端检查**

```bash
cd frontend && npm run type-check && npm run test -- --run AdGuard
# 或 make type-check && make test-frontend
```

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(frontend): AdGuard 管理页与更新入口"
```

---

### Task 9: 用户文档 + 全量 check

**Files:**
- Modify: `userdocs/user-guide.md`
- Modify: `frontend/src/content/user-guide.md`（保持同步）
- Optional: `docs/AuroraMihomo-Transparent-Proxy.md` 增加「与 AdGuard 对接」短节

- [ ] **Step 1: 文档内容要点**

- AGH 为可选组件、GPL、按需下载  
- 安装/打开方式（侧栏、iframe）  
- 与 mihomo DNS 分工；一键对接拓扑与回滚  
- TUN/手动代理限制；Private DNS 绕过  
- 不自动 setcap :53  

- [ ] **Step 2: 跑检查**

```bash
make check
```

Expected: 全绿（或仅与本改动无关的既有 baseline）

- [ ] **Step 3: Commit**

```bash
git commit -m "docs: AdGuard Home 内置使用说明"
```

---

### Task 10: 手工验收清单（执行者勾选）

- [ ] 干净环境：未装 AGH 时代理/配置/TProxy 与改前一致  
- [ ] 安装 AGH → status.installed  
- [ ] 启动 → iframe 可完成 AGH 首次向导（或降级路径）  
- [ ] 未点对接：TProxy DNS 仍指向 mihomo  
- [ ] TProxy 环境 apply 对接：AGH 查询日志出现客户端查询  
- [ ] rollback 恢复  
- [ ] 登出或无 cookie 访问 `/adguard/` → 401  
- [ ] 更新 AGH 二进制（有 bak）后仍可启动  

---

## Spec coverage 自检

| Spec 章节 | 任务 |
|-----------|------|
| §1 进程目录端口 | Task 3, 5 |
| §2 下载更新设置 | Task 1, 2, 6, 8 |
| §3 反代 iframe 鉴权 | Task 7, 8 |
| §4 wiring | Task 5, 6, 8 |
| §5 API/许可/无新表 | Task 6, 9 |
| §6 测试验收 | 各 Task 单测 + Task 10 |

## Placeholder 自检

计划内步骤含具体路径、函数名与命令；实现时若官方 CLI flag 与本文不一致，**以 `AdGuardHome -h` 为准并改 Manager**，不静默忽略。

## 类型一致性

- API：`AdGuardStatusResp`、`AdGuardWiringApplyReq`  
- Settings key 前缀 `adguard.*`  
- 反代路径固定 `/adguard/`  
- Cookie：`aurora_session`  

---

**Plan complete and saved to `docs/superpowers/plans/2026-08-02-adguardhome-embed.md`.**

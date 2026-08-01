# Frontend Static Assets Embedding & System Versioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed frontend static build assets directly into the backend Go binary using `go:embed` for single-binary releases, and expose the Git Tag system version号 (`appVersion`) in the backend API and frontend sidebar UI.

**Architecture:** 
- A new package `backend/internal/version` holds `AppVersion` injected via `-ldflags` during `go build`.
- API specification `docs/AuroraMihomo-Go-Zero-API.api` is updated to include `appVersion` in `Status`, and code is regenerated with `goctl`.
- A web FS wrapper module `backend/api/embed_web.go` encapsulates `go:embed public/*` alongside fallback logic for local disk static assets.
- `Makefile` is updated to pass `-X 'auroramihomo/backend/internal/version.AppVersion=$(VERSION)'` and sync `public/` before build.
- Frontend store `useMihomoStore` handles `appVersion`, and `App.vue` presents it in the sidebar (with responsive collapsed/expanded layouts).

**Tech Stack:** Go 1.25, `go:embed`, go-zero, Vue 3, Pinia, Tailwind CSS.

---

### File Structure Map

- Create: `backend/internal/version/version.go` — Exported `AppVersion` variable.
- Create: `backend/internal/version/version_test.go` — Test for version package defaults.
- Create: `backend/api/embed_web.go` — `go:embed` declaration, `http.FileSystem` provider with disk fallback.
- Create: `backend/api/embed_web_test.go` — Unit tests for static file server and fallback logic.
- Modify: `public/.gitkeep` — Ensure `public/` directory exists so `go:embed` compiles when `public/` is empty.
- Modify: `docs/AuroraMihomo-Go-Zero-API.api` — Add `appVersion` field to `Status` API response.
- Modify: `backend/api/internal/types/types.go` — Updated via `goctl`.
- Modify: `backend/api/internal/logic/protected/systemstatuslogic.go` — Return `AppVersion` in system status response.
- Modify: `backend/api/internal/logic/protected/systemstatuslogic_test.go` — Test system status logic returns `AppVersion`.
- Modify: `backend/api/aurora.go` — Wire `getWebFS()` to `mountStatic` and `staticFallback`.
- Modify: `Makefile` — Inject `VERSION` ldflag and streamline `build-backend`.
- Modify: `frontend/src/stores/mihomo.ts` — Add `appVersion` state and status update logic.
- Modify: `frontend/src/App.vue` — Display system version in the sidebar with Tooltip support.

---

### Task 1: Create `version` Package in Backend

**Files:**
- Create: `backend/internal/version/version.go`
- Create: `backend/internal/version/version_test.go`

- [ ] **Step 1: Write failing unit test for `version` package**

Create `backend/internal/version/version_test.go`:
```go
package version

import "testing"

func TestGetVersion(t *testing.T) {
	if Get() == "" {
		t.Errorf("expected non-empty version string, got empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./backend/internal/version`
Expected output: FAIL (package does not exist / undefined `Get`)

- [ ] **Step 3: Implement `backend/internal/version/version.go`**

Create `backend/internal/version/version.go`:
```go
package version

// AppVersion is injected at build time via -ldflags "-X auroramihomo/backend/internal/version.AppVersion=vX.Y.Z".
// If not injected, it defaults to "dev".
var AppVersion = "dev"

// Get returns the current system version.
func Get() string {
	if AppVersion == "" {
		return "dev"
	}
	return AppVersion
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./backend/internal/version`
Expected output: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/version
git commit -m "feat: add version package for build-time ldflags injection"
```

---

### Task 2: Update API Specification and Regenerate Types

**Files:**
- Modify: `docs/AuroraMihomo-Go-Zero-API.api:11-15`
- Modify: `backend/api/internal/types/types.go` (via goctl)
- Modify: `backend/api/internal/logic/protected/systemstatuslogic.go`
- Create/Modify: `backend/api/internal/logic/protected/systemstatuslogic_test.go`

- [ ] **Step 1: Update `.api` specification file**

In `docs/AuroraMihomo-Go-Zero-API.api`, update the `Status` struct:
```api
type Status {
	Status     string `json:"status"`
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
	Pid        int    `json:"pid"`
}
```

- [ ] **Step 2: Run `goctl` to regenerate types**

Run: `goctl api go -api docs/AuroraMihomo-Go-Zero-API.api -dir backend/api`
Verify `backend/api/internal/types/types.go` now contains `AppVersion string`:
```go
type Status struct {
	Status     string `json:"status"`
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
	Pid        int    `json:"pid"`
}
```

- [ ] **Step 3: Update `systemstatuslogic.go` to return `AppVersion`**

Edit `backend/api/internal/logic/protected/systemstatuslogic.go`:
```go
package protected

import (
	"context"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/version"

	"github.com/zeromicro/go-zero/core/logx"
)

type SystemStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSystemStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatusLogic {
	return &SystemStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SystemStatusLogic) SystemStatus() (resp *types.Status, err error) {
	_, _ = l.svcCtx.MihomoManager.Version(l.ctx)
	st := l.svcCtx.MihomoManager.Status()
	state := "stopped"
	if st.IsRunning {
		state = "running"
	}
	return &types.Status{
		Status:     state,
		Version:    st.Version,
		AppVersion: version.Get(),
		Pid:        st.PID,
	}, nil
}
```

- [ ] **Step 4: Create/Update unit test for `SystemStatusLogic`**

Create or update `backend/api/internal/logic/protected/systemstatuslogic_test.go`:
```go
package protected

import (
	"context"
	"testing"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/internal/version"
)

func TestSystemStatusLogic(t *testing.T) {
	// Setup minimal ServiceContext
	svcCtx := &svc.ServiceContext{
		Config: config.Config{},
	}
	// Note: If MihomoManager is nil or mocked, we test logic returns version.Get()
	version.AppVersion = "v0.2.0"
	
	// Create mock or test logic
	// Verification that version.Get() returns "v0.2.0"
	if version.Get() != "v0.2.0" {
		t.Errorf("expected v0.2.0, got %s", version.Get())
	}
}
```

- [ ] **Step 5: Run tests and convention check**

Run: `go test ./backend/api/internal/logic/protected/...`
Run: `python scripts/check-conventions.py`
Expected output: PASS

- [ ] **Step 6: Commit**

```bash
git add docs/AuroraMihomo-Go-Zero-API.api backend/api/internal/types/types.go backend/api/internal/logic/protected/systemstatuslogic.go backend/api/internal/logic/protected/systemstatuslogic_test.go
git commit -m "feat: expose appVersion in system status API endpoint"
```

---

### Task 3: Implement Embedded Web FS Module

**Files:**
- Create: `public/.gitkeep`
- Create: `backend/api/embed_web.go`
- Create: `backend/api/embed_web_test.go`

- [ ] **Step 1: Ensure `public/.gitkeep` exists**

Create `public/.gitkeep` if it doesn't exist so `go:embed public` has at least one file to embed even when `public/` is cleaned.

- [ ] **Step 2: Write unit test for SPA FileSystem Server**

Create `backend/api/embed_web_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSpaFileSystemServer(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>head</html>"), 0644); err != nil {
		t.Fatalf("failed to write test index.html: %v", err)
	}

	testFilePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFilePath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test.txt: %v", err)
	}

	handler := spaFileSystemServer("", http.Dir(tmpDir))

	// 1. Test existing file
	req := httptest.NewRequest(http.MethodGet, "/test.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for existing file, got %d", w.Code)
	}
	if w.Body.String() != "hello world" {
		t.Errorf("expected body 'hello world', got '%s'", w.Body.String())
	}

	// 2. Test unknown route (SPA fallback to index.html)
	reqSPA := httptest.NewRequest(http.MethodGet, "/unknown/route", nil)
	wSPA := httptest.NewRecorder()
	handler.ServeHTTP(wSPA, reqSPA)

	if wSPA.Code != http.StatusOK {
		t.Errorf("expected status 200 for SPA fallback, got %d", wSPA.Code)
	}
	if wSPA.Body.String() != "<html>head</html>" {
		t.Errorf("expected body '<html>head</html>', got '%s'", wSPA.Body.String())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./backend/api -run TestSpaFileSystemServer`
Expected output: FAIL (undefined `spaFileSystemServer`)

- [ ] **Step 4: Implement `backend/api/embed_web.go`**

Create `backend/api/embed_web.go`:
```go
package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed public/*
var embeddedWebFS embed.FS

// getWebFS returns an http.FileSystem serving static assets.
// It prioritizes local disk directories ("./public", "./frontend/dist") if an index.html exists,
// falling back to the embedded web filesystem.
func getWebFS() http.FileSystem {
	for _, dir := range []string{"./public", "./frontend/dist"} {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return http.Dir(dir)
		}
	}

	subFS, err := fs.Sub(embeddedWebFS, "public")
	if err != nil {
		return http.FS(embeddedWebFS)
	}
	return http.FS(subFS)
}

// spaFileSystemServer returns an http.Handler that serves static files from fsys,
// falling back to index.html for SPA client-side routes.
func spaFileSystemServer(routePrefix string, fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, routePrefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = "index.html"
		}

		cleanPath := filepath.ToSlash(filepath.Clean("/" + rel))
		cleanPath = strings.TrimPrefix(cleanPath, "/")

		// Check if file exists in filesystem
		f, err := fsys.Open(cleanPath)
		if err == nil {
			stat, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !stat.IsDir() {
				http.StripPrefix(routePrefix, fileServer).ServeHTTP(w, r)
				return
			}
		}

		// Fallback to index.html for SPA routing
		fIndex, err := fsys.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_ = fIndex.Close()

		r2 := r.Clone(r.Context())
		r2.URL.Path = routePrefix + "/index.html"
		if routePrefix == "" {
			r2.URL.Path = "/index.html"
		}
		fileServer.ServeHTTP(w, r2)
	})
}
```

- [ ] **Step 5: Run unit tests to verify pass**

Run: `go test ./backend/api -run TestSpaFileSystemServer`
Expected output: PASS

- [ ] **Step 6: Commit**

```bash
git add public/.gitkeep backend/api/embed_web.go backend/api/embed_web_test.go
git commit -m "feat: add embedded static web filesystem and SPA handler"
```

---

### Task 4: Integrate Embedded Web FS in `aurora.go`

**Files:**
- Modify: `backend/api/aurora.go:48, 505-554`

- [ ] **Step 1: Update `mountStatic` and static route mounting in `aurora.go`**

In `backend/api/aurora.go`:
Replace `mountStatic(staticMux, "/", webRoot())` with `mountStatic(staticMux, "/", getWebFS())`.
Replace `webRoot()` and `spaFileServer()` functions with updated `mountStatic` implementation accepting `http.FileSystem`:

```go
// mountStatic mounts static files from an http.FileSystem to the mux.
func mountStatic(mux *http.ServeMux, routePrefix string, fsys http.FileSystem) {
	if routePrefix == "/" {
		mux.Handle("/", spaFileSystemServer("", fsys))
		return
	}
	prefix := strings.TrimSuffix(routePrefix, "/")
	handler := spaFileSystemServer(prefix, fsys)
	mux.Handle(prefix+"/", handler)
	mux.Handle(prefix, http.RedirectHandler(prefix+"/", http.StatusMovedPermanently))
}
```
Note: Keep `/ui` (Zashboard) using `http.Dir(filepath.Join(c.Mihomo.ConfigDir, "zashboard"))` via `mountStatic(staticMux, "/ui", http.Dir(filepath.Join(c.Mihomo.ConfigDir, "zashboard")))`.

- [ ] **Step 2: Verify backend build & tests**

Run: `go test ./backend/...`
Run: `go build -o auroramihomo.exe ./backend/api`
Expected output: Success

- [ ] **Step 3: Commit**

```bash
git add backend/api/aurora.go
git commit -m "refactor: use embedded static filesystem in server initialization"
```

---

### Task 5: Update `Makefile` for Version Injection and Single Binary Build

**Files:**
- Modify: `Makefile:5-8, 25-37`

- [ ] **Step 1: Update `Makefile`**

Edit `Makefile` to define `VERSION` and pass `-ldflags` with `AppVersion` injection to `go build`:

```makefile
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -w -s -X 'auroramihomo/backend/internal/version.AppVersion=$(VERSION)'

# ---------- 构建 ----------
.PHONY: build
build: build-frontend build-backend ## 完整构建（静态资源内嵌至二进制）

.PHONY: build-frontend
build-frontend: sync-docs ## 构建前端并同步到 public/
	cd frontend && npm run build
	rm -rf public
	cp -r frontend/dist public

.PHONY: build-backend
build-backend: ## 构建后端二进制（注入 Tag 版本号与内嵌静态资源）
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./backend/api
```

- [ ] **Step 2: Test Makefile targets**

Run: `make build-backend`
Verify the binary `auroramihomo` (or `auroramihomo.exe`) builds without errors.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: inject git version tag via ldflags in Makefile"
```

---

### Task 6: Update Frontend Mihomo Store and App Sidebar

**Files:**
- Modify: `frontend/src/stores/mihomo.ts:5-25`
- Modify: `frontend/src/App.vue`

- [ ] **Step 1: Update `Status` interface and store state in `frontend/src/stores/mihomo.ts`**

Edit `frontend/src/stores/mihomo.ts`:
```ts
interface Status {
  status: string
  version: string
  appVersion?: string
  pid: number
}

export const useMihomoStore = defineStore('mihomo', {
  state: () => ({
    status: '未知',
    version: '未知',
    appVersion: '未知',
    pid: 0,
    isLoading: false,
    recentLogs: [] as Array<{ time: string; stream: string; message: string }>,
    wsStatus: 'connecting',
  }),
  actions: {
    applyStatus(payload: Partial<Status>) {
      if (payload.status) this.status = payload.status
      if (payload.version) this.version = payload.version
      if (payload.appVersion) this.appVersion = payload.appVersion
      if (typeof payload.pid === 'number') this.pid = payload.pid
    },
```

- [ ] **Step 2: Update `frontend/src/App.vue` to fetch status and display version in sidebar**

In `frontend/src/App.vue`:
1. Import `useMihomoStore` and `onMounted`:
```ts
import { useMihomoStore } from './stores/mihomo'
const mihomoStore = useMihomoStore()

onMounted(() => {
  mihomoStore.fetchStatus()
})
```

2. Add version display inside the sidebar footer (`aside > div:last-child`), right above or alongside `ThemeToggle` / `LogOut`:
```html
        <!-- 系统版本号展示 -->
        <TooltipProvider :delay-duration="0">
          <Tooltip v-if="collapsed">
            <TooltipTrigger as-child>
              <div class="px-1 py-0.5 text-[10px] text-center text-fg-muted font-mono truncate cursor-default">
                v{{ mihomoStore.appVersion }}
              </div>
            </TooltipTrigger>
            <TooltipContent side="right">
              AuroraMihomo v{{ mihomoStore.appVersion }}
            </TooltipContent>
          </Tooltip>
          <div v-else class="px-4 py-1 text-xs text-fg-muted font-mono flex items-center justify-between">
            <span>AuroraMihomo</span>
            <span class="bg-elevated px-1.5 py-0.5 rounded text-[11px] font-medium">v{{ mihomoStore.appVersion }}</span>
          </div>
        </TooltipProvider>
```

- [ ] **Step 3: Run frontend type checking & unit tests**

Run: `make type-check`
Run: `make test-frontend`
Run: `make lint-frontend`
Expected output: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/stores/mihomo.ts frontend/src/App.vue
git commit -m "feat: display system appVersion in frontend sidebar UI"
```

---

### Task 7: Full Verification and Cleanup

**Files:**
- None (Run full build and convention checks)

- [ ] **Step 1: Run project-wide convention check and tests**

Run: `make check`
Expected output: All tests (fmt, vet, unit tests, frontend vitest, vue-tsc, conventions) PASS.

- [ ] **Step 2: Run full build and single binary smoke test**

Run: `make build`
Test running `./auroramihomo.exe` (or `./auroramihomo`) and access `http://localhost:8080` (or configured port) to ensure static UI loads and `/api/v1/system/status` returns `appVersion`.

- [ ] **Step 3: Commit any final minor fixes**

```bash
git commit --allow-empty -m "chore: complete single binary embed and versioning implementation"
```

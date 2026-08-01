# 前端静态资源嵌入与系统版本号管理设计方案

## 1. 方案背景与目标

目前 AuroraMihomo 部署时，二进制文件需要与 `public/` 静态资源目录同时存在。为了实现真正的“单二进制发布”，需要利用 Go 1.16+ 的 `go:embed` 特性将构建后的前端静态资源嵌入到可执行二进制文件中。

同时，系统需要展示自身的 Git Tag 版本号（如 `v0.2.0`），以便用户在前端界面（侧边栏等）清晰感知当前 AuroraMihomo 系统版本，并与 Mihomo 内核版本进行区分。

---

## 2. 详细设计

### 2.1 前端静态资源嵌入 (`go:embed`)

#### 后端文件架构变动
在 `backend/api/` 下增加 `web.go`（或在合适位置包装）：
- 包含声明：
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
  ```
- 路由/Handler 重构：
  - 当磁盘上不存在外部 `./public/index.html` 或 `./frontend/dist/index.html` 时，使用从 `embeddedWebFS` 中导出的 `fs.FS`（定位到 `public` 子目录）。
  - `spaFileServer` / `staticFallback` 逻辑支持 `http.FileSystem`，既支持 `http.Dir`（本地开发模式），也支持 `http.FS(subFS)`（内嵌发布模式）。
  - 维持 SPA 回退逻辑（非静态文件与 API 路径回退至 `index.html`）。

#### Makefile 构建链调整
- `make build-backend` 依赖 `build-frontend` 或确保在 `go build` 前 `public/` 目录存在且包含前端最新产物。
- 若在未构建前端时单独跑 `go build`，在根目录或者 `backend/api/public/` 下保留占位（或通过 `make build` 自动同步 `public` 文件夹）。

---

### 2.2 系统版本号管理 (Git Tag + `-ldflags`)

#### 版本号生成与注入
- 在 `backend/api/aurora.go` （或专用变量文件）中声明：
  ```go
  var AppVersion = "dev"
  ```
- `Makefile` 中在编译时注入 Git Tag：
  ```make
  VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
  LDFLAGS := -w -s -X 'main.AppVersion=$(VERSION)'
  ```

#### API 契约扩展 (`docs/AuroraMihomo-Go-Zero-API.api`)
- 更新 `Status` 结构体：
  ```api
  type Status {
      Status     string `json:"status"`
      Version    string `json:"version"`
      AppVersion string `json:"appVersion"`
      Pid        int    `json:"pid"`
  }
  ```
- 按照项目规范，使用 `goctl api go -api docs/AuroraMihomo-Go-Zero-API.api -dir backend/api` 生成更新类型。
- `SystemStatusLogic` 将 `AppVersion` 返回前端。

---

### 2.3 前端侧边栏版本号展示

#### Store 与类型更新
- `frontend/src/stores/mihomo.ts` 中更新 `Status` 接口与 state，加入 `appVersion: string`（默认 `dev` 或 `未知`）。
- 收到 `/system/status` 或 WebSocket 消息更新时，更新 `appVersion`。

#### UI 交互与样式
- 位置：`frontend/src/App.vue` 侧边栏底部（`aside` 的底栏内，位于主题切换 `ThemeToggle` 与退出登录按钮之间/下方）。
- 展开状态：显示 `AuroraMihomo v0.2.0` 或 `v0.2.0`（带有细体 muted 格式，符合既有设计规范）。
- 折叠状态：用 `Tooltip` 悬停提示 `系统版本: v0.2.0`，图标/文本优雅适配折叠宽度。

---

## 3. 校验与验证

1. **静态校验与机检**：
   - 运行 `make check` / `python scripts/check-conventions.py` 验证开发规范。
   - 运行 `make type-check` 和 `make lint-frontend` 确保前端无类型或语法报错。
2. **功能验证**：
   - 执行 `make build` 编译出单二进制 `auroramihomo`。
   - 在独立无 `public/` 文件夹的环境中启动二进制，访问 Web 界面，验证前端资源加载正常、路由刷新正常。
   - 验证侧边栏底部正确呈现当前的 Tag 版本号（如 `v0.2.0`）。

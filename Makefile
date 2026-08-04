# AuroraMihomo 构建与开发任务
# Windows 下建议在 Git Bash 中执行

SHELL := /bin/bash
BINARY := auroramihomo
ifeq ($(OS),Windows_NT)
	BINARY := auroramihomo.exe
endif

CONFIG := backend/api/etc/aurora-api.yaml
IMAGE := auroramihomo:latest

# 版本注入：
# - 精确命中 git tag 且工作树干净（真发布版）：直接用 tag 名，稳定可追溯；
# - 其余情况（非 tag 提交、或 tag 上仍有未提交改动）：dev + 编译时刻（到秒），
#   每次构建可区分。tag 上带脏工作树也按开发版处理——否则「tag 名 + 未发布的
#   改动」会被误认成已发布的正式版（git describe --exact-match 只看 HEAD 是否
#   落在 tag 提交上，看不出工作树里还有没提交的东西）。
# 外部可通过 `make VERSION=xxx build` 覆盖（?= 语义），CI 发版即这样传参。
VERSION ?= $(shell if git describe --tags --exact-match >/dev/null 2>&1 && test -z "$$(git status --porcelain)"; then git describe --tags --exact-match; else echo "dev-$(shell date +%Y%m%d%H%M%S)"; fi)
LDFLAGS := -w -s -X 'auroramihomo/backend/internal/version.AppVersion=$(VERSION)'

.PHONY: help
help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ---------- 依赖 ----------
.PHONY: deps
deps: ## 安装前后端依赖
	go mod download
	cd frontend && npm ci

# ---------- 构建 ----------
.PHONY: build
build: build-frontend build-backend ## 完整构建（前端产物会同步到 public/）

.PHONY: build-frontend
build-frontend: sync-docs ## 构建前端并同步到 public/ 与 backend/api/public（后者是 go:embed 内嵌源）
	cd frontend && npm run build
	rm -rf public
	cp -r frontend/dist public
	# backend/api/public 是 go:embed all:public 的嵌入源：不同步的话二进制里
	# 只有 .gitkeep，运行时删掉磁盘 public/ 就 404（getWebFS 的降级路径形同虚设）
	rm -rf backend/api/public
	cp -r frontend/dist backend/api/public
	# .gitkeep 被 git 跟踪（保证 embed 目录在 CI checkout 后仍然存在），
	# 上面的 rm -rf 把它删了，这里恢复，避免 git 状态显示删除
	touch backend/api/public/.gitkeep

.PHONY: build-backend
build-backend: ## 构建后端二进制（注入 Tag 版本号与内嵌静态资源）
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./backend/api

# ---------- 质量 ----------
.PHONY: check
check: fmt-check vet test type-check lint-frontend test-frontend conventions docs-check ## 本地跑一遍 CI 的主要检查

.PHONY: check-all
check-all: check lint ## 含 golangci-lint 的完整检查（需本地 golangci-lint 与 go.mod 版本匹配）

# 未纳入默认 check：golangci-lint 二进制若由低版本 Go 构建，会拒绝
# 分析 go.mod 中声明的更高版本（报 "language version ... is lower than
# the targeted Go version"）。CI 用 v2.6.2 无此问题，本地按需单独跑。
.PHONY: lint
lint: ## Go 静态检查（golangci-lint，与 CI 同版本 v2.6.2）
	golangci-lint run --timeout=5m ./backend/...

.PHONY: lint-frontend
lint-frontend: ## 前端 lint（含「禁止 any」等规则，不自动修复）
	cd frontend && npx eslint . --no-fix

.PHONY: test-frontend
test-frontend: ## 前端单元测试（vitest，一次性运行）
	cd frontend && npm test

.PHONY: test-frontend-watch
test-frontend-watch: ## 前端单元测试（watch 模式，开发时用）
	cd frontend && npm run test:watch

.PHONY: conventions
conventions: ## 校验 AGENTS.md / backend/AGENTS.md / frontend/AGENTS.md 中可机检的开发规范
	python scripts/check-conventions.py --baseline scripts/conventions-baseline.txt

.PHONY: conventions-baseline
conventions-baseline: ## 重写规范基线（仅在明确接受当前存量时使用）
	python scripts/check-conventions.py --baseline scripts/conventions-baseline.txt --update-baseline

.PHONY: fmt
fmt: ## 格式化 Go 代码
	gofmt -l -w ./backend

.PHONY: fmt-check
fmt-check: ## 校验格式（有未格式化文件即失败）
	@files=$$(gofmt -l ./backend); \
	if [ -n "$$files" ]; then echo "以下文件未格式化："; echo "$$files"; exit 1; fi

.PHONY: vet
vet: ## 静态检查
	go vet ./backend/...

.PHONY: test
test: ## 运行后端测试
	go test ./backend/...

.PHONY: test-race
test-race: ## 带竞态检测运行测试
	go test -race ./backend/...

.PHONY: cover
cover: ## 输出各包测试覆盖率
	go test -cover ./backend/...

.PHONY: type-check
type-check: ## 前端类型检查
	cd frontend && npx vue-tsc --noEmit -p tsconfig.app.json

# ---------- 运行 ----------
.PHONY: run
run: build ## 构建后启动（前端由后端一并托管）
	./$(BINARY) -f $(CONFIG)

.PHONY: dev
dev: ## 仅启动后端，前端另开 `cd frontend && npm run dev`
	go run ./backend/api -f $(CONFIG)

# 自动拉起：进程退出后重新启动。
# 进程自身不做 fork 重启——Windows 没有 fork，监听 socket 也无法继承，
# 靠进程自己重建端口会出现双实例抢占的竞争窗口。由外层循环拉起更可靠，
# 生产环境请改用 systemd Restart=always / docker restart / NSSM。
.PHONY: run-supervised
run-supervised: build ## 构建后常驻运行（退出即自动重启，供 /api/v1/system/restart 使用）
	@while true; do \
		./$(BINARY) -f $(CONFIG); \
		code=$$?; \
		echo "进程退出（code=$$code），2 秒后重启…"; \
		sleep 2; \
	done

# ---------- 容器 ----------
.PHONY: docker
docker: ## 构建当前架构的镜像
	docker build -f docker/Dockerfile -t $(IMAGE) .

.PHONY: docker-multiarch
docker-multiarch: ## 构建 amd64/arm64 多架构镜像（需 buildx）
	docker buildx build --platform linux/amd64,linux/arm64 \
		-f docker/Dockerfile -t $(IMAGE) .

# ---------- 清理 ----------
.PHONY: clean
clean: ## 清理构建产物（不动 data/）
	rm -f $(BINARY)
	rm -rf public frontend/dist

.PHONY: sync-docs
sync-docs: ## 把 userdocs/ 下的用户文档同步到前端内置副本
	node scripts/sync-docs.mjs

.PHONY: docs-check
docs-check: ## 校验前端内置文档与 userdocs/ 下的原件一致
	node scripts/sync-docs.mjs --check

# syntax=docker/dockerfile:1

# ---------- 前端构建 ----------
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
# 用 ci 而非 install，保证依赖版本与 lock 文件一致、构建可复现
RUN npm ci
COPY frontend .
RUN npm run build

# ---------- 后端构建 ----------
# SQLite 驱动是纯 Go 的（github.com/libtnb/sqlite → modernc.org/sqlite），
# 所以 CGO_ENABLED=0 就能交叉编译，不再需要 xx / clang / gcc / musl-dev。
# 产物是静态二进制，运行镜像也不必装 libc 相关运行时。
#
# Go 版本须不低于 go.mod 中声明的版本，否则编译器直接拒绝构建
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend-builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

ARG TARGETOS
ARG TARGETARCH
COPY backend ./backend
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-w -s" -o auroramihomo ./backend/api

# ---------- 运行镜像 ----------
FROM --platform=$TARGETPLATFORM alpine:3.21
# 不再需要 sqlite-libs：SQLite 驱动已换成纯 Go 实现，
# 二进制里自带引擎，运行时无外部 C 库依赖。
RUN apk --no-cache add ca-certificates tzdata curl

# 非 root 运行账户。固定 uid/gid 以便宿主机为挂载卷设置对应属主。
# 注意：mihomo 的 TUN 模式仍需 NET_ADMIN 能力，通过 compose 的 cap_add 授予；
# 授予单项 capability 与「以 root 运行」的风险面不可同日而语。
RUN addgroup -g 10001 -S aurora \
    && adduser -u 10001 -S aurora -G aurora \
    && mkdir -p /data/zashboard /data/substore /data/backups /data/logs /app/public \
    && chown -R aurora:aurora /data /app

WORKDIR /app
COPY --from=backend-builder --chown=aurora:aurora /app/auroramihomo .
COPY --from=frontend-builder --chown=aurora:aurora /app/frontend/dist ./public
# 容器专用配置，数据路径直接指向 /data 卷，不再用 sed 改写
COPY --chown=aurora:aurora docker/aurora-api.docker.yaml ./etc/aurora-api.yaml

ENV TZ=Asia/Shanghai
VOLUME ["/data"]
EXPOSE 8899

USER aurora:aurora

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8899/healthz || exit 1

ENTRYPOINT ["/app/auroramihomo", "-f", "etc/aurora-api.yaml"]

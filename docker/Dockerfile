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
# 不写 --platform=$TARGETPLATFORM：那本来就是默认行为，显式写反而会让
# buildx 报 RedundantTargetPlatform 警告。
FROM alpine:3.21
# 不再需要 sqlite-libs：SQLite 驱动已换成纯 Go 实现，
# 二进制里自带引擎，运行时无外部 C 库依赖。
#
# 后四个包是 TProxy 透明代理的依赖，预装进镜像而不是让用户进容器 apk add：
# 容器内临时装的包重建即丢，用户每次重建镜像/容器都要重来一遍（真机测试记录
# 见 docs/AuroraMihomo-Transparent-Proxy-Test-Report.md 第 7.4 节）。
# 代价约 4MB，换来 TProxy 开箱可用。
#
# 为什么 TUN 模式不在此列：mihomo 的 auto-redirect 直接走 netlink 与内核
# 交互、自己维护 nftables 规则，不调用这几个命令行工具——实测在只装了
# ca-certificates/tzdata/curl 的镜像里，TUN 依然能正常建 Meta 网卡与路由。
# 只有 TProxy 需要，因为那套 mangle 规则与策略路由是面板自己用 nft / ip
# 命令下发的（见 internal/netcheck/firewall_apply.go）。
#
# iproute2 必须显式装：Alpine 自带的 ip 是 busybox applet，不支持
# `ip rule add fwmark`，策略路由建不起来。ip6tables 在 Alpine 是独立包
# （Debian 系由 iptables 一并提供），漏了它 IPv6 规则会下发失败。
RUN apk --no-cache add ca-certificates tzdata curl \
    iptables ip6tables nftables iproute2

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

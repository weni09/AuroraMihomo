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
# 版本号由 release.yml 经 build-args 注入（与二进制产物、镜像 tag 一致）；
# 本地 docker build 未传时回落 dev
ARG VERSION=dev
COPY backend ./backend
# 把前端产物放进 backend/api/public：go:embed 嵌入的就是这个目录，
# 不拷的话二进制里只有 .gitkeep，运行时删掉 /app/public 就 404
COPY --from=frontend-builder /app/frontend/dist ./backend/api/public
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-w -s -X 'auroramihomo/backend/internal/version.AppVersion=${VERSION}'" -o auroramihomo ./backend/api

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
#
# su-exec：入口脚本修正挂载卷属主后用它从 root 降权回 aurora，
# 比 busybox 自带 su 的引号语义可靠，也不引入 util-linux 全家桶。
RUN apk --no-cache add ca-certificates tzdata curl \
    iptables ip6tables nftables iproute2 su-exec

# 非 root 运行账户。固定 uid/gid 以便宿主机为挂载卷设置对应属主。
# 注意：mihomo 的 TUN 模式仍需 NET_ADMIN 能力，通过 compose 的 cap_add 授予；
# 授予单项 capability 与「以 root 运行」的风险面不可同日而语。
#
# 降权不在 Dockerfile 用 USER 完成，而是由 entrypoint.sh 在启动时
# 先以 root 修正 /data 挂载卷属主、再降回本账户（Postgres 官方镜像同款模式）。
# 这样用户无需在宿主机手工 chown；代价是容器进程的 PID 1 短暂以 root 运行。
RUN addgroup -g 10001 -S aurora \
    && adduser -u 10001 -S aurora -G aurora \
    # /app 必须显式创建：此前靠 mkdir 列表里的 /app/public 顺带创建，
    # 去掉磁盘 public/ 后漏了它，chown -R /app 会在 WORKDIR 之前执行，
    # 路径不存在导致镜像构建失败（v0.3.11 发版即栽在此处）
    && mkdir -p /app /data/zashboard /data/substore /data/backups /data/logs \
    && chown -R aurora:aurora /data /app

WORKDIR /app
COPY --from=backend-builder --chown=aurora:aurora /app/auroramihomo .
# 前端静态资源已内嵌进二进制（go:embed），不再拷贝磁盘 public/
# 容器专用配置，数据路径直接指向 /data 卷，不再用 sed 改写
COPY --chown=aurora:aurora docker/aurora-api.docker.yaml ./etc/aurora-api.yaml
# 入口脚本：以 root 修正 /data 挂载卷属主后降权到 aurora（见脚本内注释）
COPY docker/entrypoint.sh /usr/local/bin/aurora-entrypoint
RUN chmod +x /usr/local/bin/aurora-entrypoint

ENV TZ=Asia/Shanghai
VOLUME ["/data"]
EXPOSE 8899

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8899/healthz || exit 1

# 不以 USER 固定运行账户：entrypoint 在启动时负责降权（见上），
# 保留 root 启动窗口只为修正挂载卷属主
ENTRYPOINT ["/usr/local/bin/aurora-entrypoint"]
CMD ["/app/auroramihomo", "-f", "etc/aurora-api.yaml"]

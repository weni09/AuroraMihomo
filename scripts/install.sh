#!/bin/sh
# AuroraMihomo 安装脚本（Linux / macOS）
#
# 用 POSIX sh 而非 bash：Alpine 默认只有 busybox ash，
# 而 Alpine 是本项目的主要目标平台之一。
#
# 一键覆盖的范围：下载解压、装服务单元（systemd 或 OpenRC）、补齐透明代理
# 依赖（包 + 内核模块 + sysctl 转发/rp_filter）、启用并启动服务。想只装程序不动系统，
# 用 --no-deps / --no-service / --no-start 逐项关掉。
#
# 用法：
#   curl -fsSL .../install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.2.0 --dir /opt/aurora
#
# 环境变量：
#   AURORA_REPO      GitHub 仓库，形如 owner/AuroraMihomo。
#                    仓库上传后请把下面 REPO 的默认值改成真实地址，
#                    或运行时用 --repo / AURORA_REPO 指定。
#   AURORA_VERSION   指定版本，默认取最新 release
#   AURORA_DIR       安装目录，默认 /opt/auroramihomo
#   AURORA_NO_SERVICE  设为 1 则不安装服务单元（旧名 AURORA_NO_SYSTEMD 仍生效）
#   AURORA_NO_DEPS     设为 1 则不补齐透明代理依赖
#   AURORA_NO_START    设为 1 则不自动启用/启动服务

set -eu

REPO="${AURORA_REPO:-OWNER/AuroraMihomo}"
VERSION="${AURORA_VERSION:-}"
INSTALL_DIR="${AURORA_DIR:-/opt/auroramihomo}"
# 旧版只有 --no-systemd。它的语义（跳过服务单元安装）在 OpenRC 上同样成立，
# 因此保留为 --no-service 的别名，不让既有部署脚本失效。
NO_SERVICE="${AURORA_NO_SERVICE:-${AURORA_NO_SYSTEMD:-0}}"
NO_DEPS="${AURORA_NO_DEPS:-0}"
NO_START="${AURORA_NO_START:-0}"
DRY_RUN=0

while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION="$2"; shift 2 ;;
	--dir) INSTALL_DIR="$2"; shift 2 ;;
	--repo) REPO="$2"; shift 2 ;;
	--no-service | --no-systemd) NO_SERVICE=1; shift ;;
	--no-deps) NO_DEPS=1; shift ;;
	--no-start) NO_START=1; shift ;;
	--dry-run) DRY_RUN=1; shift ;;
	-h | --help)
		cat <<'EOF'
用法: install.sh [选项]

  --version <tag>   安装指定版本（默认最新）
  --dir <path>      安装目录（默认 /opt/auroramihomo）
  --repo <o/r>      GitHub 仓库
  --no-service      跳过服务单元安装（systemd / OpenRC）
  --no-deps         跳过透明代理依赖补齐（包、内核模块与 sysctl）
  --no-start        安装后不自动启用/启动服务
  --dry-run         只打印将要执行的动作，不下载也不改动系统

默认行为：装服务单元、补齐透明代理依赖、启用并启动服务。
EOF
		exit 0
		;;
	*) echo "未知参数: $1（--help 查看用法）" >&2; exit 1 ;;
	esac
done

case "$REPO" in
OWNER/*)
	printf '\033[31m错误:\033[0m 尚未配置仓库地址。\n' >&2
	printf '  请用 --repo owner/AuroraMihomo 指定，或设置 AURORA_REPO 环境变量。\n' >&2
	exit 1
	;;
esac

info() { printf '\033[32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m警告:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

# run_cmd 是所有"会改动系统"的外部命令的唯一入口。
#
# 集中一处的理由有两个：--dry-run 只需在这里拦一层就能覆盖全部副作用；
# 实际执行时把命令原文打印出来，用户装完能回答"脚本到底动了什么"。
run_cmd() {
	if [ "$DRY_RUN" = 1 ]; then
		printf '\033[36m[dry-run]\033[0m %s\n' "$*"
		return 0
	fi
	info "执行: $*"
	"$@"
}

# write_file 从 stdin 读内容写文件，同样受 --dry-run 约束。
# dry-run 下仍要把 stdin 读完，否则 here-doc 的内容会漏到后续命令里。
write_file() {
	_path=$1
	if [ "$DRY_RUN" = 1 ]; then
		cat >/dev/null
		printf '\033[36m[dry-run]\033[0m 写入 %s\n' "$_path"
		return 0
	fi
	mkdir -p "$(dirname "$_path")"
	cat >"$_path"
}

# ---------- 平台探测 ----------

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin) ;;
*) die "不支持的系统: $os（仅支持 Linux 与 macOS）" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "不支持的架构: $arch（仅支持 x86_64 与 aarch64）" ;;
esac

info "目标平台: ${os}/${arch}"

# ---------- 依赖检查 ----------

have() { command -v "$1" >/dev/null 2>&1; }

if have curl; then
	dl() { curl -fsSL "$1" -o "$2"; }
	fetch() { curl -fsSL "$1"; }
elif have wget; then
	dl() { wget -qO "$2" "$1"; }
	fetch() { wget -qO- "$1"; }
else
	die "需要 curl 或 wget"
fi

have tar || die "需要 tar"

# 写入 /opt 与服务目录需要 root。dry-run 只打印，放宽为告警，
# 让人能先以普通用户看清脚本要做什么再决定是否 sudo。
if [ "$(id -u)" -ne 0 ]; then
	if [ "$DRY_RUN" = 1 ]; then
		warn "当前非 root（dry-run 不受影响，实际安装需 sudo）"
	else
		die "需要 root 权限（请用 sudo 运行）"
	fi
fi

# init_system 决定装哪种服务单元。
#
# systemd 只认 systemctl 而不查 /run/systemd/system：后者在 chroot 或
# 装机镜像里不存在，但用户仍希望把 unit 文件落地。真的没跑起来时，
# 后面的 enable/start 会失败并给出提示，比这里预先判死更不容易误伤。
init_system() {
	if [ "$os" != linux ]; then
		# macOS 的 launchd 单元本项目未提供，按"无服务管理器"处理
		echo none
		return
	fi
	if have systemctl; then
		echo systemd
	elif have rc-update && have rc-service; then
		echo openrc
	else
		echo none
	fi
}

init_sys=$(init_system)
case "$init_sys" in
systemd) info "服务管理器: systemd" ;;
openrc) info "服务管理器: OpenRC（Alpine 等）" ;;
none) [ "$os" = linux ] && warn "未检测到 systemd 或 OpenRC，将只安装程序本身" || true ;;
esac

# 官方 Alpine cloud 镜像的根分区可能只有 100M 出头，而部署约需 55M
# （二进制 29M + 前端 3M + 内核 15M + 依赖包）。装到一半磁盘满会留下
# 半个安装，先看一眼比事后清理省事。只告警不阻断：df 的输出格式在
# busybox 与 coreutils 上有差异，判断失准时不该拦住安装。
#
# 阈值取 80M 而非 55M：解压时压缩包与解压产物会同时存在，
# 加上 apk/apt 的缓存，峰值明显高于最终占用。
check_disk_space() {
	have df || return 0
	_dir=$INSTALL_DIR
	while [ ! -d "$_dir" ] && [ "$_dir" != / ]; do
		_dir=$(dirname "$_dir")
	done
	# Available 是倒数第 3 列（后面还有 Use% 与挂载点），
	# 这样取值对"设备名过长导致首行折行"也成立
	_avail=$(df -k "$_dir" 2>/dev/null | awk 'END{print $(NF-2)}')
	case "${_avail:-}" in
	'' | *[!0-9]*) return 0 ;;
	esac
	[ "$_avail" -ge 81920 ] ||
		warn "$_dir 可用空间约 $((_avail / 1024))M，部署约需 55M（含内核与依赖包），建议先扩容"
}

check_disk_space

# ---------- 解析版本 ----------

if [ -z "$VERSION" ]; then
	info "查询最新版本…"
	# 只用 grep/sed 解析，不依赖 jq（最小系统上通常没装）
	VERSION=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" |
		grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	[ -n "$VERSION" ] || die "无法获取最新版本，请用 --version 指定"
fi
info "版本: $VERSION"

PKG="auroramihomo_${VERSION}_${os}_${arch}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${PKG}"

# ---------- 服务操作（按 init 系统分派） ----------

# stop_running_service 在替换二进制前停服务，否则文件被占用无法覆盖。
# 停成功时置 restart_after=1：升级前在运行的服务，装完必须恢复，
# 即使用户加了 --no-start（那个开关的语义是"首次安装不要自动起"，
# 不该把一台正在跑的机器留在停机状态）。
restart_after=0
stop_running_service() {
	case "$init_sys" in
	systemd)
		if systemctl is-active --quiet auroramihomo 2>/dev/null; then
			info "停止运行中的服务"
			run_cmd systemctl stop auroramihomo
			restart_after=1
		fi
		;;
	openrc)
		if [ -f /etc/init.d/auroramihomo ] &&
			rc-service auroramihomo status >/dev/null 2>&1; then
			info "停止运行中的服务"
			run_cmd rc-service auroramihomo stop
			restart_after=1
		fi
		;;
	esac
}

install_systemd_unit() {
	unit=/etc/systemd/system/auroramihomo.service
	if [ -f "$unit" ]; then
		info "保留现有 systemd 单元（如需更新请手工编辑 $unit）"
		return 0
	fi
	info "安装 systemd 单元"
	write_file "$unit" <<EOF
[Unit]
Description=AuroraMihomo
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/auroramihomo -f ${INSTALL_DIR}/etc/aurora-api.yaml
Restart=always
RestartSec=3

# 透明代理需要的权限。不使用透明代理时可删掉这两行并加 User=nobody。
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE

# UDP 代理并发高时容易撞上文件描述符上限
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF
	run_cmd systemctl daemon-reload
}

# install_openrc_service 装 Alpine 用的服务脚本。
#
# 必须用 supervise-daemon 而非默认的 start-stop-daemon：面板的
# POST /api/v1/system/restart 的约定是"优雅退出，等进程管理器拉起"
# （进程刻意不做 fork 自重启），start-stop-daemon 不做重新拉起，
# 会让重启接口变成单向关机。
install_openrc_service() {
	script=/etc/init.d/auroramihomo
	if [ -f "$script" ]; then
		info "保留现有 OpenRC 服务脚本（如需更新请手工编辑 $script）"
		return 0
	fi
	info "安装 OpenRC 服务脚本"
	write_file "$script" <<EOF
#!/sbin/openrc-run

name="auroramihomo"
description="AuroraMihomo 配置管理平台"

directory="${INSTALL_DIR}"
command="${INSTALL_DIR}/auroramihomo"
command_args="-f etc/aurora-api.yaml"
# 透明代理需要 CAP_NET_ADMIN。不用透明代理可改成低权限账户。
command_user="root:root"

# supervise-daemon 才有进程退出后重新拉起的能力，
# /api/v1/system/restart 依赖这一点。
# 不重定向 stdout/stderr（supervise-daemon 会丢弃）：面板自身已把应用
# 日志写入 $INSTALL_DIR/data/logs/aurora.log（AppLog.ToFile 默认开启），
# 且受「系统设置 · 日志」的清理任务管理；再往 /var/log 写一份既重复
# 又不受清理，长期占用根分区。
supervisor="supervise-daemon"
pidfile="/run/auroramihomo.pid"

depend() {
	need net
	after firewall
}
EOF
	run_cmd chmod +x "$script"
}

install_service() {
	case "$init_sys" in
	systemd) install_systemd_unit ;;
	openrc) install_openrc_service ;;
	esac
}

enable_service() {
	case "$init_sys" in
	systemd) run_cmd systemctl enable auroramihomo || warn "设置开机自启失败" ;;
	openrc) run_cmd rc-update add auroramihomo default || warn "加入 default 运行级别失败" ;;
	esac
}

# started 用于收尾提示：区分"已经起来了"和"要用户自己起"。
started=0
start_service() {
	# 用 restart 而非 start：升级路径上服务可能仍在运行（stop 失败时），
	# restart 两种状态都能收敛到"跑着新二进制"
	case "$init_sys" in
	systemd)
		if run_cmd systemctl restart auroramihomo; then
			started=1
		else
			warn "启动失败，请查看 journalctl -u auroramihomo -n 50"
		fi
		;;
	openrc)
		if run_cmd rc-service auroramihomo restart; then
			started=1
		else
			warn "启动失败，请查看 $INSTALL_DIR/data/logs/aurora.log"
		fi
		;;
	esac
	# dry-run 下什么都没真跑，别在收尾里谎报"已启动"
	[ "$DRY_RUN" = 1 ] && started=0
	return 0
}

# ---------- 透明代理依赖 ----------

# 判据与后端 netcheck 保持一致：防火墙工具有 nft 或 iptables 之一即可，
# ip 必须是真 iproute2（busybox 的 ip applet 不支持 fwmark 策略路由）。
# 不一致会出现"脚本说装好了、面板仍报缺"这种白费排查时间的情况。
firewall_ready() { have nft || have iptables; }

iproute2_ready() {
	have ip || return 1
	# 必须用短选项 -V：iproute2 的 ip 不认 --version（会当未知选项报错），
	# 而 busybox 的 ip 两种写法都给不出 "iproute2" 字样。
	# 写成 if 而非 `... && return 0`：后者在 grep 不匹配时会让整条
	# AND-list 返回非零，set -e 下可能直接终止脚本。
	if ip -V 2>&1 | grep -qi iproute2; then
		return 0
	fi
	ip --version 2>&1 | grep -qi iproute2
}

# install_deps 补齐 TProxy 所需的包。
#
# 包名列表与 backend/internal/netcheck/provision.go 的 requiredPackages 一致：
# Alpine 把 ip6tables 拆成独立包，Debian 系由 iptables 一并提供。
#
# 只在检测到缺失时才动包管理器：多数 Debian/Ubuntu 机器上这些本就预装，
# 于是这一步是空操作，不会因为一次 apt-get update 拖慢安装或撞上坏源。
install_deps() {
	if firewall_ready && iproute2_ready; then
		info "透明代理依赖已就绪（防火墙工具与 iproute2 均在位）"
		return 0
	fi
	if have apk; then
		run_cmd apk add --no-cache iptables ip6tables nftables iproute2 ||
			warn "apk 安装失败，请手工执行: apk add --no-cache iptables ip6tables nftables iproute2"
	elif have apt-get; then
		# 全新系统上没有索引，不先 update 则 install 必然失败
		run_cmd apt-get update || warn "刷新软件源失败，接下来的安装可能失败"
		# 不设 DEBIAN_FRONTEND 时 apt 在某些镜像里会尝试打开交互界面并挂住
		run_cmd env DEBIAN_FRONTEND=noninteractive apt-get install -y \
			--no-install-recommends iptables nftables iproute2 ||
			warn "apt-get 安装失败，请手工执行: apt-get install -y iptables nftables iproute2"
	else
		warn "未识别包管理器（无 apk / apt-get），透明代理需手工安装 iptables、nftables、iproute2"
	fi
}

# apply_sysctl 写入透明代理/网关所需的内核参数并立即生效。
#
# 内容与 scripts/sysctl-auroramihomo.conf、面板 provision 的键一致：
#   - net.ipv4.ip_forward / net.ipv6.conf.all.forwarding：网关与旁路由转发
#   - rp_filter=2：避免 TProxy 打标回环被严格反向路径校验丢掉
#
# 安装阶段写「推荐全集」；面板「自动准备」仍按探测结果只写不合规项。
# 容器内跳过（与装包/modprobe 同理）：host 网络下会改到宿主，非特权会被拒绝。
apply_sysctl() {
	if ! have sysctl; then
		warn "无 sysctl，跳过内核参数写入（网关/TProxy 可能需要手工设置转发）"
		return 0
	fi
	write_file /etc/sysctl.d/99-auroramihomo.conf <<'EOF'
# 由 AuroraMihomo 安装脚本写入，用于透明代理与网关/旁路由。
# 与 backend/internal/netcheck/provision.go、scripts/sysctl-auroramihomo.conf 一致。
# 不再需要时删除本文件即可。

# 作为局域网网关转发其它设备的 IPv4 流量
net.ipv4.ip_forward = 1

# 作为局域网网关/旁路由转发其它设备的 IPv6 流量
net.ipv6.conf.all.forwarding = 1

# 严格反向路径校验会丢弃 TProxy 打标后回环的包
net.ipv4.conf.all.rp_filter = 2

# 新建网卡的默认值，否则后出现的网卡仍是严格模式
net.ipv4.conf.default.rp_filter = 2
EOF
	# BusyBox 的 sysctl 不认 --system；-p <文件> 在 procps-ng 与 BusyBox 上都可用。
	# 对已存在的严格 rp_filter 网卡，面板「自动准备」还会按网卡补写；安装阶段
	# 先把 all/default 落到位，覆盖绝大多数场景。
	if run_cmd sysctl -p /etc/sysctl.d/99-auroramihomo.conf; then
		info "已写入并加载 /etc/sysctl.d/99-auroramihomo.conf（含 IPv4/IPv6 转发）"
	else
		warn "sysctl -p 失败，请手工执行: sysctl -p /etc/sysctl.d/99-auroramihomo.conf"
	fi
}

# bbr_supported 判断当前内核能否使用 BBR（已启用、已在 available 列表、或可加载模块）。
#
# 不把 BBR 写进 99-auroramihomo.conf 的理由：它是整机 TCP 性能优化，不是
# 透明代理硬依赖；内核不支持时若与转发项同文件，sysctl -p 失败会让人误以为
# 网关参数也没配上。
bbr_supported() {
	avail=/proc/sys/net/ipv4/tcp_available_congestion_control
	cur=/proc/sys/net/ipv4/tcp_congestion_control
	[ -r "$cur" ] || return 1
	# 已经在用
	if [ "$(cat "$cur" 2>/dev/null)" = "bbr" ]; then
		return 0
	fi
	# 已列在可用算法里
	if [ -r "$avail" ] && grep -qw bbr "$avail" 2>/dev/null; then
		return 0
	fi
	# 尝试加载模块后再看（模块名因发行版可能是 tcp_bbr）
	if have modprobe; then
		modprobe tcp_bbr >/dev/null 2>&1 || modprobe bbr >/dev/null 2>&1 || true
		# fq 与 BBR 常一起用；加载失败不在这里判死，交给 sysctl -p 报错
		modprobe sch_fq >/dev/null 2>&1 || true
	fi
	[ -r "$avail" ] && grep -qw bbr "$avail" 2>/dev/null
}

# apply_bbr_sysctl 在内核支持时写入独立的 BBR/fq drop-in 并加载。
#
# best-effort：不支持或加载失败只告警，不让安装失败；失败时删掉刚写的
# drop-in，避免开机时 sysctl 服务反复报错。
apply_bbr_sysctl() {
	if ! have sysctl; then
		return 0
	fi
	if ! bbr_supported; then
		warn "内核不支持 BBR（或无法加载 tcp_bbr），已跳过拥塞控制优化。需要时见 scripts/sysctl-auroramihomo-bbr.conf"
		return 0
	fi
	write_file /etc/sysctl.d/99-auroramihomo-bbr.conf <<'EOF'
# 由 AuroraMihomo 安装脚本写入（可选 TCP 性能优化，与网关 sysctl 分离）。
# 与 scripts/sysctl-auroramihomo-bbr.conf 一致。不需要时删除本文件即可。

net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
EOF
	if run_cmd sysctl -p /etc/sysctl.d/99-auroramihomo-bbr.conf; then
		info "已启用 BBR（net.ipv4.tcp_congestion_control=bbr, default_qdisc=fq）"
	else
		# 不留会在每次启动失败的配置
		rm -f /etc/sysctl.d/99-auroramihomo-bbr.conf 2>/dev/null || true
		warn "写入 BBR 参数失败（已回滚 drop-in）。可稍后手工对照 scripts/sysctl-auroramihomo-bbr.conf"
	fi
}

# load_modules 加载并持久化 tun 与 nft_tproxy。
#
# Alpine 上这两个模块默认未加载，/dev/net/tun 因此不存在，TUN 模式会直接
# 起不来。失败一律只告警：模块可能已编进内核（此时 modprobe 报错但功能正常），
# 也可能内核根本没有对应模块，都不该让整个安装失败。
load_modules() {
	if ! have modprobe; then
		warn "无 modprobe，跳过内核模块加载（透明代理可能不可用）"
		return 0
	fi
	[ -c /dev/net/tun ] ||
		run_cmd modprobe tun ||
		warn "加载 tun 失败（若已编入内核可忽略），TUN 模式需要 /dev/net/tun"
	grep -qE '^(nft_tproxy|xt_TPROXY) ' /proc/modules 2>/dev/null ||
		run_cmd modprobe nft_tproxy ||
		warn "加载 nft_tproxy 失败（若已编入内核可忽略），TProxy 模式需要它"

	# 持久化，避免重启后模块缺失。systemd 与 OpenRC 的 modules 服务
	# 都会读 /etc/modules-load.d/*.conf。
	write_file /etc/modules-load.d/auroramihomo.conf <<'EOF'
# 由 AuroraMihomo 安装脚本写入：透明代理所需的内核模块。
# 不再需要时删除本文件即可。
tun
nft_tproxy
EOF
}

provision_deps() {
	if [ "$os" != linux ]; then
		# macOS 只支持 TUN，由 utun 提供，没有这套包与模块
		return 0
	fi
# 容器里 modprobe/sysctl 作用于宿主内核、装的包重建即丢，都不该由脚本
		# 悄悄替用户决定。容器部署走 docker 镜像（依赖预装），sysctl 在宿主执行
		# （见 docker/docker-compose.yml 注释与 scripts/sysctl-auroramihomo.conf）。
		if [ -f /.dockerenv ] || grep -qa 'docker\|containerd\|lxc' /proc/1/cgroup 2>/dev/null; then
			warn "检测到容器环境，跳过依赖补齐：装的包重建即丢，modprobe/sysctl 会作用于宿主内核。请在宿主上处理（sysctl 见 scripts/sysctl-auroramihomo.conf）"
			return 0
		fi
		install_deps
		load_modules
		apply_sysctl
		apply_bbr_sysctl
	}

# ---------- 下载与安装 ----------

if [ "$DRY_RUN" = 1 ]; then
	info "[dry-run] 跳过下载与解压: $URL"
else
	tmp=$(mktemp -d)
	# 中断或失败时清理临时目录，避免留下半个包
	trap 'rm -rf "$tmp"' EXIT INT TERM

	info "下载 $PKG"
	dl "$URL" "$tmp/$PKG" || die "下载失败: $URL"

	# 校验和存在就验，缺失只告警不阻断（旧版本可能没发布 .sha256）
	if dl "${URL}.sha256" "$tmp/$PKG.sha256" 2>/dev/null; then
		if have sha256sum; then
			(cd "$tmp" && sha256sum -c "$PKG.sha256" >/dev/null 2>&1) ||
				die "校验和不匹配，包可能已损坏或被篡改"
			info "校验和通过"
		elif have shasum; then
			expected=$(cut -d' ' -f1 <"$tmp/$PKG.sha256")
			actual=$(shasum -a 256 "$tmp/$PKG" | cut -d' ' -f1)
			[ "$expected" = "$actual" ] || die "校验和不匹配，包可能已损坏或被篡改"
			info "校验和通过"
		else
			warn "无 sha256sum/shasum，跳过校验"
		fi
	else
		warn "该版本未提供校验和文件，跳过校验"
	fi

	info "解压到 $INSTALL_DIR"
	tar -xzf "$tmp/$PKG" -C "$tmp"
	src=$(find "$tmp" -maxdepth 1 -type d -name 'auroramihomo_*' | head -1)
	[ -n "$src" ] || die "压缩包结构异常"

	mkdir -p "$INSTALL_DIR"

	# 配置文件不覆盖：升级时用户改过的设置必须保留
	if [ -f "$INSTALL_DIR/etc/aurora-api.yaml" ]; then
		info "保留现有配置 etc/aurora-api.yaml"
		rm -f "$src/etc/aurora-api.yaml"
	fi

	stop_running_service

	cp -R "$src/." "$INSTALL_DIR/"
	chmod +x "$INSTALL_DIR/auroramihomo"
fi

# ---------- 依赖与服务 ----------

if [ "$NO_DEPS" != 1 ]; then
	provision_deps
else
	info "按 --no-deps 跳过透明代理依赖补齐"
fi

if [ "$NO_SERVICE" != 1 ] && [ "$init_sys" != none ]; then
	install_service
	if [ "$NO_START" != 1 ]; then
		enable_service
		start_service
	elif [ "$restart_after" = 1 ]; then
		# --no-start 的语义是"首次安装不要自动起"，不该把一台升级前
		# 正在跑的机器留在停机状态，所以这里只恢复运行、不动自启设置
		info "服务升级前在运行，恢复启动（--no-start 仅跳过开机自启设置）"
		start_service
	else
		info "按 --no-start 跳过启用与启动"
	fi
elif [ "$NO_SERVICE" = 1 ]; then
	info "按 --no-service 跳过服务单元安装"
fi

# ---------- 收尾 ----------

port=$(grep -E '^Port:' "$INSTALL_DIR/etc/aurora-api.yaml" 2>/dev/null | head -1 | sed -E 's/[^0-9]//g')
port="${port:-8899}"

echo
if [ "$DRY_RUN" = 1 ]; then
	info "dry-run 结束，未改动系统。去掉 --dry-run 即按上述动作实际安装"
else
	info "安装完成: $INSTALL_DIR"
fi
echo

# 收尾提示按"服务单元是否装了"分派：没装服务的机器（--no-service 或无
# init 系统）给的必须是前台启动命令，给 systemctl/rc-service 只会让人白试一次。
if [ "$NO_SERVICE" = 1 ] || [ "$init_sys" = none ]; then
	echo "启动："
	echo "  cd $INSTALL_DIR && ./auroramihomo -f etc/aurora-api.yaml"
elif [ "$init_sys" = systemd ]; then
	if [ "$started" = 1 ] && [ "$NO_START" != 1 ]; then
		echo "服务已启动并设为开机自启。"
	elif [ "$started" = 1 ]; then
		# --no-start 路径只恢复了运行，没设自启，别把两件事说成一件
		echo "服务已启动（按 --no-start 未设置开机自启）。"
	else
		echo "启动服务："
		echo "  systemctl enable --now auroramihomo"
	fi
	echo "查看日志："
	echo "  journalctl -u auroramihomo -f"
else
	if [ "$started" = 1 ] && [ "$NO_START" != 1 ]; then
		echo "服务已启动并加入 default 运行级别。"
	elif [ "$started" = 1 ]; then
		echo "服务已启动（按 --no-start 未加入 default 运行级别）。"
	else
		echo "启动服务："
		echo "  rc-update add auroramihomo default && rc-service auroramihomo start"
	fi
	echo "查看日志："
	echo "  tail -f $INSTALL_DIR/data/logs/aurora.log"
fi

echo
echo "面板地址： http://<本机IP>:${port}"
echo "初始密码： 首次启动后见 $INSTALL_DIR/data/initial_password.txt"
echo
echo "首次启动会自动下载 mihomo 内核，需要能访问 GitHub。"
echo "透明代理需要 CAP_NET_ADMIN，详见 docs/AuroraMihomo-Transparent-Proxy.md。"

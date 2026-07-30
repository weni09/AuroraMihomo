#!/bin/sh
# AuroraMihomo 安装脚本（Linux / macOS）
#
# 用 POSIX sh 而非 bash：Alpine 默认只有 busybox ash，
# 而 Alpine 是本项目的主要目标平台之一。
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
#   AURORA_NO_SYSTEMD  设为 1 则不安装 systemd 单元

set -eu

REPO="${AURORA_REPO:-OWNER/AuroraMihomo}"
VERSION="${AURORA_VERSION:-}"
INSTALL_DIR="${AURORA_DIR:-/opt/auroramihomo}"
NO_SYSTEMD="${AURORA_NO_SYSTEMD:-0}"

while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION="$2"; shift 2 ;;
	--dir) INSTALL_DIR="$2"; shift 2 ;;
	--repo) REPO="$2"; shift 2 ;;
	--no-systemd) NO_SYSTEMD=1; shift ;;
	-h | --help)
		cat <<'EOF'
用法: install.sh [选项]

  --version <tag>   安装指定版本（默认最新）
  --dir <path>      安装目录（默认 /opt/auroramihomo）
  --repo <o/r>      GitHub 仓库
  --no-systemd      跳过 systemd 单元安装
EOF
		exit 0
		;;
	*) echo "未知参数: $1（--help 查看用法）" >&2; exit 1 ;;
	esac
done

case "$REPO" in
OWNER/*)
	printf '[31m错误:[0m 尚未配置仓库地址。
' >&2
	printf '  请用 --repo owner/AuroraMihomo 指定，或设置 AURORA_REPO 环境变量。
' >&2
	exit 1
	;;
esac

info() { printf '\033[32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33m警告:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m错误:\033[0m %s\n' "$*" >&2; exit 1; }

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

# 写入 /opt 与 /etc/systemd 需要 root
if [ "$(id -u)" -ne 0 ]; then
	die "需要 root 权限（请用 sudo 运行）"
fi

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

# ---------- 下载与校验 ----------

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

# ---------- 安装 ----------

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

# 升级前停服务，否则二进制被占用无法替换
if [ "$os" = linux ] && have systemctl && systemctl is-active --quiet auroramihomo 2>/dev/null; then
	info "停止运行中的服务"
	systemctl stop auroramihomo
	restart_after=1
fi

cp -R "$src/." "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/auroramihomo"

# ---------- systemd ----------

if [ "$os" = linux ] && [ "$NO_SYSTEMD" != 1 ] && have systemctl; then
	unit=/etc/systemd/system/auroramihomo.service
	if [ -f "$unit" ]; then
		info "保留现有 systemd 单元（如需更新请手工编辑 $unit）"
	else
		info "安装 systemd 单元"
		cat >"$unit" <<EOF
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
		systemctl daemon-reload
	fi
fi

# ---------- 收尾 ----------

port=$(grep -E '^Port:' "$INSTALL_DIR/etc/aurora-api.yaml" 2>/dev/null | head -1 | sed -E 's/[^0-9]//g')
port="${port:-8899}"

echo
info "安装完成: $INSTALL_DIR"
echo

if [ "$os" = linux ] && [ "$NO_SYSTEMD" != 1 ] && have systemctl; then
	if [ "${restart_after:-0}" = 1 ]; then
		systemctl start auroramihomo
		info "服务已重启"
	else
		echo "启动服务："
		echo "  systemctl enable --now auroramihomo"
	fi
	echo "查看日志："
	echo "  journalctl -u auroramihomo -f"
else
	echo "启动："
	echo "  cd $INSTALL_DIR && ./auroramihomo -f etc/aurora-api.yaml"
fi

echo
echo "面板地址： http://<本机IP>:${port}"
echo "初始密码： 首次启动后见 $INSTALL_DIR/data/initial_password.txt"
echo
echo "首次启动会自动下载 mihomo 内核，需要能访问 GitHub。"
echo "透明代理需要 CAP_NET_ADMIN，详见 docs/AuroraMihomo-Transparent-Proxy.md。"

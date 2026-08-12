#!/bin/sh
set -e

# 容器入口：以 root 修正挂载卷属主后降权，再启动应用。
#
# 为什么需要这一步：/data 是 bind mount 进来的宿主目录，由 Docker 或用户
# 创建，属主通常是 root；而应用以非 root 账户运行，写不进去时 SQLite 会报
# "unable to open database file (14)"，服务反复重启。这里先统一把 /data
# 改成运行账户的属主，用户无需在宿主机手工 chown。
# 镜像预建的子目录（backups/logs 等）只是图层内容，被挂载卷盖住后
# 应用自身会按需重建（database.go 等处均有 MkdirAll），此处不必建。
#
# 运行账户的 uid/gid 不固定：默认镜像内的 aurora(10001)；想让数据目录
# 归宿主机当前用户直接管理时，设 AURORA_PUID/AURORA_PGID 为
# `id -u` / `id -g` 的输出，并把宿主 data/ 一并 chown 给该用户。
#
# 降权逻辑：默认降回运行账户（保持非 root 隔离）；需要以 root 跑应用
# （mihomo TUN 的 NET_ADMIN）时，由 compose 显式设 AURORA_RUN_AS_ROOT=1，
# 与注释掉的 user: "0:0" 配对使用。
PUID="${AURORA_PUID:-10001}"
PGID="${AURORA_PGID:-10001}"

if [ "$(id -u)" = "0" ]; then
	chown -R "${PUID}:${PGID}" /data 2>/dev/null || true
	if [ "${AURORA_RUN_AS_ROOT:-0}" != "1" ]; then
		exec su-exec "${PUID}:${PGID}" "$@"
	fi
fi
exec "$@"

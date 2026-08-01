# AuroraMihomo Deployment Design

## 1. Deployment Goal

AuroraMihomo should provide:

-   One container deployment
-   Zero external dependency
-   Persistent data
-   Easy upgrade

------------------------------------------------------------------------

# 2. Container Architecture

    auroramhihomo

    ├── Go Backend

    ├── Mihomo Core

    ├── Sub-Store Runtime

    ├── Zashboard

    └── Frontend

------------------------------------------------------------------------

# 3. Docker Image

Example:

    auroramhihomo:latest

Base:

    debian/alpine

Include:

-   Go binary
-   mihomo binary
-   node runtime
-   sub-store
-   web assets

------------------------------------------------------------------------

# 4. Data Directory

    /data

    ├── config/

    │   ├── base.yaml

    │   ├── remote.yaml

    │   ├── override.yaml

    │   └── config.yaml


    ├── db/

    │   └── aurora.db


    ├── backups/


    └── logs/

------------------------------------------------------------------------

# 5. Docker Compose

``` yaml
services:

 auroramhihomo:

   image: auroramhihomo/latest

   network_mode: host

   volumes:

    - ./data:/data

   cap_add:

    - NET_ADMIN

   restart: always
```

------------------------------------------------------------------------

# 6. Network

Recommended:

    network_mode: host

Reason:

-   Mihomo TUN
-   transparent proxy
-   port management

## 6.1 透明代理所需的额外授权

默认镜像以非 root 用户（uid 10001）运行，**开箱不支持透明代理**。
启用需要四项改动，缺任何一项都会失败：

```yaml
services:
  aurora:
    network_mode: host              # 桥接网络里的规则只对容器自己生效
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun   # TUN 模式必须映射设备
    user: "0:0"                     # 非 root 拿不到 cap 的 effective 位
    # security_opt:
    #   - no-new-privileges:true    # 必须注释：与 file capability 方案互斥
```

四项缺失时的症状各不相同，便于对照排查：

| 缺失项               | 症状                                                 |
| -------------------- | ---------------------------------------------------- |
| `network_mode: host` | 规则生效但局域网其它设备不受影响                     |
| `cap_add`            | 检测报告 `capNetAdmin: false`，bounding 也是 false    |
| `devices`            | 检测报告 TUN 设备未找到                              |
| `user: "0:0"`        | `capNetAdminBounding: true` 但 `capNetAdmin: false`   |

**代价**：`user: "0:0"` 加上去掉 `no-new-privileges` 会显著降低容器隔离性。
不需要透明代理时应保持默认（非 root）配置。

TProxy 所需的 `iptables` / `ip6tables` / `nftables` / `iproute2` **已预装在
镜像里**，开箱即可用，不需要进容器 `apk add`（那样装的包容器重建即丢）。
代价是镜像增大约 8.8MB——主要来自 `libmnl`、`libnftnl`、`jansson`、
`readline` 等传递依赖，三个主包自身只有约 3MB。

`iproute2` 必须显式安装：Alpine 自带的 `ip` 是 busybox applet，不支持
`ip rule add fwmark`，策略路由建不起来。`ip6tables` 在 Alpine 是独立包
（Debian 系由 `iptables` 一并提供）。

注意两种模式的依赖是**不同**的：

| 模式   | 需要 `/dev/net/tun` | 需要 nft/iptables/iproute2      |
| ------ | ------------------- | ------------------------------- |
| TUN    | 是                  | 否（mihomo 走 netlink 自管规则）|
| TProxy | 否                  | 是（面板用 nft / ip 下发规则）  |

所以上面四项改动里，`devices: /dev/net/tun` 只有 TUN 模式需要；只用 TProxy
时不必映射设备（已实测：不映射设备时 TUN 报不可用而 TProxy 正常工作）。

host 网络下容器内改 sysctl 会影响宿主，且 Docker 会拒绝
`--sysctl net.ipv4.ip_forward`，网关模式所需的转发开关要在宿主上设置。

详见 `AuroraMihomo-Transparent-Proxy.md`。实测记录有三份：

| 报告 | 覆盖 |
| --- | --- |
| `AuroraMihomo-Transparent-Proxy-Test-Report.md` | Ubuntu，二进制与容器两种形态（第 7 节为容器部分） |
| `AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md` | Ubuntu 24.04 + Docker，含真实节点的出口 IP 三段对照 |
| `AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md` | Alpine 3.24 + 二进制，含 OpenRC 与 musl 兼容性 |

------------------------------------------------------------------------

# 6.5 二进制部署

除容器外支持直接运行单个二进制。适合不想引入 Docker 的场景，
也是透明代理最省事的形态（无需处理容器的 capability 传递）。

## 在线安装

```bash
# 把 OWNER 换成实际的 GitHub 用户名/组织名
curl -fsSL https://raw.githubusercontent.com/OWNER/AuroraMihomo/main/scripts/install.sh \
  | sudo sh -s -- --repo OWNER/AuroraMihomo
```

仓库上传后建议把 `scripts/install.sh` 里 `REPO` 的默认值改成真实地址，
之后就不必每次传 `--repo`。脚本在未配置时会直接报错而不是去请求错误地址。

脚本会探测 OS/架构与服务管理器、从 GitHub Release 拉取对应压缩包并校验
sha256、解压到 `/opt/auroramihomo`、补齐透明代理依赖、安装服务单元
（systemd 或 OpenRC）、启用开机自启并启动。装完即可访问，Alpine 与
Debian/Ubuntu 走同一条命令。

服务管理器的分派逻辑：有 `systemctl` 走 systemd；否则有 `rc-update` 与
`rc-service` 走 OpenRC；都没有则只装程序，末尾提示改为前台启动命令。
刻意不查 `/run/systemd/system` 来判断 systemd 是否真在运行——chroot 与
装机镜像里它不存在，但用户仍希望 unit 文件落地；真没跑起来时后面的
`enable`/`start` 会失败并给出提示，比预先判死不容易误伤。

依赖补齐这一步是脚本里唯一会改动"系统层"的部分，判据与后端
`netcheck` 保持一致（防火墙工具有 `nft` 或 `iptables` 之一即可，
`ip` 必须是真 iproute2 而非 busybox applet），不一致会出现"脚本说装好了、
面板仍报缺"这种白费排查时间的情况。已就绪时整步跳过，因此多数
Debian/Ubuntu 机器上不会触发一次 `apt-get update`。

容器内会主动跳过依赖补齐：装的包重建即丢，`modprobe` 又会作用于宿主内核，
两者都不该由脚本悄悄替用户决定（容器部署本就该用镜像，依赖已预装）。

三个关闭开关：`--no-deps`（不动系统层）、`--no-service`（不装服务单元，
旧名 `--no-systemd` 保留为别名）、`--no-start`（装好不启用不启动）。
另有 `--dry-run` 只打印将要执行的动作，不下载也不改动系统——所有副作用
都经由 `run_cmd` / `write_file` 两个函数，因此拦一层即可全覆盖。

`--no-start` 有一处刻意的例外：升级路径上若服务原本在运行，装完仍会恢复
运行（但不设自启）。那个开关的语义是"首次安装不要自动起"，不该把一台
正在服务的机器留在停机状态。

## 离线安装

内网机器上从 Release 页面手工下载对应平台的包：

```
auroramihomo_<version>_linux_amd64.tar.gz
auroramihomo_<version>_linux_arm64.tar.gz
auroramihomo_<version>_darwin_arm64.tar.gz
```

```bash
tar -xzf auroramihomo_<version>_linux_amd64.tar.gz -C /opt/auroramihomo
cd /opt/auroramihomo && ./auroramihomo -f etc/aurora-api.yaml
```

包内含二进制、前端静态资源（`public/`）与默认配置。**不含 mihomo 内核**：
内核由面板首次启动时下载。完全离线的环境需要手工放置：

```bash
mkdir -p /opt/auroramihomo/data/bin
# 从 https://github.com/MetaCubeX/mihomo/releases 下载对应平台的 .gz
gunzip -c mihomo-linux-amd64-v1.19.29.gz > /opt/auroramihomo/data/bin/mihomo
chmod +x /opt/auroramihomo/data/bin/mihomo
```

> 前端静态资源已内嵌进二进制（go:embed）：包内的 `public/` 目录是可选
> 的覆盖层，删除后服务仍能正常提供界面，二进制会直接使用内嵌资源。

注意 Linux/macOS 的官方资产是 `.gz`（gzip 压缩的裸二进制，不是 tar 归档），
只有 Windows 是 `.zip`。

## systemd 单元

```ini
[Unit]
Description=AuroraMihomo
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/auroramihomo
ExecStart=/opt/auroramihomo/auroramihomo -f /opt/auroramihomo/etc/aurora-api.yaml
Restart=always
RestartSec=3

# 透明代理需要的权限。不用透明代理时可全部删掉，
# 以非 root 用户运行（User=aurora）更安全。
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE

# UDP 代理并发高时容易撞上文件描述符上限
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now auroramihomo
journalctl -u auroramihomo -f
```

进程自身不做 fork 自重启（见 Makefile 注释），生产环境依赖
systemd 的 `Restart=always`。

## OpenRC 单元（Alpine）

Alpine 没有 systemd。`scripts/install.sh` 检测到 OpenRC 时会自动装下面这份
脚本（内容与此处一致，`directory`/`command` 按 `--dir` 取值），
以下内容供离线安装或自定义单元时参考。

**必须用 `supervise-daemon` 而不是 `start-stop-daemon`。** 面板的
`POST /api/v1/system/restart` 的约定是「优雅退出，等进程管理器拉起」，
`start-stop-daemon` 不做重新拉起，会让重启接口变成单向关机。

```sh
# /etc/init.d/auroramihomo
#!/sbin/openrc-run

name="auroramihomo"
description="AuroraMihomo 配置管理平台"

directory="/opt/auroramihomo"
command="/opt/auroramihomo/auroramihomo"
command_args="-f etc/aurora-api.yaml"
command_user="root:root"

# supervise-daemon 才有进程退出后重新拉起的能力，
# /api/v1/system/restart 依赖这一点
supervisor="supervise-daemon"
supervise_daemon_args="--stdout /var/log/auroramihomo.log --stderr /var/log/auroramihomo.log"
pidfile="/run/auroramihomo.pid"

depend() {
    need net
    after firewall
}
```

```bash
chmod +x /etc/init.d/auroramihomo
rc-update add auroramihomo default
rc-service auroramihomo start
rc-service auroramihomo status
tail -f /var/log/auroramihomo.log
```

装好后 `ps` 里应能看到 supervise-daemon 在监管（带
`--respawn-delay 2 --respawn-max 5`）：

```
supervise-daemon auroramihomo --start --chdir /opt/auroramihomo ...
  auroramihomo -f etc/aurora-api.yaml
    data/bin/mihomo -d ./data
```

## Alpine 的前置依赖

Alpine 最小系统缺几样东西。第一组与第三组由 `install.sh` 自动补齐
（`--no-deps` 可关掉），离线安装时手工执行：

```bash
# 透明代理必需。ip6tables 在 Alpine 是独立包；iproute2 会把
# /sbin/ip 从 busybox 软链替换成真 iproute2，因此不存在 PATH 优先级问题
apk add --no-cache iptables ip6tables nftables iproute2

# 排查用，非运行必需，install.sh 不装。
# Alpine 默认只有 busybox 的 wget，不支持 -x 与 --interface
apk add --no-cache curl bind-tools

# tun 与 nft_tproxy 默认未加载，/dev/net/tun 也不存在
modprobe tun && modprobe nft_tproxy
printf 'tun\nnft_tproxy\n' > /etc/modules-load.d/auroramihomo.conf
```

模块加载失败一律只告警不中断：模块可能已编进内核（此时 `modprobe` 报错
但功能正常），也可能内核根本没有对应模块，两种都不该让整个安装失败。

`sysctl` 在 Alpine 是 BusyBox applet，不认 `--system`。面板的「自动准备」
已能识别并回退为 `sysctl -p <文件>`；手工设置时也请用 `-p`。

根分区余量值得先确认：官方 cloud 镜像的根分区可能只有 100M 出头，
而部署需求约 55M（二进制 29M + 前端 3M + mihomo 内核 15M + 依赖包）。
若根分区是整盘直接格式化的 ext4（`blkid` 看不到分区表），扩容不需要
`growpart`，宿主上扩盘后直接 `resize2fs /dev/sda` 即可。

## 权限说明

mihomo 内核是面板拉起的子进程，会继承父进程权限。若用
`setcap` 而非 root 运行，需要同时给内核二进制加 capability：

```bash
setcap cap_net_admin,cap_net_bind_service=+ep /opt/auroramihomo/auroramihomo
setcap cap_net_admin,cap_net_bind_service=+ep /opt/auroramihomo/data/bin/mihomo
```

内核每次自动更新后 capability 会丢失（二进制被替换），需要重新执行。
这是选择 root 运行的一个实际理由。

------------------------------------------------------------------------

# 7. Startup Flow

    container start

    ↓

    load database

    ↓

    check config

    ↓

    merge config

    ↓

    validate

    ↓

    start mihomo

    ↓

    start api

    ↓

    ready

------------------------------------------------------------------------

# 8. Upgrade System

Components:

## AuroraMihomo

-   image update

## Mihomo

-   version check
-   binary replace

## Zashboard

-   static asset update

## Sub-Store

-   runtime/package update

------------------------------------------------------------------------

# 9. Backup

Before update:

    backup/

    config.yaml

    database

    runtime

Rollback:

    restore

    restart

------------------------------------------------------------------------

# 10. Multi Architecture

Build:

    linux/amd64

    linux/arm64

Suitable:

-   NAS
-   ARM router
-   Mini PC

------------------------------------------------------------------------

# 11. Security

Future:

-   API Token
-   HTTPS
-   User authentication
-   Audit log

------------------------------------------------------------------------

# 12. Production Deployment

Recommended:

    Docker

    +

    host network

    +

    persistent volume

    +

    automatic backup

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
`--sysctl net.ipv4.ip_forward`（以及 `net.ipv6.conf.all.forwarding`），
网关/旁路由所需的转发与 TProxy 所需的 `rp_filter` **必须在宿主机上设置**。

推荐一次性落盘（与在线安装脚本、面板 provision 键名一致）：

```bash
# 透明代理 / 网关硬依赖相关
sudo cp scripts/sysctl-auroramihomo.conf /etc/sysctl.d/99-auroramihomo.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo.conf

# 可选：BBR + fq（整机 TCP 性能；与上项分文件，内核不支持时不要强行合并）
sudo cp scripts/sysctl-auroramihomo-bbr.conf /etc/sysctl.d/99-auroramihomo-bbr.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo-bbr.conf
```

`99-auroramihomo.conf` 内容包含：

| 键 | 值 | 用途 |
| --- | --- | --- |
| `net.ipv4.ip_forward` | `1` | IPv4 网关转发 |
| `net.ipv6.conf.all.forwarding` | `1` | IPv6 网关/旁路由转发 |
| `net.ipv4.conf.all.rp_filter` | `2` | 避免 TProxy 打标回环被丢 |
| `net.ipv4.conf.default.rp_filter` | `2` | 新建网卡默认宽松校验 |

`99-auroramihomo-bbr.conf`（可选）包含：

| 键 | 值 | 用途 |
| --- | --- | --- |
| `net.core.default_qdisc` | `fq` | 与 BBR 搭配的排队规则 |
| `net.ipv4.tcp_congestion_control` | `bbr` | TCP 拥塞控制（影响整机出站） |

`docker/docker-compose.yml` 头部注释也写了同一套步骤。二进制在线安装
（`scripts/install.sh`，未加 `--no-deps`）会自动写入转发类 conf；BBR 仅在
探测到内核支持时 best-effort 启用，失败只告警、不阻断安装，且不会把失败的
 bbr drop-in 留在磁盘上。面板「自动准备」**不**写入 BBR（避免把性能优化
当成透明代理必需步骤）。

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

依赖补齐这一步是脚本里唯一会改动"系统层"的部分，包括：

1. 包与模块：防火墙工具、`iproute2`、`tun` / `nft_tproxy`（判据与后端
   `netcheck` 一致）
2. **sysctl（网关）**：写入 `/etc/sysctl.d/99-auroramihomo.conf`（IPv4/IPv6
   转发 + `rp_filter=2`）并用 `sysctl -p` 立即加载；模板见
   `scripts/sysctl-auroramihomo.conf`，与面板 provision 键名一致
3. **sysctl（BBR，可选）**：若内核支持，另写
   `/etc/sysctl.d/99-auroramihomo-bbr.conf`（`fq` + `bbr`）；不支持或加载
   失败只告警并回滚该文件，见 `scripts/sysctl-auroramihomo-bbr.conf`

已就绪时装包会跳过；网关 sysctl 文件每次安装仍会重写为推荐全集（幂等）。
因此多数 Debian/Ubuntu 机器上不会无谓触发 `apt-get update`，但仍会确保
转发开关落盘。

容器内会主动跳过依赖补齐：装的包重建即丢，`modprobe`/`sysctl` 又会作用于
宿主内核，都不该由脚本悄悄替用户决定。容器部署用镜像（依赖已预装），
sysctl 请在**宿主**按上一节或 `docker-compose.yml` 注释执行。

三个关闭开关：`--no-deps`（不动系统层：包、模块与 sysctl）、
`--no-service`（不装服务单元，旧名 `--no-systemd` 保留为别名）、
`--no-start`（装好不启用不启动）。
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

首次启动时，若库中无 `base` 配置，服务会调用 `ConfigService.EnsureDefaultBase()`，
把嵌入的 `backend/internal/service/default_base.yaml` 写入数据库。该文件是开箱
骨架（DNS/DoH、nameserver-policy、私网 DIRECT、MATCH 兜底等），**不含**订阅、
节点、设备规则与认证信息；已有 base 不会被覆盖。
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
# /api/v1/system/restart 依赖这一点。
# 不重定向 stdout/stderr：应用日志由面板自身写 data/logs/aurora.log
# 并受日志清理任务管理，不再往 /var/log 写重复副本。
supervisor="supervise-daemon"
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
tail -f /opt/auroramihomo/data/logs/aurora.log
```

装好后 `ps` 里应能看到 supervise-daemon 在监管（带
`--respawn-delay 2 --respawn-max 5`）：

```
supervise-daemon auroramihomo --start --chdir /opt/auroramihomo ...
  auroramihomo -f etc/aurora-api.yaml
    data/bin/mihomo -d ./data
```

## AdGuard Home 服务单元

启用 AdGuard 组件并安装后，面板（在 systemd / OpenRC 环境）会把 AGH 注册为
独立的系统服务，与面板自身解耦：**面板升级、重启或崩溃期间，DNS 过滤不随
面板进程中断**；AGH 崩溃由服务管理器自动拉起。

```ini
# /etc/systemd/system/aurora-adguardhome.service
[Unit]
Description=Aurora AdGuard Home (managed by AuroraMihomo)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/auroramihomo/data/bin/AdGuardHome \
    --work-dir /opt/auroramihomo/data/adguardhome \
    --config /opt/auroramihomo/data/adguardhome/AdGuardHome.yaml \
    --no-check-update
Restart=always
RestartSec=3

# :53 绑定权限只给 AGH；面板自身不再需要整体带 CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
```

要点：

- **单元不含任何端口参数**（`--web-addr` / DNS 端口均不出现）：AGH 的端口唯一
  事实来源是 yaml（`http.address` / `dns.port`）。改端口只改 yaml 并重启服务
  （面板设置弹窗内操作即可），**单元从安装写到卸载不重写**。
- 面板对 AGH 的控制面（启停/自启/卸载）一律走 `systemctl` / `rc-service`，
  不会直接杀 PID——`Restart=always` 会把 kill 变成 3 秒后复活。
- 开机自启由单元 `enable` 状态决定（面板「设置 → AdGuard 设置 → 开机自启」开关
  驱动 `systemctl enable/disable`）；用户点「停止」只临时停进程，不影响自启。
- 卸载（面板内「彻底卸载」）顺序：解除 DNS 对接 → 停服务 → `disable` + 删单元
  → 删二进制与 work-dir。`disable` 必须先于删二进制，否则开机按残留 enable
  拉起已不存在的二进制。
- OpenRC（Alpine）环境对应 `/etc/init.d/aurora-adguardhome`，用
  `supervise-daemon`（与面板自身单元一致），`rc-update add aurora-adguardhome default`
  控制自启。
- 边界：DNS 过滤/日志/上游解析随服务常驻；TProxy / iptables 劫持规则由面板
  维护（面板重启后自动恢复），面板长期下线期间「入口劫持」不生效。

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

## AuroraMihomo（主程序）

**面板内一键自升级**（默认从 `weni09/AuroraMihomo` 仓库拉取；fork 用户可在
「系统设置 · 下载与更新出网」把主程序仓库改成自己的 `owner/AuroraMihomo`，
清空保存即停用面板内自升级。也可在配置 `Updater.SelfRepo` 设启动默认值）：

1. `GET /api/v1/system/self-update/check` 对比当前版本与 GitHub 最新 release；
2. `POST /api/v1/system/self-update`：下载与官方同名的
   `auroramihomo_<tag>_<goos>_<goarch>.tar.gz|.zip` 资产 → 与发布时附带的
   `.sha256` 比对完整性 → 临时目录验证新二进制可执行（`-version`）→
   暂存为 `<自身路径>.new` → **自动备份数据库** → 触发优雅关停；
3. 关停流程在释放数据库后调用 `SwapSelfBinary`：Unix 直接 rename 覆盖自身，
   Windows 先把自身改名为 `.old` 再把 `.new` 改回（运行中 exe 无法覆盖、
   但可重命名）；随后由进程管理器拉起新版；
4. 启动早期 `CleanupStaleSelf` 清理 `.old` 残留，并保留待生效的 `.new`
   （上次异常退出未及交换时，下次关停会继续完成升级）。

**停机窗口说明**：自升级与 `/api/v1/system/restart` 相同，是"优雅退出、
等进程管理器拉起"的语义（进程刻意不做 fork 自重启，见 Makefile 注释）。
从关停到 supervisor（systemd `Restart=always` / docker `restart` /
OpenRC `supervise-daemon` / NSSM）拉起新版之间，**面板 API 有秒级中断**，属预期。

自升级关停路径**不会**停止 mihomo / AdGuard：TProxy 规则仍在宿主上，
若先杀内核再等 supervisor 拉起主进程，会出现"规则在、内核死"的全面断网。
新主进程启动后会按数据库里记录的 PID 接管仍在运行的内核（`AttachExternal`），
再继续正常托管。普通 `/system/restart` 与信号退出仍会停内核。

**升级路径约束**：

- 依赖进程管理器拉起，`start-stop-daemon` 不满足（见 §OpenRC 单元）；
- Windows 上替换成功后遗留的 `.old` 由下次启动清理，两个文件共存只是中间态；
- 升级前的数据库自动备份失败仅记录不阻断（备份目录权限问题不该卡死升级，
  升级本身已通过 sha256 与可执行性校验）。

**install.sh 离线/命令行升级**：`scripts/install.sh` 走"停服 → 校验 → 解压 →
保留配置 → 覆盖 → 重启"，升级前在运行的服务装完会恢复运行（含 `--no-start`
场景）。配置保留见 §4，服务单元保留现有文件。

## Mihomo

-   version check
-   binary replace（更新前先备份为 `.bak`，解压产物先经临时校验再覆盖）

## Zashboard

-   static asset update（目录级 .bak + 失败回滚）

## Sub-Store

-   runtime/package update

------------------------------------------------------------------------

# 9. Backup

## 数据库在线备份

- `POST /api/v1/system/backup`：`VACUUM INTO` 生成独立副本
  `aurora-<时间戳>.db`，无需停服；落盘目录取配置 `Backup.Dir`，留空为
  `<Mihomo.ConfigDir>/backups`（容器镜像已预建 `/data/backups`）；
  按 `Backup.MaxKeep`（默认 7）保留最近份数，更旧自动清理。
- 主程序自升级前会自动执行一次同样的备份。
- 前端入口：系统设置 →「主程序升级与备份」→「立即备份」。

## 恢复步骤

1. 停服：`systemctl stop auroramihomo`（或 `docker stop`）；
2. 用备份文件替换数据文件（默认 `data/aurora.db`），删除同目录的
   `-wal` / `-shm` 残留（它们是旧库的 WAL 附属，与新库不匹配）；
3. 重启并确认。备份文件本身是完整数据库，可用 `sqlite3` 直接打开核对。

## Rollback

    恢复备份 → 重启（备份覆盖的是数据库与磁盘配置，`config.yaml`
    另有合并前自动备份，见 ConfigService）

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

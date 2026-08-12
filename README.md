# AuroraMihomo

Mihomo 内核运行时与配置管理平台：订阅聚合、配置合并、冲突处理、内嵌面板，一体化管理。

## 功能

- **订阅管理**：远程订阅与手动粘贴节点，支持流量信息展示（已用/总量/到期）、自定义 User-Agent、独立处理管道
- **组合订阅**：多订阅聚合，12 种处理算子（过滤/改名/加国旗/地区筛选/排序/JS 脚本等），多种输出格式
- **分享分发**：订阅与组合均可生成免登录分享链接，支持 `?target=` 切换客户端格式、`?filter=` 临时筛选
- **配置合并**：Base / Remote / Override 三层模型，自动检测冲突并支持本地优先、远程优先、自动合并、手动指定四种解决策略
- **版本管理**：配置变更自动备份（保留 10 份）与版本回滚
- **内核管理**：mihomo 启停、重载、日志实时推送、配置校验失败自动回滚
- **透明代理**：Linux（TUN / TProxy）与 macOS（TUN），带环境检测、强制确认与自动回滚
- **内嵌面板**：Zashboard 挂载在 `/ui/`
- **自动更新**：mihomo 与 Zashboard 定时更新，内置 8 个 CDN 源依次回退

单个静态二进制，不依赖 CGO 与外部 C 库，Alpine 与 Debian/Ubuntu 通用。

完整使用说明见 [使用文档](userdocs/user-guide.md)，部署运行后也可在面板侧边栏的「使用文档」里直接查看。

## 快速开始

三种部署方式，按场景选：

| 方式 | 适合 | 前置要求 |
|---|---|---|
| [Docker](#方式一docker) | 大多数场景，升级最省事 | Docker CLI（compose 方式另需 Docker Compose） |
| [二进制](#方式二二进制) | 不想引入 Docker；透明代理最省事 | 无（静态二进制） |
| [源码构建](#方式三源码构建) | 二次开发 | Go 1.25+、Node 22+ |

---

### 方式一：Docker

镜像已发布到 GitHub Container Registry（`ghcr.io/weni09/auroramihomo`，多架构 amd64/arm64），**免本地构建**，`docker run` 或 `docker compose` 直接拉取即可。默认拉 `latest` 标签；要锁定版本，设环境变量 `AURORA_VERSION`（如 `AURORA_VERSION=0.11.0`）。

> 镜像名是全小写的：GHCR 包名强制小写，写成大写（如 `ghcr.io/weni09/AuroraMihomo`）会报 `NAME_INVALID` 拉不下来。

**无法直连 GitHub？** 镜像存放在 GHCR，需要先换镜像加速站再拉取：

```bash
# compose：只改前缀（含仓库路径、不含 tag），AURORA_VERSION 锁定版本照常生效
AURORA_REGISTRY=ghcr.nju.edu.cn/weni09/auroramihomo docker compose up -d

# docker run：整条镜像名换成加速站
docker run -d --name auroramihomo ... ghcr.nju.edu.cn/weni09/auroramihomo:latest
```

本仓库实测 `ghcr.nju.edu.cn`（南京大学镜像站）可匿名拉取；其余加速站接口不稳定，用前先自行验证。容器内下载 mihomo 内核 / Zashboard 面板走内置 CDN 源（默认含 ghproxy.com 等），也可在「系统设置 → 下载与更新出网」或环境变量 `AURORA_CDN_PROVIDERS` 调整。

#### 方式 A：docker compose（推荐，含健康检查与安全项）

**1. 获取 compose 文件**

无需 clone 仓库，只下载这一个文件即可：

```bash
mkdir -p auroramihomo && cd auroramihomo
curl -fsSL -o docker-compose.yml \
  https://raw.githubusercontent.com/weni09/AuroraMihomo/main/docker/docker-compose.yml
```

**2. 数据目录**

数据默认落在 compose 文件同级的 `./data`（相对路径按 compose 文件所在目录解析）。换位置时用绝对路径覆盖：

```bash
AURORA_DATA_DIR=/opt/aurora/data docker compose up -d
```

容器默认以内置账户 `aurora`（uid/gid 10001）运行，启动时会自动修正挂载目录属主，**无需手工 chown**。两种可选调整：

- **让数据归当前用户管理**（宿主机不用 sudo 就能读写 `data/`）：把容器运行账户改成你的 uid/gid，并把目录一并 chown 给自己：

  ```bash
  chown -R "$(id -u):$(id -g)" data
  AURORA_PUID=$(id -u) AURORA_PGID=$(id -g) docker compose up -d
  ```

- **不关心属主，直接放开写权限**（容器以任意 uid 都能写，属主保持现状）：

  ```bash
  sudo chmod -R a+rwX data   # 777 的温和版：只加读写位，不额外设执行位
  ```

**3. 设置 JWT 密钥（生产环境建议）**

不设也能跑（首次启动会随机生成并存入数据库），但显式设置便于多实例共用与灾备恢复：

```bash
# 编辑 docker-compose.yml，取消 AURORA_JWT_SECRET 注释并填入随机长串
openssl rand -hex 32   # 生成一个
```

**4. 启动**

```bash
docker compose up -d
```

首次会自动从 GHCR 拉取镜像；随后容器内会自动下载 mihomo 内核与 Zashboard 面板，需要能访问 GitHub，视网速可能要几分钟。

**5. 确认启动成功**

```bash
docker compose ps        # 状态应为 healthy
curl -fsS http://127.0.0.1:8899/healthz && echo OK
```

**6. 取初始密码并登录**

```bash
docker compose logs | grep 初始管理员密码
# 或直接读文件
docker exec auroramihomo cat /data/initial_password.txt
```

浏览器打开 `http://<宿主机IP>:8899`，用该密码登录。

> 首次成功登录或在「系统设置 → 管理员密码」改密后，程序会自动删除该明文文件。请仍尽快修改初始密码。

**升级**

```bash
# 可选：更新 compose 文件本身
curl -fsSL -o docker-compose.yml \
  https://raw.githubusercontent.com/weni09/AuroraMihomo/main/docker/docker-compose.yml
docker compose pull    # 拉取新镜像
docker compose up -d
```

数据都在 `data/` 卷里，升级不丢配置。

#### 方式 B：docker run（免 clone，一条命令）

数据目录无需预先 chown，容器启动时自动修正属主。想让数据归当前用户管理，加 `-e AURORA_PUID=$(id -u) -e AURORA_PGID=$(id -g)` 并把 `data/` chown 给自己（写法见方式 A 第 2 步）：

```bash
mkdir -p data
docker run -d \
  --name auroramihomo \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN \
  -v "$(pwd)/data:/data" \
  -e AURORA_JWT_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/weni09/auroramihomo:latest
```

初始密码：`docker logs auroramihomo | grep 初始管理员密码`。升级：`docker rm -f auroramihomo` 后重新执行上面的启动命令（`latest` 会拉取最新版）。

#### 内核 / Zashboard 初始化失败与手动放置

首次启动时容器内会自动从 GitHub 下载 mihomo 内核与 Zashboard 面板。若下载失败（无法直连 GitHub、CDN 源全挂等），日志会反复出现：

- `mihomo binary missing, downloading...` / `zashboard assets missing, downloading...` —— 启动时检测到缺失，开始下载
- `查询 ... 最新版本失败` —— 连不上 `api.github.com`（多为网络问题）
- `download failed via ...` / `all download sources failed: ...` —— 版本能查到，但所有下载源都失败（CDN 源被墙/失效）

注意：**面板本身不受影响**，仍可正常访问（`FailOnEnsureError=false`，内核缺失只记日志不退出）；症状是代理内核起不来、`/ui/` 返回 404。先尝试「系统设置 → 下载与更新出网」调整 CDN 源顺序，或设 `AURORA_CDN_PROVIDERS` / `AURORA_GITHUB_API`（见「环境变量」表）；仍不行就按下面手工放置。

**mihomo 内核 → 容器内 `/data/bin/mihomo`，即宿主挂载目录 `data/bin/`**：

```bash
# 从 https://github.com/MetaCubeX/mihomo/releases/latest 选对应平台资产，
# 如 mihomo-linux-amd64-v1.19.29.gz（Linux/macOS 是 .gz 裸二进制，Windows 是 .zip）
mkdir -p data/bin
curl -fsSL -o data/bin/mihomo.gz https://github.com/MetaCubeX/mihomo/releases/download/v1.19.29/mihomo-linux-amd64-v1.19.29.gz
gunzip -f data/bin/mihomo.gz
chmod +x data/bin/mihomo
docker restart auroramihomo   # 内核在启动时检测，放好后需重启容器
```

**Zashboard 面板 → 容器内 `/data/zashboard/`（存在 `index.html` 即生效，无需注册），即宿主 `data/zashboard/`**：

```bash
# 资产名固定为 dist.zip，可直接用 latest 下载：
# https://github.com/Zephyruso/zashboard/releases/latest/download/dist.zip
rm -rf data/zashboard && mkdir -p data/zashboard
curl -fsSL -o /tmp/zashboard.zip https://github.com/Zephyruso/zashboard/releases/latest/download/dist.zip
unzip -o /tmp/zashboard.zip -d /tmp/zash
find /tmp/zash -name index.html          # 找到含 index.html 的目录
cp -r /tmp/zash/<含 index.html 的目录>/* data/zashboard/
```

放好后刷新 `http://<宿主机IP>:8899/ui/` 即可，无需重启。无法直连 GitHub 时，给上面的 URL 套 CDN 前缀（如 `https://ghproxy.com/https://github.com/...`）。手工放置的文件无需担心属主：容器（重启）时入口脚本会自动把 `/data` 修正为运行账户所有。

#### 透明代理的额外改动

默认配置不支持透明代理。需要时编辑 `docker-compose.yml`，取消 `user: "0:0"`、`AURORA_RUN_AS_ROOT` 与 `devices` 的注释，并注释掉 `no-new-privileges`。这会降低容器隔离性，详见[透明代理文档](docs/AuroraMihomo-Transparent-Proxy.md)。

另外在**宿主机**写入转发与 `rp_filter`（容器内不会改 sysctl；host 网络下 Docker 也会拒绝相关 `--sysctl`）：

```bash
sudo cp scripts/sysctl-auroramihomo.conf /etc/sysctl.d/99-auroramihomo.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo.conf
# 可选：BBR（内核需支持；与转发项分文件）
sudo cp scripts/sysctl-auroramihomo-bbr.conf /etc/sysctl.d/99-auroramihomo-bbr.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo-bbr.conf
```

TProxy 需要的 `iptables`/`nftables`/`iproute2` 已预装在镜像里，开箱可用。两种模式的依赖不同：TUN 需要映射 `/dev/net/tun` 但不需要这些命令行工具（mihomo 自己走 netlink）；TProxy 反之，不需要 tun 设备。

---

### 方式二：二进制

产物是静态二进制，不依赖 CGO 与外部 C 库，同一个包在 Alpine（musl）与 Debian/Ubuntu（glibc）上通用。

#### 在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/weni09/AuroraMihomo/main/scripts/install.sh \
  | sudo sh -s -- --repo weni09/AuroraMihomo
```

一条命令装完即可访问，Debian/Ubuntu 与 Alpine 都是。脚本会：

1. 探测系统、架构与服务管理器（systemd 或 OpenRC）
2. 从 Release 拉对应包、校验 sha256、解压到 `/opt/auroramihomo`
3. 补齐透明代理依赖：缺失时装 `iptables`/`nftables`/`iproute2`（Alpine 还有独立的 `ip6tables`），加载 `tun` 与 `nft_tproxy` 并写 `/etc/modules-load.d/` 持久化；**写入 `/etc/sysctl.d/99-auroramihomo.conf`**（`ip_forward`、`ipv6.conf.all.forwarding`、`rp_filter=2`，见 `scripts/sysctl-auroramihomo.conf`）并 `sysctl -p` 加载；内核支持时 **best-effort 启用 BBR**（独立文件 `99-auroramihomo-bbr.conf` / `scripts/sysctl-auroramihomo-bbr.conf`，失败只告警不阻断安装）
4. 装服务单元 —— systemd 装 unit，Alpine 装 OpenRC 脚本（用 `supervise-daemon`，面板的「重启」依赖它）
5. 启用开机自启并启动服务

**开箱默认配置：** 首次启动若数据库中还没有基础配置（base），会自动写入
`backend/internal/service/default_base.yaml`（构建时嵌入二进制）。内容从真实部署
提炼并去掉个人数据：公共 DNS/DoH、`nameserver-policy`、私网直连规则、`MATCH,DIRECT`
兜底、TUN 排除地址等。**不会**预写订阅节点、设备 SRC-IP、内网 DNS、口令或
`tproxy-port`（透明代理仍由面板开关写入）。已有 base 时不会覆盖。

升级时重跑同一条命令：保留现有配置与已有服务单元，自动停服替换再拉起。

想先看清它要做什么，加 `--dry-run` 只打印不执行：

```bash
curl -fsSL https://raw.githubusercontent.com/weni09/AuroraMihomo/main/scripts/install.sh \
  | sudo sh -s -- --repo weni09/AuroraMihomo --dry-run
```

可选参数：

```bash
sudo sh install.sh --version v0.2.0      # 装指定版本
sudo sh install.sh --dir /srv/aurora     # 换安装目录
sudo sh install.sh --no-deps             # 不动系统：不装包、不加载内核模块、不写 sysctl
sudo sh install.sh --no-service          # 不装服务单元（旧名 --no-systemd 仍可用）
sudo sh install.sh --no-start            # 装好但不启用/不启动
```

`--no-deps` 之后透明代理仍需手工补齐依赖，命令见下面「Alpine 补充说明」。容器内运行时脚本会跳过依赖补齐（装的包重建即丢、`modprobe`/`sysctl` 会作用于宿主内核），容器部署请用 Docker 镜像；**宿主机**请另执行：

```bash
sudo cp scripts/sysctl-auroramihomo.conf /etc/sysctl.d/99-auroramihomo.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo.conf
# 可选 BBR
sudo cp scripts/sysctl-auroramihomo-bbr.conf /etc/sysctl.d/99-auroramihomo-bbr.conf
sudo sysctl -p /etc/sysctl.d/99-auroramihomo-bbr.conf
```

（`docker/docker-compose.yml` 注释里也有同一套步骤。）

#### 离线安装

内网机器上，先在有网的机器下载对应平台的包：

```
auroramihomo_<版本>_linux_amd64.tar.gz
auroramihomo_<版本>_linux_arm64.tar.gz
auroramihomo_<版本>_darwin_arm64.tar.gz
```

拷到目标机后：

```bash
sudo mkdir -p /opt/auroramihomo
sudo tar -xzf auroramihomo_<版本>_linux_amd64.tar.gz --strip-components=1 -C /opt/auroramihomo
cd /opt/auroramihomo
./auroramihomo -f etc/aurora-api.yaml
```

包内含二进制与默认配置（前端资源已内嵌进二进制），**不含 mihomo 内核**。完全离线时需手工放置内核：

```bash
# 从 https://github.com/MetaCubeX/mihomo/releases 下载对应平台的 .gz
# 注意 Linux/macOS 官方发的是 .gz（gzip 压缩的裸二进制，不是 tar 归档）
sudo mkdir -p /opt/auroramihomo/data/bin
gunzip -c mihomo-linux-amd64-v1.19.29.gz | sudo tee /opt/auroramihomo/data/bin/mihomo >/dev/null
sudo chmod +x /opt/auroramihomo/data/bin/mihomo
```

同理，Zashboard 面板可手工放到 `data/zashboard/`；不放则 `/ui/` 不可用，不影响主面板。

#### 作为服务运行

在线安装脚本已按系统自动装好服务单元（systemd 或 OpenRC）并启动，这一节只在离线安装或想自定义单元时才需要。

**systemd（Debian/Ubuntu 等）**

```bash
sudo tee /etc/systemd/system/auroramihomo.service >/dev/null <<'EOF'
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

# 透明代理需要的权限。不用透明代理可删掉这两行并加 User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE

# UDP 代理并发高时容易撞上文件描述符上限
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now auroramihomo
sudo systemctl status auroramihomo
sudo journalctl -u auroramihomo -f      # 看日志
```

进程自身不做 fork 自重启，生产环境依赖 systemd 的 `Restart=always`。

**OpenRC（Alpine）** 在线安装脚本会自动装下面这份脚本，离线安装时手工放。要点是用 `supervise-daemon` 而非 `start-stop-daemon` —— 面板的「重启」是「优雅退出、等进程管理器拉起」，后者不做拉起，会让重启变成单向关机：

```bash
sudo tee /etc/init.d/auroramihomo >/dev/null <<'EOF'
#!/sbin/openrc-run
name="auroramihomo"
directory="/opt/auroramihomo"
command="/opt/auroramihomo/auroramihomo"
command_args="-f etc/aurora-api.yaml"
command_user="root:root"
# 不重定向 stdout/stderr：应用日志由面板写 <data>/logs/aurora.log
# 并受日志清理任务管理，不再往 /var/log 写重复副本。
supervisor="supervise-daemon"
pidfile="/run/auroramihomo.pid"
depend() { need net; after firewall; }
EOF

sudo chmod +x /etc/init.d/auroramihomo
sudo rc-update add auroramihomo default
sudo rc-service auroramihomo start
sudo tail -f /opt/auroramihomo/data/logs/aurora.log
```

#### Alpine 补充说明

在线安装脚本已自动处理下面第一组（透明代理必需项）。离线安装、或用了 `--no-deps` 时需手工执行：

```bash
# ip6tables 是独立包；iproute2 会把 /sbin/ip 从 busybox 换成真 iproute2
sudo apk add --no-cache iptables ip6tables nftables iproute2
# tun 与 nft_tproxy 默认未加载
sudo modprobe tun && sudo modprobe nft_tproxy
printf 'tun\nnft_tproxy\n' | sudo tee /etc/modules-load.d/auroramihomo.conf
```

排查工具脚本不装（不是运行所必需，装不装取决于你的习惯）：

```bash
# 默认只有 busybox 的 wget，不支持 -x 与 --interface
sudo apk add --no-cache curl bind-tools
```

根分区余量值得先确认：官方 cloud 镜像可能只有 100M 出头，而部署约需 55M（二进制 29M + 前端 3M + 内核 15M + 依赖包）。安装脚本会在可用空间不足时告警，但不阻断。

取初始密码：

```bash
sudo cat /opt/auroramihomo/data/initial_password.txt
```

---

### 方式三：源码构建

前置：Go 1.25+、Node 22.18+

```bash
make deps    # 安装前后端依赖
make build   # 构建前端（同步到 go:embed 内嵌源）并编译后端
make run     # 启动
```

不使用 make 时：

```bash
go mod download
cd frontend && npm ci && npm run build && cd ..
rm -rf backend/api/public && cp -r frontend/dist backend/api/public  # go:embed 内嵌源
touch backend/api/public/.gitkeep
go build -o auroramihomo ./backend/api
./auroramihomo -f backend/api/etc/aurora-api.yaml
```

交叉编译（`CGO_ENABLED=0`，无需任何 C 工具链）：

```bash
CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags="-w -s" -o auroramihomo ./backend/api
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-w -s" -o auroramihomo ./backend/api
```

访问地址：

| 地址 | 说明 |
|---|---|
| `http://127.0.0.1:8899` | 管理面板 |
| `http://127.0.0.1:8899/ui/` | Zashboard 内嵌面板 |
| `http://127.0.0.1:8899/healthz` | 健康检查 |

### 开发模式

后端与前端分别启动，Vite 会把 API 代理到后端：

```bash
make dev                        # 后端，监听 8899
cd frontend && npm run dev      # 前端，监听 5173
```

## 配置

配置文件为 `backend/api/etc/aurora-api.yaml`，容器内使用 `docker/aurora-api.docker.yaml`。

关键项可用环境变量覆盖（优先级高于配置文件）：

| 环境变量 | 说明 |
|---|---|
| `AURORA_JWT_SECRET` | JWT 签名密钥。生产环境建议显式设置，否则首次启动会随机生成并存入数据库 |
| `AURORA_JWT_EXPIRE` | 令牌有效期（秒），默认 86400 |
| `AURORA_DATA_SOURCE` | SQLite 数据库路径 |
| `AURORA_CONFIG_DIR` | 数据目录（config.yaml、备份、内核、面板均在此） |
| `AURORA_MIHOMO_BINARY` | mihomo 二进制路径，留空则用数据目录下的默认位置 |
| `AURORA_HOST` / `AURORA_PORT` | 监听地址与端口 |
| `AURORA_AUTO_UPDATE` | 是否启用自动更新（true/false） |
| `AURORA_AUTO_UPDATE_CRON` | 自动更新 cron 表达式（6 段，含秒） |
| `AURORA_GITHUB_API` | GitHub API 地址，可指向自建镜像 |
| `AURORA_CDN_PROVIDERS` | CDN 源列表，逗号分隔 |

## 数据目录

```
data/
├── aurora.db              # SQLite 数据库（订阅、组合、设置、版本记录）
├── config.yaml            # 合并生成的 mihomo 配置，内核实际加载的就是它
├── backups/               # 配置备份，保留最近 10 份
├── bin/mihomo             # 内核二进制（自动下载）
├── zashboard/             # 面板静态资源（自动下载）
├── substore/              # Sub-Store 脚本运行目录
├── netbackup/             # 透明代理启用前的防火墙/路由快照
├── logs/aurora.log        # 应用日志（8MB × 5 份滚动）
└── initial_password.txt   # 首次启动生成的初始密码；登录或改密后自动删除
```

目录位置由配置项 `Mihomo.ConfigDir` 决定（容器内是 `/data`）。备份整个目录即可完整迁移。

## 运维

### 备份与恢复

```bash
# 备份（停服后拷贝最稳妥，避免 SQLite 写入中途被复制）
sudo systemctl stop auroramihomo
sudo tar -czf aurora-backup-$(date +%F).tar.gz -C /opt/auroramihomo data
sudo systemctl start auroramihomo

# 恢复
sudo systemctl stop auroramihomo
sudo tar -xzf aurora-backup-2026-07-30.tar.gz -C /opt/auroramihomo
sudo systemctl start auroramihomo
```

配置文件本身还有另一层保护：每次合并前自动备份到 `data/backups/`，界面「配置中心 → 版本」可直接回滚。

### 升级

| 部署方式 | 升级命令 |
|---|---|
| Docker | `docker compose pull && docker compose up -d`（可选先重新下载 compose 文件） |
| 二进制（在线） | 重跑安装脚本，会保留配置并自动停服替换 |
| 二进制（离线） | 停服 → 覆盖二进制 → 启动。**不要覆盖 `etc/` 与 `data/`** |
| 源码 | `git pull && make build && make run` |

mihomo 内核与 Zashboard 面板的升级独立于本体，在「系统设置」页手动触发或开启自动更新。

### 卸载

```bash
# Docker
docker compose down
# 数据仍在 ./data，确认不再需要后再删

# 二进制
sudo systemctl disable --now auroramihomo
sudo rm /etc/systemd/system/auroramihomo.service
sudo systemctl daemon-reload
sudo rm -rf /opt/auroramihomo      # 会一并删除 data/，先备份
```

若曾启用过 TProxy 透明代理，卸载前请先在界面关闭开关，让程序把防火墙规则与策略路由拆干净。直接删程序会把规则留在宿主上。

### 排障

**服务起不来**

```bash
sudo journalctl -u auroramihomo -n 100 --no-pager    # systemd
docker compose logs --tail=100   # Docker
tail -100 /opt/auroramihomo/data/logs/aurora.log     # 应用日志
```

**Docker 反复报 `unable to open database file (14)`**：SQLite 打不开 `/data/aurora.db`，通常是挂载的数据目录不可写。新镜像（入口脚本）启动时会自动把属主修正为运行账户（默认 uid/gid 10001，可用 `AURORA_PUID`/`AURORA_PGID` 调整），若仍报错请确认：已 `docker compose pull` 拉到新镜像；数据目录（含 `AURORA_DATA_DIR` 覆盖的场景）存在且宿主机可写。老版本镜像救急：`sudo chmod -R a+rwX <数据目录>`，或按运行账户 chown（默认 `sudo chown -R 10001:10001 <数据目录>`，改了 `AURORA_PUID` 就用对应 uid）。

**内核下载失败**：日志里出现 `download failed via ...`。通常是网络问题，可在「系统设置 → 下载与更新出网」调整 CDN 源顺序，或手工放置内核（Docker 见上文「内核 / Zashboard 初始化失败与手动放置」；二进制见离线安装）。

**端口被占用**：默认用 8899（面板）与 9090（内核控制 API）。改端口用环境变量 `AURORA_PORT`，内核控制端口在「配置中心」的 `external-controller` 里改。

**忘记管理员密码**：没有内置重置命令。密码哈希存在 `settings` 表的 `admin_password` 键，删掉该行后重启，程序会重新生成初始密码并写入 `data/initial_password.txt`：

```bash
sudo systemctl stop auroramihomo
sqlite3 /opt/auroramihomo/data/aurora.db "delete from settings where key='admin_password';"
sudo systemctl start auroramihomo
sudo cat /opt/auroramihomo/data/initial_password.txt
```

宿主没装 `sqlite3` 时可用任意 SQLite 客户端，或直接从备份恢复。

**配置合并后内核起不来**：程序会自动校验并回滚到上一份可用配置，日志里能看到回滚记录。也可在「配置中心 → 版本」手工回滚。

## 常用命令

```bash
make check             # 格式检查 + 静态检查 + 测试 + 前端类型检查
make test-race         # 带竞态检测运行测试
make cover             # 查看测试覆盖率
make sync-docs         # 同步 userdocs/ 到前端内置副本
make docker            # 构建镜像
make docker-multiarch  # 构建 amd64/arm64 多架构镜像
```

## 发布版本

GitHub Actions **只在手动打 tag 并 push 后触发**，日常推送分支不跑流水线。

```bash
# 先在本地确认全绿，避免推了 tag 才发现问题
make check

git tag v0.2.0
git push origin v0.2.0
```

推送后自动执行：质量门禁（完整 CI）→ 构建五平台二进制 → 创建 Release 并附上校验和。

门禁不过就不会构建产物，也不会创建 Release，因此不存在「发出了测试不通过的包」这种情况。

想在正式打 tag 前先验一遍，可以在 Actions 页面手动运行 Release（填一个临时版本号）。手动运行只构建产物供下载，**不创建 Release**。也可以单独手动运行 CI 只跑检查。

发错了 tag 需要重来：

```bash
git tag -d v0.2.0                  # 删本地
git push origin :refs/tags/v0.2.0  # 删远端
```

已创建的 Release 需要在 GitHub 页面手工删除，删 tag 不会连带删除它。

## 分享链接

订阅、组合、文件模板都会自动生成免登录的分享链接：

```
http://<地址>/api/v1/share/<token>               # 订阅与组合，默认 Mihomo YAML
http://<地址>/api/v1/share/<token>?target=surge  # 指定客户端格式
http://<地址>/api/v1/share/<token>?filter=香港    # 临时按关键词筛选节点
http://<地址>/api/v1/file/<token>                # 文件模板直链（不支持 target/filter）
```

`target` 支持：`clash` `mihomo` `base64` `plain` `links` `surge` `surgemac` `loon` `qx` `singbox` `v2ray` `json` `stash` `surfboard` `shadowrocket` `egern`。

链接凭据即链接本身，可在「Sub-Store 管理 → 分享管理」集中改名、设有效期、重置凭据或撤销。详见[使用文档](userdocs/user-guide.md#分享管理)。

## 透明代理

让局域网设备无需各自设置代理即可分流。支持 Linux（TUN / TProxy）与 macOS（仅 TUN），Windows 不支持。

在「系统设置 → 透明代理」启用。面板会先检测运行环境，缺依赖时给出可复制的安装命令；条件不具备时开关不可用。

启用后**必须在 90 秒内确认网络正常**，否则自动拆除规则并关闭开关——规则配错可能让你同时失去 SSH 与面板访问，这个确认窗口是唯一的补救通道。回滚意图会持久化，面板崩溃重启后仍会生效。

两种模式都会**一并接管本机自身的流量**（宿主上的 `curl`、`apt` 也按分流规则走节点），没有只代理局域网设备的开关。SSH、面板端口、内核 API 与面板自身的出站始终直连。本机 DNS 指向回环（systemd-resolved 的 `127.0.0.53`）时本机的域名分流不生效，检测会告警。

两种模式的取舍、防护机制、本机流量的接管细节、以及终端设备的四种接入方式（手动代理 / 只改 DNS / 网关模式 / 旁路由），见[透明代理文档](docs/AuroraMihomo-Transparent-Proxy.md)。

真机实测记录（含完整命令与输出）：

- [Ubuntu 24.04 / Docker](docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md)
- [Alpine 3.24 / 二进制](docs/AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md)（含 OpenRC 与 musl 兼容性）
- [Ubuntu 二进制与容器](docs/AuroraMihomo-Transparent-Proxy-Test-Report.md)

> 首次启用 TProxy 建议在有物理或控制台访问的机器上验证。

## 安全说明

- 管理员口令以 PBKDF2-HMAC-SHA256（21 万轮）加盐哈希存储
- 登录失败 5 次（5 分钟窗口内）锁定 15 分钟
- 除分享链接、文件直链、登录接口外，所有 API 均需 JWT；WebSocket 连接同样校验令牌
- 分享链接与文件直链设计上即为公开访问，请勿在其中放置敏感内容
- 建议不要将服务直接暴露在公网；如需暴露请置于反向代理之后并启用 HTTPS

## 默认 CDN 源

`ghproxy.com` → `mirror.ghproxy.com` → `gh.ddlc.top` → `ghproxy.net` → `gitdl.cn` → `gh.llkk.cc` → `ghp.ci` → `github`（官方兜底）

按顺序尝试，失败自动回退到下一个。可在「系统设置」页调整。

## 鸣谢

本项目的运行与面板能力建立在下列开源项目之上，谨致谢意：

- **[Mihomo](https://github.com/MetaCubeX/mihomo)**（Clash.Meta）— 代理内核，负责规则分流、TUN/TProxy 与出站协议实现
- **[Zashboard](https://github.com/Zephyruso/zashboard)** — 内嵌控制面板，提供代理组、连接与流量等可视化管理界面
- **[Sub-Store](https://github.com/sub-store-org/Sub-Store)** — 订阅管理脚本生态。其核心组件（单条/组合订阅、模板转换、分享）在本仓库以 Go 重写（`backend/internal/substore` 与 `service` 层）；脚本类操作符以 goja 内嵌执行原版脚本，订阅工作流与其操作符语义保持一致

上述项目的许可证、商标与品牌归属其各自作者与社区；本仓库仅为集成与配置管理用途。

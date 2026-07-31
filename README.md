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
| [Docker](#方式一docker) | 大多数场景，升级最省事 | Docker 与 Docker Compose |
| [二进制](#方式二二进制) | 不想引入 Docker；透明代理最省事 | 无（静态二进制） |
| [源码构建](#方式三源码构建) | 二次开发 | Go 1.25+、Node 22+ |

---

### 方式一：Docker

**1. 获取代码**

```bash
git clone https://github.com/OWNER/AuroraMihomo.git
cd AuroraMihomo
```

**2. 准备数据目录**

镜像默认以非 root 账户 `aurora`（uid 10001）运行，宿主目录属主要匹配，否则容器写不进去：

```bash
mkdir -p data
sudo chown -R 10001:10001 data
```

**3. 设置 JWT 密钥（生产环境建议）**

不设也能跑（首次启动会随机生成并存入数据库），但显式设置便于多实例共用与灾备恢复：

```bash
# 编辑 docker/docker-compose.yml，取消 AURORA_JWT_SECRET 注释并填入随机长串
openssl rand -hex 32   # 生成一个
```

**4. 启动**

```bash
docker compose -f docker/docker-compose.yml up -d
```

首次启动会自动下载 mihomo 内核与 Zashboard 面板，需要能访问 GitHub，视网速可能要几分钟。

**5. 确认启动成功**

```bash
docker compose -f docker/docker-compose.yml ps        # 状态应为 healthy
curl -fsS http://127.0.0.1:8899/healthz && echo OK
```

**6. 取初始密码并登录**

```bash
docker compose -f docker/docker-compose.yml logs | grep 初始管理员密码
# 或直接读文件
docker exec auroramihomo cat /data/initial_password.txt
```

浏览器打开 `http://<宿主机IP>:8899`，用该密码登录。

> 该明文密码文件**不会自动删除**。登录后请立即在「系统设置 → 管理员密码」修改密码，然后手工删除 `data/initial_password.txt`。

**升级**

```bash
git pull
docker compose -f docker/docker-compose.yml up -d --build
```

数据都在 `data/` 卷里，升级不丢配置。

**透明代理的额外改动**

默认配置不支持透明代理。需要时编辑 `docker/docker-compose.yml`，取消 `user: "0:0"` 与 `devices` 的注释，并注释掉 `no-new-privileges`。这会降低容器隔离性，详见[透明代理文档](docs/AuroraMihomo-Transparent-Proxy.md)。

TProxy 需要的 `iptables`/`nftables`/`iproute2` 已预装在镜像里，开箱可用。两种模式的依赖不同：TUN 需要映射 `/dev/net/tun` 但不需要这些命令行工具（mihomo 自己走 netlink）；TProxy 反之，不需要 tun 设备。

---

### 方式二：二进制

产物是静态二进制，不依赖 CGO 与外部 C 库，同一个包在 Alpine（musl）与 Debian/Ubuntu（glibc）上通用。

#### 在线安装

```bash
curl -fsSL https://raw.githubusercontent.com/OWNER/AuroraMihomo/main/scripts/install.sh \
  | sudo sh -s -- --repo OWNER/AuroraMihomo
```

脚本会探测系统与架构、从 Release 拉对应包、解压到 `/opt/auroramihomo`、安装 systemd 单元。升级时重跑同一条命令即可，它会保留现有配置并自动停服替换。

Alpine 等无 systemd 的系统上，脚本检测不到 `systemctl` 会跳过服务安装（不报错），装完只有程序本身、末尾的提示也仍是 systemd 命令 —— 服务需按下面「作为服务运行」的 OpenRC 部分手工配置。

可选参数：

```bash
sudo sh install.sh --version v0.2.0      # 装指定版本
sudo sh install.sh --dir /srv/aurora     # 换安装目录
sudo sh install.sh --no-systemd          # 不装 systemd 单元
```

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

包内含二进制、前端资源（`public/`）与默认配置，**不含 mihomo 内核**。完全离线时需手工放置内核：

```bash
# 从 https://github.com/MetaCubeX/mihomo/releases 下载对应平台的 .gz
# 注意 Linux/macOS 官方发的是 .gz（gzip 压缩的裸二进制，不是 tar 归档）
sudo mkdir -p /opt/auroramihomo/data/bin
gunzip -c mihomo-linux-amd64-v1.19.29.gz | sudo tee /opt/auroramihomo/data/bin/mihomo >/dev/null
sudo chmod +x /opt/auroramihomo/data/bin/mihomo
```

同理，Zashboard 面板可手工放到 `data/zashboard/`；不放则 `/ui/` 不可用，不影响主面板。

#### 作为服务运行

在线安装脚本已自动装好 systemd 单元。手工安装时：

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

**Alpine（OpenRC）** 没有 systemd，`install.sh` 的 systemd 单元用不上，需手工放服务脚本。要点是用 `supervise-daemon` 而非 `start-stop-daemon` —— 面板的「重启」是「优雅退出、等进程管理器拉起」，后者不做拉起，会让重启变成单向关机：

```bash
sudo tee /etc/init.d/auroramihomo >/dev/null <<'EOF'
#!/sbin/openrc-run
name="auroramihomo"
directory="/opt/auroramihomo"
command="/opt/auroramihomo/auroramihomo"
command_args="-f etc/aurora-api.yaml"
command_user="root:root"
supervisor="supervise-daemon"
supervise_daemon_args="--stdout /var/log/auroramihomo.log --stderr /var/log/auroramihomo.log"
pidfile="/run/auroramihomo.pid"
depend() { need net; after firewall; }
EOF

sudo chmod +x /etc/init.d/auroramihomo
sudo rc-update add auroramihomo default
sudo rc-service auroramihomo start
sudo tail -f /var/log/auroramihomo.log
```

Alpine 还需补几个默认没有的东西（透明代理必需项 + 排查工具）：

```bash
# ip6tables 是独立包；iproute2 会把 /sbin/ip 从 busybox 换成真 iproute2
sudo apk add --no-cache nftables iproute2 ip6tables
# 默认只有 busybox 的 wget，不支持 -x 与 --interface
sudo apk add --no-cache curl bind-tools
# tun 与 nft_tproxy 默认未加载
sudo modprobe tun && sudo modprobe nft_tproxy
printf 'tun\nnft_tproxy\n' | sudo tee /etc/modules-load.d/auroramihomo.conf
```

另外注意根分区余量：官方 cloud 镜像可能只有 100M 出头，而部署约需 55M（二进制 29M + 前端 3M + 内核 15M + 依赖包）。

取初始密码：

```bash
sudo cat /opt/auroramihomo/data/initial_password.txt
```

---

### 方式三：源码构建

前置：Go 1.25+、Node 22.18+

```bash
make deps    # 安装前后端依赖
make build   # 构建前端并同步到 public/，再编译后端
make run     # 启动
```

不使用 make 时：

```bash
go mod download
cd frontend && npm ci && npm run build && cd ..
rm -rf public && cp -r frontend/dist public   # 后端从 ./public 提供前端资源
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
└── initial_password.txt   # 首次启动生成的初始密码，登录后请手工删除
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
| Docker | `git pull && docker compose -f docker/docker-compose.yml up -d --build` |
| 二进制（在线） | 重跑安装脚本，会保留配置并自动停服替换 |
| 二进制（离线） | 停服 → 覆盖二进制与 `public/` → 启动。**不要覆盖 `etc/` 与 `data/`** |
| 源码 | `git pull && make build && make run` |

mihomo 内核与 Zashboard 面板的升级独立于本体，在「系统设置」页手动触发或开启自动更新。

### 卸载

```bash
# Docker
docker compose -f docker/docker-compose.yml down
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
docker compose -f docker/docker-compose.yml logs --tail=100   # Docker
tail -100 /opt/auroramihomo/data/logs/aurora.log     # 应用日志
```

**内核下载失败**：日志里出现 `download failed via ...`。通常是网络问题，可在「系统设置 → 下载与更新出网」调整 CDN 源顺序，或手工放置内核（见离线安装）。

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

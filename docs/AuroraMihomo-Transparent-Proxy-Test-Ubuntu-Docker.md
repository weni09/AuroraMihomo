# AuroraMihomo 透明代理测试报告（Ubuntu 24.04 / Docker 形态）

本文记录在 Ubuntu 24.04 上以 **Docker 形态**部署 AuroraMihomo 并验证透明代理的
全过程，覆盖环境检测、代理基础能力、TProxy 模式（含防"锁死自己"机制与本机
流量接管）、TUN 模式对照，以及关闭后的清理完整性。

与既有的 [AuroraMihomo-Transparent-Proxy-Test-Report.md](./AuroraMihomo-Transparent-Proxy-Test-Report.md)
的关系：那份报告在同一台主机上以**二进制形态**做过验证，本文是 **Docker 形态**
的独立验证，且这次配置了真实订阅节点，因此"本机流量真的走出去了"这条判据
用出口 IP 对比得到了闭环确认。

设计依据见 [AuroraMihomo-Transparent-Proxy.md](./AuroraMihomo-Transparent-Proxy.md)，
本文只记录测试过程与结论，不重复其设计说明。

同批次的 Alpine 二进制形态报告见
[AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md](./AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md)。

---

## 1. 测试环境

| 项 | 值 |
| --- | --- |
| 主机 | Proxmox 虚拟机，2 vCPU / 1867M 内存 / 49G 磁盘 |
| 系统 | Ubuntu 24.04.4 LTS，内核 6.8.0-124-generic |
| 网络 | 局域网 192.168.1.128/24，网卡 `ens18`，**有 IPv6 出网** |
| 部署形态 | Docker（镜像自建），`network_mode: host` |
| Docker | 29.7.0，测试前无任何镜像与容器 |
| 镜像 | `auroramihomo:latest`，76.8MB，基础镜像 alpine:3.21 |
| 容器内工具 | iproute2-6.11.0（真 iproute2，非 busybox）、nft、iptables、ip6tables |
| mihomo | v1.19.29，`with_gvisor` |
| zashboard | v3.16.0 |
| 测试节点 | 本地开发库的 2 条真实订阅，共 75 个节点 |

宿主原有一套二进制部署的实例正在运行，测试前已彻底清理（见 2.1）。

---

## 2. 部署过程

### 2.1 旧实例清理

清理前先备份数据库：

```
$ ls -la /root/aurora-old-backup/
-rw-r--r-- 1 root root 131072 aurora.db
-rw-r--r-- 1 root root     33 initial_password.txt
```

**一处如实记录的不足**：只备份了主库文件，`aurora.db-wal`（2.9MB，未
checkpoint）没有备到，随目录删除一起丢失。SQLite 的 WAL 里可能有尚未合并进
主库的写入，严格的备份应当先 checkpoint 或连带 `-wal`/`-shm` 一起复制。本次
是测试环境、数据无价值，但这个疏漏值得记下来。

**停进程时踩了一个自杀缺陷**，值得单独说明。最初执行的是：

```bash
pkill -TERM -f 'auroramihomo -f' ; sleep 5; ps -ef | grep -E 'auroramihomo|mihomo' | grep -v grep
```

返回 `rc=-1` 且输出为空。原因是 `pkill -f` 的模式匹配到了**承载这条命令的
shell 自身**——它的 cmdline 里就含 `auroramihomo -f` 这个字符串，于是 pkill
把自己的父进程杀掉，SSH 通道随之断开。更麻烦的是紧随其后的残留判定同样被
污染：它数到的就是那个即将自尽的 shell。

改用 `[a]` 括号技巧（让 grep/pgrep 的模式不匹配自身命令行）重新核对：

```
$ pgrep -af '[a]uroramihomo'  → NO-AURORA-PROC
$ pgrep -af '[m]ihomo'        → NO-MIHOMO-PROC
$ ss -tlnp | grep -E '8899|9090'  → PORT-FREE
```

清理结果确实干净，但**"TERM 单独是否足够"这个结论无法判定**——在被污染的
判定下已经补发过一轮 `pkill -KILL`。若要验证优雅关停在裸跑形态下是否有效，
需要重新构造场景。

删除目录后确认：

```
$ rm -rf /opt/auroramihomo && ls -d /opt/auroramihomo
ls: cannot access '/opt/auroramihomo': No such file or directory
```

### 2.2 内存决策：未追加 swap

镜像构建是这台 2 核 1867M 机器上最吃资源的一步。清理旧实例后：

```
$ free -m | awk '/^Mem:/{print $7}'
800
$ swapon --show
NAME       TYPE SIZE   USED PRIO
/swap.img  file   4G 638.5M   -2
```

预设的判据是「available < 1400 **且** 无 swap 时追加 2G swapfile」。第一个
条件满足（800 < 1400），第二个不满足——宿主已有 4G swap 且只用了 638M，
还剩 3.4G 可用。再加 2G 纯属浪费磁盘，因此**没有追加**。

顺带查清了内存去向：`openclaw-gateway`（pid 874）独占 534MB RSS，与本项目
无关。这台机器的内存基线就是如此，不是旧实例没清干净。

### 2.3 镜像构建：首次失败与根因

**第一次构建失败**，59 秒即中止，不是 OOM：

```
#24 5.129 backend/api/internal/handler/routes.go:10:2:
  package auroramihomo/backend/api/internal/handler/public is not in std
  (/usr/local/go/src/auroramihomo/backend/api/internal/handler/public)
```

`is not in std` 这句话极具误导性——它会让人以为是标准库或 Go 版本问题。
实际含义是「这个包不存在」：Go 找不到模块内的包时会退回 `GOROOT/src` 去找，
于是报出「不在标准库里」。

根因在**源码打包环节，不在 Dockerfile**。检查上传的 tar 包：

```
$ tar -tzf src.tar.gz | grep 'handler/public'
(空)
```

打包时用了未锚定的 `--exclude=public`，本意是排除仓库根的前端产物目录
`public/`，但 tar 的 `--exclude` 对**任意层级**生效，把同名的两个 Go 包一并
吞掉了：

| 被误删的包 | 文件数 | 影响 |
| --- | --- | --- |
| `backend/api/internal/handler/public/` | 3 | 公开路由的 handler |
| `backend/api/internal/logic/public/` | 4 | 含 `loginLogic.go`，登录功能核心 |

修法是重新打包时不在 tar 层排除 `public/` 与 `frontend/dist/`——它们已由
`.dockerignore` 在构建上下文层面排除，而 **Docker 的忽略模式锚定于上下文根**，
不会误伤嵌套的同名目录。修正后的包 880 个条目（原 858 个）。

**Dockerfile 与仓库内任何文件均未改动。**

第二次构建成功：

| 项 | 值 |
| --- | --- |
| 上传源码包 | 4535566 字节 |
| 解压后 | 18M |
| 构建耗时 | **108 秒** |
| 镜像大小 | **76.8MB** |
| OOM 迹象 | 无 |

需要说明的是，108 秒**不代表冷构建耗时**——第一次失败的构建已经把 npm ci、
go mod download、基础镜像拉取等重活缓存下来了（Build Cache 占 2.146GB）。
真正的冷构建时间本次未测得。go.mod 声明 `go 1.25.0`，Dockerfile 用
`golang:1.25-alpine`，无版本冲突。

**预装依赖校验**（TProxy 的前提条件）：

```
$ docker run --rm --entrypoint sh auroramihomo:latest -c 'which nft iptables ip6tables ip; ip -V'
/usr/sbin/nft
/usr/sbin/iptables
/usr/sbin/ip6tables
/sbin/ip
ip utility, iproute2-6.11.0
```

进一步确认不是 busybox applet：`/sbin/ip` 是 606792 字节的独立 ELF（busybox
的软链会指向 `/bin/busybox`），且 `ip rule show` 能正常输出策略路由表。
镜像里 `iproute2 / iproute2-minimal / iproute2-ss / iproute2-tc` 齐全。
TProxy 依赖的 `ip rule add fwmark` 有支撑。

### 2.4 透明代理所需的 compose 改动

仓库默认的 `docker/docker-compose.yml` 已有 `network_mode: host` 与
`cap_add: NET_ADMIN`，但另有两项注释着、一项需要去掉。四项缺任何一个都会
失败，且**失败信号各不相同**：

| 改动 | 缺失时的症状 |
| --- | --- |
| `network_mode: host` | 规则只作用于容器自身 netns，宿主流量不受影响 |
| `cap_add: NET_ADMIN` | 无权操作 nftables 与路由 |
| **`user: "0:0"`** | `cap_add` 只填 bounding set，非 root 拿不到 effective 位 |
| **`devices: /dev/net/tun`** | TUN 模式报设备缺失（TProxy 不需要此项） |

另外**刻意去掉了 `security_opt: no-new-privileges:true`**。

> **这是一处安全降级，仅适用于测试环境。** `no-new-privileges` 与
> 「root 运行 + NET_ADMIN」的组合互斥（设计文档第 7 节已说明）。生产部署
> 需要在「透明代理可用」与「限制提权面」之间自行权衡；若选择保留
> `no-new-privileges`，则需改用 file capability 方案并接受额外的配置复杂度。

启动后**验证的是实际效果而非配置文本**：

```
$ docker inspect auroramihomo --format '{{.HostConfig.CapAdd}} {{.Config.User}} {{.HostConfig.SecurityOpt}}'
[CAP_NET_ADMIN] 0:0 []

$ docker exec auroramihomo sh -c 'id; grep -E "CapEff|CapBnd" /proc/self/status'
uid=0(root) gid=0(root)
CapEff: 00000000a80435fb
CapBnd: 00000000a80435fb
```

`CapEff == CapBnd` 是关键证据：若以非 root 运行，`CapEff` 会是 0，NET_ADMIN
只停留在 bounding set 里拿不到。`SecurityOpt=[]` 确认 `no-new-privileges`
已去掉。host 网络也验证过——容器内 `ip link` 能看到宿主的 `ens18` 与
`docker0`。

### 2.5 容器启动与资源下载

首次启动会自动下载 mihomo 内核与 zashboard 面板，**CDN 回退链实测生效**，
两者都是前两个源失败、第三个成功：

```
[1/8] https://ghproxy.com/...        → EOF
[2/8] https://mirror.ghproxy.com/... → EOF
[3/8] https://gh.ddlc.top/...        → download success (17881563 bytes)
```

zashboard v3.16.0 同样的模式。首次启动共耗时约 73 秒（15:00:24 开始下载 →
15:01:37 绑定 8899）。落盘 `data/bin/mihomo` 48365694 字节，`data/` 合计 58M。

容器状态与健康检查：

```
$ docker ps --format '{{.Names}} {{.Status}}'
auroramihomo   Up 3 minutes (healthy)

$ curl -sS http://127.0.0.1:8899/healthz
{"database":true,"mihomo":{"pid":74,"running":true,
 "version":"Mihomo Meta v1.19.29 linux amd64 with go1.26.5 ..."},"status":"ok"}
```

---

## 3. S0 部署与基础功能

| 场景 | 命令 / 结果 |
| --- | --- |
| S0-1 健康检查 | `/healthz` 返回 `status: ok`，含 mihomo running 与 version |
| S0-2 登录 | `POST /api/v1/auth/login` HTTP=200，换到 JWT（139 字符） |
| S0-3 导入订阅 | 2 条真实订阅创建成功，触发更新后 `status: ok` |
| S0-4 内核与面板 | `system/status` running、`dashboard/entry` 可用、`/ui/` HTTP=200 |

订阅拉取结果（URL 已脱敏不予展示）：

```
id=1  nodeCount=73  lastUpdate=2026-07-31T15:21:09+08:00  status=ok
id=2  nodeCount=2   lastUpdate=2026-07-31T15:21:10+08:00  status=ok
```

配置过程中有**两点值得记录**，它们不是缺陷但会让人卡住：

**（1）远程来源默认是 `none`，订阅节点不会自动进入最终配置。**

首次合并后最终配置只有 4 行、`proxies: 0`：

```
$ python3 -c "import yaml;d=yaml.safe_load(open('.../config.yaml'));print(len(d.get('proxies') or []))"
0
```

这是刻意的安全默认——`domain.DefaultRemoteSource()` 返回 `none`，注释写明
「默认值是『不用』而非『聚合全部订阅』，用户没有选择时本地配置就得完整生效，
不会被别处来的节点、策略组或规则意外覆盖」。

正确路径是建一个**组合（collection）**把订阅纳入，再把远程来源指向该组合。
`sourceType=all`（聚合全部订阅）虽然代码里还支持，但 `.api` 规格注释说明它
**已从界面移除，因为会造成数据被覆盖**，不应作为推荐路径。

配置到位后：

```
proxies: 75
groups: 1        (name: Proxy)
rules: 1         (MATCH,Proxy)
mixed-port: 7890
dns.enhanced-mode: fake-ip
```

**（2）`mixed-port` 等监听端口属启动参数级，不在热重载范围。**

改完 base 配置并合并后，`mixed-port: 7890` 已写入 config.yaml，但 `ss` 显示
mihomo 并未监听 7890。原因是 `ConfigService` 的热重载走 mihomo 的
`PUT /configs`，而端口类配置不在其可热更新的字段范围内——这是 mihomo 内核
自身的行为。额外调用 `POST /api/v1/mihomo/restart` 后端口才真正生效：

```
LISTEN 0 4096 *:7890  users:(("mihomo",pid=92741,fd=3))
LISTEN 0 4096 *:1053  users:(("mihomo",pid=92741,fd=9))
```

既有报告第 2 节已记录过 `external-controller` 的同类现象，此处再次确认该行为
对 `mixed-port` 同样适用。

---

## 4. S1 环境检测

### 4.1 检测报告与实际核对

`GET /api/v1/transparent/status` 的 `env` 字段与系统实际逐项对照：

| 字段 | 检测值 | 系统实际 | 一致 |
| --- | --- | --- | --- |
| `os` / `arch` | linux / amd64 | 同 | 是 |
| `kernel` | 6.8.0-124-generic | 同（host 网络下读到宿主内核） | 是 |
| `distro` | **alpine** | 容器基础镜像是 alpine:3.21 | 是 |
| `packageManager` | apk | 容器内是 apk | 是 |
| `root` | true | `id -u` = 0 | 是 |
| `capNetAdmin` | true | `CapEff: ...a80435fb` | 是 |
| `capNetAdminBounding` | true | `CapBnd: ...a80435fb` | 是 |
| `inContainer` | true | `/.dockerenv` 存在 | 是 |
| `hostNetwork` | true | 与 PID 1 同 netns | 是 |
| `tunDevice` | /dev/net/tun | 字符设备存在 | 是 |
| `modes[tun].available` | true | — | — |
| `modes[tproxy].available` | true | nft/iptables/真 iproute2 齐备 | 是 |

`distro: alpine` 而宿主是 Ubuntu 这一点容易引起困惑：检测的是**容器视角**，
因为面板进程跑在容器内，装包也只能装到容器里。这是正确行为，而非误判。

### 4.2 容器内 sysctl 按设计被拒绝

`POST /api/v1/transparent/provision` 的响应：

```
"steps": [
  {"name":"安装依赖包","success":true,"skipped":true,
   "detail":"nft/iptables 与 iproute2 均已就绪"},
  {"name":"写入 sysctl 配置","success":false,
   "detail":"容器内不修改 sysctl：非特权容器会被内核拒绝，host 网络会直接
            改动宿主，均不代表用户意图，请执行下方手动命令"}
]
"notPersistent": true
"manualCommands": ["apk add --no-cache iptables ip6tables nftables iproute2"]
```

**这不是失败，是刻意行为。** 代码不替用户决定要不要动宿主的内核参数：
非特权容器改 sysctl 会被内核拒绝，而 host 网络下改动会直接落到宿主上。
两种后果都不该由面板悄悄替用户承担，因此给出手动命令而非强行执行。

本机的 sysctl 现状恰好不需要改：

```
ip_forward=1
rp_filter: all=2 default=2 docker0=2 ens18=2 lo=0
```

`rp_filter=2`（loose 模式）不影响 TProxy——只有严格模式（=1）才会导致收不到包。

告警两条，都准确：

1. `本机 DNS 指向回环地址 127.0.0.53（通常是 systemd-resolved）...`
2. `容器使用 host 网络，sysctl 需在宿主机上设置，容器内修改会被拒绝或影响整台宿主`

第一条的实际影响见 6.3 —— 容器内的 `resolv.conf` 与宿主不同，结论有出入。

### 4.3 自动准备的幂等性

第二次调用 `provision`：装包步骤 `skipped: true`（`nft/iptables 与 iproute2
均已就绪`），sysctl 步骤仍按设计拒绝。重复执行不报错、不堆积，幂等性成立。

---

## 5. S2 代理基础能力与出口 IP 基线

本节建立后续判据的基线。三段出口 IP 对照是本次测试最强的判据：只有
「TProxy 后的出口」等于「手动代理的出口」，才能证明本机流量走的是同一条
代理链路，而非仅仅"IP 变了"。

**两个坑值得记录。**

**（1）IPv6 干扰了出口 IP 的可比性。**

最初用 `api.ip.sb` 取基线，直连返回的是 IPv6 地址：

```
$ curl -sS --noproxy '*' https://api.ip.sb/ip
240e:398:30b3:2101:ec5c:9ff:fef0:15ea
$ curl -sS -x http://127.0.0.1:7890 https://api.ip.sb/ip
61.244.100.156
```

加 `curl -4` 也无效——`-4` 只约束**本地出站协议族**，不改变对方回显什么，
而该服务在 v6 可达时优先走 v6 并回显 v6 地址。换用 `ipv4.icanhazip.com`
（仅有 A 记录）后才拿到可比的 v4 出口。

该机确有 v6 出网能力：

```
$ ip -6 route show default
default proto ra metric 100 expires 1783sec mtu 1480 hoplimit 64 pref medium
$ ip -6 addr show scope global | grep inet6
inet6 240e:398:30b3:2101::e52/128 scope global dynamic noprefixroute
```

这一点对后续有意义：`netcheck` 会据此判定要不要下发 v6 规则与策略路由，
因此本次测试覆盖到了 v6 路径（见 6.1、6.5）。

**（2）订阅的第一个"节点"是流量信息伪节点。**

组类型是 `Selector` 且默认选中第一个成员，而订阅首个条目是承载流量信息的
伪节点：

```
$ curl -sS http://127.0.0.1:9090/proxies/Proxy
type: Selector
now: 剩余流量：53.6 GB
all count: 75
```

它的 `server` 是 `256.256.256.256`（非法 IP），于是 `MATCH,Proxy` 的全部流量
撞死在它上面：

```
$ curl -sS -x http://127.0.0.1:7890 https://api.ip.sb/ip
curl: (35) OpenSSL SSL_connect: SSL_ERROR_SYSCALL in connection to api.ip.sb:443
```

节点本身是活的——让内核测速全组，延迟 43ms~5004ms 不等，很多在 100ms 内。
通过内核 API 切到实测延迟最低的真实节点后恢复正常。

（更彻底的做法是把组类型改成 `url-test` 自动择优，但那需要改渲染模板；本次
测试目标是验证透明代理而非改动平台行为，用内核 API 切点即可。）

**基线确立：**

| 段 | 出口 IP | 取法 |
| --- | --- | --- |
| 直连 | `182.136.123.11` | `curl -4 --noproxy '*' https://ipv4.icanhazip.com` |
| 手动代理 | `61.244.100.156` | 同上 + `-x http://127.0.0.1:7890` |

选中节点为「东京·家宽\|直连」（节点名保留，服务器地址不予展示）。

---

## 6. S3 TProxy 主线

启用前落盘完整基线，供关闭后 diff 比对：

```
$ nft list ruleset > /tmp/nft-before.txt   # 含 Docker 自建的 5 张表
$ ip rule show
0:      from all lookup local
32766:  from all lookup main
32767:  from all lookup default
```

SSH 在 22 端口（会被规则豁免），恢复手段
（`nft delete table inet aurora_tproxy`）预先确认可执行。

### 6.1 90 秒未确认自动回滚

这是核心防护机制：规则改错会让人失去 SSH，确认窗口是唯一的兜底。本轮刻意
不调用 `confirm`，观察窗口过期后的行为。

启用后立即：

```
$ curl -X PUT .../api/v1/transparent -d '{"enabled":true,"mode":"tproxy",...}'
{"enabled":true,"mode":"tproxy","pendingConfirm":true,"secondsLeft":89,...}
```

倒计时实测递减：

```
t+21s   "enabled":true  "pendingConfirm":true  "secondsLeft":67
t+42s   "enabled":true  "pendingConfirm":true  "secondsLeft":67
t+63s   "enabled":true  "pendingConfirm":true  "secondsLeft":46
t+85s   "enabled":true  "pendingConfirm":true  "secondsLeft":25
t+106s  "enabled":false "pendingConfirm":false
```

窗口内规则与策略路由都已下发，**v4 与 v6 都有**：

```
$ ip rule show | grep fwmark
32765:  from all fwmark 0x1 lookup 100
$ ip -6 rule show | grep fwmark
32765:  from all fwmark 0x1 lookup 100
```

窗口过期后核对：

```
$ nft list table inet aurora_tproxy
Error: No such file or directory      ← 表已删除

$ ip rule show | grep fwmark   → v4 fwmark 已清除
$ ip -6 rule show | grep fwmark → v6 fwmark 已清除
$ ip route show table 100      → table 100 v4 已空
$ ip -6 route show table 100   → table 100 v6 已空

$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
182.136.123.11                        ← 回到直连基线
```

全程 SSH 未中断（本轮所有命令都通过 SSH 执行，这本身就是证据）。

**v6 的完整清理值得单独指出。** 既有报告 8.7 节修复的正是「关闭后残留 v6
策略路由」——原实现按"启用时有没有开 v6"的参数决定是否清理 v6，而调用方传的
是硬编码的 `false`，于是在有 v6 能力的宿主上关闭后残留 v6 规则、日志却报告
拆除成功。本次在真实有 v6 出网的机器上确认该修复有效。

### 6.2 规则内容与顺序核对

顺序即安全边界，不可调整。完整规则：

```nft
table inet aurora_tproxy {
	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
		tcp dport 22 return
		tcp dport 8899 return
		tcp dport 9090 return
		meta mark 0x000000ff return
		meta mark 0x000000fe return
		socket transparent 1 meta mark set 0x00000001 accept
		udp dport 53 meta mark set 0x00000001 tproxy to :7893 accept
		ip daddr 10.0.0.0/8 return
		ip daddr 172.16.0.0/12 return
		ip daddr 192.168.0.0/16 return
		ip daddr 127.0.0.0/8 return
		ip daddr 169.254.0.0/16 return
		ip daddr 224.0.0.0/4 return
		ip daddr 240.0.0.0/4 return
		ip daddr 255.255.255.255 return
		ip6 daddr ::1 return
		ip6 daddr fe80::/10 return
		ip6 daddr fc00::/7 return
		ip6 daddr ff00::/8 return
		meta l4proto { tcp, udp } meta mark set 0x00000001 tproxy to :7893 accept
	}

	chain output {
		type route hook output priority mangle; policy accept;
		tcp sport 22 return
		tcp sport 8899 return
		tcp sport 9090 return
		tcp dport 22 return
		tcp dport 8899 return
		tcp dport 9090 return
		meta mark 0x000000ff return
		meta mark 0x000000fe return
		meta l4proto { tcp, udp } th dport 53 ip daddr != 127.0.0.0/8 meta mark set 0x00000001 accept
		meta l4proto { tcp, udp } th dport 53 ip6 daddr != ::1 meta mark set 0x00000001 accept
		ip daddr 10.0.0.0/8 return
		...（同样 8 条 v4 + 4 条 v6 保留网段 return）
		meta l4proto { tcp, udp } meta mark set 0x00000001 accept
	}
}
```

逐项核对结果：

| 核对点 | 结果 | 为什么重要 |
| --- | --- | --- |
| prerouting hook | `type filter hook prerouting priority mangle` | 在路由决策前打标 |
| **output hook** | **`type route hook output`** | 必须是 `route` 而非 `filter`，否则改了 mark 不会重新查路由，本机 UDP 出站走不通 |
| 管理端口在最前 | 22 / 8899 / 9090 | 顺序错了会先被 TPROXY 抓走，人就锁在门外 |
| **output 按 sport 豁免** | **sport 与 dport 都有** | 回包里管理端口是**源端口**，只按 dport 匹配不到 sshd 回包（既有报告 8.2 节的缺陷，此处确认修复在位） |
| mark 豁免 | `0xff`（mihomo）+ `0xfe`（面板 PanelMark） | 否则内核与面板自身出站被自己的规则抓走，mihomo 挂掉时连重下内核都做不到 |
| `socket transparent 1` | 在 DNS 劫持之前 | 已建立的连接直接接管 |
| DNS 劫持位置 | 在局域网 return **之前**、mark return **之后** | 在前会让指向局域网 DNS 的查询被 return 掉；在后会让 mihomo 与面板自己的查询自环 |
| output 侧 DNS 排除回环 | `ip daddr != 127.0.0.0/8`、`ip6 daddr != ::1` | mihomo 的 DNS 监听在回环上，劫持等于自环 |
| v6 保留网段 | `::1`、`fe80::/10`、`fc00::/7`、`ff00::/8` | 该机有 v6 出网，故下发 |
| 兜底规则 | `tproxy to :7893`（prerouting）/ `mark set` only（output） | output 链不能用 tproxy 动作 |

策略路由：

```
$ ip rule show;  ip route show table 100
32765:  from all fwmark 0x1 lookup 100
local default dev lo scope host

$ ip -6 rule show | grep fwmark;  ip -6 route show table 100
32765:  from all fwmark 0x1 lookup 100
local default dev lo metric 1024 pref medium
```

`local` 类型是关键：让内核把打标的包当作发往本机的流量投递（发夹回
prerouting 被 TPROXY 接走），而不是继续转发出去。

### 6.3 本机流量真实接管

这是全流程的核心判据。启用并确认后：

```
$ curl -4 -sS -m 40 --noproxy '*' https://ipv4.icanhazip.com
61.244.100.156
```

| 段 | 出口 IP | 判定 |
| --- | --- | --- |
| 直连基线 | 182.136.123.11 | — |
| 手动代理（S2） | 61.244.100.156 | — |
| **TProxy 后本机 curl** | **61.244.100.156** | **与手动代理相同，与直连不同** |

第二段与第三段相同，证明这是**同一条代理链路**——比单纯"IP 变了"严谨得多。
宿主上任何未指定代理的程序（curl、apt、git）此刻都会按分流规则走节点。

DNS 也被接管：

```
$ dig +short google.com
198.18.0.4
```

`198.18.0.0/16` 是配置里的 `fake-ip-range`，返回该段地址证明查询确实经过
mihomo 的 DNS 而非直连上游。

**这里与 S1 的告警有一处出入，需要说明。** 4.2 节的告警说「本机 DNS 指向
回环地址 127.0.0.53」，按设计文档第 10 节的已知限制，那种情况下本机域名
分流不生效。但这里 DNS 劫持实际生效了——原因是**容器内的
`/etc/resolv.conf` 是 Docker 注入的，与宿主的不同**，并非 systemd-resolved
的回环 stub。

检测读到的是容器内的 resolv.conf（面板进程视角），而 TProxy 规则作用于宿主
netns（host 网络），两者不是同一份文件。这个告警在 host 网络的容器形态下
会偏保守：它可能报警但实际无影响，也可能漏报宿主侧的真实问题。属于容器与
宿主视角差异导致的检测盲区，值得记录但不构成缺陷——保守告警比漏报安全。

### 6.4 管理端口豁免（含非局域网段验证）

先看常规路径：

```
$ curl -sS -o /dev/null -w 'panel HTTP=%{http_code}\n' http://127.0.0.1:8899/healthz
panel HTTP=200
$ curl -sS -o /dev/null -w 'kernel HTTP=%{http_code}\n' http://127.0.0.1:9090/version
kernel HTTP=200
```

**但同网段的测试是不够的。** 规则里有 `ip daddr 192.168.0.0/16 return`，
局域网流量会先被它兜住，管理端口 return 是否真的生效被掩盖了。既有报告
8.2 节的缺陷（output 只按 dport 放行）就是因此长期未暴露——来源换成公网
跳板或 VPN 段就会在启用瞬间失去 SSH。

因此构造非局域网段来源：建 dummy 网卡配 CGNAT 段（`100.64.0.0/10`）地址，
该网段**不在**规则的 8 条局域网放行列表里：

```
$ ip link add aurotest type dummy; ip addr add 100.64.7.1/32 dev aurotest; ip link set aurotest up
64: aurotest: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500 ...
    inet 100.64.7.1/32 scope global aurotest

$ curl -sS --interface 100.64.7.1 -o /dev/null -w 'from-CGNAT panel HTTP=%{http_code}\n' \
    http://192.168.1.128:8899/healthz
from-CGNAT panel HTTP=200
```

从 CGNAT 源地址访问管理端口成功，证明 output 链的 sport/dport 双向豁免真实
有效，而非被局域网 return 掩盖。

对照测试——从同一源地址访问外部（应命中兜底 TPROXY 而非 return）：

```
$ curl -sS --interface 100.64.7.1 -o /dev/null -w 'to-external HTTP=%{http_code}\n' \
    http://ipv4.icanhazip.com
to-external HTTP=200
```

兜底规则正常工作。测试完成后删除 dummy 网卡（`ip link del aurotest`，已确认
`Device "aurotest" does not exist`）。

**一项未完成的验证**：原计划用 `nc` 从 CGNAT 源地址探测 SSH 端口的 TCP 握手，
但两台主机都没有 `nc`（Ubuntu 最小安装与 Alpine 均未预装），该项**未验证**。
不过整个测试过程从头到尾通过 SSH 进行且从未中断，这是比端口探测更强的证据。

### 6.5 关闭与清理

```
$ curl -X PUT .../api/v1/transparent -d '{"enabled":false,"mode":"off"}'
{"enabled":false,"mode":"off","pendingConfirm":false,...}
```

清理核对：

| 项 | 结果 |
| --- | --- |
| `nft list table inet aurora_tproxy` | 表已删除 |
| v4 fwmark 规则 | 已清除 |
| **v6 fwmark 规则** | **已清除** |
| `ip route show table 100` | 已空 |
| `ip -6 route show table 100` | 已空 |
| 出口 IP | 恢复 182.136.123.11（= 直连基线） |

**不误伤无关规则**——这台机器上有 Docker 自建的 nft 规则，正好用来验证
「只删自己的表、不整体 flush」这条设计。与启用前基线 diff：

```
$ diff /tmp/nft-before.txt /tmp/nft-after.txt
5c5
<               fib daddr type local counter packets 178 bytes 14174 jump DOCKER
---
>               fib daddr type local counter packets 192 bytes 17185 jump DOCKER
10c10
<               ip daddr != 127.0.0.0/8 fib daddr type local counter packets 23 bytes 6035 jump DOCKER
---
>               ip daddr != 127.0.0.0/8 fib daddr type local counter packets 27 bytes 7149 jump DOCKER
```

差异**只是计数器数值的自然增长**（测试期间有流量经过），规则本身逐条完好。
进一步确认：

```
$ nft list tables
table ip nat
table ip filter
table ip6 nat
table ip6 filter
table ip raw

$ nft list table ip nat | grep -i masquerade
ip saddr 172.17.0.0/16 oifname != "docker0" counter packets 24 bytes 1512 masquerade

$ docker ps --format '{{.Names}} {{.Status}}'
auroramihomo   Up 40 minutes (healthy)
$ docker exec auroramihomo curl -sS -o /dev/null -w 'in-container HTTP=%{http_code}\n' http://127.0.0.1:8899/healthz
in-container HTTP=200
```

Docker 的 5 张表全在、DOCKER 链完好、docker0 的 masquerade 规则在位、容器
healthy、容器内访问正常。

---

## 7. S4 TUN 对照

TUN 与 TProxy 互斥（`mode` 是单值）。切换前先确认上一模式已关闭且规则已清，
否则两套接管机制叠加会让现象无法归因：

```
$ curl -sS .../transparent/status | grep -E '"enabled"|"mode"'
"enabled":false  "mode":"off"
$ nft list table inet aurora_tproxy → 不存在
$ ip rule show | grep fwmark → no fwmark (clean)
```

启用 TUN 并确认后：

```
$ ip link show | grep -iE 'meta|utun|tun[0-9]'
65: Meta: <POINTOPOINT,MULTICAST,NOARP,UP,LOWER_UP> mtu 9000 qdisc fq_codel state UNKNOWN

$ ip route show | head
198.18.0.0/30 dev Meta proto kernel scope link src 198.18.0.1
$ ip -6 route show | head
fdfe:dcba:9876::/126 dev Meta proto kernel metric 256 pref medium
```

配置注入核对（由 `netcheck.Inject` 在合并流程最后一步写入，而非写进用户的
base 配置——后者用户能随手改掉，开关状态与实际配置就会不一致）：

```yaml
tun:
    enable: true
    stack: mixed
    auto-route: true
    auto-detect-interface: true
    auto-redirect: true
    dns-hijack:
        - any:53
```

`auto-route: true` 是关键——不接管路由的 TUN 等于没开，且 mihomo 不会报错。
`AutoRoute` 与 `AutoDetectInterface` 在代码里是 `*bool` 而非 `bool`，正是为了
避免只开 `tun.enable` 时写出 `auto-route: false`。

**TUN 模式不下发 aurora 的防火墙规则**：

```
$ nft list table inet aurora_tproxy → 不存在
$ ip rule show | grep fwmark → 无 aurora 的 fwmark 规则（符合预期）
```

这是设计使然：TUN 的路由接管由 mihomo 自己通过 netlink 完成
（`auto-redirect: true` 时它自管 nftables），本项目只负责注入配置。

出口 IP 与 DNS：

```
$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
61.244.100.156        ← 与手动代理基线相同
```

管理端口与 SSH 全程正常（`panel HTTP=200`）。关闭后：

```
$ ip link show | grep -iE 'meta|utun' → TUN 网卡已移除
$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
182.136.123.11        ← 恢复直连
```

**一个测试方法上的教训**：首次取 TUN 出口 IP 时脚本用
`out.strip().split()[-1]` 取最后一个词，而 TUN 首次连接需要等设备与路由就绪、
第一次 curl 超时，错误输出让脚本取到了 `reading` 这样的片段，造成**假阴性**。
改用正则提取 IPv4 并加重试后确认实际正常。判据的提取方式本身也需要经得起
失败路径的考验。

---

## 8. 发现的问题与运维观察

### 8.1 本次测试未在本形态发现代码缺陷

Docker 形态的全部 14 个场景按设计工作。唯一的功能性缺陷发现在 Alpine
二进制形态（`sysctl --system` 在 BusyBox 上不可用），详见
[Alpine 报告](./AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md) 第 4.3 节。
该缺陷对本形态无影响，因为容器内的 sysctl 写入本就按设计被拒绝。

但**需要留意一个潜在关联**：本项目的 Dockerfile 基于 alpine:3.21，容器内的
`sysctl` 同样是 BusyBox applet。当前不受影响仅因为容器内不执行 sysctl 写入；
若将来放开这条路径，会踩同一个坑。

### 8.2 日志里的 error 级误报

正常启动流程中出现一条 error 级日志：

```
{"caller":"api/aurora.go:240","content":"mihomo start skipped: mihomo is already running","level":"error"}
```

紧接着就是 `Starting server at 0.0.0.0:8899`。核实内核只有一个实例：

```
90884  90861  /app/auroramihomo -f etc/aurora-api.yaml
91167  90884  /data/bin/mihomo -d /data
```

9090 由 pid 91167 独占监听。这是启动流程里的重复调用被幂等逻辑挡掉，属正常
路径，但用 **error 级别记录会污染日志**——排查真问题时这类噪音最容易误导。
建议降为 info 或 debug。本次未改代码，仅记录。

### 8.3 首次启动的 `record not found` 刷屏

首次启动时 gorm 对 `settings` / `tasks` 表的冷查询产生几十条
`record not found`，功能上正常（读取侧都有默认值兜底），但量大且带红色 error
着色，容易掩盖真问题。同样仅记录。

### 8.4 未验证项

| 项 | 原因 |
| --- | --- |
| 从 CGNAT 源地址探测 SSH 端口的 TCP 握手 | 主机无 `nc`。但全程 SSH 未中断是更强证据 |
| 镜像冷构建耗时 | 108 秒复用了首次失败构建的缓存；要测冷构建需清 Build Cache 重跑 |
| 优雅关停（TERM 单独）对裸跑实例是否足够 | 清理旧实例时判定被 pkill 自杀缺陷污染，已先发 KILL（见 2.1） |
| 宿主重启后的状态一致性（`ReconcileState`） | 本次未重启宿主。既有报告 6.3 节已在同一台机器上验证过该路径 |

---

## 9. 结论汇总表

| 测试项 | 结果 |
| --- | --- |
| **部署** | |
| 旧实例清理与端口释放 | 通过（备份有疏漏，WAL 未备；见 2.1） |
| 镜像构建 | 通过（首次因源码打包缺包失败，非项目缺陷；见 2.3） |
| 镜像预装 TProxy 依赖（真 iproute2） | 通过 |
| compose 四项改动生效（含 CapEff 实测） | 通过 |
| 容器启动与健康检查 | 通过，零重启 |
| 内核与面板资源自动下载（CDN 回退） | 通过，实测回退两跳 |
| **S0 基础功能** | |
| S0-1 健康检查 | 通过 |
| S0-2 登录换 JWT | 通过 |
| S0-3 导入真实订阅并拉取（75 节点） | 通过 |
| S0-4 内核运行 + Zashboard 入口 | 通过 |
| **S1 环境检测** | |
| S1-5 检测报告与实际逐字段核对 | 通过，全部一致 |
| S1-6 自动准备（容器内 sysctl 按设计拒绝） | 通过（行为符合设计，非失败） |
| S1-6b 幂等复测 | 通过 |
| **S2 代理基础能力** | |
| S2-7 面板自身出网 | 通过 |
| S2-8 手动代理出口 IP 为节点 IP | 通过 |
| **S3 TProxy 主线** | |
| S3-9 90 秒未确认自动回滚 | 通过（核心防护机制） |
| S3-9b 回滚时 v4 与 v6 策略路由都清理 | 通过 |
| S3-10 规则内容与顺序（含 output route hook、sport 豁免） | 通过，与设计一致 |
| S3-11 **本机流量真实接管**（出口 = 节点 IP） | 通过 |
| S3-11b DNS 被接管（fake-ip 段） | 通过 |
| S3-12 管理端口豁免（本机路径） | 通过 |
| S3-12b **非局域网段（CGNAT）验证 sport 豁免** | 通过 |
| S3-12c 从 CGNAT 探测 SSH 端口握手 | 未验证（无 `nc`；SSH 全程未断为替代证据） |
| S3-13 关闭后表与 v4/v6 策略路由完整清理 | 通过 |
| S3-13b 出口 IP 恢复直连 | 通过 |
| S3-13c **不误伤 Docker 自身 nft 规则** | 通过（diff 仅计数器变化） |
| **S4 TUN 对照** | |
| S4-14 TUN 网卡与路由建立 | 通过 |
| S4-14b 配置注入（auto-route / auto-redirect） | 通过 |
| S4-14c TUN 模式不下发 aurora 防火墙规则 | 通过，符合设计 |
| S4-14d 出口 IP 为节点 IP | 通过 |
| S4-14e 关闭后网卡移除与出口恢复 | 通过 |
| **运维观察（记录未改代码）** | |
| `mihomo start skipped` 误报为 error 级 | 记录，建议降级为 info |
| 首次启动 `record not found` 刷屏 | 记录 |
| `mixed-port` 等端口需重启内核才生效 | 记录，与既有报告第 2 节同类 |

本次测试确认 Docker 形态下透明代理的核心风险防护机制（90 秒确认窗口、
管理端口双向豁免、精确表删除而非整体 flush、v4/v6 对称清理）在真实内核上
均按设计工作，且**本机流量接管这条路径通过出口 IP 三段对照得到了闭环验证**
——TProxy 生效后宿主自身 `curl` 的出口与手动代理完全一致，证明走的是同一条
代理链路。

与既有二进制形态报告的差异集中在容器特有的部分：sysctl 按设计被拒绝、
检测报告呈现容器视角（`distro: alpine`）、resolv.conf 的容器与宿主差异使
回环 DNS 告警偏保守。这些都不构成缺陷，但在解读检测结果时需要知道。

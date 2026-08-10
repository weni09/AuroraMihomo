# AuroraMihomo 透明代理测试报告（Alpine 3.24 / 二进制形态）

本文记录在 **Alpine Linux** 上以**二进制形态**部署 AuroraMihomo 并验证透明代理
的全过程。这是本项目**首次在 Alpine 平台上的验证**——此前
[既有报告](./AuroraMihomo-Transparent-Proxy-Test-Report.md) 只覆盖了 Ubuntu
（二进制与 Docker 两种形态）。

Alpine 用 musl 而非 glibc、用 BusyBox 提供大部分基础命令、用 OpenRC 而非
systemd。这些差异不是理论上的：本次测试因此发现了一个真实缺陷（见 4.3），
其根因与既有报告第 4 节修复的 `ip --version` 误判同源。

设计依据见 [AuroraMihomo-Transparent-Proxy.md](./AuroraMihomo-Transparent-Proxy.md)。
同批次的 Ubuntu / Docker 形态报告见
[AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md](./AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md)。

---

## 1. 测试环境

| 项 | 值 |
| --- | --- |
| 主机 | Proxmox 虚拟机，**1 vCPU / 972M 内存** |
| 系统 | **Alpine Linux 3.24.1**，内核 6.18.35-0-virt |
| 磁盘 | `/dev/sda` ext4 **直接挂 `/`（无分区表）**，原 114.5M，测试前扩容到 1.0G |
| 网络 | 局域网 192.168.1.129/24，网卡 `eth0`，**有 IPv6 出网** |
| 部署形态 | 二进制（本地交叉编译 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`） |
| 服务管理 | **OpenRC + supervise-daemon**（Alpine 无 systemd） |
| apk 源 | **已换阿里云** `mirrors.aliyun.com/alpine/v3.24/{main,community}` |
| apk-tools | 3.0.6-r0 |
| 二进制 | 31150242 字节，纯静态 ELF（无 INTERP 段） |
| mihomo | v1.19.29，`with_gvisor` |
| 测试节点 | 本地开发库的 2 条真实订阅，共 75 个节点 |

初始状态是全新系统：**无 nft、无 iptables、无 iproute2**（`ip` 是 BusyBox
applet）、`/dev/net/tun` 不存在、`tun` 与 `nft_tproxy` 模块均未加载。

---

## 2. 部署过程

### 2.1 磁盘扩容（前置条件）

根分区原本装不下本项目：

```
$ df -h /
/dev/sda    114.5M   91.2M   14.3M  86% /
```

部署需求约 55M（二进制 29M + 前端 3M + mihomo 内核 15M + 依赖包 5M），
而只剩 14.3M。

扩容方式比常见情况简单，因为 `/dev/sda` 是**整盘直接格式化的 ext4，没有
分区表**：

```
$ blkid
/dev/sda: LABEL="/" UUID="2e2ec842-..." BLOCK_SIZE="1024" TYPE="ext4"
```

因此不需要 `growpart` 或 `fdisk` 调整分区，在 Proxmox 宿主 `qm resize` 之后
直接 `resize2fs /dev/sda` 即可。扩容到 1.0G 后可用 931.8M。

### 2.2 busybox ip 的原始状态（装 iproute2 前的不可重现证据）

这一步必须在装 iproute2 **之前**执行——装完就再也测不到这个状态了。

```
$ which ip; ip -V 2>&1 | head -3
/sbin/ip
BusyBox v1.37.0 (2026-01-10 15:38:28 UTC) multi-call binary.

Usage: ip [OPTIONS] address|route|link|tunnel|neigh|rule [ARGS]

$ ls -l /sbin/ip /bin/busybox
-rwxr-xr-x    1 root root  804616 Jan 10  2026 /bin/busybox
lrwxrwxrwx    1 root root      12 Jun 16 21:21 /sbin/ip -> /bin/busybox

$ which nft iptables ip6tables
(rc=2，三者都不存在)
```

这台机器天然是 BusyBox 的 `ip`，正好是 `netcheck` 检测逻辑的真实正向用例。
既有报告第 4 节修复的是一个**反向**误判：`isRealIproute2()` 原来只试
`ip --version`（长选项），而真 iproute2 不认这个选项，导致标准发行版上的真
iproute2 被误判成 busybox、TProxy 被整体判为不可用。修复后改成先试 `ip -V`。

本次测试提供了另一侧的验证：这台机器**确实是** busybox，修复后的检测逻辑
不应把它误判成真 iproute2。装依赖前面板尚未部署，故系统侧证据先行留存，
面板视角的判定结果见第 4 节（届时 iproute2 已装，两模式均判为可用，
与实际相符）。

### 2.3 apk 源换阿里云

备份原配置：

```
$ cat /etc/apk/repositories.bak
https://dl-cdn.alpinelinux.org/alpine/v3.24/main
https://dl-cdn.alpinelinux.org/alpine/v3.24/community
#https://dl-cdn.alpinelinux.org/alpine/v3.24/testing
```

写入阿里云源后确认生效——**关键是看 `apk update` 输出里的实际 URL**，
而不只是看命令是否成功（可能读了缓存或旧源）：

```
$ apk update
v3.24.1-288-g48d72323daf [https://mirrors.aliyun.com/alpine/v3.24/main]
v3.24.1-286-gc68d2ad57ee [https://mirrors.aliyun.com/alpine/v3.24/community]
OK: 28656 distinct packages available
```

**一个值得记录的兼容性观察**：apk-tools 3.x 引入了
`/etc/apk/repositories.d/` 目录形式，但 3.0.6 **仍然正常读取平文件
`/etc/apk/repositories`**，无需迁移到目录形式。日志确认该目录本身并不存在：

```
$ ls -la /etc/apk/repositories.d/
ls: /etc/apk/repositories.d/: No such file or directory
```

### 2.4 依赖安装与 `/sbin/ip` 的替换

```
$ apk add --no-cache nftables iproute2 ip6tables
( 1/16) Installing libmnl (1.0.5-r2)
( 3/16) Installing libxtables (1.8.13-r0)
( 4/16) Installing iptables (1.8.13-r0)
( 7/16) Installing iproute2-minimal (7.0.0-r0)
( 9/16) Installing iproute2-tc (7.0.0-r0)
(10/16) Installing iproute2-ss (7.0.0-r0)
(11/16) Installing iproute2 (7.0.0-r0)
(15/16) Installing nftables (1.1.6-r1)
(16/16) Installing nftables-openrc (1.1.6-r1)
OK: 81.4 MiB in 95 packages
```

**Alpine 特有点一：`ip6tables` 是独立包。** 其他发行版通常并在 iptables 里，
Alpine 拆开了。漏装会导致 v6 相关操作失败。项目的 Dockerfile 已正确处理
（`apk add ... iptables ip6tables nftables iproute2`），二进制部署需手动装。

**Alpine 特有点二：iproute2 直接覆盖 `/sbin/ip`，因此没有 PATH 优先级问题。**
这一点原本是担心的风险——若 busybox 的 `ip` 仍排在真 iproute2 之前，检测会
继续判为不可用。实测：

```
$ which -a ip
/sbin/ip
$ ip -V
ip utility, iproute2-v7.0.0
$ ls -l /sbin/ip
-rwxr-xr-x    1 root root  614992 Apr 16 13:27 /sbin/ip
```

`/sbin/ip` 从 12 字节的 busybox 软链变成了 614992 字节的独立 ELF，
`which -a` 只有一个候选。风险不存在。

验证真 iproute2 的关键能力（TProxy 的策略路由依赖它）：

```
$ ip rule help
Usage: ip rule { add | del } SELECTOR ACTION
SELECTOR := [ not ] [ from PREFIX ] [ to PREFIX ] [ tos TOS ]
            [ fwmark FWMARK[/MASK] ]
```

`fwmark FWMARK[/MASK]` 在选择器里，`ip rule add fwmark 1 table 100` 可用。
BusyBox 的 `ip rule` 不支持 fwmark，这正是它无法承载 TProxy 的原因。

顺带确认 iptables 后端一致，不存在 legacy/nft 混用风险：

```
$ iptables --version
iptables v1.8.13 (nf_tables)
```

### 2.5 内核模块加载与持久化

```
$ modprobe tun;  modprobe nft_tproxy
tun-rc=0
tproxy-rc=0

$ ls -l /dev/net/tun
crw-rw-rw-    1 root netdev  10, 200 Jul 31 07:16 /dev/net/tun

$ grep -E '^(tun|nft_tproxy)' /proc/modules
nft_tproxy 12288 0 - Live 0xffffffffc0b31000
tun 73728 0 - Live 0xffffffffc0aeb000
```

`/dev/net/tun` 出现且是**字符设备**（权限位以 `c` 开头）——这是 `netcheck`
的 `haveTunDevice()` 的判据，它用 `isCharDevice()` 而非仅检查文件存在，
因为普通文件冒充设备节点会让后续操作在更深的地方失败。

持久化到 `/etc/modules-load.d/auroramihomo.conf`（内容 `tun` 与 `nft_tproxy`
各一行），避免重启后模块缺失。

### 2.6 musl 兼容性验证

Alpine 用 musl libc。动态链接的 Go 二进制在这里会因缺 glibc 而无法运行，
因此 `CGO_ENABLED=0` 的静态编译是必需的。实测：

```
$ ./auroramihomo --help
Usage of ./auroramihomo:
  -f string
        the config file (default "etc/aurora-api.yaml")
（rc=0）

$ ldd /opt/auroramihomo/auroramihomo
/lib/ld-musl-x86_64.so.1: /opt/auroramihomo/auroramihomo: Not a valid dynamic program

$ readelf -l /opt/auroramihomo/auroramihomo | grep -A1 INTERP
（无输出）
```

`ldd` 的报错**是静态链接的预期表现，不是错误**——musl 的动态加载器发现这个
文件没有动态段，因此拒绝处理它。`readelf` 确认无 INTERP 段，是纯静态二进制。
本地交叉编译的产物在 Alpine 上直接可跑，无需任何兼容层。

### 2.7 OpenRC + supervise-daemon 服务

Alpine 无 systemd。服务脚本用 `supervise-daemon` 而非 `start-stop-daemon`，
理由是：面板的 `POST /api/v1/system/restart` 的设计约定是「优雅退出，等进程
管理器拉起」（进程刻意不做 fork 自重启——Windows 没有 fork，监听 socket
也无法继承，自行重建端口会有双实例抢占窗口）。`start-stop-daemon` 不做
重新拉起，会让重启接口变成单向关机。

```bash
#!/sbin/openrc-run
name="auroramihomo"
directory="/opt/auroramihomo"
command="/opt/auroramihomo/auroramihomo"
command_args="-f etc/aurora-api.yaml"
command_user="root:root"
supervisor="supervise-daemon"
pidfile="/run/auroramihomo.pid"
depend() { need net; after firewall; }
```

启动后确认监管链路真实存在：

```
$ rc-service auroramihomo start
 * Starting auroramihomo ... [ ok ]
$ rc-service auroramihomo status
 * status: started

$ ps -ef | grep -E '[a]uroramihomo|[m]ihomo'
25537 root  supervise-daemon auroramihomo --start --chdir /opt/auroramihomo
          --pidfile /run/auroramihomo.pid --respawn-delay 2 --respawn-max 5
          --respawn-period 1800 --user root root ... /opt/auroramihomo/auroramihomo -- -f etc/aurora-api.yaml
25538 root  /opt/auroramihomo/auroramihomo -f etc/aurora-api.yaml
25673 root  data/bin/mihomo -d ./data
```

进程树是 supervise-daemon(25537) → auroramihomo(25538) → mihomo(25673)，
`--respawn-delay 2 --respawn-max 5 --respawn-period 1800` 提供了重启接口
所需的拉起能力。

（注：`/api/v1/system/restart` 的端到端行为本次未单独测试，仅确认了
supervisor 在位这一前提条件。）

### 2.8 补装 curl 与 bind-tools

首次尝试验证 healthz 时：

```
$ curl -sS -m 20 http://127.0.0.1:8899/healthz
sh: curl: not found
```

**Alpine 默认不装 `curl`**（只有 BusyBox 的 `wget`），也没有 `dig`。这不是
项目的依赖，但本次测试的全部 API 调用与 DNS 验证都依赖它们：BusyBox 的
`wget` 不支持 `-x`（代理）与 `--interface`（指定源地址），无法完成 S2 的
手动代理验证与 S3-12 的非局域网段验证。

```
$ apk add --no-cache curl bind-tools
(19/19) Installing curl (8.21.0-r0)
OK: 92.5 MiB in 114 packages
$ curl --version | head -1
curl 8.21.0 (x86_64-alpine-linux-musl) libcurl/8.21.0 OpenSSL/3.5.7 ...
$ dig -v
DiG 9.20.26
```

对实际部署的意义：容器形态的镜像已预装 curl（healthcheck 用它），但二进制
部署到 Alpine 时，若运维需要用 curl 排查，需自行补装。

另有一项影响本次测试脚本的差异：**Alpine 无 `python3`**。日志中几处原本用
`python3 -c "import yaml..."` 核对最终配置的命令无输出，改用 `grep`/`sed`
完成。

---

## 3. S0 部署与基础功能

| 场景 | 结果 |
| --- | --- |
| S0-1 健康检查 | `{"database":true,"mihomo":{"pid":25673,"running":true,...},"status":"ok"}` |
| S0-2 登录 | HTTP=200，换到 JWT |
| S0-3 导入订阅 | 2 条真实订阅，`status: ok`，nodeCount 73 与 2 |
| S0-4 内核与面板 | `system/status` running、`dashboard/entry` 可用、首页 HTTP=200（1788 字节）、`/ui/` HTTP=200 |

首次启动的内核下载同样经历了 CDN 回退：

```
mihomo binary missing, downloading...
downloading mihomo v1.19.29 (mihomo-linux-amd64-compatible-v1.19.29.gz)
trying download source [1/8]: https://ghproxy.com/...
download failed via ghproxy.com: ... read: connection reset by peer
trying download source [2/8]: https://mirror.ghproxy.com/...
（最终由后续源成功）
```

内核落地后 `/opt/auroramihomo/data` 合计约 58M，磁盘可用 824M，余量充足。

配置过程中的两点与 Ubuntu 形态相同，此处不重复展开（详见 Ubuntu 报告第 3 节）：

- **远程来源默认 `none`**，订阅节点不会自动进入最终配置，须建组合并把远程
  来源指向它。配置到位后：`proxies: 75`、`groups: 1 (Proxy)`、
  `rules: 1 (MATCH,Proxy)`、`mixed-port: 7890`、`dns.enhanced-mode: fake-ip`
- **`mixed-port` 属启动参数级**，不在 mihomo 的 `PUT /configs` 热更新范围，
  改完须重启内核。重启后监听就位：

```
LISTEN 0 4096 *:7890  users:(("mihomo",pid=26014,fd=3))
LISTEN 0 4096 *:1053  users:(("mihomo",pid=26014,fd=9))
```

---

## 4. S1 环境检测

### 4.1 检测报告与实际核对

| 字段 | 检测值 | 系统实际 | 一致 |
| --- | --- | --- | --- |
| `os` / `arch` | linux / amd64 | 同 | 是 |
| `kernel` | 6.18.35-0-virt | 同 | 是 |
| `distro` | alpine | `/etc/os-release` ID=alpine | 是 |
| `packageManager` | apk | 同 | 是 |
| `root` | true | `id -u` = 0 | 是 |
| `capNetAdmin` / `capNetAdminBounding` | true / true | root 隐含全部 capability | 是 |
| `inContainer` | **false** | 裸机部署，无 `/.dockerenv` | 是 |
| `hostNetwork` | true | 与 PID 1 同 netns | 是 |
| `tunDevice` | /dev/net/tun | 字符设备（2.5 节已确认） | 是 |
| `modes[tun].available` | true | — | — |
| `modes[tproxy].available` | **true** | nft/iptables/真 iproute2 均已装 | 是 |

`modes[tproxy].available: true` 是 2.2 节留下的对照的答案：装 iproute2 之后
检测正确判为可用。修复后的 `isRealIproute2()` 逻辑在 iproute2 7.0.0 上工作
正常（既有报告在 6.1.0 与 6.11.0 上验证过，本次补上 7.0.0）。

### 4.2 三条告警的准确性

```
"warnings": [
  "net.ipv4.ip_forward 为 0，作为网关/旁路由时需要开启（sysctl -w net.ipv4.ip_forward=1，持久化写 /etc/sysctl.d/）",
  "net.ipv4.conf.all.rp_filter 为 1（严格反向路径校验），会导致 TProxy 收不到包，建议设为 0 或 2（注意内核取 all 与网卡各自值的较大者，逐网卡也需一并调整：eth0）",
  "本机 DNS 指向回环地址 ::1（通常是 systemd-resolved），..."
]
```

三条都准确，逐一核对：

**（1）`ip_forward = 0`** —— 实际值确认：

```
$ cat /proc/sys/net/ipv4/ip_forward
0
```

**（2）`rp_filter = 1`** —— 这条最关键，它会让 TProxy 收不到包：

```
$ for f in /proc/sys/net/ipv4/conf/*/rp_filter; do printf '%s=' "$(basename $(dirname $f))"; cat $f; done
all=1
default=1
eth0=1
lo=1
```

告警特意指出「内核取 all 与网卡各自值的较大者，逐网卡也需一并调整」并列出了
`eth0` —— 这个细节很重要：只改 `all` 而网卡自身残留为 1 时，`max(all, iface)`
仍是 1，改了不生效。

**（3）回环 DNS** —— `/etc/resolv.conf` 由 dhcpcd 生成：

```
# Generated by dhcpcd from eth0.dhcp, eth0.ra
nameserver 192.168.1.251
nameserver 61.139.2.69
nameserver ::1
```

含 `nameserver ::1` 触发告警。这台机器没有 systemd-resolved（Alpine 无
systemd），`::1` 来自 dhcpcd 的 RA 处理。实际影响见 6.3——因为还有两个
非回环 nameserver，v4 的 DNS 劫持仍然生效。

### 4.3 缺陷 #1：`sysctl --system` 在 BusyBox 上不可用

**这是本次测试的主要发现。**

`POST /api/v1/transparent/provision` 的响应（三步）：

```json
{
  "success": false,
  "message": "跳过 1 项，完成 1 项，失败 1 项...",
  "steps": [
    {"name":"安装依赖包", "success":true, "skipped":true,
     "detail":"nft/iptables 与 iproute2 均已就绪"},
    {"name":"写入 sysctl 配置", "command":"write /etc/sysctl.d/99-auroramihomo.conf",
     "success":true, "detail":"已写入 /etc/sysctl.d/99-auroramihomo.conf（4 项）"},
    {"name":"使 sysctl 生效", "command":"sysctl --system", "success":false,
     "detail":"sysctl: unrecognized option: system\nBusyBox v1.37.0 ... multi-call binary.\n\nUsage: sysctl [-enq] { -a | -p [FILE]... | [-w] [KEY[=VALUE]]... }"}
  ],
  "manualCommands": [
    "apk add --no-cache iptables ip6tables nftables iproute2",
    "printf '%s\\n' 'net.ipv4.ip_forward = 1...' | sudo tee /etc/sysctl.d/99-auroramihomo.conf",
    "sudo sysctl --system"
  ]
}
```

**根因**：Alpine 的 `sysctl` 是 BusyBox applet，不支持 `--system` 长选项
（那是 procps-ng 的 GNU 扩展）：

```
$ which -a sysctl; readlink -f $(which sysctl)
/sbin/sysctl
/bin/busybox
$ ls -l /sbin/sysctl
lrwxrwxrwx    1 root root  12 Jun 16 21:21 /sbin/sysctl -> /bin/busybox
```

**后果**：配置文件成功写入，但**内核值没有生效**。回读 `/proc` 确认：

```
$ cat /etc/sysctl.d/99-auroramihomo.conf
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 2
net.ipv4.conf.default.rp_filter = 2
net.ipv4.conf.eth0.rp_filter = 2

$ cat /proc/sys/net/ipv4/ip_forward
0                                    ← 仍是 0
$ for f in /proc/sys/net/ipv4/conf/*/rp_filter; do ...; done
all=1  default=1  eth0=1  lo=1       ← 仍是 1
```

而 `rp_filter = 1` 恰恰是告警里说的「会导致 TProxy 收不到包」。也就是说
**「自动准备」报告部分成功，但 TProxy 实际仍不可用**。

**设计上值得称许的一点**：`provision.go` 刻意把「写文件」与「使生效」分成
两步分别报告，注释明确写着「文件写成功 ≠ 已生效」。正因如此这个缺陷被如实
暴露，而不是静默通过。若两步合成一个「配置 sysctl」步骤，用户会看到成功
提示而实际不通，排查难度高得多。

`manualCommands` 里也带着同样有问题的 `sudo sysctl --system`，用户照抄同样
失败——这一处也要一并修。

**已验证的修法**：BusyBox 的 `sysctl` 支持 `-p FILE`：

```
$ sysctl -p /etc/sysctl.d/99-auroramihomo.conf
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 2
net.ipv4.conf.default.rp_filter = 2
net.ipv4.conf.eth0.rp_filter = 2
（rc=0）

$ cat /proc/sys/net/ipv4/ip_forward
1                                    ← 生效
$ for f in ...; do ...; done
all=2  default=2  eth0=2  lo=1       ← 生效
```

Alpine 也有 `procps-ng-4.0.6-r0` 包提供 GNU 版 `sysctl`（支持 `--system`）。

**同类根因**：这与既有报告第 4 节修复的 `ip --version` 误判是**同一类问题**
——假设了 GNU 工具的行为，在 BusyBox 环境下失效。这类缺陷有个共同特征：
在开发者常用的发行版上永远不会暴露。

**需要留意的关联**：本项目的 Dockerfile 基于 `alpine:3.21`，容器内的
`sysctl` 同样是 BusyBox applet。当前不受影响，仅因为容器内的 sysctl 写入
本就按设计被拒绝（见 Ubuntu 报告 4.2）；若将来放开这条路径，会踩同一个坑。

**建议修法**（三选一，倾向第一个）：

| 方案 | 做法 | 评价 |
| --- | --- | --- |
| 1 | 回退式调用：先试 `sysctl --system`，失败则遍历 `/etc/sysctl.d/*.conf` 逐个 `sysctl -p <file>` | 与既有 `isRealIproute2()` 的「长选项失败就试短选项」策略一致，不引入新依赖。推荐 |
| 2 | 直接写 `/proc/sys/...` | 最可靠，但绕过了 sysctl 的语义（如 `-e` 忽略未知键） |
| 3 | 检测到 BusyBox 时把 `procps-ng` 加进 `requiredPackages` | 引入新依赖，且改变了「只装网络工具」的边界 |

无论哪种方案，`manualCommands` 里的对应命令都要同步调整。

为让测试继续，当时先手动执行 `sysctl -p` 使内核值就位（TProxy 需要
`rp_filter ≠ 1`）。

**修复与回归验证（测试后补充）**

已按方案 1 修复：`applySysctl` 拆出 `reloadSysctl`，先试 `--system`，
仅在错误输出匹配「选项不被识别」类措辞时回退为 `sysctl -p <本文件>`。
刻意不对所有失败都回退 —— 像 `cannot stat /proc/sys/...` 这类错误说明
某个键在当前内核上不存在，换成 `-p` 是同样结果，转成成功等于掩盖真实问题。
`manualCommands` 也改为 `sudo sysctl -p <文件>`，它在 procps-ng 与
BusyBox 上都可用。

在同一台 Alpine 上回归：先把内核值改回缺陷会暴露的状态
（`rp_filter=1`、`ip_forward=0`）并删掉 drop-in，部署修复版后重新调用
`provision`：

```
{"name":"使 sysctl 生效", "command":"sysctl --system", "success":true,
 "skipped":true,
 "detail":"当前系统的 sysctl 不支持 --system（常见于 BusyBox），已改用逐文件加载。
           原始输出：sysctl: unrecognized option: system ..."}
{"name":"使 sysctl 生效（逐文件加载）",
 "command":"sysctl -p /etc/sysctl.d/99-auroramihomo.conf", "success":true,
 "detail":"net.ipv4.ip_forward = 1\nnet.ipv4.conf.all.rp_filter = 2\n..."}
```

关键判据是回读 `/proc` 确认内核值真的变了（修复前这一步是不变的）：

```
$ cat /proc/sys/net/ipv4/ip_forward
1
$ for f in /proc/sys/net/ipv4/conf/*/rp_filter; do ...; done
all=2  default=2  eth0=2  lo=1
```

`manualCommands` 也已确认给出 `sudo sysctl -p /etc/sysctl.d/99-auroramihomo.conf`。
随后在 `rp_filter=2` 的状态下复测 TProxy 端到端：出口 IP 变为节点 IP、
关闭后规则清理干净，与修复前手动置位时的行为一致。

单元测试补了三个用例（`netcheck` 包由 70 个增至 73 个，全绿）：
回退路径生效、回退也失败时如实报错、`manualCommands` 不含 `--system`。
配套更新了设计文档第 2 节与用户指南「自动准备」一节。

### 4.4 幂等复测

第二次调用 `provision`：

- 装包步骤 `skipped: true`（`nft/iptables 与 iproute2 均已就绪`）
- 写 sysctl 文件 `success: true`
- `sysctl --system` 仍失败（同一缺陷）

配置文件 16 行（含注释头），`sort | uniq -d` 无输出——**无重复行堆积**，
证明是整体重写而非追加，幂等性成立。

---

## 5. S2 代理基础能力与出口 IP 基线

与 Ubuntu 形态遇到相同的两个坑（详见 Ubuntu 报告第 5 节），此处只记结果：

- **IPv6 干扰**：该机同样有 v6 出网（`240e:398:30b3:2101::c3c/128`，
  默认路由 `via fe80::1cc3:beff:fe91:a44f dev eth0`），故统一用
  `ipv4.icanhazip.com`（仅 A 记录）取可比的 v4 出口
- **伪节点**：订阅首个条目是承载流量信息的伪节点（`server: 256.256.256.256`），
  组类型 `Selector` 默认选中它。通过内核 API 切到实测低延迟的真实节点

**基线确立：**

| 段 | 出口 IP |
| --- | --- |
| 直连 | `182.136.123.11` |
| 手动代理（mixed-port 7890） | `116.48.48.234` |

选中节点为「女神·香港\|直连」（服务器地址不予展示）。注意与 Ubuntu 机器的
直连出口相同（同一局域网出口），但节点不同（各自独立选点），因此两台的
第三段判据不会互相混淆。

---

## 6. S3 TProxy 主线

启用前基线：该机原本**无任何 nft 规则**（`nft list ruleset` 输出 0 行），
`ip rule` 只有默认三条。这使得关闭后的 diff 比对格外干净。

### 6.1 90 秒未确认自动回滚

启用后立即：

```
{"enabled":true,"mode":"tproxy","pendingConfirm":true,"secondsLeft":89,...}
```

倒计时实测递减，并在窗口过期后自动回滚：

```
t+21s    "enabled":true  "pendingConfirm":true  "secondsLeft":67
t+42s    "enabled":true  "pendingConfirm":true  "secondsLeft":67
t+63s    "enabled":true  "pendingConfirm":true  "secondsLeft":46
t+84s    "enabled":true  "pendingConfirm":true  "secondsLeft":25
t+105s   "enabled":false "pendingConfirm":false
```

窗口内规则与 v4/v6 策略路由都已下发：

```
$ ip rule show | grep fwmark
32765:  from all fwmark 0x1 lookup 100
$ ip route show table 100
local default dev lo scope host
$ ip -6 rule show | grep fwmark
32765:  from all fwmark 0x1 lookup 100
$ ip -6 route show table 100
local default dev lo metric 1024 pref medium
```

窗口过期后：

```
$ nft list table inet aurora_tproxy   → Error: No such file or directory（表已删除）
$ ip rule show | grep fwmark          → v4 fwmark 已清除
$ ip -6 rule show | grep fwmark       → v6 fwmark 已清除
$ ip route show table 100             → table 100 v4 已空
$ ip -6 route show table 100          → table 100 v6 已空
$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
182.136.123.11                        → 回到直连基线
```

全程 SSH 未中断。**v4 与 v6 的对称清理**再次确认了既有报告 8.7 节的修复
（拆除路径不再依赖「启用时有没有开 v6」这个参数，一律清理两个家族）在真实
有 v6 出网的机器上有效。

### 6.2 规则内容与顺序核对

规则内容与 Ubuntu 形态完全一致（同一份生成代码），完整规则见
[Ubuntu 报告 6.2 节](./AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md)。
本机核对到位的要点：

```nft
chain prerouting {
    type filter hook prerouting priority mangle; policy accept;
    tcp dport 22 return
    tcp dport 8899 return
    tcp dport 9090 return
    meta mark 0x000000ff return          # mihomo 自身
    meta mark 0x000000fe return          # 面板自身（PanelMark）
    socket transparent 1 meta mark set 0x00000001 accept
    udp dport 53 meta mark set 0x00000001 tproxy to :7893 accept
    ip daddr 10.0.0.0/8 return
    ...（8 条 v4 + 4 条 v6 保留网段）
    meta l4proto { tcp, udp } meta mark set 0x00000001 tproxy to :7893 accept
}
chain output {
    type route hook output priority mangle; policy accept;   # route 而非 filter
    tcp sport 22 return                  # 按源端口豁免（回包）
    tcp sport 8899 return
    tcp sport 9090 return
    tcp dport 22 return                  # 也按目的端口豁免（主动连出）
    ...
}
```

与 Ubuntu 形态的一处差异：本机 prerouting 有 `tcp dport 9090`，因为
`external-controller` 监听在 `127.0.0.1:9090`，`keepPorts()` 每次现取实际
配置的端口而非硬编码。

### 6.3 本机流量真实接管

启用并确认后：

```
$ curl -4 -sS -m 40 --noproxy '*' https://ipv4.icanhazip.com
116.48.48.234
```

| 段 | 出口 IP | 判定 |
| --- | --- | --- |
| 直连基线 | 182.136.123.11 | — |
| 手动代理（S2） | 116.48.48.234 | — |
| **TProxy 后本机 curl** | **116.48.48.234** | **与手动代理相同，与直连不同** |

第二段与第三段相同，证明本机流量走的是**同一条代理链路**。宿主上任何未指定
代理的程序此刻都按分流规则走节点。

DNS 也被接管：

```
$ dig +short google.com
198.18.0.4
```

返回 `fake-ip-range`（198.18.0.0/16）内的地址，证明查询经过 mihomo 的 DNS。

**这里与 4.2 节的第三条告警需要一起看**：告警说 `resolv.conf` 含
`nameserver ::1`（回环），按设计文档第 10 节的已知限制，回环 DNS 下本机域名
分流不生效。但实测生效了——原因是该文件里除了 `::1` 还有
`192.168.1.251` 与 `61.139.2.69` 两个**非回环** nameserver，`dig` 优先用了
它们，而指向非回环地址的 DNS 查询会被 output 链的劫持规则捕获
（`th dport 53 ip daddr != 127.0.0.0/8`）。

告警本身没错——它警告的是「若查询走了回环地址则不会被劫持」，这一点仍然
成立。只是这台机器的 resolv.conf 是混合的，实际路径落在了可劫持的那一侧。
告警偏保守，不构成缺陷。

### 6.4 管理端口豁免（含非局域网段验证）

常规路径：

```
$ curl -sS -o /dev/null -w 'panel HTTP=%{http_code}\n' http://127.0.0.1:8899/healthz
panel HTTP=200
$ curl -sS -o /dev/null -w 'kernel HTTP=%{http_code}\n' http://127.0.0.1:9090/version
kernel HTTP=200
```

**非局域网段验证**（这是必需的，同网段会被 `ip daddr 192.168.0.0/16 return`
掩盖，既有报告 8.2 节的缺陷正因此长期未暴露）：

```
$ ip link add aurotest type dummy; ip addr add 100.64.7.1/32 dev aurotest; ip link set aurotest up
4: aurotest: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500 ...
    inet 100.64.7.1/32 scope global aurotest

$ curl -sS --interface 100.64.7.1 -o /dev/null -w 'from-CGNAT panel HTTP=%{http_code}\n' \
    http://192.168.1.129:8899/healthz
from-CGNAT panel HTTP=200
```

CGNAT 段（`100.64.0.0/10`）不在规则的 8 条局域网放行列表里，从它访问管理
端口成功，证明 output 链的 sport/dport 双向豁免真实有效。

对照：从同一源地址访问外部（应命中兜底 TPROXY）：

```
$ curl -sS --interface 100.64.7.1 -o /dev/null -w 'to-external HTTP=%{http_code}\n' \
    http://ipv4.icanhazip.com
to-external HTTP=200
```

测试后删除 dummy 网卡，已确认 `Device "aurotest" does not exist`。

**未验证项**：原计划用 `nc` 从 CGNAT 源地址探测 SSH 端口握手，Alpine 未预装
`nc`，该项未验证。但整个测试全程通过 SSH 进行且从未中断，是更强的证据。

### 6.5 关闭与清理

| 项 | 结果 |
| --- | --- |
| `nft list table inet aurora_tproxy` | 表已删除 |
| v4 fwmark 规则 | 已清除 |
| v6 fwmark 规则 | 已清除 |
| `ip route show table 100` | 已空 |
| `ip -6 route show table 100` | 已空 |
| 出口 IP | 恢复 182.136.123.11 |
| **与启用前基线 diff** | **完全为空**（该机原本无任何 nft 规则） |

```
$ diff /tmp/nft-before.txt /tmp/nft-after.txt
（无输出）→ 无关规则完好
```

---

## 7. S4 TUN 对照

前置确认 TProxy 已完全关闭（两模式互斥，叠加会让现象无法归因）：

```
"enabled":false  "mode":"off"
$ nft list table inet aurora_tproxy → 不存在
$ ip rule show | grep fwmark → no fwmark (clean)
```

启用 TUN 并确认后：

```
$ ip link show | grep -iE 'meta|utun|tun[0-9]'
5: Meta: <POINTOPOINT,MULTICAST,NOARP,UP,LOWER_UP> mtu 9000 qdisc pfifo_fast state UNKNOWN qlen 500

$ ip route show
default via 192.168.1.251 dev eth0 proto dhcp src 192.168.1.129 metric 1002
192.168.1.0/24 dev eth0 proto dhcp scope link src 192.168.1.129 metric 1002
198.18.0.0/30 dev Meta proto kernel scope link src 198.18.0.1
$ ip -6 route show
fdfe:dcba:9876::/126 dev Meta proto kernel metric 256 pref medium
fe80::/64 dev Meta proto kernel metric 256 pref medium
```

配置注入核对（由 `netcheck.Inject` 在合并流程最后一步写入）：

```yaml
tun:
    enable: true
    stack: mixed
    auto-route: true
    dns-hijack:
        - any:53
    auto-redirect: true
```

**TUN 模式不下发 aurora 的防火墙规则**：

```
$ nft list table inet aurora_tproxy → 不存在
$ ip rule show | grep fwmark → 无 aurora 的 fwmark 规则（符合预期）
```

符合设计——TUN 的路由接管由 mihomo 自己通过 netlink 完成，本项目只负责
注入配置。

出口与 DNS：

```
$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
116.48.48.234        ← 与手动代理基线相同
$ dig +short google.com
198.18.0.5           ← fake-ip 段
$ timeout 10 ping -c 3 1.1.1.1
3 packets transmitted, 3 packets received, 0% packet loss
round-trip min/avg/max = 269.189/280.548/286.887 ms
```

ping 的 280ms 延迟也印证了流量绕经节点（直连到 1.1.1.1 通常远低于此）。

管理端口与 SSH 全程正常。关闭后：

```
$ ip link show | grep -iE 'meta|utun' → TUN 网卡已移除
$ curl -4 -sS --noproxy '*' https://ipv4.icanhazip.com
182.136.123.11        ← 恢复直连
$ ip route show
default via 192.168.1.251 dev eth0 proto dhcp src 192.168.1.129 metric 1002
```

**一个测试方法上的教训**：首次取 TUN 出口 IP 时脚本用
`out.strip().split()[-1]` 取最后一个词，而 TUN 首次连接需要等设备与路由就绪、
第一次 curl 超时，错误输出让脚本取到了 `reading` 这样的片段，造成**假阴性**
（一度以为 TUN 下本机接管失败）。改用正则提取 IPv4 并加重试后确认实际正常。
判据的提取方式本身也需要经得起失败路径的考验。

---

## 8. Alpine 平台特有的观察汇总

本节汇总本次首验在 Alpine 上遇到的平台差异，供后续部署与开发参考。

| 项 | 观察 | 影响 |
| --- | --- | --- |
| **musl vs glibc** | `CGO_ENABLED=0` 的静态二进制直接可跑，无 INTERP 段；`ldd` 报 "Not a valid dynamic program" 是预期表现 | 无需兼容层。但若将来引入 CGO 依赖，需为 musl 单独构建 |
| **`ip6tables` 独立包** | Alpine 拆分了，其他发行版通常并入 iptables | 二进制部署需显式安装；Dockerfile 已正确处理 |
| **iproute2 覆盖 `/sbin/ip`** | 装包后 busybox 软链被替换为独立 ELF，`which -a ip` 只有一个候选 | 无 PATH 优先级风险（原本担心的问题不存在） |
| **无 systemd** | 用 OpenRC；`supervise-daemon` 才有重新拉起能力，`start-stop-daemon` 没有 | `/api/v1/system/restart` 依赖 supervisor 在位 |
| **无 curl / dig / python3** | 默认只有 BusyBox 的 wget，不支持 `-x` 与 `--interface` | 运维排查需补装 `curl bind-tools` |
| **apk-tools 3.x 的源配置** | 3.0.6 仍读平文件 `/etc/apk/repositories`，`repositories.d/` 目录不存在 | 换源无需迁移到目录形式 |
| **BusyBox `sysctl` 不支持 `--system`** | **缺陷 #1**，见 4.3 | 自动准备的 sysctl 步骤失效，`rp_filter` 保持 1 使 TProxy 不可用 |
| **`resolv.conf` 由 dhcpcd 生成** | 含 `nameserver ::1`（来自 RA），但同时有非回环 nameserver | 回环 DNS 告警偏保守，实际 v4 劫持仍生效 |
| **磁盘可能极小** | 原始镜像根分区仅 114.5M | 部署前需确认余量；无分区表时 `resize2fs` 即可扩容 |
| **内核模块需手动加载** | `tun` 与 `nft_tproxy` 默认未加载，`/dev/net/tun` 不存在 | 需 `modprobe` 并写 `/etc/modules-load.d/` 持久化 |

---

## 9. 发现的问题

### 缺陷 #1：`sysctl --system` 在 BusyBox 环境不可用（已修复并回归验证）

详见 4.3 节。摘要：

- **位置**：`backend/internal/netcheck/provision.go`，`applySysctl` 执行 `sysctl --system`
- **现象**：Alpine 的 `sysctl` 是 BusyBox applet，不认 `--system`；配置文件写入成功但内核值未生效
- **后果**：`rp_filter` 保持 1，而这正是「会导致 TProxy 收不到包」的条件。「自动准备」报告部分成功，实际 TProxy 仍不可用
- **影响面**：所有 Alpine / BusyBox 环境的二进制部署。Docker 形态当前不受影响（容器内 sysctl 写入本就按设计拒绝），但镜像基于 alpine:3.21，若放开该路径会踩同一坑
- **同类根因**：与既有报告第 4 节的 `ip --version` 误判同源——假设 GNU 工具行为
- **修法**：已按回退式调用修复（先 `--system`，仅在「选项不被识别」时逐文件 `-p`），
  `manualCommands` 同步改为 `-p`。其它类型的失败不回退、如实报错
- **回归验证**：在同一台 Alpine 上复现缺陷场景后部署修复版，回读 `/proc` 确认
  `ip_forward` 0→1、`rp_filter` 全部 1→2；详见 4.3 节末尾

### 未验证项

| 项 | 原因 |
| --- | --- |
| 从 CGNAT 源地址探测 SSH 端口握手 | Alpine 无 `nc`。全程 SSH 未断为替代证据 |
| `/api/v1/system/restart` 端到端 | 仅确认了 supervise-daemon 在位这一前提，未实际触发重启 |
| 宿主重启后的状态一致性（`ReconcileState`） | 本次未重启宿主 |

---

## 10. 结论汇总表

| 测试项 | 结果 |
| --- | --- |
| **部署** | |
| 磁盘扩容（无分区表直接 resize2fs） | 通过 |
| busybox ip 原始状态取证 | 通过（不可重现证据已留存） |
| apk 源换阿里云并确认实际 URL | 通过 |
| 依赖安装（nftables/iproute2/ip6tables） | 通过 |
| iproute2 覆盖 `/sbin/ip`、`ip rule` 支持 fwmark | 通过 |
| 内核模块加载与持久化 | 通过 |
| **musl 兼容性（静态二进制直接可跑）** | 通过 |
| OpenRC + supervise-daemon 服务 | 通过 |
| 补装 curl / bind-tools | 通过（Alpine 默认缺失） |
| **S0 基础功能** | |
| S0-1 健康检查 | 通过 |
| S0-2 登录换 JWT | 通过 |
| S0-3 导入真实订阅并拉取（75 节点） | 通过 |
| S0-4 内核运行 + Zashboard 入口 | 通过 |
| **S1 环境检测** | |
| S1-5 检测报告与实际逐字段核对 | 通过，全部一致 |
| S1-5b busybox → 真 iproute2 的判定切换 | 通过（iproute2 7.0.0 上确认） |
| S1-5c 三条告警的准确性 | 通过，均准确 |
| S1-6 自动准备：装包 | 通过（已就绪故 skipped） |
| S1-6 自动准备：写 sysctl 文件 | 通过 |
| **S1-6 自动准备：使 sysctl 生效** | **初测未通过（缺陷 #1），已修复并回归验证** |
| S1-6b 幂等复测（无重复行堆积） | 通过 |
| **S2 代理基础能力** | |
| S2-7 面板自身出网 | 通过 |
| S2-8 手动代理出口 IP 为节点 IP | 通过 |
| **S3 TProxy 主线** | |
| S3-9 90 秒未确认自动回滚 | 通过（核心防护机制） |
| S3-9b 回滚时 v4 与 v6 策略路由都清理 | 通过 |
| S3-10 规则内容与顺序 | 通过，与设计一致 |
| S3-11 **本机流量真实接管**（出口 = 节点 IP） | 通过 |
| S3-11b DNS 被接管（fake-ip 段） | 通过 |
| S3-12 管理端口豁免（本机路径） | 通过 |
| S3-12b **非局域网段（CGNAT）验证 sport 豁免** | 通过 |
| S3-12c 从 CGNAT 探测 SSH 端口握手 | 未验证（无 `nc`） |
| S3-13 关闭后表与 v4/v6 策略路由完整清理 | 通过 |
| S3-13b 出口 IP 恢复直连 | 通过 |
| S3-13c 不误伤无关规则 | 通过（diff 完全为空） |
| **S4 TUN 对照** | |
| S4-14 TUN 网卡与路由建立 | 通过 |
| S4-14b 配置注入（auto-route / auto-redirect） | 通过 |
| S4-14c TUN 模式不下发 aurora 防火墙规则 | 通过，符合设计 |
| S4-14d 出口 IP 为节点 IP | 通过 |
| S4-14e 关闭后网卡移除与出口恢复 | 通过 |

本次是 Alpine 平台的首次验证。透明代理的核心机制（90 秒确认窗口、管理端口
双向豁免、v4/v6 对称清理、精确表删除）在 Alpine 上与 Ubuntu 表现一致，
**本机流量接管通过出口 IP 三段对照得到闭环验证**。

Alpine 特有的平台差异大多不构成障碍——musl 下静态二进制直接可跑、iproute2
覆盖 busybox 软链因此无 PATH 问题、apk-tools 3.x 仍兼容平文件源配置。
唯一的真实缺陷是 BusyBox `sysctl` 不支持 `--system`（缺陷 #1），它使
「自动准备」的内核参数调整失效，而 `rp_filter=1` 正是 TProxy 不通的直接
原因。该缺陷已按回退式调用修复，并在同一台机器上复现场景后完成回归验证
（见 4.3 节末尾）。

# AuroraMihomo 透明代理设计

透明代理让局域网设备无需各自设置代理即可分流。本文覆盖两种实现方式的取舍、
环境检测项、风险防护，以及终端设备的接入方法。

适用平台：Linux（TUN / TProxy）与 macOS（仅 TUN）。Windows 不支持。

---

## 1. 两种模式的对比

|                | TUN                                  | TProxy                        |
| -------------- | ------------------------------------ | ----------------------------- |
| mihomo 配置    | `tun.enable: true`                   | `tproxy-port: 7893`           |
| 平台           | Linux / macOS                        | 仅 Linux                      |
| TCP / UDP      | 都支持                               | 都支持                        |
| ICMP           | 支持（ping 可分流）                  | 不支持                        |
| 谁写防火墙规则 | mihomo 自己（`auto-redirect`）       | **本面板**                    |
| 退出时清理     | mihomo 自动清理                      | 面板负责拆除                  |
| 风险           | 较低                                 | 较高，配错可能导致主机失联    |
| 所需权限       | `CAP_NET_ADMIN`（macOS 必须 root）   | `CAP_NET_ADMIN`               |

**默认推荐 TUN。** 它把路由与防火墙的管理交给 mihomo，进程退出时规则自动
回收；TProxy 需要面板代管策略路由与 mangle 规则，出错面更大。

TProxy 的存在意义是覆盖没有 TUN 设备的环境（例如宿主未加载 `tun` 模块、
容器未映射 `/dev/net/tun` 且无法调整）。

### 为什么不做第三种模式（redir）

mihomo 还有第三条透明代理路径 `redir-port`（iptables `nat` 表的 REDIRECT）。
本面板**不**把它做成一键模式，`domain.Config.RedirPort` 只是 YAML 直通字段，
没有任何注入或防火墙逻辑消费它。

理由不是实现成本，而是它相对上面两种是**纯降级**：

- **UDP 接管不了。** REDIRECT 是 nat 表的 TCP 目标，没有 UDP 等价物。
  DNS 尚可靠 DNAT 打到 `dns.listen` 绕过去，但 QUIC / HTTP3（UDP 443）
  只有两个选择：整体 REJECT 逼客户端回落 TCP，或者放它直连——后者是
  静默的流量泄漏，用户看到的现象是"开了透明代理但某些站点没走代理"。
  TUN 与 TProxy 都没有这个问题。
- **ICMP 也不支持**，这一点和 TProxy 相同，但 TUN 支持。
- **唯一必需的场景已经很罕见。** 只有"内核既无 TPROXY 支持、又拿不到
  TUN 设备、但 nat 表可用"才非它不可，实际就是老 OpenVZ 那类虚拟化。
  现代内核里 `nft_tproxy` 自 4.19 起可用，`xt_TPROXY` 更早，TUN 是标配。
- **macOS 上不成立。** 见 §9，pf 的 `rdr` 没有 UDP 重定向。

若将来确实要补，需要的不只是一个开关：独立的 nat 规则生成器（现有
`firewall.go` 是 mangle + TPROXY，无法复用）、UDP/QUIC 的单独策略与界面
告知、以及把 `IPTChainPrefix` 那条至今为空的 iptables 回退路径真正实现。

因此界面上 `redir-port` 保留为高级用户的原生字段，表单 help 明确写出
"面板不代管其防火墙规则"，避免用户误以为它和「透明代理」页的开关有关。

### TUN 的 stack 选项

| stack    | 说明                                               |
| -------- | -------------------------------------------------- |
| `system` | 内核栈，开销最低                                   |
| `gvisor` | 用户态栈，隔离性好                                 |
| `mixed`  | TCP 走内核、UDP 走 gvisor。**默认值**，兼顾两者    |

注意：`system` 与 `mixed` 会被宿主防火墙拦截，Debian/Ubuntu 上若启用了
ufw/firewalld 需要放行内核进程。

---

## 2. 环境检测

**探测本身只读**：检测过程不安装任何东西、不改 sysctl、不动防火墙，
只把"缺什么"与"怎么补"如实报出来。

缺依赖时界面给出可直接复制的命令，也提供一个「尝试自动准备环境」按钮
（见 2.1）。手动命令始终可见——自动失败时它是兜底，成功时用户也常需要
把它记进自己的部署脚本。

### 2.1 自动准备环境（决策变更）

早期版本刻意**完全不**代为安装依赖或修改 sysctl，理由是：这些操作需要
root、会改动宿主状态，而面板可能跑在容器里（容器内装的包重启即丢），
代为执行既不可靠也不可审计。

真机测试（见 `AuroraMihomo-Transparent-Proxy-Test-Report.md`）表明这些顾虑
成立，但不足以否掉整个功能——手动步骤本身是可靠且用户频繁需要的，
而"不可靠"这部分可以靠**如实回报每一步的原始输出**加**始终提供等价手动
命令**来消化。因此改为提供自动准备，同时保留原理由中仍然成立的部分。

改动后的原则：**探测只读；修复需用户显式触发、可审计、默认不动。**

具体边界（这些是刻意的限制，不是尚未实现）：

| 做 | 不做 |
| --- | --- |
| 装 `iptables`/`nftables`/`iproute2`（包名硬编码） | 不接受调用方传入任何包名或命令 |
| 写 `/etc/sysctl.d/99-auroramihomo.conf` | 不改 `/etc/sysctl.conf` 或别人的 drop-in |
| 只写确实不合规的键 | 不无条件全写（用户可能有意关着转发） |
| 整体重写自己的文件（幂等） | 不追加（否则重复执行会堆积同名键） |
| 容器内允许装包，但标注不持久 | 容器内不碰 sysctl（见下） |
| — | 不做 `modprobe`（重启即失效，容器内基本失败） |

不接受外部传入命令是因为那等于开一个远程命令执行入口；容器内不碰 sysctl
是因为非特权容器会被内核拒绝，而 host 网络下则会**直接改到宿主上**——
两种结果都不该由面板替用户默默决定。

两个实现上容易踩空的点：

- **`rp_filter` 的 max 语义**。内核对某网卡取
  `max(conf.all.rp_filter, conf.<iface>.rp_filter)`，所以只把 `all` 改成 2，
  在任何仍是 1 的网卡上依然按严格模式丢包。检测会枚举
  `/proc/sys/net/ipv4/conf/*/rp_filter` 找出这些网卡，写配置时连 `default`
  与它们一起写。设 2 而非 0：保留基本的源地址校验，且与 Debian/Ubuntu
  默认值一致。
- **写了文件 ≠ 已生效**。drop-in 需要额外加载一次，因此写文件与使其生效是
  两个独立步骤、分别报告。混为一谈会让用户以为搞定了却仍然丢包。
- **加载 drop-in 要兼顾 BusyBox**。`sysctl --system` 是 procps-ng 的扩展，
  Alpine 这类以 BusyBox 为基础的发行版只支持 `sysctl -p FILE`。实现先试
  `--system`，仅在识别到「选项不被识别」类错误时回退为 `sysctl -p <本文件>`；
  其它失败（例如某个键在当前内核上不存在）不回退、如实报错 —— 换个选项也是
  同样结果，转成成功等于掩盖真实问题。给用户的手动命令一律用 `-p`，它在两种
  实现上都可用。这个坑在 Alpine 真机上暴露过：配置写入成功但 `rp_filter`
  保持严格模式，TProxy 静默收不到包（见 Alpine 测试报告 4.3 节）。

### 检测项

| 类别   | 检测内容                                                             |
| ------ | -------------------------------------------------------------------- |
| 权限   | `euid == 0`；`/proc/self/status` 的 `CapEff` / `CapBnd`               |
| TUN    | `/dev/net/tun` 与 `/dev/tun` 是否为字符设备；`tun` 模块是否加载       |
| TProxy | `nft` / `iptables` / `ip6tables` / `ip` 是否存在                      |
|        | `nft_tproxy` / `nft_socket` / `xt_TPROXY` / `xt_socket` 模块          |
|        | iptables 后端是 `nf_tables` 还是 `legacy`                             |
|        | `ip` 是否来自 busybox（不支持 `ip rule add fwmark`）                  |
| 容器   | `/.dockerenv`、`/proc/1/cgroup`；netns 是否与 PID 1 相同（host 网络） |
| 发行版 | `/etc/os-release` 的 `ID` / `ID_LIKE`；包管理器 `apk` / `apt-get`     |
| sysctl | `ip_forward`、`rp_filter`（含逐网卡的 `rp_filter`）                   |
| 本机 DNS | `/etc/resolv.conf` 的 `nameserver` 是否指向回环（见第 5 节 ①）      |
| IPv6   | `/proc/net/if_inet6` 有无全局地址、`/proc/net/ipv6_route` 有无默认路由（见第 5 节 ②） |

后两项服务于"本机流量被接管"这条路径：它们不影响模式可用性，只决定本机流量的
实际表现，因此以 Warning 形式呈现而不是把模式判为不可用。

### 容器里最容易踩的一个坑

`docker-compose.yml` 里写了 `cap_add: NET_ADMIN` **不等于**进程真的持有该
权限。`cap_add` 只填充 bounding 与 permitted 集；非 root 用户运行且二进制
没有 file capability 时，effective 集仍是空的。

检测结果会把这两种情况分开报告：

- `capNetAdmin: false` + `capNetAdminBounding: false` → 根本没给权限
- `capNetAdmin: false` + `capNetAdminBounding: true` → **给了但拿不到**

后者的修复方式完全不同（见第 7 节 Docker 部分），所以必须区分。

### 依赖安装命令

```bash
# Alpine
apk add --no-cache iptables ip6tables nftables iproute2

# Debian / Ubuntu
apt-get update && apt-get install -y --no-install-recommends iptables nftables iproute2
```

Alpine 最小镜像可能只带 `iproute2-minimal`，它不足以支撑策略路由，
需要显式安装完整的 `iproute2`。

Alpine 上装 `iproute2` 后 `/sbin/ip` 会由 busybox 软链被替换成独立的
iproute2 可执行文件，因此不存在「两个 ip 争 PATH 优先级」的问题 ——
这一点在 3.24.1 + iproute2 7.0.0 上实测确认过。裸机部署另需注意 Alpine
默认不带 `curl`（只有 busybox 的 wget，不支持 `-x` 与 `--interface`），
排查透明代理时通常要先 `apk add curl bind-tools`。

`tun` 与 `nft_tproxy` 在 Alpine 上默认未加载、`/dev/net/tun` 也不存在，
需 `modprobe` 并写入 `/etc/modules-load.d/` 才能在重启后保持：

```bash
modprobe tun && modprobe nft_tproxy
printf 'tun\nnft_tproxy\n' > /etc/modules-load.d/auroramihomo.conf
```

---

## 3. TProxy 的规则设计

仅在选择 TProxy 模式时由面板下发。

### 专用容器，绝不整体 flush

规则全部放进独立的 nftables 表 `table inet aurora_tproxy`（iptables 回退
路径用 `AURORA_TP` 前缀的自建链）。拆除即删表，不需要逐条比对"哪条是我加
的"。

**永不执行 `nft flush ruleset` 或 `iptables -F`** —— 宿主上通常还有 Docker、
fail2ban、k8s 的规则，整体清空会一并抹掉。

### 规则顺序即安全边界

顺序不可调整，每一条都有明确用途：

1. **放行管理端口**（SSH 22、面板端口、内核 API 端口）
   顺序错了会在规则生效瞬间劫持 SSH 与面板连接，操作者再也无法关掉开关。
   面板与内核 API 的端口取运行时的**实际配置**（分别来自 `aurora-api.yaml`
   的 `Port` 与 `config.yaml` 的 `external-controller`），不是写死的
   8899/9090——用户改过端口后，写死的值等于没放行。内核 API 端口在每次下发时
   现取，因为它随时可以在界面上改。
2. **放行内核与面板自身的出站**（`meta mark 0xff` / `0xfe return`）
   不放行 `0xff` 则 mihomo 发出的包被自己的 TPROXY 规则再次捕获，形成自环，
   对应一个已知的高 CPU 故障；该值必须与配置里的 `routing-mark` 一致。
   `0xfe` 是面板自身的标记，理由见第 4.7 节。
3. **劫持 DNS**（`prerouting` 是 UDP/53；`output` 链上 TCP 与 UDP 都劫持）
   必须在局域网放行之前，否则同网段的 DNS 查询直接放行，域名类分流规则失效。
4. **放行局域网网段**
5. **其余 TCP/UDP 交给 TPROXY**

`output` 链必须用 `type route hook output`，否则改了 mark 也不会重新查路由，
本机自身的 UDP 出不去。

### 两条链的方向语义不同

`prerouting` 与 `output` 的骨架相同，但第 1、3 步的写法必须不同，
不能"整理"成同一种：

| | `prerouting`（外来流量） | `output`（本机流量） |
| --- | --- | --- |
| 管理端口 | 按 `dport` 放行（入站到我们的服务） | 按 `sport` **与** `dport` 放行 |
| DNS | 直接 `tproxy`（该动作只在 prerouting 可用） | 打标后由策略路由发夹回 prerouting |

`output` 链按 `sport` 放行是必须的：我们自己服务的**回包**里管理端口是源端口
（sshd 的回包是 `sport=22`、`dport=` 客户端随机端口）。只按 `dport` 放行匹配不到
它们，回包会被打标、经 `local` 路由当作本机流量投递而永远送不出去。

这曾经是一个真实缺陷，长期没暴露是因为后面的局域网网段 `return` 兜住了同网段的
SSH 客户端；一旦 SSH 来源不在那几个私有网段（公网跳板、VPN 段、非默认私有段），
启用 TProxy 的瞬间就会失联——正是这套设计要防的那件事。
实测复现与修复验证见测试报告第 9 节。

`dport` 侧的放行同时保留，使"从本机主动 ssh 到别的机器"维持直连。

### 策略路由

```bash
ip rule add fwmark 1 table 100
ip route add local 0.0.0.0/0 dev lo table 100
# 宿主具备 IPv6 出网能力时，同时下发 v6 的对应两条
ip -6 rule add fwmark 1 table 100
ip -6 route add local ::/0 dev lo table 100
```

`local` 路由类型是关键：它让内核把打了标记的包当作发往本机的流量投递，
而不是继续转发。这也是本机流量能被接管的机制（见第 4.7 节）。

**拆除时 v4 与 v6 一律清理**，不看启用时有没有开 v6。曾经按参数决定清不清 v6，
而调用方传的是硬编码的 `false`，结果在有 IPv6 出网能力的宿主上关闭后残留了
v6 的 `ip rule` 与 `local ::/0` 路由，日志却报告"规则已拆除"。拆除路径不该依赖
记住启用时的参数——进程重启后那个参数根本无从得知。多执行两条清理命令没有代价
（目标不存在时被视为成功），却消除了整类残留。

### 应用顺序

1. `nft --check -f -` 干跑校验（只解析不应用，能挡住语法错误与内核不支持的表达式）
2. 建立策略路由
3. 下发规则

第 2 步必须在第 3 步之前。反序会出现"规则已生效、路由未就绪"的黑洞，
表现就是应用瞬间断网。任一步失败都会自动拆除已完成的部分。

---

## 4. 防"锁死自己"

TProxy 规则配错会让操作者失去对机器的访问，而那时他已经无法通过面板关掉
开关。以下几层防护：

### 4.1 管理端口豁免

SSH、面板、内核 API 三个端口在规则最前面放行。SSH 断了还能从别的设备连
面板，面板断了还能 SSH 进去关闭；两个都被劫持就只剩物理接触主机一条路。

### 4.2 应用前快照

`iptables-save`、`ip6tables-save`、`nft list ruleset`、`ip rule show`、
`ip route show table all` 全部存到 `<ConfigDir>/netbackup/<时间戳>/`。
这是自动回滚失效时的手工兜底。

### 4.3 强制确认 + 自动回滚

启用后进入**待确认**状态，必须在 **90 秒**内点击界面上的"网络正常，确认"，
否则自动拆除规则并关闭开关。

90 秒的取舍：太短则来不及切到别的设备验证，太长则真出问题时断网持续过久。

确认动作本身就是验证——请求能到达面板，说明网络仍通。

### 4.4 回滚意图持久化

待确认状态写入数据库（`transparent.pending_until`），**不只放在内存里**。

原因：只靠进程内定时器的话，面板崩溃或被 kill 后规则会永久留在宿主上，
而此时网络可能已经不通。持久化后，进程下次启动时 `RecoverPending` 会发现
这条未确认记录并回滚。

启动顺序上，`RecoverPending` 在配置合并之前执行：回滚会关掉开关，
之后的合并才会生成不带 tun/tproxy 的配置。

### 4.5 关闭操作永远可用

即使环境已变得不支持（依赖被卸载等），关闭请求也一律接受。否则用户会陷入
"开着但关不掉"。

### 4.6 状态核实（ReconcileState）

第 4.3/4.4 节解决的是"启用后还没确认"这一段的风险。但即便确认过，规则
本身不持久化到宿主重启（见第 10 节）——重启后 nftables 状态被内核清空，
数据库里"已确认启用"的记录却不会跟着变，界面会一直显示"已开启"，用户
没有任何信号能察觉网络实际上根本没被接管。这是在真机测试（见
AuroraMihomo-Transparent-Proxy-Test-Report.md 第 6.3 节）中发现的。

启动时紧跟 `RecoverPending` 之后执行：若 `enabled=true`、`mode=tproxy`
且没有待确认记录，去实际探测 `aurora_tproxy` 表是否还存在于宿主上
（`nft list table inet aurora_tproxy`）。不存在则回落为关闭。

刻意不选择"探测到规则消失就静默重新下发"：那等于绕过了启用时本该有的
90 秒确认窗口，与"规则变更必须经用户确认"这条设计原则相悖。用户如果
仍需要 TProxy，重新走一次正常的启用流程即可。

它只做"拆规则 + 落库"，不像 `disable()` 那样在末尾重新下发配置——
启动流程紧随其后就有一次合并，会按新状态（`enabled=false`）重新生成，
再触发一次等于白拉一遍所有订阅。也因此这个方法只能在启动流程里调用。

TUN 模式不需要这一层：路由与防火墙完全由 mihomo 自己在每次启动时按
`config.yaml` 的 `tun.enable` 重建，不存在"记录说开着、内核没跟上"的
缺口。

### 4.7 面板自身出站的豁免

面板拉订阅、查版本、下载内核的请求也是本机流量，同样会被接管（见第 5 节）。
不豁免会带来两个后果：

- 「优先经由本地 Mihomo 代理出网」这个显式设置被无声地绕过——用户关掉它
  想直连，实际仍然走了 mihomo；
- mihomo 挂掉时面板一并失去出网能力，连重新下载内核都做不到，
  等于把恢复手段也赔进去了。

做法与 mihomo 用 `routing-mark` 豁免自己是同一手法：面板给自己发出的连接打上
`SO_MARK = 0xfe`（`net.Dialer.Control` 里设，仅 Linux；其它平台是空操作），
规则里 `meta mark 0xfe return`。取 `0xfe` 是为了紧挨 mihomo 的 `0xff`，
两者语义相邻、在 `nft list` 输出里便于对照。

打标失败（通常是缺 `CAP_NET_ADMIN`）**只记一次日志并继续拨号**，不当作错误。
让缺一个 capability 演变成"面板完全无法联网"是不可接受的取舍；这种失败是持续
状态而非偶发，每次拨号都记日志会把订阅刷新这类高频路径的日志刷满。

---

## 5. 本机流量的接管

**两种模式都会接管本机自身的流量**，不只是局域网设备的。这一点容易被忽略，
因为第 6 节讲的全是终端设备侧的配置。

| 模式 | 本机流量怎么被接管 |
| --- | --- |
| TProxy | `output` 链打 fwmark 1 → `ip rule fwmark 1 table 100` + `local` 默认路由把包发夹回 `prerouting` → 在那里被 TPROXY 接走 |
| TUN | `auto-route: true` 装默认路由进 TUN 设备，本机出站一并进入 |

也就是说，开启后宿主上的 `curl`、`apt`、`git` 都会按分流规则走节点。
这是随模式启用的固定行为，没有单独的开关。

三条限制值得单独说明，它们决定了本机流量的实际表现：

**① 回环 DNS 会让本机的域名分流失效。** `output` 链的 DNS 劫持刻意排除回环目标
（mihomo 自己的 DNS 就监听在回环上，不排除会自环）。所以在把 `nameserver` 指向
`127.0.0.53` 的机器上（systemd-resolved 的默认形态），本机的 DNS 查询不经过
mihomo，域名类规则对本机流量不生效——只按 IP 分流。检测会读 `/etc/resolv.conf`，
发现回环 `nameserver` 时明确告警并给出两种改法（改成非回环地址，或关掉
`DNSStubListener`）。局域网设备不受此限制。

**② IPv6 只在宿主确实能走 v6 时才接管。** 判据是同时有全局单播地址
（`/proc/net/if_inet6` 的 scope `00`）与非 `lo` 的默认路由
（`/proc/net/ipv6_route`）。两者齐备才下发 v6 规则与 v6 策略路由；否则兜底规则
加 `meta nfproto ipv4` 限定，v6 包不被打标。

这个取舍是不对称的：只下发规则而没有策略路由，v6 包会被打标后无路可走，
从"不分流"恶化成"不通"。所以宁可漏判不可误判，没探测到 v6 出网能力时会告警
说明"只接管 IPv4"。

**③ 管理端口与面板自身始终直连**，见第 3 节的方向语义与第 4.7 节。

---

## 6. 终端设备接入方式

透明代理生效后，局域网设备需要把流量导向运行 AuroraMihomo 的主机。
四种方式，按侵入性从低到高排列。

### 6.1 手动代理（最简单，无需透明代理）

不改任何网络设置，各设备自己填代理地址。适合只有少量设备、或想先验证
节点可用性的场景。

mihomo 侧需要：

```yaml
mixed-port: 7890
allow-lan: true
bind-address: "*"
```

- **macOS**：系统设置 → 网络 → 选中网卡 → 详细信息 → 代理 → 填 HTTP/HTTPS 代理
- **iOS / Android**：Wi-Fi → 当前网络 → 配置代理 → 手动，主机填面板主机 IP，端口 7890
- **Windows**：设置 → 网络和 Internet → 代理 → 手动设置代理

局域网不可信时应配 `authentication` 与 `lan-allowed-ips`，否则同网段任何
设备都能用你的代理。

### 6.2 只改 DNS（改动小，分流能力有限）

路由器 DHCP 只下发 DNS 指向面板主机，配合 `enhanced-mode: fake-ip` 与
`dns-hijack`。访问 fake-ip 段的流量会被路由到面板主机。

**注意**：Android 的"私人 DNS"（Private DNS）会绕过 DHCP 下发的 DNS，
必须在系统设置里关闭，否则分流不生效。

### 6.3 网关模式（把面板主机设为局域网网关）

面板主机需要开启转发：

```bash
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1
# 持久化：写入 /etc/sysctl.d/99-aurora.conf
```

终端设备二选一：

- 路由器 DHCP 把网关（option 3）与 DNS（option 6）都指向面板主机
- 各设备手动改网关与 DNS

若终端设备接在 Linux 网桥后面，还需要 `br_netfilter` 模块与
`net.bridge.bridge-nf-call-iptables=1`。

### 6.4 旁路由 / 单臂路由

面板主机只有一块网卡，与主路由在同一网段。主路由继续负责 DHCP，
但把网关与 DNS 指向面板主机；面板主机自己的网关仍指向主路由。

三个常见陷阱：

1. **不对称路由**：终端发给旁路由、主路由却直接回包。需要
   `net.ipv4.conf.all.rp_filter=0`，必要时在旁路由上做 MASQUERADE。
2. **ICMP 重定向**：主路由会告诉终端"绕过旁路由直连"，需要
   `net.ipv4.conf.all.send_redirects=0`。
3. **DHCP 冲突**：不要在旁路由上再开一个 DHCP 服务。

### 验证方法

不要只看浏览器能否上网（可能命中缓存或直连规则）。

```bash
# 出口 IP 是否为节点 IP
curl -s https://api.ipify.org; echo
# DNS 是否被接管（fake-ip 模式下应返回 198.18.x.x 段）
nslookup www.google.com
```

---

## 7. 部署形态对透明代理的影响

### 二进制部署

TUN 与 TProxy 都需要 `CAP_NET_ADMIN`。三种做法：

```bash
# 1. 以 root 运行（最简单，权限最大）
sudo ./auroramihomo

# 2. 给二进制附加 capability（推荐，权限最小）
sudo setcap cap_net_admin,cap_net_bind_service=+ep ./auroramihomo

# 3. systemd 授予（见部署文档的 unit 示例）
```

注意：mihomo 内核是被面板拉起的子进程，会继承父进程的权限。
若用 `setcap` 方案，需要同时给内核二进制加上（内核路径见
`<ConfigDir>/bin/mihomo`），或让面板以 root 运行。

### Docker 部署

默认镜像以非 root 用户（uid 10001）运行，**开箱不支持透明代理**。
要启用需要改 compose，代价是降低容器隔离性：

```yaml
services:
  aurora:
    network_mode: host          # 必须：桥接网络里的规则只对容器自己生效
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun   # TUN 模式必须映射设备
    user: "0:0"                 # 必须：非 root 拿不到 cap 的 effective 位
    # security_opt:
    #   - no-new-privileges:true  # 必须注释掉：与 file capability 方案互斥
```

四点都是必需的，缺任何一项都会失败，且失败信号不同：

| 缺失项              | 症状                                   |
| ------------------- | -------------------------------------- |
| `network_mode: host`| 规则生效但局域网其它设备不受影响       |
| `cap_add`           | 检测报告 `capNetAdmin: false`（bounding 也是 false） |
| `devices`           | 检测报告 TUN 设备未找到                |
| `user: "0:0"`       | `capNetAdminBounding: true` 但 `capNetAdmin: false` |

另外，host 网络下容器内改 sysctl 会影响宿主，Docker 会拒绝
`--sysctl net.ipv4.ip_forward`，需要在宿主上设置。

---

## 8. 接口

三个端点，均需鉴权（`protected` 组）。

| 方法 | 路径                            | 说明                             |
| ---- | ------------------------------- | -------------------------------- |
| GET  | `/api/v1/transparent/status`    | 当前开关 + 环境检测报告          |
| PUT  | `/api/v1/transparent`           | 修改开关与模式                   |
| POST | `/api/v1/transparent/confirm`   | 确认网络正常，取消自动回滚       |

环境不支持时 `PUT` 返回 400，错误信息里带上缺什么与修复命令，前端直接展示。

状态每次都重新探测而不缓存：用户可能刚按提示装完依赖就回来点开关，
缓存会让他看到过期的结论。

---

## 9. 配置注入

开关状态在配置合并的最后一步（`GenerateYAML` 之前）覆写到最终配置，
而不是写进用户的 base 配置。后者用户可以随手改掉，开关状态与实际配置
就会不一致。

TUN 模式注入：

```yaml
tun:
  enable: true
  stack: mixed
  auto-route: true              # 必须显式为 true
  auto-detect-interface: true
  auto-redirect: true           # 仅 Linux，让 mihomo 自管防火墙
  dns-hijack:
    - any:53
```

`auto-route` 必须显式写 `true`：不接管路由的 TUN 等于没开，而 mihomo
对此不报错，只是静默不生效。

TProxy 模式注入 `tproxy-port` 与 `routing-mark: 0xff`（与防火墙规则里的
放行标记一致）。

**关闭时同样要主动写 `tun.enable: false`**，只是"不写 tun 段"不够——
上次开启时留在磁盘上的配置不覆盖就等于没关掉。

同理，关闭时 `tproxy-port` 会被清零、`routing-mark` 在等于我们注入的
`0xff` 时被清掉（不等于则保留，那是用户自己配的）。这带来一个用户可见
的后果：**在「基础配置 → 端口设置」里手填的 `tproxy-port` 会被开关覆盖**
——开关关着时该值在生成配置时被清空，开关开着走 TProxy 时实际端口取
透明代理页的设置值。这是"开关说了算"的必然结果，但表单上不写清楚用户
只会发现自己填的端口不生效且查不到原因，所以该字段的 help 明确说明了
这一点。`redir-port` 不在注入范围内，填了就一定生效。

两个已知的字段冲突，注入时会处理：

- `auto-detect-interface` 与 `interface-name` 互斥（注入时清空后者）
- `route-address-set` 与 `routing-mark` 互斥（mihomo 文档明确指出）

---

## 10. 已知限制

- **Windows 不支持**。TProxy 是 Linux 特性；TUN 在 Windows 上需要
  Wintun 驱动，且权限模型与 Unix 差异较大，目前未覆盖。
- **macOS 只有 TUN**，且必须 root（macOS 没有 capability 机制）。
  `redir-port` 在 macOS 上需要自己写 pfctl `rdr` 规则，而 pf 没有 UDP
  重定向也没有 TPROXY 等价物，因此没有实现。
- **`redir-port` 不是一键模式**，面板不为它下发或清理 iptables `nat`
  规则，完整理由见 §1「为什么不做第三种模式（redir）」。
- **规则不持久化到重启**。宿主重启后 nftables 规则与策略路由都会消失。
  未确认状态由 `RecoverPending` 处理，超时的直接回滚；已确认的启用由
  `ReconcileState` 处理，启动时探测 `aurora_tproxy` 表是否还存在，
  不存在则回落为关闭——不会静默重新下发规则（那等于绕过了确认窗口），
  用户需要重新走一次启用流程。两者均在启动时、配置合并之前执行。
- **同时存在其它 VPN 的 TUN 设备**时可能冲突。
- **iptables legacy 与 nft 后端混用**的机器上，规则可能看着存在却永不匹配。
  检测会报告当前后端，但无法自动解决。
- **本机接管不可单独关闭**。两种模式都会一并接管本机自身的流量，
  没有"只代理局域网设备"的开关（见第 5 节）。
- **回环 DNS 下本机域名分流不生效**。`nameserver` 指向 `127.0.0.53` 这类
  回环 stub 时，本机 DNS 不经 mihomo，本机流量只按 IP 分流。检测会告警，
  但不会替用户改 `resolv.conf`（那是系统级配置，且 systemd-resolved
  会覆写它）。局域网设备不受影响。
- **IPv6 仅在宿主确实能走 v6 时才接管**。缺全局 v6 地址或 v6 默认路由时
  只接管 IPv4 并告警。这是刻意的：下发 v6 规则却没有 v6 策略路由会让
  v6 流量被打标后无路可走，比不接管更糟。

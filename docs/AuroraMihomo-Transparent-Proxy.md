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

面板只做**只读探测**，不代为安装依赖、不修改 sysctl。理由：这些操作需要
root、会改动宿主状态，而面板可能跑在容器里（容器内装的包重启即丢），
代为执行既不可靠也不可审计。

缺依赖时界面给出可直接复制的命令，由使用者自行决定是否执行。

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
| sysctl | `ip_forward`、`rp_filter`、`route_localnet`                           |

### 容器里最容易踩的一个坑

`docker-compose.yml` 里写了 `cap_add: NET_ADMIN` **不等于**进程真的持有该
权限。`cap_add` 只填充 bounding 与 permitted 集；非 root 用户运行且二进制
没有 file capability 时，effective 集仍是空的。

检测结果会把这两种情况分开报告：

- `capNetAdmin: false` + `capNetAdminBounding: false` → 根本没给权限
- `capNetAdmin: false` + `capNetAdminBounding: true` → **给了但拿不到**

后者的修复方式完全不同（见第 6 节 Docker 部分），所以必须区分。

### 依赖安装命令

```bash
# Alpine
apk add --no-cache iptables ip6tables nftables iproute2

# Debian / Ubuntu
apt-get update && apt-get install -y --no-install-recommends iptables nftables iproute2
```

Alpine 最小镜像可能只带 `iproute2-minimal`，它不足以支撑策略路由，
需要显式安装完整的 `iproute2`。

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

1. **放行管理端口**（SSH 22、面板 8899、内核 API 9090）
   顺序错了会在规则生效瞬间劫持 SSH 与面板连接，操作者再也无法关掉开关。
2. **放行内核自身出站**（`meta mark 0xff return`）
   不放行则 mihomo 发出的包被自己的 TPROXY 规则再次捕获，形成自环，
   对应一个已知的高 CPU 故障。这里的 `0xff` 必须与配置里的 `routing-mark` 一致。
3. **劫持 DNS**（UDP/53）
   必须在局域网放行之前，否则同网段的 DNS 查询直接放行，域名类分流规则失效。
4. **放行局域网网段**
5. **其余 TCP/UDP 交给 TPROXY**

`output` 链必须用 `type route hook output`，否则改了 mark 也不会重新查路由，
本机自身的 UDP 出不去。

### 策略路由

```bash
ip rule add fwmark 1 table 100
ip route add local 0.0.0.0/0 dev lo table 100
```

`local` 路由类型是关键：它让内核把打了标记的包当作发往本机的流量投递，
而不是继续转发。

### 应用顺序

1. `nft --check -f -` 干跑校验（只解析不应用，能挡住语法错误与内核不支持的表达式）
2. 建立策略路由
3. 下发规则

第 2 步必须在第 3 步之前。反序会出现"规则已生效、路由未就绪"的黑洞，
表现就是应用瞬间断网。任一步失败都会自动拆除已完成的部分。

---

## 4. 防"锁死自己"

TProxy 规则配错会让操作者失去对机器的访问，而那时他已经无法通过面板关掉
开关。四层防护：

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

---

## 5. 终端设备接入方式

透明代理生效后，局域网设备需要把流量导向运行 AuroraMihomo 的主机。
四种方式，按侵入性从低到高排列。

### 5.1 手动代理（最简单，无需透明代理）

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

### 5.2 只改 DNS（改动小，分流能力有限）

路由器 DHCP 只下发 DNS 指向面板主机，配合 `enhanced-mode: fake-ip` 与
`dns-hijack`。访问 fake-ip 段的流量会被路由到面板主机。

**注意**：Android 的"私人 DNS"（Private DNS）会绕过 DHCP 下发的 DNS，
必须在系统设置里关闭，否则分流不生效。

### 5.3 网关模式（把面板主机设为局域网网关）

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

### 5.4 旁路由 / 单臂路由

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

## 6. 部署形态对透明代理的影响

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

## 7. 接口

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

## 8. 配置注入

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

两个已知的字段冲突，注入时会处理：

- `auto-detect-interface` 与 `interface-name` 互斥（注入时清空后者）
- `route-address-set` 与 `routing-mark` 互斥（mihomo 文档明确指出）

---

## 9. 已知限制

- **Windows 不支持**。TProxy 是 Linux 特性；TUN 在 Windows 上需要
  Wintun 驱动，且权限模型与 Unix 差异较大，目前未覆盖。
- **macOS 只有 TUN**，且必须 root（macOS 没有 capability 机制）。
  `redir-port` 在 macOS 上需要自己写 pfctl `rdr` 规则，而 pf 没有 UDP
  重定向也没有 TPROXY 等价物，因此没有实现。
- **规则不持久化到重启**。宿主重启后 nftables 规则与策略路由都会消失。
  面板重启时会检测到未确认状态并回到关闭；已确认的启用需要重新开启。
- **同时存在其它 VPN 的 TUN 设备**时可能冲突。
- **iptables legacy 与 nft 后端混用**的机器上，规则可能看着存在却永不匹配。
  检测会报告当前后端，但无法自动解决。

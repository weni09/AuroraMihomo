# AuroraMihomo 透明代理真机测试报告

本文记录在真实 Linux 主机上对透明代理功能的验证，包括环境检测、
TUN 模式、TProxy 模式（含防"锁死自己"机制）、宿主重启后的行为、
自动准备环境，以及本机自身流量被接管这条路径（第 8 节）。

在本次测试之前，项目里 `netcheck`/`transparent_service` 相关的测试全部
基于 mock（假路径、假 `exec.Command` 返回值），没有一处在真实内核上
执行过 `nft`/`ip` 命令。这是第一次真机验证，过程中发现并修复了两个
真实问题：一个环境检测 bug（第 4 节）与一个状态一致性缺口（第 6.3 节），
均已在同一台测试机上完成回归验证。

设计依据见 [AuroraMihomo-Transparent-Proxy.md](./AuroraMihomo-Transparent-Proxy.md)，
本文只记录测试过程与结论，不重复其设计说明。

---

## 1. 测试环境

| 项目       | 值                                                              |
| ---------- | --------------------------------------------------------------- |
| 主机       | Proxmox 虚拟机，2 vCPU / 2.2G 内存 / 49G 磁盘                    |
| 系统       | Ubuntu 24.04.4 LTS (Noble Numbat)                                |
| 内核       | 6.8.0-124-generic                                                |
| 网络       | 局域网 192.168.1.128/24，网卡 `ens18`                            |
| 部署方式   | 两轮：① 二进制（第 2–6 节）② Docker（第 7 节）                     |
| 初始状态   | 全新系统，未安装 `nftables`/`iptables`/`iproute2`（仅内置最小 `iproute2`） |

第一轮用二进制部署：本机（Windows）交叉编译后 SFTP 上传，非 systemd，
手动 `nohup` 前台跑。第二轮在同一台机器上装 Docker 重测容器部署形态。

部署布局对齐 `scripts/install.sh` 的默认目录结构：

```
/opt/auroramihomo/
├── auroramihomo          # 交叉编译产物：GOOS=linux GOARCH=amd64 CGO_ENABLED=0
├── etc/aurora-api.yaml   # 基于仓库默认配置，未作改动（Host: 0.0.0.0, Port: 8899）
├── public/               # 前端 `npm run build` 产物（webRoot() 运行时读取，非 embed）
└── data/                 # 首次启动自动生成，含自动下载的 mihomo 内核
```

依赖安装：

```bash
apt-get update
apt-get install -y --no-install-recommends iptables nftables iproute2
modprobe nft_tproxy
```

安装后确认：`iptables --version` 输出 `(nf_tables)` 后端，与系统的 `nft`
一致，不存在文档第 9 节提到的"legacy 与 nft 混用"风险。

---

## 2. 基础功能 smoke test

在进入透明代理测试前，先验证部署本身与核心功能可用：

- 面板 HTTP 服务：`http://192.168.1.128:8899/healthz` 返回 `status: ok`
- 登录：`POST /api/v1/auth/login` 用 `initial_password.txt` 里的初始密码成功换取 JWT
- mihomo 内核首次启动自动下载成功（验证了本机能访问 GitHub 拉取 release）
- 订阅 CRUD：创建/查询/删除一条订阅，行为符合预期
- Zashboard 内嵌面板：`/api/v1/dashboard/entry` 正确返回带 `hostname/port/secret` 的入口地址，`/ui/` 可从局域网正常访问

### 一个值得记录的运维细节：改 `external-controller` 后必须重启内核

测试中把 `external-controller` 从默认的 `127.0.0.1:9090` 改成 `0.0.0.0:9090`
再"保存本地配置并重新生成最终配置"（`ApplyLocalOnly` → 热重载路径），
`config.yaml` 已经写入新值，但 `ss -tlnp` 显示 mihomo 进程仍监听着旧的
`127.0.0.1:9090`。

原因：`ConfigService.MergeAndApplyDetailed` 的热重载走的是 mihomo 的
`PUT /configs`（RESTful 配置更新），而 `external-controller` 属于 mihomo
启动参数级别的配置，不在 `PUT /configs` 可热更新的字段范围内——这是
mihomo 内核自身的行为，不是本项目的 bug。额外调用一次
`POST /api/v1/mihomo/restart`（重启内核进程）后，新的监听地址才真正生效。

不算缺陷，但建议：界面上修改 `external-controller`（或其它同类"仅生效于
启动时"的字段）保存成功后，可以提示用户"需要重启内核才能生效"，
否则容易让人以为改了不生效是 bug。本次测试未改动代码或文档来处理这一点，
仅记录在此，留给后续评估是否需要加提示。

---

## 3. 环境检测核对

`GET /api/v1/transparent/status` 首次探测结果：

```json
{
  "root": true,
  "capNetAdmin": true,
  "capNetRaw": true,
  "capNetAdminBounding": true,
  "inContainer": false,
  "hostNetwork": true,
  "tunDevice": "/dev/net/tun",
  "modes": [
    { "mode": "tun", "available": true },
    { "mode": "tproxy", "available": false,
      "reason": "缺少必要工具：iproute2（当前是 busybox 内置的 ip）" }
  ],
  "warnings": ["net.ipv4.ip_forward 为 0，..."]
}
```

root/capability/容器/TUN 设备的判定均与实际环境相符，`ip_forward=0` 的
告警符合预期（未手动开启网关转发）。

但 TProxy 被误判为不可用，理由是"busybox 的 ip"——而这台机器装的是
标准 apt 源的 iproute2 6.1.0，`ip -V` 能正常输出版本串。这是一个真实的
检测 bug，见第 4 节。

---

## 4. 发现的 bug：`ip --version` 探测误判

### 现象

`isRealIproute2()`（`backend/internal/netcheck/detect_linux.go`）用
`ip --version`（长选项）判断是否为真正的 iproute2。在这台 Ubuntu
24.04 上手工执行：

```
$ ip -V
ip utility, iproute2-6.1.0, libbpf 1.3.0

$ ip --version; echo "exit=$?"
Option "-version" is unknown, try "ip -help".     # 这行在 stderr
exit=255
```

iproute2 6.1.0 的 `ip` 不认 `--version`（它把长选项归一化成 `-version`
再当未知选项拒绝），只认 `-V`。探测代码用
`exec.Command(...).CombinedOutput()` 会把 stderr 一并收进来，因此拿到的
是那句报错文字而非空串（`len(out) != 0`，走不到返回 `""` 的分支），
其中不含 `"iproute2"` 字样，导致 `strings.Contains(out, "iproute2")`
为 false，把明明装着的正版 iproute2 误判成 busybox 的 `ip` 替代品，
进而把 TProxy 模式整体判定为不可用。

此前的单元测试（`detect_linux_test.go`）测不出这个问题：`fakeEnv` 的
`version` mock 函数签名是 `func(name string, _ ...string) string`，
直接忽略了传入的 flag 参数，只按命令名返回固定字符串，无法区分
"同一个命令在不同 flag 下行为不同"这种真机才会暴露的情况。

### 修复

- `isRealIproute2()` 改为优先尝试 `-V`，失败（不含 "iproute2"）再回落尝试
  `--version`，兼容两种情况。
- 扩展 `fakeEnv` 的 mock，支持按"命令+参数"精确匹配返回值
  （新增 `versionArgs` 字段），使测试能够模拟"不同 flag 不同输出"。
- 新增回归测试 `TestTProxyAcceptsIproute2WhenLongFlagUnsupported`，
  复现真机上 `-V` 正常、`--version` 报错的组合，断言此时 TProxy 应判定可用。

修复后重新交叉编译、部署到测试机，`GET /transparent/status` 的 TProxy
判定变为：

```json
{ "mode": "tproxy", "available": true,
  "reason": "可用。注意：面板会修改宿主防火墙与路由，首次启用请确保有控制台或物理访问手段" }
```

涉及文件：

- `backend/internal/netcheck/detect_linux.go`（修复）
- `backend/internal/netcheck/detect_linux_test.go`（mock 扩展 + 回归测试）

用交叉编译的 Linux 测试二进制在目标机上跑过 `go test`（`-c` 产出可执行
文件后上传执行，因为宿主是 Windows 无法直接跑 `//go:build linux` 的
测试）：新增的回归测试通过。另发现一个与本次改动无关的既有测试失败
（`TestTUNDistinguishesBoundingOnlyCapability`，容器场景下"设备缺失"分支
先于"bounding capability 提示"分支返回，导致断言的关键字找不到），
不在本次范围内处理，仅记录以便后续排查。

---

## 5. TUN 模式验证

| 步骤 | 操作 | 结果 |
| --- | --- | --- |
| 1 | `PUT /transparent` 启用 TUN | 返回 `pendingConfirm: true, secondsLeft: 88` |
| 2 | 检查网卡与路由 | `ip link show` 出现 `Meta` 网卡；`ip route` 出现 `198.18.0.0/30 dev Meta` |
| 3 | 验证面板仍可访问 | `/healthz` 正常返回 |
| 4 | `POST /transparent/confirm` | 返回 `pendingConfirm: false` |
| 5 | `PUT /transparent` 关闭 | 返回成功 |
| 6 | 检查网卡与路由是否清理 | `ip link show` 不再有 `Meta`；`ip route` 不再有 `198.18.0.0/30` |

结论：TUN 模式的生效与清理均由 mihomo 自身管理，面板侧的开关只是
透传，行为符合设计文档第 1 节的描述。全程未观察到异常。

---

## 6. TProxy 模式验证

这是本次测试的核心，因为它涉及面板直接修改宿主防火墙与策略路由，
理论上存在"配错规则导致 SSH 与面板同时失联"的风险。测试前确认了
除 SSH 外还有 VNC/控制台可用作备用访问手段。

### 6.1 场景 A：故意不确认，验证 90 秒自动回滚

开启前先手工加一条与透明代理无关的 nftables 表，用于后续验证"绝不
整体 flush"：

```bash
nft add table inet mytest
nft add chain inet mytest testchain
nft add rule inet mytest testchain counter comment 'unrelated-test-rule'
```

启用 TProxy 后立即检查规则实际内容：

```
table inet aurora_tproxy {
    chain prerouting {
        type filter hook prerouting priority mangle; policy accept;
        tcp dport 22 return
        tcp dport 8899 return
        tcp dport 9090 return
        meta mark 0x000000ff return
        socket transparent 1 meta mark set 0x00000001 accept
        udp dport 53 meta mark set 0x00000001 tproxy to :7893 accept
        ip daddr 10.0.0.0/8 return
        ... (其余 LAN 网段 return)
        meta l4proto { tcp, udp } meta mark set 0x00000001 tproxy to :7893 accept
    }
    chain output {
        type route hook output priority mangle; policy accept;
        ...
    }
}
```

规则顺序与设计文档第 3 节完全一致：管理端口豁免在最前，其次内核自身
出站放行，然后 DNS 劫持，再放行局域网，最后才是兜底的 TPROXY 转发。
`ip rule show` 确认策略路由 `32765: from all fwmark 0x1 lookup 100`，
`ip route show table 100` 确认 `local default dev lo`。

此时 SSH 与面板（`curl /healthz`）均正常访问。

**故意在 90 秒窗口内不做任何确认操作**，等待超时。结果：

```
{"@timestamp":"...T22:17:59...","content":"透明代理未在时限内确认，自动回滚","level":"error"}
{"@timestamp":"...T22:17:59...","content":"透明代理规则已拆除","level":"info"}
{"@timestamp":"...T22:17:59...","content":"透明代理已关闭","level":"info"}
```

超时后核对：

- `nft list table inet aurora_tproxy` → `No such file or directory`（已删除）
- `ip rule show` → 只剩系统默认三条，`fwmark 0x1` 规则已消失
- `nft list table inet mytest` → **完好无损**，验证了"绝不整体 flush"

**结论：核心防护机制（90 秒未确认自动回滚）按设计正确工作，且回滚精确
针对面板自建的表，不影响宿主上其它无关规则。**

### 6.2 场景 B：正常确认

再次启用并在窗口内 `POST /transparent/confirm`，确认后 `pendingConfirm`
变为 `false`。复查规则仍然生效，SSH、面板（8899）在整个过程中均可
直接访问，符合"管理端口豁免"的设计。

### 6.3 场景 C：宿主重启后的行为（发现问题并已修复）

在已确认启用的状态下，重启整台虚拟机（`reboot`）。重启前记录：

```bash
nft list ruleset > /tmp/pre_reboot_ruleset.txt   # 42 行规则
```

重启后（`uptime -s` 确认确实重启过）：

```bash
$ nft list ruleset
（空）
$ ip rule show
0:      from all lookup local
32766:  from all lookup main
32767:  from all lookup default
```

规则与策略路由确实随宿主重启完全消失（包括手工加的 `mytest` 表，这是
内核层面 nftables 状态本身不持久化的正常行为，不是面板的锅）。

由于本次是手动 `nohup` 启动、未配置 systemd，面板进程本身也不会随
宿主重启自动拉起，手动重新启动面板后，检查状态接口：

```json
{ "enabled": true, "mode": "tproxy", "pendingConfirm": false, ... }
```

**这里出现了设计文档第 9 节没有覆盖到的情况**：接口报告"已启用、
无待确认"，界面会照常显示"透明代理已开启（TProxy）"，但此时宿主上
一条相关规则都没有——网络实际上完全没有被接管，界面呈现与真实状态
不一致。

代码层面确认过原因：`TransparentService.RecoverPending()` 只处理
`transparent.pending_until` 非空（即"启用后还没来得及确认"）的情况；
而这次是"已经确认过"，`pending_until` 早被 `Confirm()` 清空，启动流程
里没有任何步骤会去检测"规则是否还真实存在于宿主上"。文档第 9 节写的
"已确认的启用需要重新开启"是对用户操作层面的建议，但代码没有主动
提示或纠正这个不一致——用户如果不主动去点开关，会一直以为透明代理在
生效。

验证了"关闭"操作在这种不一致状态下仍能正常工作（`Teardown` 对"规则本来
就不存在"是幂等的，不会报错），只是"状态显示"本身有误导性。

#### 修复：`ReconcileState`

新增 `TransparentService.ReconcileState`，在启动流程里紧跟
`RecoverPending` 之后调用（两者分工不同：`RecoverPending` 处理"启用后
还没确认"，`ReconcileState` 处理"已经确认过，但规则可能已经因为宿主
重启而失效"）。

只在 `enabled=true`、`mode=tproxy`、且没有待确认记录时才会介入——TUN
模式的路由与防火墙完全由 mihomo 自己在每次启动时按 `config.yaml` 重建，
不存在这个缺口，不需要也不应该被这段逻辑碰。

核实方式是新增的 `Applier.RulesActive(ctx)`：执行
`nft list table inet aurora_tproxy`，成功即规则还在；报错信息里出现
"No such file"/"does not exist" 视为"规则已正常消失"（不算探测出错）；
其它类型的错误（如 `nft` 命令本身不可执行）不能贸然断言"规则不存在"，
保守起见维持现状不做改动，只留日志——避免把"探测失败"误判成"确实没
规则"，反而在环境有临时故障时错误地关掉一个原本仍然有效的开关。

探测到规则确实消失后，处理方式是回落为关闭，而不是静默重新下发规则：
重新下发等于绕过了启用时本该有的 90 秒确认窗口，与"规则变更必须经用户
确认"这条设计原则相悖。用户如果仍需要 TProxy，重新走一次正常的启用
流程即可。

实现上它只做"拆规则 + 落库"，刻意不复用 `disable()`：后者末尾会调
`reloadFn` 触发一次带远程刷新的合并，而 `ReconcileState` 只在启动流程里
被调用，紧随其后本来就有一次同样的合并。复用 `disable()` 会让每次"带
TProxy 开关的宿主重启"都白拉一遍所有订阅，且那一次合并用的是没有超时
约束的 `rootCtx`。配置由后续那次合并按新状态重新生成即可
（`InjectOptions` 读的就是这里刚写下的 `enabled=false`）。

涉及文件：

- `backend/internal/netcheck/firewall.go`（新增 `NFTRulesCheckCommand`）
- `backend/internal/netcheck/firewall_apply.go`（新增 `Applier.RulesActive`）
- `backend/internal/service/transparent_service.go`（新增
  `ReconcileState`，`transparentApplier` 接口新增 `RulesActive` 方法）
- `backend/api/aurora.go`（启动流程里紧跟 `RecoverPending` 调用）
- 对应的单元测试（`firewall_apply_test.go` 新增 3 个用例，
  `transparent_service_test.go` 新增 4 个用例覆盖"规则已消失则回落"、
  "规则还在则不动"、"待确认阶段不介入"、"TUN 模式不介入"、"探测失败
  时保持现状"）

#### 真机回归验证

用与场景 A/B 相同的手法复现"已确认启用、规则却已消失"的状态：启用
TProxy 并确认，然后手工执行

```bash
nft delete table inet aurora_tproxy
ip rule del fwmark 1 table 100
ip route flush table 100
```

精确模拟宿主重启对 nftables/策略路由状态的清空效果（比真的重启一次
虚拟机更快、更可控），再重启面板进程（`pkill` 后重新拉起）。

修复前（第一轮测试）：状态接口报告 `enabled: true, mode: tproxy`，
与宿主实际状态不符。

修复后（本轮，用带 `ReconcileState` 的新二进制重新部署）：

```json
{ "enabled": false, "mode": "off", "pendingConfirm": false }
```

日志确认了完整的修正过程：

```
{"content":"检测到透明代理记录为已启用，但宿主上的防火墙规则已不存在（通常是宿主重启导致），回落为关闭状态。如需继续使用请重新启用","level":"error"}
{"content":"透明代理规则已拆除","level":"info"}
{"content":"透明代理已关闭","level":"info"}
```

**结论：修复后状态一致性问题已消除，界面不会再出现"显示已开启但宿主
上实际没有任何规则"的情况。**

### 6.4 场景 D：幂等性

- 关闭后重复调用关闭：正常返回成功，无报错
- 规则已被宿主重启清空后调用关闭（即场景 C 的后续）：同样正常返回，
  日志无异常，`Teardown` 的幂等设计（把"File exists"/"No such file"类
  错误视为成功）符合预期

### 6.5 场景 E：不误伤无关规则

已在场景 A 中验证：面板自建的表与手工添加的无关表`mytest`全程独立，
开启、超时回滚、确认关闭均未影响到`mytest`。

第 7 节的 Docker 轮次里还有一个更有说服力的天然对照：Docker 自己会往
宿主 nftables 写一整套规则（`table ip nat` 下的 `DOCKER` 链等）。整轮
TProxy 与 TUN 的开启、回滚、关闭过程中，这套规则始终完好——两个互不
知情的防火墙管理者共存而不互相破坏，比人造的 `mytest` 表更贴近真实场景。

---

## 7. Docker 部署形态验证

在同一台机器上装 Docker（`docker-ce` 29.6.2 + Compose v5.3.1，官方 apt 源）
后重测一遍容器部署。

### 7.1 镜像构建

`docker build -f docker/Dockerfile` 在 VM 上直接构建成功（多阶段：
node:22-alpine 构前端、golang:1.25-alpine 构后端、alpine:3.21 做运行镜像）。
产物 67.9MB。2G 内存的机器配 4G swap 足够跑完 `npm ci` + `vite build` +
`go build`，没有 OOM。

构建时 Docker 给了一条警告，属实但无害，可顺手清掉：

```
RedundantTargetPlatform: Setting platform to predefined $TARGETPLATFORM
in FROM is redundant as this is the default behavior (line 31)
```

### 7.2 默认配置（非 root，安全默认值）

`docker compose up -d` 后容器进入 `healthy`（镜像内置的 `curl /healthz`
健康检查生效）。验证通过的项：

- 从局域网访问 `http://192.168.1.128:8899/` 与 `/healthz` 均正常
- mihomo 内核自动下载成功。日志显示前两个 CDN 源失败后自动回退到第三个
  成功，**证实了多源回退机制在真实网络下确实起作用**
- Zashboard 资源自动下载，`/ui/` 返回 200
- 订阅 CRUD（含中文名 UTF-8 往返）正常
- 数据落到宿主挂载卷 `./data`，属主为 `10001:10001`，与 compose 注释里
  要求的 `chown -R 10001:10001 data` 一致
- `docker compose restart` 后：旧密码仍可登录、内核不重复下载，
  说明卷持久化有效

此时透明代理的环境检测结论（**全部符合预期**，是正确的"不可用"判定）：

```json
{
  "root": false, "capNetAdmin": false, "capNetAdminBounding": true,
  "inContainer": true, "hostNetwork": true, "tunDevice": "",
  "distro": "alpine", "packageManager": "apk"
}
```

- `capNetAdmin: false` + `capNetAdminBounding: true` —— 正是设计文档第 2 节
  重点描述的容器陷阱（`cap_add` 只填 bounding 集，非 root 拿不到 effective
  位），检测把两者区分开了
- TUN 不可用，理由指向缺 `devices: /dev/net/tun` 映射
- TProxy 不可用，理由是缺 `nft 或 iptables` 与真正的 `iproute2`——
  运行镜像只装了 `ca-certificates tzdata curl`，且 Alpine 的 `ip` 是
  busybox 内置的。这里报"当前是 busybox 内置的 ip"是**真判定**，
  已在容器内实测确认（`/sbin/ip` 属于 BusyBox v1.37.0），
  与第 4 节修复的那个误判不是一回事
- 安装提示按 `packageManager: apk` 正确给出了 Alpine 的 `apk add` 命令

### 7.3 按文档启用透明代理

照设计文档与部署文档说的四项改动改 compose（`user: "0:0"`、
`devices: /dev/net/tun`、注释掉 `security_opt: no-new-privileges`、
`network_mode: host` 本来就开着），`--force-recreate` 后检测变为：

```
root: True | capNetAdmin: True | bounding: True
tunDevice: '/dev/net/tun' | inContainer: True | hostNetwork: True
  tun:    available=True
  tproxy: available=False（仍缺 nft/iptables/iproute2）
```

**文档描述的四项改动与实际效果完全对得上。**

TUN 模式在容器里实测可用：启用后宿主的网络命名空间里出现 `Meta` 网卡与
`198.18.0.0/30` 路由（host 网络的效果得到验证），mihomo 还自建了
`table inet mihomo` 管自己的规则；关闭后网卡、路由、该表全部自动清理，
Docker 的 `table ip nat` 毫发无损。

TProxy 需要先补依赖。因为 `user: "0:0"` 下容器是 root，面板给出的那条
`apk add --no-cache iptables ip6tables nftables iproute2` 可以直接执行，
装完后 TProxy 判定即变为可用——**面板的安装提示在容器里是可操作的**。
补依赖后实测：规则正确下发到宿主（内容与第 6.1 节一致）、策略路由建立、
90 秒未确认自动回滚同样正常工作、Docker 规则始终完好。

顺带在 Alpine 的 iproute2 **6.11.0** 上复核了第 4 节那个 bug：
`ip -V` 正常输出、`ip --version` 同样以退出码 255 报
`Option "-version" is unknown`。这说明该行为不是 Ubuntu 6.1.0 的个例，
跨越很远的版本都一致，`-V` 才是唯一可靠的写法。同时确认 busybox 的 `ip`
在 `-V` 与 `--version` 两种写法下都不会输出 "iproute2" 字样，
所以修复里保留的回落尝试不会把 busybox 误判成真 iproute2。

### 7.4 镜像不预装 TProxy 依赖（已修复）

初次测试时容器镜像只装了 `ca-certificates tzdata curl`，因此**开箱状态下
TProxy 在 Docker 部署里始终不可用**，必须由使用者进容器手工 `apk add`；
而容器内装的包在镜像或容器重建后就丢了，得反复操作。

**已在 Dockerfile 里预装** `iptables ip6tables nftables iproute2`。

代价比预估的大：初版报告按容器内 `apk add` 前后的 `apk info -s` 差值估成
"约 4MB"，实际重新构建镜像后是 **67.9MB → 76.7MB，+8.8MB**。差在传递依赖
——包数从 34 个涨到 42 个，多出 `libmnl`、`libnftnl`、`jansson`、`readline`、
`ncurses-terminfo-base`、`gmp`、`libelf`、`libcap2`，以及 iproute2 拆出的
`iproute2-minimal`/`-ss`/`-tc` 子包。三个主包自身只有约 3MB。

顺带去掉了 7.1 节记录的 `RedundantTargetPlatform` 警告（运行镜像那层的
`--platform=$TARGETPLATFORM` 本就是默认行为），现在构建零警告。

**重新构建镜像后的实测结论**：

- 容器内 `ip -V` 输出 `ip utility, iproute2-6.11.0`（不再是 busybox），
  `nft`/`iptables`/`ip6tables` 均在 `/usr/sbin` 下
- 环境检测直接报 `tproxy: available=true`，**无需任何手工安装**
- 实际启用 TProxy 成功：规则与策略路由正确落到宿主，Docker 自身的
  `table ip nat` / `DOCKER` 链完好，90 秒未确认自动回滚照常工作
- 刻意**不映射** `/dev/net/tun` 做这轮测试，结果 TUN 报不可用而 TProxy
  可用——验证了"TProxy 不需要 TUN 设备"这个常被混淆的点

顺带确认了一件容易搞反的事：**TUN 模式并不需要这几个包**。mihomo 的
`auto-redirect` 直接走 netlink 与内核交互、自己维护 nftables 规则，不调用
这些命令行工具；7.3 节里 TUN 在只装了 `ca-certificates/tzdata/curl` 的镜像
里就能正常建 `Meta` 网卡与路由，就是证据。只有 TProxy 需要，因为那套 mangle
规则与策略路由是面板自己用 `nft` / `ip` 命令下发的。

---

### 7.5 自动准备环境按钮的真机验证

功能说明见设计文档 §2.1。这里记录在同一台机器上的实测过程。

**造缺依赖状态**：先 `apt-get remove nftables`，但 TProxy 仍报可用——
检测判据是"nft **或** iptables"，只卸一个不足以造出缺失态。再卸掉
`iptables` 后才如实变为不可用，`missing: ["nft 或 iptables"]`。
（顺带一提：卸 nftables 会连带卸掉 `docker-ce`，它依赖 nftables；
测试后已重装恢复。）

**装包**：`POST /transparent/provision`，`installPackages=true`。结果：

```
success: true   message: 完成 2 项，跳过 1 项（已满足）
[刷新软件源]     apt-get update                                    成功
[安装软件包]     DEBIAN_FRONTEND=noninteractive apt-get install…    成功
[写入 sysctl 配置]                                                跳过（已合规）
执行后重新探测: tproxy available=true
```

三点符合设计：`apt-get update` 确实在 install 之前；
`DEBIAN_FRONTEND=noninteractive` 真的带上了；**同一个响应里返回的
重新探测结果已变为可用**，不需要用户自己再刷一次。
装完随即启用 TProxy，规则与策略路由正常下发、90 秒自动回滚正常。

**sysctl（含 rp_filter 的 max 语义）**：把 `ip_forward` 置 0、
`conf.all.rp_filter` 与 `conf.ens18.rp_filter` 都置 1，造出不合规状态。
检测的告警文案正确点出了 `ens18`：

```
net.ipv4.conf.all.rp_filter 为 1（严格反向路径校验），会导致 TProxy 丢包，
建议设为 0 或 2；注意内核取 all 与各网卡的最大值，以下网卡也需一并调整：ens18
```

执行准备后写入 4 项并 `sysctl --system` 加载，回读 `/proc/sys/...`
确认**内核真实值**全部生效：

| 键 | 执行后 |
| --- | --- |
| `net.ipv4.ip_forward` | 1 |
| `net.ipv4.conf.all.rp_filter` | 2 |
| `net.ipv4.conf.default.rp_filter` | 2 |
| `net.ipv4.conf.ens18.rp_filter` | 2 |

这验证了那个最容易踩空的点：只改 `all` 不够，必须连具体网卡一起改。

**"改得越少越好"**：第一轮测试时这台机器的 `ip_forward` 已是 1、
`rp_filter` 已是 2，sysctl 步骤如实跳过并且**没有创建配置文件**
（`ls /etc/sysctl.d/99-auroramihomo.conf` 报不存在）。

**幂等**：连续再执行两次，两个步骤都标记为跳过、消息为"环境已满足条件，
无需改动"，配置文件里 `net.ipv4.ip_forward = 1` 仍只出现 1 次
（整体重写而非追加的效果）。

**手动命令**：`printf … | sudo tee /etc/sysctl.d/99-auroramihomo.conf`
与 `sudo sysctl --system`，可直接粘贴执行。sysctl 无需改动的那一轮里
只返回了装包命令，不会给出多余的空操作。

生成的配置文件自带注释说明每一项为什么需要，以及"删掉此文件并
`sysctl --system` 即可恢复发行版默认"的卸载方法。

测试后已把机器恢复原状：删掉 drop-in、`ip_forward` 置回 0、重装 Docker。
注意一个细节：**删掉 drop-in 并 `sysctl --system` 不会撤销已生效的运行时值**
（内核会保留到有人显式改回），所以恢复时需要手动 `sysctl -w` 置回。

---

## 8. 本机流量接管的专项验证

本轮针对"本机自身流量被接管"这条路径做专项验证。起因是审查时发现：
两种模式其实**一直都在接管本机流量**（TProxy 靠 `output` 链 + 策略路由发夹，
TUN 靠 `auto-route`），但这一行为既没写进文档，也从未被验证过——
前面第 5、6 节只检查了规则文本与 `/healthz` 可达，没有一处确认宿主上的
`curl` 是否真的从节点出去。

同一台测试机（Ubuntu 24.04，内核 6.8.0-124，`ens18`）。这台机器恰好具备
两个关键特征，使新逻辑能被真实触发：
`nameserver` 是 systemd-resolved 的 `127.0.0.53`，且有真实的 IPv6
全局地址与多路径 v6 默认路由。

先在这台机器上跑了全部 17 个后端包的单元测试（源码同步过去用系统 Go 执行，
而非交叉编译的测试二进制——后者找不到 `testdata` 会产生假失败），全部通过。

### 8.1 修掉两个长期存在的红测

跑单测时发现两个**与本次改动无关的既有失败**（在改动前的 `HEAD` 上复现，
确认不是本轮引入）：`TestTUNDistinguishesBoundingOnlyCapability` 与
`TestBridgeNetworkContainerWarnsOnScope`。

根因：两个用例往 `sysClassMiscTun` 写了一个**普通文件**来"模拟"TUN 设备，
但 `checkTUN` 用 `isCharDevice` 判定，普通文件过不了这一关。于是探测一路走到
"设备缺失"分支，那些本该校验 capability 提示与桥接网络范围提示的断言
全部落在错误的分支上。这类失败最坏的地方是它红得没有意义——
失败信息指向设备缺失，与用例意图无关，久而久之就会被当成噪声忽略。

改为把探测路径指向 `/dev/null`（任何 Linux 上都是真字符设备，CI 容器里也是），
两个用例随即通过并真正校验到它们本来要校验的东西。

### 8.2 D1：`output` 链管理端口放行方向错误（会断 SSH）

原实现在 `output` 链里只有 `tcp dport 22 return`。但 `output` 处理出站包，
sshd 对入站 SSH 的**回包**是 `sport=22`、`dport=` 客户端随机端口，
这条规则匹配不到它。

为了拿到直接证据，往两条链的对应位置插了计数器：

```
tcp dport 22 counter packets 0   bytes 0      comment "verify-dport22"
tcp sport 22 counter packets 164 bytes 35379  comment "verify-sport22"
```

`dport` 侧始终为 0，`sport` 侧随 SSH 会话持续增长——**修复前那 164 个回包
没有任何放行规则能接住它们**。

它此前没有暴露，是因为后面的 `ip daddr 192.168.0.0/16 return` 兜住了同网段
客户端（本次测试的 SSH 来源 192.168.1.121 正在这个网段内）。为验证"来源不在
私有网段就会断"，加了一块 `dummy` 网卡带 `100.64.0.2/24`（CGNAT 段，
不在任何 LAN 放行网段内），并在独立表 `d1repro` 里按**修复前的顺序**重建
`output` 链（用 `counter` 代替打标动作，以免真的断网），然后从该地址连本机 sshd：

```
tcp dport 22 counter packets 9 bytes 484  comment "prefix-dport-exempt"
tcp sport 22 counter packets 6 bytes 414  comment "WOULD-BE-HIJACKED"
```

6 个 SSH 回包穿过了所有 `return` 抵达本该打标的位置。真实规则里这些包会被打上
fwmark 1、经 `local` 路由当作本机流量投递而永远送不出去，SSH 当场断开。
修复后的链在 `tcp sport 22 return` 处就把同样的流量拦住了。

**结论：这是一个会导致"启用瞬间失去 SSH"的真实缺陷，已修复并双向验证
（修复前会断、修复后不断）。**

### 8.3 D2：本机 DNS 劫持与回环 stub

`output` 链原先完全没有 DNS 规则，本机 DNS 查询直接被放行，
域名类分流规则对本机流量全部失效。修复后加了打标规则并排除回环目标。
用计数器分别观测两类查询：

| 查询目标 | 命中规则 | 计数 |
| --- | --- | --- |
| 外部 DNS `223.5.5.5` | `dport 53 ip daddr != 127.0.0.0/8` | 3 包 |
| 回环 stub `127.0.0.53` | 被排除，不打标 | 2 包 |

与设计完全一致：外部 DNS 被接管，回环不碰（否则与 mihomo 自己的 DNS 自环）。
也正因如此，这台用 systemd-resolved 的机器上本机域名分流确实不生效——
环境检测如实告警了：

```
本机 DNS 指向回环地址 127.0.0.53（常见于 systemd-resolved），本机自身的
DNS 查询不会被劫持，域名类分流规则对本机流量不生效（局域网设备不受影响）。
需要本机也按域名分流时，可把 /etc/resolv.conf 的 nameserver 改为非回环地址，
或关闭 systemd-resolved 的 DNSStubListener
```

### 8.4 D3：IPv6 规则与家族限定

这台机器有真实 v6 出网能力，因此下发了完整 v6 规则。实测确认：

- `ip -6 rule` 出现 `from all fwmark 0x1 lookup 100`，
  `ip -6 route show table 100` 出现 `local default dev lo`
- 规则里出现 `ip6 daddr ::1/128 return`、`fe80::/10`、`fc00::/7`、`ff00::/8`
  以及 v6 的 DNS 打标
- TProxy 开启期间 `curl -6` 与 `curl -4` 均返回 200，两个家族都没断

v6 包确实在被打标（计数器显示 77 包命中兜底规则）且网络正常——因为 v6 策略路由
同时存在。反过来验证家族限定：在独立表里按"未启用 v6"的形态下发
`meta nfproto ipv4` 兜底规则，v6 流量 143 包**不命中**该规则，
说明没有 v6 出网能力时 v6 不会被打标，不会形成黑洞。

### 8.5 D4：面板自身出站的豁免

给 `meta mark 0xfe return` 位置插入计数器后触发一次
`GET /api/v1/update/check`（面板访问 `api.github.com`）：

```
meta mark 0x000000fe counter packets 19 bytes 2961 comment "panel-egress"
```

19 个包带着 `0xfe` 标记被放行，说明 `SO_MARK` 确实设上了、规则也确实匹配。
接口返回 `{"success":true,"message":"mihomo 已是最新；zashboard 已是最新..."}`，
面板出网未受影响。

### 8.6 D5：管理端口取实际配置

规则里放行的是 `22 / 8899 / 9090`，其中 8899 来自 `aurora-api.yaml` 的 `Port`、
9090 来自 `config.yaml` 的 `external-controller`，而非写死的常量。
单测另外覆盖了改成 8443/19090 的情形与"内核端口每次现取"（用户改完端口
重新启用应放行新端口）。

### 8.7 D6：拆除后 v6 策略路由残留（真机新发现）

**这是本轮真机测试发现的、mock 测不出来的问题。** 关闭透明代理后核对系统状态：

```
=== POST-TEARDOWN v4 rule ===   (none - clean)
=== POST-TEARDOWN v6 rule ===
32765:	from all fwmark 0x1 lookup 100        ← 残留
=== POST-TEARDOWN v6 table 100 ===
local default dev lo metric 1024 pref medium  ← 残留
```

而日志照样报告成功：`透明代理规则已拆除` / `透明代理已关闭`。

根因：`Applier.Teardown(ctx, enableIPv6)` 按参数决定是否清理 v6，
而调用方 `TransparentService.disable()` 传的是**硬编码的 `false`**。
在没有 v6 能力的机器上这不会暴露（本来就没下发 v6 规则），
只有像这台一样具备 v6 出网能力的宿主才会留下残留。

修法不是"把 `false` 改成正确的值"，而是**去掉这个参数**：拆除一律清理 v4 与 v6。
理由是这类"拆除依赖记住启用时的参数"的设计天生易错——进程重启后、
数据库与实际状态不一致时，那个参数根本无从得知。而 v6 清理命令在没有 v6 规则时
只会返回"不存在"（早已被视为成功），无条件执行没有代价。

修复后复测，两个家族的策略路由都被完整清理：

```
=== POST-TEARDOWN v4 rule ===   (none - clean)
=== POST-TEARDOWN v6 rule ===   (none - clean)
=== POST-TEARDOWN table 100 v4/v6 === (both empty)
nft list table inet aurora_tproxy → Error: No such file or directory
```

### 8.8 既有防护未回归

同一轮里复核了前面几节验证过的机制，均未受本次改动影响：

- 90 秒窗口内确认 → `pendingConfirm` 转 `false`，规则保留，SSH 与面板全程可用
- 重复关闭幂等，第二次调用照常返回成功
- 手工创建的无关表 `mytest` 与 Docker 自己的 `nat`/`filter`/`raw` 表全程完好，
  关闭后依然存在——"绝不整体 flush"仍然成立
- 关闭后 `curl -4` / `curl -6` 均返回 200

测试后已清理所有临时探针（计数器规则、`d1repro` 表、`dummy` 网卡、
`mytest` 表、上传的测试二进制与源码），机器恢复到验证前状态。

---

## 9. 测试结论汇总

| 测试项 | 结果 |
| --- | --- |
| 基础功能（登录/订阅/内核下载/Zashboard 对接） | 通过 |
| 环境检测（root/capability/容器/TUN 设备判定） | 通过 |
| 环境检测（TProxy 的 iproute2 判定） | **发现 bug，已修复**（见第 4 节） |
| TUN 模式启用/确认/关闭/清理 | 通过 |
| TProxy 规则顺序与内容 | 通过，与设计文档一致 |
| TProxy 90 秒未确认自动回滚 | 通过（核心防护机制验证通过） |
| TProxy 管理端口豁免（SSH/面板/内核 API） | 通过，全程可访问 |
| TProxy 不误伤无关防火墙规则 | 通过 |
| TProxy 关闭操作幂等性 | 通过 |
| 宿主重启后规则清空 | 符合文档描述 |
| 宿主重启后面板状态一致性 | **发现问题，已修复并回归验证**（见 6.3） |
| Docker 镜像构建与容器启动 | 通过（2G 内存 + 4G swap 足够，无 OOM） |
| Docker 默认配置的环境检测判定 | 通过，容器 capability 陷阱被正确区分 |
| Docker 内核/面板资源自动下载 | 通过，多 CDN 源回退机制实测生效 |
| Docker 卷持久化与容器重启 | 通过，密码与内核均不丢 |
| Docker 下按文档四项改动启用 TUN | 通过，与文档描述完全一致 |
| Docker 下 TProxy（预装依赖后开箱可用） | 通过，含 90 秒自动回滚 |
| Docker 下 TProxy 不依赖 /dev/net/tun | 通过（刻意不映射设备复测） |
| Docker 与透明代理规则共存 | 通过，Docker 自身 nft 规则全程完好 |
| Docker 开箱支持 TProxy | **初测不支持，已在 Dockerfile 预装依赖并复测通过**（见 7.4） |
| 自动准备：装包并重新探测 | 通过，同一响应里模式即变为可用（见 7.5） |
| 自动准备：sysctl 含 rp_filter 逐网卡 | 通过，回读 /proc 确认内核真实值生效 |
| 自动准备：只改必要项 / 幂等 | 通过，已合规则跳过且不建文件，重复执行不堆积 |
| 全部 17 个后端包单元测试（真实 Linux） | 通过（见第 8 节） |
| 既有红测：TUN 假设备模拟错误 | **发现存量问题，已修复**（见 8.1） |
| 本机流量接管：output 链放行方向 | **发现会断 SSH 的缺陷，已修复并双向验证**（见 8.2） |
| 本机流量接管：本机 DNS 劫持与回环排除 | 通过，计数器确认外部 DNS 被接管、回环不碰（见 8.3） |
| 本机流量接管：回环 stub 告警 | 通过，systemd-resolved 机器上如实告警 |
| 本机流量接管：IPv6 规则与策略路由 | 通过，v4/v6 同时可用且不黑洞（见 8.4） |
| 本机流量接管：面板自身出站豁免 | 通过，`mark 0xfe` 计数增长且更新检查正常（见 8.5） |
| 管理端口取实际配置而非硬编码 | 通过（见 8.6） |
| 关闭后策略路由完整清理 | **发现 v6 残留，已修复并复测**（见 8.7） |

本次测试确认了透明代理的核心风险防护机制（90 秒确认窗口、持久化的
待确认状态、管理端口豁免、精确表删除而非整体 flush）在真实 Linux
内核上均按设计工作，这是此前只有 mock 测试时无法确认的；两种部署形态
（二进制、Docker）都走了一遍。同时发现并修复了两个真实问题：

1. `ip --version` 探测误判，导致 TProxy 在常见发行版上被错误地判定为
   不可用（第 4 节）。已在 iproute2 6.1.0（Ubuntu）与 6.11.0（Alpine）
   两个相隔较远的版本上确认该行为一致，并确认修复不会把 busybox 的 `ip`
   误判成真 iproute2。
2. TProxy 已确认启用后，若宿主重启导致规则失效，面板状态不会被重新
   校验，界面会一直显示"已开启"而实际网络完全没有被接管（第 6.3 节）。

第 8 节的专项验证针对"本机自身流量被接管"这条此前无文档、无验证的路径，
又发现并修复了三个问题，其中两个属于同一类根因——**规则的正确性依赖了
某个只在特定环境下才成立的巧合**：

3. `output` 链只按 `dport` 放行管理端口，匹配不到 sshd 的回包（回包里
   管理端口是源端口）。它没暴露纯粹是因为局域网网段的 `return` 兜住了
   同网段的 SSH 客户端；来源换成非私有网段就会在启用瞬间失去 SSH。
   已用计数器与 CGNAT 段的 dummy 网卡双向验证（第 8.2 节）。
4. 本机 DNS 从未被劫持，导致域名类分流规则对本机流量完全失效（第 8.3 节）。
5. 拆除时按"启用时有没有开 v6"的参数决定是否清理 v6，而调用方传的是
   硬编码的 `false`，于是有 IPv6 出网能力的宿主上关闭后残留 v6 策略路由，
   日志却报告拆除成功（第 8.7 节）。

第 5 项特别值得记录：它在没有 v6 能力的机器上永远不会暴露，
而项目此前的所有 mock 测试都不涉及真实的 `ip -6` 执行结果。
修法也从"把参数传对"改成"去掉这个参数、一律清理两个家族"——
拆除路径不该依赖记住启用时的状态，进程重启后那个状态无从得知。

另外修掉了两个与本次改动无关的存量红测（第 8.1 节）：它们用普通文件冒充
字符设备，导致探测走进"设备缺失"分支，断言全部落在错误的分支上。
这类"红得没有意义"的测试久而久之会被当成噪声，反而掩盖真实回归。

两处修复均已交叉编译部署到同一台测试机，用真实命令（而非仅靠单元测试
里的 mock）复现问题场景并验证修复生效，过程与结果记录在对应小节。

另有两项记录下来但**未改动代码**、留待评估的事项：

- 改 `external-controller` 后热重载不生效、需重启内核（第 2 节）。
  已在用户文档里补了提示，代码未动。
- Docker 开箱不支持 TProxy（第 7.4 节），两个可选方向已列出。

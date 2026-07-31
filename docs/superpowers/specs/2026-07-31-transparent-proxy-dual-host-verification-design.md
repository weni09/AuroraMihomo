# 透明代理双主机部署验证 —— 设计

日期：2026-07-31
状态：待实施

## 背景与范围界定

用户最初的表述是「考虑增加路由本机功能：使 mihomo 所在主机流量通过 mihomo」。
经代码核对，**这个功能已经完整实现并通过真机验证**，不是待开发项：

- `backend/internal/netcheck/`（13 文件）与 `backend/internal/service/transparent_service.go` 已实现
- 4 个 API 端点已接线：`GET /api/v1/transparent/status`、`PUT /api/v1/transparent`、
  `POST /api/v1/transparent/confirm`、`POST /api/v1/transparent/provision`
- `ServiceContext` 已构造、`aurora.go` 启动流程已调用 `RecoverPending` / `ReconcileState`
- 前端有 `stores/transparent.ts` 与 `SettingsView.vue` 的开关 UI
- 113 个单元测试（netcheck 83 + service 30），其中 100 个可在 Windows 上跑
- `docs/AuroraMihomo-Transparent-Proxy-Test-Report.md`（841 行）记录了在
  192.168.1.128（Ubuntu 24.04）上二进制与 Docker 两种形态的验证，期间修了 5 个真实缺陷

本机流量接管是两种模式的固定行为，无独立开关：TProxy 靠 `output` 链打 fwmark
配合 `ip rule` + `local` 默认路由发夹回 `prerouting`；TUN 靠 `auto-route: true`。

因此本任务的实质是**验证任务而非开发任务**：在两种部署形态（Ubuntu/Docker、
Alpine/二进制）上各跑一遍完整全流程，产出两份独立测试报告。Alpine 是全新平台，
此前从未验证过。

若测试中发现代码缺陷，先报告给用户，修复方案单独确认后再动代码，
不在测试过程中顺手修改。

## 目标与非目标

### 目标

1. 在 192.168.1.128（Ubuntu 24.04）以 Docker 形态部署，支持透明代理
2. 在 192.168.1.129（Alpine 3.24.1）以二进制形态部署，支持透明代理，
   并把 apk 源换成阿里云镜像
3. 两台各跑完整全流程测试（代理 + 透明代理），产出两份独立 Markdown 报告

### 非目标

- 不新增功能。「只代理局域网设备、不接管本机」的开关、`redir-port` 模式、
  防火墙规则持久化到宿主重启 —— 这些都是当前不支持的能力，属独立开发任务
- 不写自动化测试框架。一次性真机验证，框架成本高于收益
- 不修改 `no-new-privileges` 之外的安全默认值

## 方案选择

已评估三个方案，用户选定方案 A。

| 方案 | 内容 | 结论 |
|---|---|---|
| **A（选定）** | TProxy 为主线做完整验证，TUN 作对照各跑一轮启用/关闭 | 采纳 |
| B | TUN 为主线，TProxy 只验规则下发 | 否决：TUN 的接管由 mihomo 内部完成，本项目代码路径只到「注入配置」，验证不到 netcheck 的规则生成/下发/回滚 |
| C | 双模式对等全覆盖 | 否决：Alpine 仅 1 核 972M，反复切模式要重启内核与重新合并，耗时长中间态多，而增量收益集中在逻辑简单的 TUN 侧 |

选 A 的理由：TProxy 是纯 nftables + 策略路由，规则可见、可用计数器精确证明流量
走向、拆除可验证。既有报告的 5 个缺陷全部出在 TProxy 路径，说明这条路复杂度更高，
更值得在新平台上复验。

## 前置条件

两项由用户完成，是实施的硬性阻塞项：

1. **Alpine 磁盘扩容**。当前 `/dev/sda` ext4 直接挂 `/`，总容量 114.5M、已用 86%、
   仅剩 14.3M。部署需求约 55M（二进制 29M + 前端 3M + mihomo 内核 15M + 依赖包 5M）。
   扩容命令（`/dev/sda` 无分区表，不需要 growpart）：

   ```bash
   # Proxmox 宿主
   qm resize <VMID> scsi0 +4G
   # Alpine 内
   cat /sys/block/sda/size          # 未变则 echo 1 > /sys/block/sda/device/rescan
   resize2fs /dev/sda
   ```

2. **节点来源**。使用本地开发库 `data/aurora.db` 中已有的 2 条真实订阅
   （均已成功拉取节点，缓存 15176 / 573 字节）。测试时从本地库读出 URL 经 API 导入，
   不落地到任何文件。报告中节点信息脱敏：节点名保留，IP/域名/密码打码。

## 执行载体

所有远程操作由本地 Python + paramiko（5.0.0）驱动，目标机上不装任何测试框架。

```
Windows (D:\goWork\AuroraMihomo)
  ├─ 交叉编译 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 → auroramihomo
  ├─ npm run build → frontend/dist
  └─ paramiko SSH/SFTP
        ├─ 192.168.1.128  Ubuntu 24.04 / Docker 形态
        └─ 192.168.1.129  Alpine 3.24 / 二进制形态 + OpenRC
```

测试脚本作为临时产物放在仓库外的临时目录，不进提交。

**所有功能验证走面板的 HTTP API**（`/api/v1/transparent/*` 等），不直接调内部函数 ——
验证真实用户路径。底层状态用 `nft`、`ip rule`、`ss`、`curl` 等系统命令交叉确认。

## 两台主机的部署差异

| 项 | 128 Ubuntu / Docker | 129 Alpine / 二进制 |
|---|---|---|
| 清理 | 彻底清理：停旧进程、删 `/opt/auroramihomo`（含 data） | 全新，无需清理 |
| 包源 | 不动 | 换阿里云 `mirrors.aliyun.com/alpine/v3.24/{main,community}` |
| 依赖 | 镜像已预装 nftables/iproute2/ip6tables | `apk add nftables iproute2 ip6tables` |
| 权限 | compose 打开 `user: "0:0"`、注掉 `no-new-privileges`、`cap_add: NET_ADMIN`、`network_mode: host` | 直接 root 运行 |
| TUN 设备 | 打开 `devices: /dev/net/tun` | 宿主自带 |
| 进程管理 | `restart: unless-stopped` | OpenRC service 脚本（Alpine 无 systemd） |
| sysctl | 必须在宿主设（host 网络下 Docker 拒绝 `--sysctl`） | 面板「自动准备」写 `/etc/sysctl.d/99-auroramihomo.conf` |
| 内核二进制 | 容器内自动下载 | 自动下载 |

### 已知风险

1. **Docker 侧要关掉 `no-new-privileges:true`**。它与 `user: "0:0"` + NET_ADMIN
   的组合互斥（见 `docs/AuroraMihomo-Transparent-Proxy.md` §7）。测试环境可接受，
   报告中需明确标注这是安全降级。
2. **Alpine 的 `ip6tables` 是独立包**，漏装会导致 v6 相关操作失败。
3. **内核二进制的 capability 是既有空白**。面板以 root 运行时不受影响
   （内核作为子进程继承权限），但用 setcap 方案时需同时给
   `<ConfigDir>/bin/mihomo` 加 capability，代码无任何辅助。本次两台都以 root
   运行，规避此问题，但报告应记录。

## 测试场景清单

每台各跑一遍，5 组共 14 项。

### S0 部署与基础（4 项）

1. `/healthz` 返回 `status: ok`
2. `POST /api/v1/auth/login` 用 `initial_password.txt` 换取 JWT
3. 导入 2 条真实订阅并拉取节点成功
4. mihomo 内核启动 + `/api/v1/dashboard/entry` + `/ui/` 可访问

### S1 环境检测（2 项）

5. `GET /api/v1/transparent/status` 的 `Report` 与实际环境逐字段核对。
   Alpine 上重点复验 `ip -V` 判定（既有报告第 4 节修过的 bug，
   需在 iproute2 6.11 上确认不回归），以及容器 capability 区分（Docker 侧）
6. `POST /api/v1/transparent/provision` 装包 + 写 sysctl，
   同一响应里模式转为可用；回读 `/proc` 确认 sysctl 真实生效；重复执行幂等

### S2 代理基础能力（2 项，透明代理开启前）

7. 面板自身出网走 mihomo 代理，`PanelMark`（0xfe）生效
8. 通过 mihomo 混合端口手动代理：`curl -x` 出口 IP 为节点 IP

### S3 TProxy 主线（5 项）

9. **启用 → 90 秒不确认 → 自动回滚**（核心防护机制）
10. 启用 → 确认 → `nft list table inet aurora_tproxy` 规则内容与顺序核对
    （管理端口 → mark return → DNS 劫持 → 局域网 → 兜底，顺序即安全边界）
11. **本机流量真实接管**：

    ```bash
    curl -s https://api.ip.sb/ip     # 出口变为节点 IP
    dig +short google.com            # fake-ip 模式应返回 198.18.x.x
    nft list table inet aurora_tproxy | grep -A2 counter   # 计数器增长
    ip rule show | grep 'fwmark 0x1'                       # 策略路由在位
    ```

    出口 IP 需与 S2 第 8 项手动代理测得的节点 IP 一致，才能证明是同一条链路。
    若 mihomo 未启用 fake-ip，`dig` 判据改为「解析结果与直连 DNS 不同」
    或查 mihomo 连接日志确认查询经过内核，不以 198.18 段为唯一判据。

12. 管理端口豁免：全程 SSH 不断、面板与 9090 可访问。
    含从非局域网段验证 `output` 链的 sport 放行（既有报告 8.2 节的缺陷类型，
    仅靠同网段测试会被局域网 return 规则掩盖）。
    非局域网段的构造方式：在被测机上建 dummy 网卡并配 CGNAT 段
    （100.64.0.0/10）地址，从该地址发起到管理端口的连接，
    观察 nft 计数器确认命中的是管理端口 return 规则而非局域网 return 规则。
13. 关闭 → 规则与策略路由（含 v6）完整清理、出口 IP 恢复直连

### S4 TUN 对照（1 项）

14. 启用 → 确认 Meta 网卡与默认路由 → 出口 IP 变化 → 关闭并确认清理。
    TUN 与 TProxy 互斥（`mode` 是单值），切换前须确认上一模式已关闭且规则已清，
    避免两套接管机制叠加导致的现象无法归因。

## 交付物

1. `docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md`
2. `docs/AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md`

结构对齐既有报告：测试环境 → 部署过程 → 逐场景（命令 + 真实输出 + 结论）→
发现的问题 → 结论汇总表。

报告写作要求（与既有报告风格一致）：

- 贴真实命令输出，不编造、不美化
- 失败与意外如实记录，包括绕过方式
- 订阅与节点信息脱敏
- 若某场景未能验证，明确写「未验证」及原因，不含糊带过

## 失败与回滚

| 风险 | 处理 |
|---|---|
| 测试中失去 SSH | 90 秒确认窗口会自动回滚；两台都是虚拟机，最坏情况从 PVE 控制台介入 |
| 规则残留 | `nft delete table inet aurora_tproxy` + `ip rule del fwmark 1 table 100` + `ip route flush table 100`（v4/v6 都清） |
| Alpine 磁盘再次不足 | 清 apk cache；必要时改为本地上传 mihomo 内核而非面板下载 |
| 发现代码缺陷 | 先报告，修复方案单独确认后再改；不在测试过程中顺手修 |

## 验证标准

任务完成的判据：

1. 两台主机的面板均可正常访问、订阅可拉取、mihomo 内核运行
2. 两台主机的 TProxy 模式均验证到「宿主自身 curl 的出口 IP 变为节点 IP」
3. 90 秒自动回滚在两台上均验证通过
4. 关闭后两台的规则与策略路由均确认清理干净、出口 IP 恢复
5. 两份报告写完，逐场景有真实命令输出

14 项场景中若有未能验证的，不视为失败，但必须在报告中明确标注
「未验证」及具体原因（环境限制、依赖缺失、被其他缺陷阻塞等），
并说明是否影响上述 5 条判据。含糊带过或以「应该可以」代替实测结论，
视为任务未完成。

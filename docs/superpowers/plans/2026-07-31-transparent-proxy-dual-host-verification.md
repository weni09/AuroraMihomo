# 透明代理双主机部署验证 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Ubuntu 24.04（Docker 形态）与 Alpine 3.24（二进制形态）两台主机上部署 AuroraMihomo 并支持透明代理，各跑一遍完整全流程测试，产出两份独立测试报告。

**Architecture:** 本地 Windows 交叉编译 + paramiko SSH 驱动远程操作。128 用 `docker compose` 部署（host 网络 + NET_ADMIN + root），129 用二进制 + OpenRC 部署。所有功能验证走面板 HTTP API，底层状态用 `nft`/`ip`/`ss`/`curl` 交叉确认。TProxy 为主线，TUN 为对照。

**Tech Stack:** Go 1.26（交叉编译 GOOS=linux GOARCH=amd64 CGO_ENABLED=0）、Node 22（前端构建）、Docker 29.7、nftables、iproute2、OpenRC、Python 3.12 + paramiko 5.0.0

设计依据：`docs/superpowers/specs/2026-07-31-transparent-proxy-dual-host-verification-design.md`

---

## 环境事实（已实测确认，不要重新假设）

| 项 | 192.168.1.128 | 192.168.1.129 |
|---|---|---|
| 系统 | Ubuntu 24.04.4，内核 6.8.0-124-generic | Alpine 3.24.1，内核 6.18.35-0-virt |
| CPU / 内存 | 2 核 / 1867M（旧实例运行时可用 790M） | 1 核 / 972M（可用 822M） |
| 磁盘 | `/dev/sda2` 49G，用 45% | `/dev/sda` **已扩容至 1.0G**，可用 931.8M |
| Docker | 29.7.0 已装，**无任何镜像与容器** | 未装（不需要） |
| nft / iptables | 都有 | **都没有** |
| `ip` | 真 iproute2 | **BusyBox v1.37.0** |
| `/dev/net/tun` | 存在 | **不存在**，`tun` 模块未加载 |
| `ip_forward` | — | 0 |
| resolv.conf | — | 含 `nameserver ::1`（会触发回环 stub 告警） |
| 已有 nft 规则 | Docker 自建 `table ip nat` / `table ip filter` | 无 |
| 旧实例 | `/opt/auroramihomo` 裸跑中（pid 84638 面板 + 84655 内核），非 systemd | 无 |
| 服务管理 | Docker restart 策略 | OpenRC（无 systemd） |
| apk-tools | — | 3.0.6-r0 |

SSH 凭据：两台均 `root` / `123456!@#`。

---

## 文件结构

**测试驱动脚本**（放临时目录 `C:\Users\ADMINI~1\AppData\Local\Temp\2\opencode\ttest\`，不进仓库）：

- `sshlib.py` — paramiko 封装：`Host` 类提供 `run()` / `put()` / `api()`，统一超时与输出记录
- `t128_deploy.py` — 128 清理 + 构建 + 部署
- `t129_deploy.py` — 129 换源 + 装依赖 + 部署 + OpenRC
- `scenarios.py` — S0~S4 场景执行，输出结构化日志供写报告
- `evidence/` — 每条命令的原始输出落盘，报告引用它，不靠记忆

**仓库内交付物**：

- Create: `docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md`
- Create: `docs/AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md`

**可能需要修改**（仅在测试发现缺陷且用户确认修复方案后）：

- `docker/docker-compose.yml` — 若需固化透明代理配置
- `backend/internal/netcheck/*` — 若发现检测或规则缺陷

---

## Task 1: 搭建 SSH 驱动库与证据留存

**Files:**
- Create: `C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest/sshlib.py`
- Create: `C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest/evidence/`

- [ ] **Step 1: 写 SSH 封装库**

```python
# sshlib.py — 远程执行与证据留存
# 设计意图：所有远程命令的原始输出必须落盘，报告引用文件而非依赖对话记忆。
# 既有测试报告的价值来自"贴真实输出"，靠记忆复述会失真。
import io, json, os, time, datetime
import paramiko

EVID = os.path.join(os.path.dirname(os.path.abspath(__file__)), "evidence")
os.makedirs(EVID, exist_ok=True)

class Host:
    def __init__(self, name, ip, password="123456!@#", user="root"):
        self.name, self.ip, self.user, self.password = name, ip, user, password
        self._c = None
        self.log_path = os.path.join(EVID, f"{name}.log")

    def connect(self):
        c = paramiko.SSHClient()
        c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        c.connect(self.ip, username=self.user, password=self.password,
                  timeout=15, allow_agent=False, look_for_keys=False)
        self._c = c
        return self

    def close(self):
        if self._c:
            self._c.close()
            self._c = None

    def run(self, cmd, timeout=120, check=False, quiet=False):
        """执行命令，返回 (rc, output)。output 合并 stdout+stderr。

        check=True 时 rc 非 0 抛异常 —— 用于部署步骤这类"失败必须停"的场景；
        测试场景一律 check=False，因为"命令失败"本身常常就是要记录的结论。
        """
        if not self._c:
            self.connect()
        i, o, e = self._c.exec_command(cmd, timeout=timeout, get_pty=False)
        out = o.read().decode(errors="replace") + e.read().decode(errors="replace")
        rc = o.channel.recv_exit_status()
        stamp = datetime.datetime.now().strftime("%H:%M:%S")
        rec = f"\n[{stamp}] $ {cmd}\n(rc={rc})\n{out.rstrip()}\n"
        with open(self.log_path, "a", encoding="utf-8") as f:
            f.write(rec)
        if not quiet:
            print(f"[{self.name}] $ {cmd}\n(rc={rc}) {out.rstrip()[:2000]}")
        if check and rc != 0:
            raise RuntimeError(f"{self.name}: 命令失败 rc={rc}: {cmd}\n{out}")
        return rc, out

    def put(self, local, remote):
        if not self._c:
            self.connect()
        sftp = self._c.open_sftp()
        try:
            sftp.put(local, remote)
        finally:
            sftp.close()
        size = os.path.getsize(local)
        with open(self.log_path, "a", encoding="utf-8") as f:
            f.write(f"\n[PUT] {local} -> {remote} ({size} bytes)\n")
        print(f"[{self.name}] PUT {os.path.basename(local)} -> {remote} ({size} bytes)")

    def put_text(self, content, remote):
        """写文本文件到远端。用 SFTP 而非 echo 重定向，避免引号与换行转义问题。"""
        if not self._c:
            self.connect()
        sftp = self._c.open_sftp()
        try:
            with sftp.open(remote, "w") as f:
                f.write(content)
        finally:
            sftp.close()
        with open(self.log_path, "a", encoding="utf-8") as f:
            f.write(f"\n[WRITE] {remote} ({len(content)} chars)\n{content}\n")
        print(f"[{self.name}] WRITE {remote} ({len(content)} chars)")

H128 = lambda: Host("128-ubuntu-docker", "192.168.1.128")
H129 = lambda: Host("129-alpine-binary", "192.168.1.129")
```

- [ ] **Step 2: 验证库可用**

Run:
```bash
python -c "import sys; sys.path.insert(0,r'C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest'); from sshlib import H128,H129; h=H128().connect(); h.run('hostname; uname -r'); h.close(); h2=H129().connect(); h2.run('hostname; uname -r'); h2.close()"
```
Expected: 两台主机名与内核版本正常输出，`evidence/` 下生成两个 `.log` 文件。

---

## Task 2: 本地构建产物

**Files:**
- 产物：`D:/goWork/AuroraMihomo/auroramihomo`（linux amd64 二进制）
- 产物：`D:/goWork/AuroraMihomo/frontend/dist/`

- [ ] **Step 1: 交叉编译后端**

```bash
cd D:/goWork/AuroraMihomo
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -ldflags="-w -s" -o auroramihomo-linux-amd64 ./backend/api
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

Expected: 生成 `auroramihomo-linux-amd64`，约 29M。

- [ ] **Step 2: 确认二进制格式正确**

Run: `python -c "print(open(r'D:/goWork/AuroraMihomo/auroramihomo-linux-amd64','rb').read(20))"`
Expected: 以 `b'\x7fELF\x02\x01\x01\x00'` 开头（64 位 ELF），不是 PE。

- [ ] **Step 3: 构建前端**

```bash
cd D:/goWork/AuroraMihomo/frontend
npm run build
```

Expected: `frontend/dist/` 生成，含 `index.html` 与 `assets/`。

- [ ] **Step 4: 打包前端产物便于上传**

```bash
cd D:/goWork/AuroraMihomo/frontend
tar -czf ../dist-frontend.tar.gz -C dist .
```

Expected: 生成 `dist-frontend.tar.gz`。

（不提交这些产物，`.gitignore` 已忽略 `public/` 与二进制；`auroramihomo-linux-amd64` 与 `dist-frontend.tar.gz` 测完删除。）

---

## Task 3: 清理 128 旧实例

**Files:** 无本地文件变更

- [ ] **Step 1: 先备份旧库再动手**

理由：用户已同意彻底清理，但备份成本极低，而误删无法恢复。备份留在 `/root/` 下不占 `/opt`。

```python
h = H128().connect()
h.run("mkdir -p /root/aurora-old-backup", check=True)
h.run("cp -a /opt/auroramihomo/data/aurora.db /root/aurora-old-backup/ 2>/dev/null; "
      "cp -a /opt/auroramihomo/data/initial_password.txt /root/aurora-old-backup/ 2>/dev/null; "
      "ls -la /root/aurora-old-backup/")
```

Expected: 备份目录列出 `aurora.db`。

- [ ] **Step 2: 停旧进程**

```python
h.run("pkill -TERM -f 'auroramihomo -f' ; sleep 5; "
      "ps -ef|grep -E 'auroramihomo|mihomo'|grep -v grep || echo '已全部退出'")
```

Expected: 输出「已全部退出」。若仍有残留，追加 `pkill -KILL -f auroramihomo` 并记录。

- [ ] **Step 3: 确认端口释放**

```python
h.run("ss -tlnp | grep -E '8899|9090' || echo '端口已释放'")
```

Expected: 「端口已释放」。

- [ ] **Step 4: 删除旧目录并确认内存回收**

```python
h.run("rm -rf /opt/auroramihomo && ls -d /opt/auroramihomo 2>&1 | tail -1")
h.run("free -m | head -2")
```

Expected: `No such file or directory`；可用内存明显上升（预期 1500M+）。

- [ ] **Step 5: 检查构建所需内存，不足则加 swap**

```python
rc, out = h.run("free -m | awk '/^Mem:/{print $7}'")   # available
avail = int(out.strip().split()[-1])
rc, sw = h.run("swapon --show || echo no-swap")
```

判据：`available < 1400` 且无 swap 时，加 2G swapfile：

```python
h.run("fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile "
      "&& swapon /swapfile && free -m | head -3", check=True)
```

Expected: 记录实际决定（加了/没加）与理由到报告。加了 swap 要在报告中标注这是为构建临时添加。

---

## Task 4: 在 128 构建镜像并部署

**Files:**
- 远端 Create: `/opt/aurora-docker/docker-compose.yml`（改自仓库版本）
- 远端 Create: `/opt/aurora-docker/data/`

- [ ] **Step 1: 上传源码到 128**

镜像构建需要完整源码（Dockerfile 里有前端与后端两个构建阶段）。用 git archive 打包当前工作树，避免传 `.git` 与 `data/`。

```bash
cd D:/goWork/AuroraMihomo
tar --exclude=.git --exclude=data --exclude=node_modules --exclude=frontend/node_modules `
    --exclude=frontend/dist --exclude=public --exclude='*.exe' `
    --exclude=auroramihomo-linux-amd64 --exclude=dist-frontend.tar.gz `
    -czf ../aurora-src.tar.gz .
```

```python
h.run("mkdir -p /opt/aurora-build", check=True)
h.put(r"D:\goWork\aurora-src.tar.gz", "/opt/aurora-build/src.tar.gz")
h.run("cd /opt/aurora-build && tar -xzf src.tar.gz && ls", check=True)
```

Expected: 解出 `backend/ frontend/ docker/ go.mod` 等。

- [ ] **Step 2: 构建镜像**

```python
rc, out = h.run("cd /opt/aurora-build && docker build -f docker/Dockerfile -t auroramihomo:latest . 2>&1 | tail -40",
                timeout=2400)
```

Expected: 最后出现 `naming to docker.io/library/auroramihomo:latest` 或 `writing image sha256:`。
若 OOM（`signal: killed` / `Killed`），回到 Task 3 Step 5 加 swap 后重试，并记录。

- [ ] **Step 3: 确认镜像与预装依赖**

```python
h.run("docker images auroramihomo:latest --format '{{.Repository}}:{{.Tag}} {{.Size}}'")
h.run("docker run --rm --entrypoint sh auroramihomo:latest -c "
      "'which nft iptables ip6tables ip; ip -V'")
```

Expected: 镜像存在；四个命令都有路径；`ip -V` 输出含 `iproute2`（证明装的是真 iproute2 而非 busybox）。

- [ ] **Step 4: 写透明代理版 compose**

关键改动四项（每项都是必需，缺任一项透明代理不可用，且失败信号不同）：
1. `user: "0:0"` — 否则 `cap_add` 只进 bounding set，effective 位拿不到
2. 注掉 `no-new-privileges:true` — 与 file capability / root 提权互斥
3. `devices: /dev/net/tun` — TUN 模式必需（TProxy 不需要）
4. `network_mode: host` + `cap_add: NET_ADMIN` — 仓库版本已有

```python
compose = """services:
  auroramihomo:
    image: auroramihomo:latest
    container_name: auroramihomo
    restart: unless-stopped
    # 透明代理/TUN 需要 host 网络：规则与路由必须作用于宿主 netns
    network_mode: "host"
    cap_add:
      - NET_ADMIN
    # 以 root 运行：cap_add 只填 bounding set，非 root 拿不到 effective 位
    user: "0:0"
    # TUN 模式必需的设备映射（TProxy 不需要）
    devices:
      - /dev/net/tun:/dev/net/tun
    # 注意：本次测试刻意去掉 no-new-privileges:true —— 它与 root + NET_ADMIN
    # 的组合互斥。这是安全降级，仅用于测试环境。
    volumes:
      - ./data:/data
    environment:
      - TZ=Asia/Shanghai
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://127.0.0.1:8899/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
"""
h.run("mkdir -p /opt/aurora-docker/data", check=True)
h.put_text(compose, "/opt/aurora-docker/docker-compose.yml")
```

- [ ] **Step 5: 启动容器**

以 root 运行，data 目录属主无需改（root 可写任意目录）。

```python
h.run("cd /opt/aurora-docker && docker compose up -d", check=True, timeout=180)
h.run("sleep 20; docker ps --format '{{.Names}} {{.Status}}'")
h.run("docker logs auroramihomo --tail 40")
```

Expected: 容器 `Up`；日志出现监听 8899 的信息与初始密码生成提示。

- [ ] **Step 6: 取初始密码**

```python
rc, pw = h.run("cat /opt/aurora-docker/data/initial_password.txt")
```

Expected: 32 字符密码。记入证据文件但**不写进报告正文**。

---

## Task 5: 部署 129 Alpine（换源 + 依赖 + 二进制 + OpenRC）

**Files:**
- 远端 Create: `/etc/apk/repositories`（替换）
- 远端 Create: `/opt/auroramihomo/{auroramihomo,etc/aurora-api.yaml,public/}`
- 远端 Create: `/etc/init.d/auroramihomo`（OpenRC 脚本）

- [ ] **Step 1: 备份并替换 apk 源为阿里云**

```python
a = H129().connect()
a.run("cp /etc/apk/repositories /etc/apk/repositories.bak && cat /etc/apk/repositories.bak", check=True)
a.put_text("https://mirrors.aliyun.com/alpine/v3.24/main\n"
           "https://mirrors.aliyun.com/alpine/v3.24/community\n",
           "/etc/apk/repositories")
a.run("cat /etc/apk/repositories", check=True)
```

- [ ] **Step 2: 更新索引并验证换源生效**

```python
a.run("apk update 2>&1 | tail -5", check=True, timeout=180)
```

Expected: 输出中的仓库 URL 是 `mirrors.aliyun.com`，且 `OK: ... distinct packages available`。
若 apk-tools 3.0.6 对 `/etc/apk/repositories` 纯文本格式不兼容（3.x 引入了
`/etc/apk/repositories.d/`），改写 `/etc/apk/repositories.d/aliyun.list` 并记录差异。

- [ ] **Step 3: 安装透明代理依赖**

```python
a.run("apk add --no-cache nftables iproute2 ip6tables 2>&1 | tail -20", check=True, timeout=300)
a.run("which nft iptables ip6tables ip; ip -V", check=True)
```

Expected: `ip -V` 现在输出 `ip utility, iproute2-6.x`（不再是 BusyBox）。
注意 iproute2 装完后 `/sbin/ip` 可能仍是 busybox 链接，需确认 `which ip` 指向 `/usr/sbin/ip`
或 `/usr/bin/ip`；若 PATH 顺序导致仍解析到 busybox，记录并说明对 netcheck 判定的影响。

- [ ] **Step 4: 加载 tun 与 nft_tproxy 模块并持久化**

```python
a.run("modprobe tun; modprobe nft_tproxy; "
      "ls -l /dev/net/tun; grep -E '^(tun|nft_tproxy)' /proc/modules")
a.put_text("tun\nnft_tproxy\n", "/etc/modules-load.d/auroramihomo.conf")
```

Expected: `/dev/net/tun` 出现且是字符设备（`crw-`）；两个模块在 `/proc/modules` 中。

- [ ] **Step 5: 上传二进制与前端**

```python
a.run("mkdir -p /opt/auroramihomo/etc /opt/auroramihomo/public /opt/auroramihomo/data", check=True)
a.put(r"D:\goWork\AuroraMihomo\auroramihomo-linux-amd64", "/opt/auroramihomo/auroramihomo")
a.put(r"D:\goWork\AuroraMihomo\dist-frontend.tar.gz", "/opt/auroramihomo/fe.tar.gz")
a.run("cd /opt/auroramihomo && chmod +x auroramihomo && tar -xzf fe.tar.gz -C public "
      "&& rm fe.tar.gz && ls public | head", check=True)
```

Expected: `auroramihomo` 可执行；`public/` 下有 `index.html`。

- [ ] **Step 6: 写配置文件**

基于仓库默认配置，只改数据路径（Alpine 上放 `/opt/auroramihomo/data`）。

```python
cfg = """Name: aurora-api
Host: 0.0.0.0
Port: 8899
Timeout: 300000

Server:
  ReadHeaderTimeoutSec: 10
  ReadTimeoutSec: 60
  WriteTimeoutSec: 360
  IdleTimeoutSec: 120
  MaxHeaderBytes: 1048576

TrustedProxies: []

Log:
  Mode: console
  Level: info

DataSource: "./data/aurora.db"

Mihomo:
  BinaryPath: ""
  ConfigDir: "./data"

SubStore:
  NodePath: "node"
  SubStoreScript: "./data/substore/substore.js"

Bootstrap:
  EnsureOnStart: true
  FailOnEnsureError: false

AutoUpdate:
  Enabled: false
  Cron: "0 0 4 * * *"

Updater:
  MihomoRepo: "MetaCubeX/mihomo"
  ZashboardRepo: "Zephyruso/zashboard"
  GitHubAPI: "https://api.github.com"
  TimeoutSec: 180
  CDNProviders:
    - ghproxy.com
    - mirror.ghproxy.com
    - gh.ddlc.top
    - ghproxy.net
    - gitdl.cn
    - gh.llkk.cc
    - ghp.ci
    - github
"""
a.put_text(cfg, "/opt/auroramihomo/etc/aurora-api.yaml")
```

- [ ] **Step 7: 写 OpenRC 服务脚本**

Alpine 无 systemd。`supervise-daemon` 而非 `start-stop-daemon`：前者原生支持
进程退出后自动重启，正好满足 `/api/v1/system/restart` 的"退出后由进程管理器拉起"约定。

```python
initd = """#!/sbin/openrc-run

name="auroramihomo"
description="AuroraMihomo 配置管理平台"

directory="/opt/auroramihomo"
command="/opt/auroramihomo/auroramihomo"
command_args="-f etc/aurora-api.yaml"
command_user="root:root"

# 用 supervise-daemon：面板的 /api/v1/system/restart 依赖"进程退出后被拉起"，
# start-stop-daemon 不做这件事，会导致重启接口变成单向关机。
supervisor="supervise-daemon"
supervise_daemon_args="--stdout /var/log/auroramihomo.log --stderr /var/log/auroramihomo.log"
pidfile="/run/auroramihomo.pid"

depend() {
    need net
    after firewall
}
"""
a.put_text(initd, "/etc/init.d/auroramihomo")
a.run("chmod +x /etc/init.d/auroramihomo && rc-update add auroramihomo default", check=True)
```

- [ ] **Step 8: 启动并确认**

```python
a.run("rc-service auroramihomo start", check=True, timeout=120)
a.run("sleep 20; rc-service auroramihomo status; ss -tlnp | grep 8899")
a.run("tail -40 /var/log/auroramihomo.log")
rc, pw129 = a.run("cat /opt/auroramihomo/data/initial_password.txt")
```

Expected: 服务 `started`；8899 在监听；日志无 fatal；初始密码取到。

- [ ] **Step 9: 确认磁盘余量足够后续内核下载**

```python
a.run("df -h /")
```

Expected: 可用 > 400M。若不足，先 `apk cache clean` 并记录。

---

## Task 6: S0 部署与基础验证（两台各跑）

**Files:** Create: `C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest/scenarios.py`

> Task 7~10 的所有步骤都依赖本任务 Step 1 定义的 `api()` 与 `login()`，
> 以及 Task 1 定义的 `Host.run/put/put_text` 与 `H128/H129`。
> 执行 Task 7 及之后的任务前，确保 `scenarios.py` 已 `from sshlib import *`。

- [ ] **Step 1: 写 API 调用辅助**

```python
# scenarios.py 片段：通过远端 curl 调用面板 API。
# 刻意在被测机上用 curl 而不是从 Windows 直连 —— 透明代理启用后
# 从外部访问与从本机访问的路径不同，本机视角才能验证"面板自己没被锁死"。
import json, re

def api(h, method, path, token=None, body=None, base="http://127.0.0.1:8899"):
    hdr = "-H 'Content-Type: application/json'"
    if token:
        hdr += f" -H 'Authorization: Bearer {token}'"
    data = f"-d '{json.dumps(body, ensure_ascii=False)}'" if body is not None else ""
    cmd = f"curl -sS -m 60 -X {method} {hdr} {data} '{base}{path}'"
    rc, out = h.run(cmd, timeout=120)
    return rc, out

def login(h, password):
    rc, out = api(h, "POST", "/api/v1/auth/login", body={"password": password})
    m = re.search(r'"token"\s*:\s*"([^"]+)"', out)
    if not m:
        raise RuntimeError(f"登录失败: {out}")
    return m.group(1)
```

- [ ] **Step 2: S0-1 健康检查**

```python
h.run("curl -sS -m 15 http://127.0.0.1:8899/healthz")
```
Expected: 含 `"status":"ok"`。

- [ ] **Step 3: S0-2 登录换 JWT**

```python
token = login(h, pw.strip())
print("token 长度:", len(token))
```
Expected: 拿到 JWT（三段点分结构）。

- [ ] **Step 4: S0-3 导入真实订阅**

从本地库读出 URL（不落地文件），经 API 创建。

```python
# 本地执行：读订阅 URL
import sqlite3
con = sqlite3.connect(r"file:D:\goWork\AuroraMihomo\data\aurora.db?mode=ro", uri=True)
subs = con.execute("SELECT name,url,user_agent FROM subscriptions WHERE enabled=1 AND url<>''").fetchall()
con.close()
# 远端执行：逐条创建并立即更新
for name, url, ua in subs:
    api(h, "POST", "/api/v1/subscriptions", token,
        {"name": name, "url": url, "enabled": True, "userAgent": ua or ""})
rc, out = api(h, "GET", "/api/v1/subscriptions", token)
```

Expected: 返回 2 条订阅。随后对每条调 `POST /api/v1/subscriptions/{id}/update`，
确认 `lastUpdate` 有值、节点数 > 0。

**报告脱敏要求**：订阅 URL 不得出现在报告中；节点名可保留，节点地址打码。

- [ ] **Step 5: S0-4 内核启动与面板入口**

```python
api(h, "POST", "/api/v1/config/merge", token)          # 先合并生成 config.yaml
api(h, "POST", "/api/v1/mihomo/start", token)
h.run("sleep 8; ss -tlnp | grep -E '9090|7890|7891|7893' ")
api(h, "GET", "/api/v1/system/status", token)
api(h, "GET", "/api/v1/dashboard/entry", token)
h.run("curl -sSI -m 15 http://127.0.0.1:8899/ui/ | head -3")
```

Expected: mihomo 进程在跑、9090 监听、`system/status` 显示内核 running、
`dashboard/entry` 返回带 hostname/port/secret 的入口、`/ui/` 返回 200。

- [ ] **Step 6: 提交阶段性证据**

此时不改仓库代码，无需 git commit。把 `evidence/*.log` 归档留用：

```bash
Copy-Item "C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest/evidence/*.log" "C:/Users/ADMINI~1/AppData/Local/Temp/2/opencode/ttest/evidence/s0-snapshot/" -Force
```

---

## Task 7: S1 环境检测验证（两台各跑）

- [ ] **Step 1: S1-5 取检测报告并逐字段核对**

```python
rc, out = api(h, "GET", "/api/v1/transparent/status", token)
```

核对项（把 API 返回与系统实际逐条比对，两边都要贴进报告）：

```python
h.run("id -u; grep -E 'CapEff|CapBnd' /proc/self/status; "
      "test -c /dev/net/tun && echo tun-chardev-ok || echo tun-missing; "
      "cat /etc/os-release|grep ^ID=; "
      "which nft iptables ip; ip -V 2>&1|head -1; "
      "cat /proc/sys/net/ipv4/ip_forward; cat /proc/sys/net/ipv4/conf/all/rp_filter; "
      "test -f /.dockerenv && echo in-container || echo not-container; "
      "cat /etc/resolv.conf|grep -v '^#'")
```

**129 的重点**：`ip` 装 iproute2 前是 BusyBox，装后应转为真 iproute2。
若在装依赖之前先取一次 status，可验证 busybox 被正确判为不可用 —— 这个对照很有价值，
执行顺序上应在 Task 5 Step 3 之前抢先取一次并记录（若已错过，说明未验证并给出原因）。

**128 的重点**：容器内 `inContainer=true`、`hostNetwork=true`、
`capNetAdmin` 与 `capNetAdminBounding` 均为 true（因为 `user: "0:0"`）。

Expected: 所有字段与实际一致。不一致即为缺陷，记录后暂停并报告用户。

- [ ] **Step 2: S1-6 自动准备（provision）**

```python
rc, before = h.run("cat /proc/sys/net/ipv4/ip_forward; ls /etc/sysctl.d/ 2>/dev/null")
rc, out = api(h, "POST", "/api/v1/transparent/provision", token,
              {"installPackages": True, "applySysctl": True})
rc, after = h.run("cat /proc/sys/net/ipv4/ip_forward; "
                  "cat /etc/sysctl.d/99-auroramihomo.conf 2>/dev/null; "
                  "for f in /proc/sys/net/ipv4/conf/*/rp_filter; do echo -n \"$f=\"; cat $f; done")
```

Expected（129 二进制）：`ip_forward` 从 0 变 1；`99-auroramihomo.conf` 生成；
响应里 modes 转为可用。
Expected（128 Docker）：sysctl 部分应**拒绝执行**并说明原因（容器内改 sysctl
要么被内核拒绝、要么直接改到宿主，代码刻意不替用户决定）；报告需记录这个行为。

- [ ] **Step 3: S1-6b 幂等复测**

```python
api(h, "POST", "/api/v1/transparent/provision", token, {"installPackages": True, "applySysctl": True})
h.run("ls -la /etc/sysctl.d/99-auroramihomo.conf; wc -l /etc/sysctl.d/99-auroramihomo.conf")
```

Expected: 重复执行不报错、文件不堆积重复行（整体重写而非追加）。

---

## Task 8: S2 代理基础能力（两台各跑，透明代理开启前）

- [ ] **Step 1: 记录直连出口 IP 作为基线**

```python
rc, direct_ip = h.run("curl -sS -m 20 https://api.ip.sb/ip || curl -sS -m 20 https://ifconfig.me")
```

Expected: 拿到宿主的真实公网 IP。这是后续判断"出口变了"的基线，必须先记录。

- [ ] **Step 2: S2-8 手动代理验证节点可用**

```python
h.run("grep -E '^mixed-port|^port|^socks-port' /opt/aurora-docker/data/config.yaml 2>/dev/null || "
      "grep -E '^mixed-port|^port|^socks-port' /opt/auroramihomo/data/config.yaml")
rc, proxy_ip = h.run("curl -sS -m 30 -x http://127.0.0.1:7890 https://api.ip.sb/ip")
```

Expected: `proxy_ip` 与 `direct_ip` **不同**，且是节点出口 IP。
这个值要留作 S3-11 的对照 —— 透明代理生效后本机 curl 应得到同一个 IP。
若混合端口不是 7890，按配置实际值调整。

- [ ] **Step 3: S2-7 面板自身出网的 PanelMark 验证**

透明代理未开启时 `PanelMark` 不产生可见效果（没有规则匹配它），
所以这一项的完整验证放在 S3 开启之后（Task 9 Step 6）。此处只记录前置状态：

```python
api(h, "GET", "/api/v1/update/check", token)   # 面板主动出网，应成功
```

Expected: 返回版本信息，证明面板出网正常（基线）。

---

## Task 9: S3 TProxy 主线（两台各跑，5 项）

**这是全流程的核心。每一步都可能导致失去 SSH，逐步执行并保留恢复手段。**

- [ ] **Step 1: 记录启用前的完整网络基线**

```python
h.run("nft list ruleset > /tmp/nft-before.txt; wc -l /tmp/nft-before.txt; "
      "ip rule show > /tmp/iprule-before.txt; cat /tmp/iprule-before.txt; "
      "ip route show table all | head -20")
```

Expected: 基线落盘。128 上应看到 Docker 自己的 `table ip nat` 等 —— 关闭后要确认它们完好。

- [ ] **Step 2: S3-9 启用但不确认，验证 90 秒自动回滚**

```python
import time
rc, out = api(h, "PUT", "/api/v1/transparent", token,
              {"enabled": True, "mode": "tproxy", "tproxyPort": 7893, "tunStack": "mixed"})
# 立即确认规则已下发且处于待确认状态
api(h, "GET", "/api/v1/transparent/status", token)
h.run("nft list table inet aurora_tproxy | head -30; ip rule show | grep fwmark")
# 等待超过窗口
time.sleep(100)
api(h, "GET", "/api/v1/transparent/status", token)
h.run("nft list table inet aurora_tproxy 2>&1 | tail -2; ip rule show | grep fwmark || echo '策略路由已清除'")
```

Expected: 启用后 `pendingConfirm=true` 且 `secondsLeft` 递减、规则在位；
100 秒后 `enabled=false`、表已删除、fwmark 规则消失。SSH 全程不断。

- [ ] **Step 3: S3-10 启用并确认，核对规则顺序**

```python
api(h, "PUT", "/api/v1/transparent", token,
    {"enabled": True, "mode": "tproxy", "tproxyPort": 7893, "tunStack": "mixed"})
api(h, "POST", "/api/v1/transparent/confirm", token)
api(h, "GET", "/api/v1/transparent/status", token)
h.run("nft list table inet aurora_tproxy")
```

核对清单（顺序即安全边界，任一处顺序错误都记为缺陷）：
1. 管理端口 return 在最前（22、8899、9090；prerouting 按 dport，output 按 sport **和** dport）
2. `meta mark 0xff return`（mihomo）与 `meta mark 0xfe return`（面板）
3. DNS 劫持在局域网 return **之前**、在 mark return **之后**
4. 8 条局域网/保留网段 return
5. 兜底 TPROXY 规则带 `meta nfproto ipv4` 限定（无 v6 出网时）
6. output 链 hook 是 `type route hook output`

- [ ] **Step 4: S3-11 本机流量真实接管（核心判据）**

```python
rc, tp_ip = h.run("curl -sS -m 30 https://api.ip.sb/ip")
h.run("dig +short google.com @127.0.0.1 2>/dev/null || nslookup google.com 127.0.0.1 2>&1|tail -5")
h.run("dig +short google.com 2>/dev/null || nslookup google.com 2>&1|tail -5")
h.run("nft list table inet aurora_tproxy | grep -E 'counter|packets' | head -20")
h.run("ip rule show | grep 'fwmark 0x1'; ip route show table 100")
```

Expected:
- `tp_ip` 等于 Task 8 Step 2 的 `proxy_ip`（同一条链路，这是最强判据）
- 计数器 packets > 0
- `ip rule` 有 `fwmark 0x1 lookup 100`，`table 100` 有 `local default dev lo`
- DNS：若启用了 fake-ip，解析结果落在 198.18.0.0/16；否则以"与直连 DNS 结果不同"
  或 mihomo 连接日志中出现该查询为判据，不以 198.18 段为唯一判据

- [ ] **Step 5: S3-12 管理端口豁免（含非局域网段）**

```python
# 局域网段（从 Windows 直连，已隐含验证：SSH 没断）
h.run("curl -sS -m 10 http://127.0.0.1:8899/healthz")
h.run("curl -sS -m 10 http://127.0.0.1:9090/version -H 'Authorization: Bearer '$(grep -oP '(?<=^secret: ).*' data/config.yaml 2>/dev/null||echo '')")
# 非局域网段：建 dummy 网卡配 CGNAT 地址，从该源地址连管理端口
h.run("ip link add aurotest type dummy 2>/dev/null; ip addr add 100.64.7.1/32 dev aurotest 2>/dev/null; "
      "ip link set aurotest up; ip addr show aurotest")
# self_ip 取被测机自身的局域网地址：128 上是 "192.168.1.128"，129 上是 "192.168.1.129"
self_ip = "192.168.1.128"   # 在 129 上执行时改为 192.168.1.129
h.run(f"curl -sS -m 10 --interface 100.64.7.1 http://{self_ip}:8899/healthz")
h.run("nft list table inet aurora_tproxy | grep -B2 -A2 '8899'")
```

Expected: 从 CGNAT 源地址访问管理端口成功（命中管理端口 return 而非局域网 return）。
测完删除 dummy 网卡：`ip link del aurotest`。

若此步失败，即为既有报告 8.2 节同类缺陷的回归，立即记录并报告用户。

- [ ] **Step 6: S2-7 补验 PanelMark（此时才可见）**

```python
h.run("nft list table inet aurora_tproxy | grep -A1 '0xfe'")
api(h, "GET", "/api/v1/update/check", token)     # 面板出网
h.run("nft list table inet aurora_tproxy | grep -A1 '0xfe'")   # 计数器应增长
```

Expected: `mark 0xfe return` 的计数器在面板出网后增长，证明面板流量走了豁免路径。

- [ ] **Step 7: S3-13 关闭并验证完整清理**

```python
api(h, "PUT", "/api/v1/transparent", token, {"enabled": False, "mode": "off"})
api(h, "GET", "/api/v1/transparent/status", token)
h.run("nft list table inet aurora_tproxy 2>&1 | tail -2")
h.run("ip rule show; ip -6 rule show; ip route show table 100 2>&1; ip -6 route show table 100 2>&1")
rc, back_ip = h.run("curl -sS -m 20 https://api.ip.sb/ip")
h.run("nft list ruleset > /tmp/nft-after.txt; diff /tmp/nft-before.txt /tmp/nft-after.txt && echo '无关规则完好'")
```

Expected:
- 表已删除、v4 与 v6 的 fwmark 规则都清掉、table 100 空
- `back_ip` 回到 Task 9 Step 1 的 `direct_ip`
- 与基线 diff 为空（128 上 Docker 的规则完好无损）—— 证明只删自己的表、未整体 flush

---

## Task 10: S4-14 TUN 对照（两台各跑，1 项）

- [ ] **Step 1: 确认 TProxy 已完全关闭**

```python
api(h, "GET", "/api/v1/transparent/status", token)
h.run("nft list table inet aurora_tproxy 2>&1|tail -1; ip rule show|grep fwmark || echo clean")
```

Expected: `mode=off`、无残留。两种模式互斥，叠加会让现象无法归因。

- [ ] **Step 2: 启用 TUN 并确认**

```python
api(h, "PUT", "/api/v1/transparent", token,
    {"enabled": True, "mode": "tun", "tproxyPort": 7893, "tunStack": "mixed"})
api(h, "POST", "/api/v1/transparent/confirm", token)
h.run("sleep 8; ip link show | grep -iE 'meta|utun|tun'; ip route show | head -10")
h.run("grep -A8 '^tun:' data/config.yaml 2>/dev/null || "
      "grep -A8 '^tun:' /opt/aurora-docker/data/config.yaml 2>/dev/null || "
      "grep -A8 '^tun:' /opt/auroramihomo/data/config.yaml")
```

Expected: 出现 `Meta` 网卡；默认路由指向它；配置里 `tun.enable: true`、
`auto-route: true`、`auto-detect-interface: true`、`dns-hijack: [any:53]`。

- [ ] **Step 3: 验证出口 IP 变化**

```python
rc, tun_ip = h.run("curl -sS -m 30 https://api.ip.sb/ip")
```

Expected: `tun_ip` 为节点 IP（应与 S2 的 `proxy_ip` 一致）。

- [ ] **Step 4: 关闭并确认清理**

```python
api(h, "PUT", "/api/v1/transparent", token, {"enabled": False, "mode": "off"})
h.run("sleep 8; ip link show|grep -iE 'meta|utun' || echo 'TUN 网卡已移除'; "
      "grep -E '^  enable:' -A0 -B2 data/config.yaml 2>/dev/null | head")
rc, final_ip = h.run("curl -sS -m 20 https://api.ip.sb/ip")
```

Expected: TUN 网卡消失、配置里 `tun.enable: false`、出口 IP 回到直连。

---

## Task 11: 写 Ubuntu/Docker 测试报告

**Files:**
- Create: `docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md`

- [ ] **Step 1: 按结构写报告**

结构对齐既有 `docs/AuroraMihomo-Transparent-Proxy-Test-Report.md`：

```markdown
# AuroraMihomo 透明代理测试报告（Ubuntu 24.04 / Docker 形态）

## 1. 测试环境
## 2. 部署过程
### 2.1 旧实例清理
### 2.2 镜像构建
### 2.3 透明代理所需的 compose 改动（四项，含安全降级说明）
### 2.4 容器启动
## 3. S0 部署与基础功能
## 4. S1 环境检测
## 5. S2 代理基础能力
## 6. S3 TProxy 主线
### 6.1 90 秒未确认自动回滚
### 6.2 规则内容与顺序
### 6.3 本机流量真实接管
### 6.4 管理端口豁免（含非局域网段）
### 6.5 关闭与清理（含不误伤 Docker 自身规则）
## 7. S4 TUN 对照
## 8. 发现的问题
## 9. 结论汇总表
```

写作要求：
- 每个场景贴真实命令与输出（从 `evidence/128-ubuntu-docker.log` 摘取，不凭记忆）
- 出口 IP 打码保留可比性（如 `1.2.3.x` 形式，同一 IP 用同一打码串）
- 订阅 URL 一律不出现；节点名可保留
- 未验证项明确标注原因
- 安全降级（去掉 `no-new-privileges`）单独说明

- [ ] **Step 2: 自检报告**

检查：无占位符、结论汇总表覆盖全部 14 项、每项有「通过/未通过/未验证」明确判定、
脱敏无遗漏（grep 检查报告中不含订阅域名与真实公网 IP）。

```bash
Select-String -Path docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md -Pattern 'TBD|TODO|待补|qiuyin|650998'
```
Expected: 无匹配。

- [ ] **Step 3: 提交**

```bash
git add docs/AuroraMihomo-Transparent-Proxy-Test-Ubuntu-Docker.md
git commit -m "docs: 新增 Ubuntu/Docker 形态透明代理测试报告"
```

---

## Task 12: 写 Alpine/二进制测试报告

**Files:**
- Create: `docs/AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md`

- [ ] **Step 1: 按结构写报告**

```markdown
# AuroraMihomo 透明代理测试报告（Alpine 3.24 / 二进制形态）

## 1. 测试环境
## 2. 部署过程
### 2.1 磁盘扩容（前置条件）
### 2.2 apk 源换阿里云
### 2.3 依赖安装与 busybox ip 的替换
### 2.4 内核模块加载与持久化
### 2.5 二进制部署与 OpenRC 服务
## 3. S0 部署与基础功能
## 4. S1 环境检测
### 4.1 busybox ip 被正确判定为不可用（装 iproute2 前）
### 4.2 装 iproute2 后转为可用
### 4.3 回环 DNS stub 告警（resolv.conf 含 ::1）
### 4.4 自动准备
## 5. S2 代理基础能力
## 6. S3 TProxy 主线（同 Ubuntu 报告的六个小节）
## 7. S4 TUN 对照
## 8. Alpine 平台特有的观察
## 9. 发现的问题
## 10. 结论汇总表
```

第 8 节需覆盖的 Alpine 特有点（这些是本次首验的增量价值）：
- musl + `CGO_ENABLED=0` 的二进制在 Alpine 上直接可跑，无 glibc 依赖问题
- `ip6tables` 是独立包
- busybox `ip` 与真 iproute2 的共存与 PATH 优先级
- OpenRC + `supervise-daemon` 对 `/api/v1/system/restart` 的支持情况
- 无 systemd-resolved，`resolv.conf` 由 dhcpcd 生成且含 `::1`

- [ ] **Step 2: 自检报告**

同 Task 11 Step 2 的检查项。

- [ ] **Step 3: 提交**

```bash
git add docs/AuroraMihomo-Transparent-Proxy-Test-Alpine-Binary.md
git commit -m "docs: 新增 Alpine/二进制形态透明代理测试报告"
```

---

## Task 13: 收尾

- [ ] **Step 1: 确认两台主机处于干净可用状态**

```python
for h in (H128().connect(), H129().connect()):
    h.run("nft list table inet aurora_tproxy 2>&1|tail -1")
    h.run("ip rule show|grep fwmark || echo 'no fwmark rule'")
    h.run("ip -6 rule show|grep fwmark || echo 'no v6 fwmark rule'")
    h.run("ip link show|grep -iE 'meta|aurotest' || echo 'no leftover iface'")
    h.run("curl -sS -m 15 http://127.0.0.1:8899/healthz")
    h.close()
```

Expected: 无规则残留、无遗留网卡（含测试用的 `aurotest` dummy）、面板健康。

- [ ] **Step 2: 清理本地临时产物**

```bash
Remove-Item D:\goWork\AuroraMihomo\auroramihomo-linux-amd64 -ErrorAction SilentlyContinue
Remove-Item D:\goWork\AuroraMihomo\dist-frontend.tar.gz -ErrorAction SilentlyContinue
Remove-Item D:\goWork\aurora-src.tar.gz -ErrorAction SilentlyContinue
git status --short
```

Expected: `git status` 中无新增的构建产物。

- [ ] **Step 3: 若发现代码缺陷，汇总报告给用户**

不在本计划内修改代码。把缺陷清单、影响面、建议修复方向整理后交用户决策，
每项修复走独立的设计 → 计划流程。

- [ ] **Step 4: 决定 128 上遗留的 swap 与备份如何处置**

```python
h = H128().connect()
h.run("swapon --show; ls -la /root/aurora-old-backup/")
```

询问用户：为构建临时加的 swap 是否保留、旧库备份是否删除。不擅自决定。

---

## 执行顺序约束

1. **Task 5 Step 3（装 iproute2）之前必须先取一次 129 的 `transparent/status`**，
   否则「busybox 被正确判为不可用」这个对照就丢了。可在 Task 5 Step 2 后插入取值。
   实际执行时若顺序已过，如实记为未验证。
2. Task 9 各步严格顺序执行，每步确认 SSH 仍通再继续。
3. Task 10（TUN）必须在 Task 9 Step 7（TProxy 关闭）确认清理后才开始。
4. 两台主机的 Task 6~10 可以交替进行，但**不要同时启用两台的透明代理** ——
   若两台互为网络路径的一部分，同时启用会让故障无法归因。

## 中止条件

出现以下情况立即停止并报告用户，不自行绕过：

- 任一主机失去 SSH 且 90 秒后未自愈
- 规则下发导致宿主完全断网
- 发现会导致数据丢失的缺陷
- 镜像构建反复 OOM 且加 swap 后仍失败

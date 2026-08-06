package adguard

import (
	"fmt"
	"strings"
)

// 服务单元模板：AGH 由系统服务管理器（systemd / OpenRC）看护时的 unit 内容。
//
// 核心设计（见 docs/superpowers/plans/2026-08-06-adguard-service-ization.md D2）：
// unit 只固化安装期不变的参数（二进制路径 / work-dir / config），绝不固化任何
// 端口参数——AGH 的端口（Web/DNS）唯一事实来源是 yaml，改端口 = 改 yaml +
// systemctl restart，unit 写一次永不重写。若在此加回 --web-addr 之类的参数，
// 改端口就必须重写 unit + daemon-reload，属于回归（测试有断言拦截）。

// serviceExecArgs 组装服务单元命令行。与 Manager.startLocked 的差异：
// 不传 --web-addr（yaml 已是 http.address 的唯一来源），其余参数保持一致。
func serviceExecArgs(binPath, workDir, cfgFile string) []string {
	args := []string{binPath}
	if workDir != "" {
		args = append(args, "--work-dir", workDir)
	}
	if cfgFile != "" {
		args = append(args, "--config", cfgFile)
	}
	// AGH 自动更新由面板 updater 统一调度，禁掉 AGH 自带检查（与 Manager.Start 一致）
	args = append(args, "--no-check-update")
	return args
}

// systemdQuoted 为 ExecStart 参数加引号：systemd 按空白切分参数，路径含空格
// 时不引号会被切成两段导致启动失败。
func systemdQuoted(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// renderSystemdUnit 渲染 systemd 单元。ExecStart 与 Start 幂等语义：
// systemd 自身保证「已在运行则 start 是 no-op」，面板无需额外探活。
func renderSystemdUnit(binPath, workDir, cfgFile string) string {
	execStart := make([]string, 0, 5)
	for _, a := range serviceExecArgs(binPath, workDir, cfgFile) {
		execStart = append(execStart, systemdQuoted(a))
	}
	return fmt.Sprintf(`[Unit]
Description=Aurora AdGuard Home (managed by AuroraMihomo)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3
# :53 绑定权限只给 AGH，面板不再需要整体带 CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
`, strings.Join(execStart, " "))
}

// renderOpenRCService 渲染 Alpine 用的 init.d 脚本（对标面板自身
// /etc/init.d/auroramihomo 的写法）。必须用 supervise-daemon：它才有进程
// 退出后重新拉起的能力；start-stop-daemon 不会拉起，崩溃即常停。
func renderOpenRCService(binPath, workDir, cfgFile string) string {
	return fmt.Sprintf(`#!/sbin/openrc-run

name="aurora-adguardhome"
description="Aurora AdGuard Home (managed by AuroraMihomo)"

directory="%s"
command="%s"
command_args="%s"
command_user="root:root"

supervisor="supervise-daemon"
pidfile="/run/aurora-adguardhome.pid"

depend() {
    need net
    after firewall
}
`, workDir, binPath, strings.Join(serviceExecArgs(binPath, workDir, cfgFile), " "))
}

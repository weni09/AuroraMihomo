package adguard

import (
	"fmt"
	"path/filepath"
	"strings"
)

// 服务单元模板：AGH 由系统服务管理器（systemd / OpenRC）看护时的 unit 内容。
//
// 核心设计（见 docs/superpowers/plans/2026-08-06-adguard-service-ization.md D2）：
// unit 只固化安装期不变的参数（二进制路径 / work-dir / config），绝不固化任何
// 端口参数——AGH 的端口（Web/DNS）唯一事实来源是 yaml，改端口 = 改 yaml +
// systemctl restart，unit 写一次永不重写。若在此加回 --web-addr 之类的参数，
// 改端口就必须重写 unit + daemon-reload，属于回归（测试有断言拦截）。

// serviceArgs 只返回命令行参数（不含二进制路径）。
// OpenRC 的 command 与 command_args 分离，若 args 再带 bin 会执行两次二进制。
func serviceArgs(workDir, cfgFile string) []string {
	args := make([]string, 0, 5)
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

// absPath 写 unit 前把相对路径压成绝对路径：systemd 默认 cwd 常为 /，
// 相对 data/bin/... 会找不到二进制。
//
// 以 "/" 开头的路径视为 Linux 绝对路径原样保留——服务单元只在 Linux 部署，
// Windows 开发机上 filepath.Abs("/opt/...") 会变成 D:\opt\...，污染 unit 与单测。
func absPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// systemdQuoted 为 ExecStart 参数加引号：systemd 按空白切分参数，路径含空格
// 时不引号会被切成两段导致启动失败。
func systemdQuoted(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// openrcQuoted 为 OpenRC command_args 单项加引号（路径含空格时）。
func openrcQuoted(s string) string {
	if strings.ContainsAny(s, " \t\"'") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// renderSystemdUnit 渲染 systemd 单元。ExecStart 与 Start 幂等语义：
// systemd 自身保证「已在运行则 start 是 no-op」，面板无需额外探活。
func renderSystemdUnit(binPath, workDir, cfgFile string) string {
	binPath = absPath(binPath)
	workDir = absPath(workDir)
	cfgFile = absPath(cfgFile)
	parts := []string{systemdQuoted(binPath)}
	for _, a := range serviceArgs(workDir, cfgFile) {
		parts = append(parts, systemdQuoted(a))
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
`, strings.Join(parts, " "))
}

// renderOpenRCService 渲染 Alpine 用的 init.d 脚本（对标面板自身
// /etc/init.d/auroramihomo 的写法）。必须用 supervise-daemon：它才有进程
// 退出后重新拉起的能力；start-stop-daemon 不会拉起，崩溃即常停。
//
// command 与 command_args 分离：command 只放二进制，command_args 只放参数，
// 切勿把 bin 再塞进 args（否则会 exec 两次路径）。
func renderOpenRCService(binPath, workDir, cfgFile string) string {
	binPath = absPath(binPath)
	workDir = absPath(workDir)
	cfgFile = absPath(cfgFile)
	argParts := serviceArgs(workDir, cfgFile)
	quoted := make([]string, 0, len(argParts))
	for _, a := range argParts {
		quoted = append(quoted, openrcQuoted(a))
	}
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
`, workDir, binPath, strings.Join(quoted, " "))
}

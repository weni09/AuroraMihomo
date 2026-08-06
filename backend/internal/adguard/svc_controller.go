package adguard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// 服务控制器：把 AGH 从「面板托管子进程」提升为「系统服务」。
// 面板只做控制面（安装 / 启停 / 自启 / 卸载），进程存活与崩溃重启交给
// systemd / OpenRC——面板升级或重启期间 DNS 过滤不随面板进程中断。
//
// 控制面一律走 systemctl / rc-service，禁止直接 kill：systemd 的
// Restart=always 会把「杀 PID」变成「杀死后 3 秒复活」，等于没停。

const (
	serviceName      = "aurora-adguardhome"
	systemdUnitFile  = "aurora-adguardhome.service"
	openRCInitScript = "aurora-adguardhome"
)

// systemdUnitPath / openRCInitPath 是包级变量便于单测改写为临时路径。
var (
	systemdUnitPath = "/etc/systemd/system/" + systemdUnitFile
	openRCInitPath  = "/etc/init.d/" + openRCInitScript
)

// ServiceController 抽象 AGH 的系统服务管理。nil 表示无服务管理器
// （Windows 等），调用方回落既有的 exec 子进程托管路径。
type ServiceController interface {
	// Install 注册服务单元（不 enable 不 start，语义由 Start/SetBootEnabled 决定）。
	Install(ctx context.Context, binPath, workDir, cfgFile string) error
	// Uninstall 先停再 disable，最后删单元——顺序不能反，否则开机又装回来。
	Uninstall(ctx context.Context) error
	// Start 注册开机自启（enable）并拉起进程，对齐「启动即期望自启」的现状语义。
	Start(ctx context.Context) error
	// Stop 只停进程，保留 enable：用户临时停 ≠ 取消开机自启。
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	// SetBootEnabled 只改开机自启（enable/disable），不启停进程。
	SetBootEnabled(ctx context.Context, enabled bool) error
	// IsEnabled 服务是否已注册开机自启。
	IsEnabled(ctx context.Context) bool
	// Active 服务是否在运行（systemctl is-active / rc-service status）。
	Active(ctx context.Context) bool
}

// lookPathFn 可注入便于单测模拟系统环境（Windows 开发机上无 systemctl）。
var lookPathFn = exec.LookPath

// detectServiceManager 与 scripts/install.sh 的分派一致：有 systemctl 走
// systemd；否则有 rc-update + rc-service 走 OpenRC；都没有（含 Windows）
// 返回 none。刻意不查 /run/systemd/system——chroot 与容器里该目录可能缺失，
// 与 install.sh 的取舍保持一致。
func detectServiceManager() string {
	if _, err := lookPathFn("systemctl"); err == nil {
		return "systemd"
	}
	if _, err := lookPathFn("rc-update"); err == nil {
		if _, err := lookPathFn("rc-service"); err == nil {
			return "openrc"
		}
	}
	return "none"
}

// NewServiceController 探测服务管理器并返回对应实现；探测不到返回 nil，
// 调用方回落 exec 子进程托管（Manager 的既有路径）。
func NewServiceController() ServiceController {
	switch detectServiceManager() {
	case "systemd":
		return &systemdController{}
	case "openrc":
		return &openrcController{}
	default:
		return nil
	}
}

// runCmdFn 可注入便于单测记录命令序列；默认 exec.CommandContext。
var runCmdFn = func(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return nil
}

// outputCmdFn 执行查询类命令并返回 stdout；失败（非 0 退出）返回空。
// systemctl is-enabled/is-active 用退出码表达状态本身，失败不是错误。
var outputCmdFn = func(ctx context.Context, name string, args ...string) string {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cmdOKFn 执行命令并返回是否成功（exit 0），用于 rc-service status 这类
// 以退出码表达状态的查询；失败输出静默丢弃，查询类调用不打印噪音。
var cmdOKFn = func(ctx context.Context, name string, args ...string) bool {
	return exec.CommandContext(ctx, name, args...).Run() == nil
}

// systemdController 管理 /etc/systemd/system/aurora-adguardhome.service。
type systemdController struct{}

func (c *systemdController) Install(ctx context.Context, binPath, workDir, cfgFile string) error {
	content := renderSystemdUnit(binPath, workDir, cfgFile)
	if err := os.WriteFile(systemdUnitPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := runCmdFn(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

func (c *systemdController) Uninstall(ctx context.Context) error {
	// 顺序：stop → disable → 删 unit。disable 先于删文件，保证删后
	// 开机不会按残留 symlink 拉起已不存在的二进制。
	_ = runCmdFn(ctx, "systemctl", "stop", serviceName)
	_ = runCmdFn(ctx, "systemctl", "disable", serviceName)
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	return runCmdFn(ctx, "systemctl", "daemon-reload")
}

func (c *systemdController) Start(ctx context.Context) error {
	if err := runCmdFn(ctx, "systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}
	return runCmdFn(ctx, "systemctl", "start", serviceName)
}

func (c *systemdController) Stop(ctx context.Context) error {
	return runCmdFn(ctx, "systemctl", "stop", serviceName)
}

func (c *systemdController) Restart(ctx context.Context) error {
	return runCmdFn(ctx, "systemctl", "restart", serviceName)
}

func (c *systemdController) SetBootEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		return runCmdFn(ctx, "systemctl", "enable", serviceName)
	}
	return runCmdFn(ctx, "systemctl", "disable", serviceName)
}

// IsEnabled 只认 is-enabled 输出 "enabled"：disabled 与非 0 退出都算 false
// （static/not-found 的退出码差异不可靠，直接比对输出最稳）。
func (c *systemdController) IsEnabled(ctx context.Context) bool {
	return outputCmdFn(ctx, "systemctl", "is-enabled", serviceName) == "enabled"
}

func (c *systemdController) Active(ctx context.Context) bool {
	return outputCmdFn(ctx, "systemctl", "is-active", serviceName) == "active"
}

// openrcController 管理 /etc/init.d/aurora-adguardhome + rc-update。
type openrcController struct{}

func (c *openrcController) Install(ctx context.Context, binPath, workDir, cfgFile string) error {
	content := renderOpenRCService(binPath, workDir, cfgFile)
	if err := os.WriteFile(openRCInitPath, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write openrc init script: %w", err)
	}
	return nil
}

func (c *openrcController) Uninstall(ctx context.Context) error {
	_ = runCmdFn(ctx, "rc-service", openRCInitScript, "stop")
	_ = runCmdFn(ctx, "rc-update", "delete", openRCInitScript)
	if err := os.Remove(openRCInitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove openrc init script: %w", err)
	}
	return nil
}

func (c *openrcController) Start(ctx context.Context) error {
	if err := runCmdFn(ctx, "rc-update", "add", openRCInitScript, "default"); err != nil {
		return fmt.Errorf("rc-update add: %w", err)
	}
	return runCmdFn(ctx, "rc-service", openRCInitScript, "start")
}

func (c *openrcController) Stop(ctx context.Context) error {
	return runCmdFn(ctx, "rc-service", openRCInitScript, "stop")
}

func (c *openrcController) Restart(ctx context.Context) error {
	return runCmdFn(ctx, "rc-service", openRCInitScript, "restart")
}

func (c *openrcController) SetBootEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		return runCmdFn(ctx, "rc-update", "add", openRCInitScript, "default")
	}
	return runCmdFn(ctx, "rc-update", "delete", openRCInitScript)
}

func (c *openrcController) IsEnabled(ctx context.Context) bool {
	out := outputCmdFn(ctx, "rc-update", "show")
	// rc-update show 逐行列出各 runlevel 的服务；本服务只由面板管控，
	// 出现名字即视为已加入（不区分 runlevel，避免解析格式变化）。
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, openRCInitScript) {
			return true
		}
	}
	return false
}

func (c *openrcController) Active(ctx context.Context) bool {
	// supervise-daemon 的 status：服务启动中退出码 0，已停止非 0。
	return cmdOKFn(ctx, "rc-service", openRCInitScript, "status")
}

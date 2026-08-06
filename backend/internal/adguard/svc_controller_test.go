package adguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdRecorder 替换 runCmdFn 记录命令序列，便于断言「控制面走服务管理器」。
type cmdRecorder struct {
	calls []string
}

func (r *cmdRecorder) fn(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func (r *cmdRecorder) called(seq ...string) bool {
	if len(r.calls) != len(seq) {
		return false
	}
	for i, c := range seq {
		if r.calls[i] != c {
			return false
		}
	}
	return true
}

// TestSystemdUnitNoPortArgs 是 D2 的防回归断言：unit 绝不固化端口参数，
// 否则改端口就得重写 unit。
func TestSystemdUnitNoPortArgs(t *testing.T) {
	u := renderSystemdUnit("/opt/auroramihomo/data/bin/AdGuardHome",
		"/opt/auroramihomo/data/adguardhome",
		"/opt/auroramihomo/data/adguardhome/AdGuardHome.yaml")
	for _, forbidden := range []string{"--web-addr", "3000", "dns.port", "--dns"} {
		if strings.Contains(u, forbidden) {
			t.Fatalf("unit 不应包含端口参数 %q:\n%s", forbidden, u)
		}
	}
	for _, want := range []string{
		"ExecStart=",
		"--work-dir /opt/auroramihomo/data/adguardhome",
		"--no-check-update",
		"Restart=always",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("unit 缺少 %q:\n%s", want, u)
		}
	}
}

// TestSystemdUnitQuotesPath 路径含空格时必须引号，否则 systemd 按空白切参数。
func TestSystemdUnitQuotesPath(t *testing.T) {
	u := renderSystemdUnit("/opt/aurora mihomo/bin/AdGuardHome", "/opt/aurora mihomo/data/agh", "")
	if !strings.Contains(u, `"/opt/aurora mihomo/bin/AdGuardHome"`) {
		t.Fatalf("含空格路径未加引号:\n%s", u)
	}
}

func TestOpenRCServiceNoPortArgs(t *testing.T) {
	bin := "/opt/auroramihomo/data/bin/AdGuardHome"
	s := renderOpenRCService(bin,
		"/opt/auroramihomo/data/adguardhome",
		"/opt/auroramihomo/data/adguardhome/AdGuardHome.yaml")
	for _, forbidden := range []string{"--web-addr", "3000", "dns.port"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("init.d 不应包含端口参数 %q:\n%s", forbidden, s)
		}
	}
	for _, want := range []string{
		"supervisor=\"supervise-daemon\"",
		`command="` + bin + `"`,
		"command_args=\"",
		"after firewall",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("init.d 缺少 %q:\n%s", want, s)
		}
	}
	// 防回归：command_args 不得再含二进制路径，否则 OpenRC 会 exec 两次。
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "command_args=") && strings.Contains(line, bin) {
			t.Fatalf("command_args 不应重复包含二进制路径:\n%s", line)
		}
	}
}

// TestUnitTemplatesAbsPath 相对路径写入 unit 前应压成绝对路径。
func TestUnitTemplatesAbsPath(t *testing.T) {
	u := renderSystemdUnit("data/bin/AdGuardHome", "data/adguardhome", "data/adguardhome/AdGuardHome.yaml")
	if strings.Contains(u, "ExecStart=data/") || strings.Contains(u, `ExecStart="data/`) {
		t.Fatalf("systemd unit 仍含相对路径:\n%s", u)
	}
	if !strings.Contains(u, "AdGuardHome") {
		t.Fatalf("systemd unit 缺少二进制名:\n%s", u)
	}
	s := renderOpenRCService("data/bin/AdGuardHome", "data/adguardhome", "")
	if strings.Contains(s, `command="data/`) || strings.Contains(s, "command=data/") {
		t.Fatalf("openrc command 仍含相对路径:\n%s", s)
	}
}

// TestDetectServiceManager 模拟系统环境：有 systemctl 走 systemd；
// 只有 rc-* 走 openrc；都没有（Windows 开发机）返回 none。
func TestDetectServiceManager(t *testing.T) {
	cases := []struct {
		name string
		have map[string]bool
		want string
	}{
		{"systemd", map[string]bool{"systemctl": true, "rc-update": true, "rc-service": true}, "systemd"},
		{"openrc", map[string]bool{"systemctl": false, "rc-update": true, "rc-service": true}, "openrc"},
		{"none", map[string]bool{"systemctl": false, "rc-update": true, "rc-service": false}, "none"},
		{"windows", map[string]bool{}, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := lookPathFn
			lookPathFn = func(name string) (string, error) {
				if tc.have[name] {
					return "/usr/bin/" + name, nil
				}
				return "", errors.New("executable file not found")
			}
			t.Cleanup(func() { lookPathFn = old })
			if got := detectServiceManager(); got != tc.want {
				t.Fatalf("detectServiceManager() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSystemdControllerSequence 断言命令序列与「禁止直接 kill」的控制面约定。
func TestSystemdControllerSequence(t *testing.T) {
	oldRun, oldPath := runCmdFn, systemdUnitPath
	rec := &cmdRecorder{}
	runCmdFn = rec.fn
	systemdUnitPath = filepath.Join(t.TempDir(), systemdUnitFile)
	t.Cleanup(func() {
		runCmdFn = oldRun
		systemdUnitPath = oldPath
	})

	ctx := context.Background()
	c := &systemdController{}

	if err := c.Install(ctx, "/opt/bin/AdGuardHome", "/opt/data/agh", ""); err != nil {
		t.Fatal(err)
	}
	if !rec.called("systemctl daemon-reload") {
		t.Fatalf("Install 命令序列异常: %v", rec.calls)
	}

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !rec.called("systemctl daemon-reload",
		"systemctl enable aurora-adguardhome",
		"systemctl start aurora-adguardhome") {
		t.Fatalf("Start 命令序列异常: %v", rec.calls)
	}

	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !rec.called("systemctl daemon-reload",
		"systemctl enable aurora-adguardhome",
		"systemctl start aurora-adguardhome",
		"systemctl stop aurora-adguardhome") {
		t.Fatalf("Stop 命令序列异常: %v", rec.calls)
	}

	if err := c.SetBootEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !rec.called("systemctl daemon-reload",
		"systemctl enable aurora-adguardhome",
		"systemctl start aurora-adguardhome",
		"systemctl stop aurora-adguardhome",
		"systemctl disable aurora-adguardhome") {
		t.Fatalf("SetBootEnabled(false) 命令序列异常: %v", rec.calls)
	}

	if err := c.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	// Uninstall 顺序固定：stop → disable → 删 unit → daemon-reload。
	// disable 必须先于删文件，否则开机按残留 symlink 拉起已删的二进制。
	if !rec.called("systemctl daemon-reload",
		"systemctl enable aurora-adguardhome",
		"systemctl start aurora-adguardhome",
		"systemctl stop aurora-adguardhome",
		"systemctl disable aurora-adguardhome",
		"systemctl stop aurora-adguardhome",
		"systemctl disable aurora-adguardhome",
		"systemctl daemon-reload") {
		t.Fatalf("Uninstall 命令序列异常: %v", rec.calls)
	}
	if _, err := os.Stat(systemdUnitPath); err == nil {
		t.Fatal("Uninstall 后 unit 文件应已删除")
	}
}

// TestOpenRCControllerSequence 同 systemd 的序列断言，走 rc-* 命令。
func TestOpenRCControllerSequence(t *testing.T) {
	oldRun, oldPath := runCmdFn, openRCInitPath
	rec := &cmdRecorder{}
	runCmdFn = rec.fn
	openRCInitPath = filepath.Join(t.TempDir(), openRCInitScript)
	t.Cleanup(func() {
		runCmdFn = oldRun
		openRCInitPath = oldPath
	})

	ctx := context.Background()
	c := &openrcController{}

	if err := c.Install(ctx, "/opt/bin/AdGuardHome", "/opt/data/agh", ""); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("OpenRC Install 不应执行命令（仅写脚本）: %v", rec.calls)
	}

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !rec.called("rc-update add aurora-adguardhome default",
		"rc-service aurora-adguardhome start") {
		t.Fatalf("Start 命令序列异常: %v", rec.calls)
	}

	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !rec.called("rc-update add aurora-adguardhome default",
		"rc-service aurora-adguardhome start",
		"rc-service aurora-adguardhome stop") {
		t.Fatalf("Stop 命令序列异常: %v", rec.calls)
	}

	if err := c.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	if !rec.called("rc-update add aurora-adguardhome default",
		"rc-service aurora-adguardhome start",
		"rc-service aurora-adguardhome stop",
		"rc-service aurora-adguardhome stop",
		"rc-update delete aurora-adguardhome") {
		t.Fatalf("Uninstall 命令序列异常: %v", rec.calls)
	}
	if _, err := os.Stat(openRCInitPath); err == nil {
		t.Fatal("Uninstall 后 init.d 脚本应已删除")
	}
}

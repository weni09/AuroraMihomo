package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/updater"
)

// 端到端级：build plan → 写 snapshot → patch upstream / base listen → rollback 恢复。
// 不拉真实 AGH 进程，只用 yaml 与临时库。
func TestAdGuardWiringApplyRollbackRoundtrip(t *testing.T) {
	cfgSvc, db, _ := newTestConfigService(t)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "adguardhome")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 最小 AGH 配置：DNS 1053，带一条可被替换的上游
	aghYAML := []byte("bind_host: 127.0.0.1\ndns:\n  port: 1053\n  upstream_dns:\n    - 1.1.1.1\n")
	if err := os.WriteFile(filepath.Join(workDir, "AdGuardHome.yaml"), aghYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	base := `
mode: rule
dns:
  enable: true
  listen: 0.0.0.0:1053
proxies: []
`
	if err := cfgSvc.UpdateBaseConfig(base); err != nil {
		t.Fatal(err)
	}
	// 写入最终 config.yaml，供 KernelDNSPort
	if _, err := cfgSvc.ApplyLocalOnly(context.Background()); err != nil {
		t.Fatalf("ApplyLocalOnly: %v", err)
	}

	binPath := filepath.Join(dir, "bin", "AdGuardHome-fake")
	_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)

	mgr := adguard.NewManager(adguard.Config{BinaryPath: binPath, WorkDir: workDir, WebAddr: "127.0.0.1:3000"})
	upd := updater.New(updater.Config{DataDir: dir, AdGuardBinaryPath: binPath})
	// Transparent 用空实现即可：本测不真的下发防火墙
	transp := NewTransparentService(db, nil, nil, nil,
		func() (string, error) { return cfgSvc.GetBaseConfig() },
		func(c string) error { return cfgSvc.UpdateBaseConfig(c) },
	)
	svc := NewAdGuardService(db, upd, mgr, transp, cfgSvc, workDir, "127.0.0.1:3000")

	opts := WiringOptions{
		RedirectTProxy:  false, // 无 TProxy 环境
		ResolveConflict: true,
		PatchUpstream:   true,
	}
	plan, err := svc.WiringApply(context.Background(), opts)
	if err != nil {
		t.Fatalf("WiringApply: %v", err)
	}
	if plan == nil || !plan.DidResolveConflict || !plan.DidPatchUpstream {
		t.Fatalf("plan 标志不符: %+v", plan)
	}

	// base listen 已改
	listen, err := cfgSvc.BaseDNSListen()
	if err != nil {
		t.Fatal(err)
	}
	if listen != "127.0.0.1:1054" {
		t.Fatalf("apply 后 base listen=%q", listen)
	}
	raw, _ := os.ReadFile(filepath.Join(workDir, "AdGuardHome.yaml"))
	if !strings.Contains(string(raw), "127.0.0.1:1054") {
		t.Fatalf("AGH yaml 应含新上游, got:\n%s", raw)
	}
	if v, _ := db.GetSetting(settingAdGuardWiring); v != adguardWiringOn {
		t.Fatalf("wiring 应为 on, got %q", v)
	}
	// 本测未勾选 Redirect：override 不应改写 TProxy DNS 目标
	// （dnsPortFn 未注入时回落 DefaultDNSPort=1053，与 AGH 同值但路径不同）
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Wiring != adguardWiringOn || st.WiringLabel != "已对接" {
		t.Fatalf("Status wiring: %+v", st)
	}
	if st.Snapshot == nil {
		t.Fatal("Status 应附带 snapshot")
	}
	if st.Snapshot.DidRedirect {
		t.Fatal("未勾选 Redirect 时 snapshot.DidRedirect 应为 false")
	}

	if err := svc.WiringRollback(context.Background()); err != nil {
		t.Fatalf("WiringRollback: %v", err)
	}
	listen, _ = cfgSvc.BaseDNSListen()
	if listen != "0.0.0.0:1053" {
		t.Fatalf("rollback 后 listen 应恢复, got %q", listen)
	}
	raw, _ = os.ReadFile(filepath.Join(workDir, "AdGuardHome.yaml"))
	if !strings.Contains(string(raw), "1.1.1.1") {
		t.Fatalf("rollback 后上游应恢复 1.1.1.1, got:\n%s", raw)
	}
	if v, err := db.GetSetting(settingAdGuardWiring); err == nil && v == adguardWiringOn {
		t.Fatal("rollback 后 wiring 不应仍为 on")
	}
}

func TestConfigServiceBaseDNSListen(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	if err := svc.UpdateBaseConfig("mode: rule\ndns:\n  listen: 0.0.0.0:1053\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.BaseDNSListen()
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0.0.0:1053" {
		t.Fatalf("BaseDNSListen=%q", got)
	}
	if err := svc.SetBaseDNSListen("127.0.0.1:1054"); err != nil {
		t.Fatal(err)
	}
	got, err = svc.BaseDNSListen()
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:1054" {
		t.Fatalf("SetBaseDNSListen 后 = %q", got)
	}
}

// serviceFakeController 记录服务模式编排调用（不触碰真实系统服务）。
type serviceFakeController struct {
	enabled          bool
	active           bool
	installed        bool
	uninstalled      bool
	started          bool
	stopped          bool
	installArgs      []string
	bootEnabledCalls []bool
}

func (f *serviceFakeController) Install(_ context.Context, binPath, workDir, cfgFile string) error {
	f.installed = true
	f.installArgs = []string{binPath, workDir, cfgFile}
	return nil
}

func (f *serviceFakeController) Uninstall(context.Context) error {
	f.uninstalled = true
	return nil
}

func (f *serviceFakeController) Start(context.Context) error { f.started = true; return nil }
func (f *serviceFakeController) Stop(context.Context) error  { f.stopped = true; return nil }

func (f *serviceFakeController) Restart(context.Context) error {
	f.started = true
	f.stopped = true
	return nil
}

func (f *serviceFakeController) SetBootEnabled(_ context.Context, enabled bool) error {
	f.enabled = enabled
	f.bootEnabledCalls = append(f.bootEnabledCalls, enabled)
	return nil
}

func (f *serviceFakeController) IsEnabled(context.Context) bool { return f.enabled }
func (f *serviceFakeController) Active(context.Context) bool    { return f.active }

// newTestAdGuardSvcServiceMode 构造服务模式下的 AdGuardService（controller 已注入）。
func newTestAdGuardSvcServiceMode(t *testing.T) (*AdGuardService, *serviceFakeController) {
	t.Helper()
	cfgSvc, db, _ := newTestConfigService(t)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "adguardhome")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "bin", "AdGuardHome-fake")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	upd := updater.New(updater.Config{DataDir: dir, AdGuardBinaryPath: binPath})
	mgr := adguard.NewManager(adguard.Config{BinaryPath: binPath, WorkDir: workDir, WebAddr: "127.0.0.1:3000"})
	ctrl := &serviceFakeController{enabled: true}
	mgr.SetController(ctrl)
	svc := NewAdGuardService(db, upd, mgr, nil, cfgSvc, workDir, "127.0.0.1:3000")
	return svc, ctrl
}

// TestAdGuardRegisterService 服务模式下注册系统服务单元，参数为安装期路径。
func TestAdGuardRegisterService(t *testing.T) {
	svc, ctrl := newTestAdGuardSvcServiceMode(t)
	svc.registerServiceIfNeeded(context.Background())
	if !ctrl.installed {
		t.Fatal("服务模式应注册系统服务单元")
	}
	if len(ctrl.installArgs) != 3 ||
		ctrl.installArgs[0] != svc.updater.AdGuardBinaryPath() ||
		ctrl.installArgs[1] != svc.workDir ||
		ctrl.installArgs[2] != filepath.Join(svc.workDir, "AdGuardHome.yaml") {
		t.Fatalf("注册参数异常: %v", ctrl.installArgs)
	}
}

// TestAdGuardStop_ServiceMode 服务模式 Stop 只停进程，保留 enable——
// 用户临时停 ≠ 取消开机自启，DesiredRunning 仍反映系统真实状态。
func TestAdGuardStop_ServiceMode(t *testing.T) {
	svc, ctrl := newTestAdGuardSvcServiceMode(t)
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !ctrl.stopped {
		t.Fatal("服务模式 Stop 应调用 controller.Stop")
	}
	if !ctrl.enabled {
		t.Fatal("服务模式 Stop 不应 disable（开机自启保留）")
	}
	if !svc.DesiredRunning() {
		t.Fatal("服务模式 Stop 后 DesiredRunning 应仍为 true（enable 保留）")
	}
}

// TestAdGuardStop_ExecMode exec 模式 Stop 清期望运行态（历史行为）。
func TestAdGuardStop_ExecMode(t *testing.T) {
	cfgSvc, db, _ := newTestConfigService(t)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "adguardhome")
	_ = os.MkdirAll(workDir, 0o755)
	binPath := filepath.Join(dir, "bin", "AdGuardHome-fake")
	_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)
	upd := updater.New(updater.Config{DataDir: dir, AdGuardBinaryPath: binPath})
	mgr := adguard.NewManager(adguard.Config{BinaryPath: binPath, WorkDir: workDir, WebAddr: "127.0.0.1:3000"})
	svc := NewAdGuardService(db, upd, mgr, nil, cfgSvc, workDir, "127.0.0.1:3000")

	if err := svc.setDesiredRunning(true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if svc.DesiredRunning() {
		t.Fatal("exec 模式 Stop 后 DesiredRunning 应为 false")
	}
}

// TestAdGuardDesiredRunning_ServiceMode 自启状态跟随系统 enable 真实状态。
func TestAdGuardDesiredRunning_ServiceMode(t *testing.T) {
	svc, ctrl := newTestAdGuardSvcServiceMode(t)
	ctrl.enabled = false
	if svc.DesiredRunning() {
		t.Fatal("controller disable 后 DesiredRunning 应为 false")
	}
	ctrl.enabled = true
	if !svc.DesiredRunning() {
		t.Fatal("controller enable 后 DesiredRunning 应为 true")
	}
}

// TestAdGuardSetBootEnabled 自启开关：服务模式驱动 controller + settings 回显。
func TestAdGuardSetBootEnabled(t *testing.T) {
	svc, ctrl := newTestAdGuardSvcServiceMode(t)
	if err := svc.SetBootEnabled(context.Background(), false); err != nil {
		t.Fatalf("SetBootEnabled(false): %v", err)
	}
	if ctrl.enabled {
		t.Fatal("SetBootEnabled(false) 应 disable 系统服务")
	}
	if svc.DesiredRunning() {
		t.Fatal("disable 后 DesiredRunning 应为 false")
	}

	if err := svc.SetBootEnabled(context.Background(), true); err != nil {
		t.Fatalf("SetBootEnabled(true): %v", err)
	}
	if !ctrl.enabled {
		t.Fatal("SetBootEnabled(true) 应 enable 系统服务")
	}
}

// TestAdGuardShouldStartAtBoot_ServiceMode 服务模式下面板不负责拉起（恒 false）。
func TestAdGuardShouldStartAtBoot_ServiceMode(t *testing.T) {
	svc, _ := newTestAdGuardSvcServiceMode(t)
	if svc.ShouldStartAtBoot() {
		t.Fatal("服务模式 ShouldStartAtBoot 应恒 false（系统服务自己起）")
	}
}

// TestAdGuardUninstall_ServiceMode 卸载顺序：Stop → 注销服务 → 删二进制 →
// 删 workdir → 清 settings。注销必须发生在删二进制之前。
func TestAdGuardUninstall_ServiceMode(t *testing.T) {
	svc, ctrl := newTestAdGuardSvcServiceMode(t)
	bin := svc.updater.AdGuardBinaryPath()
	workDir := svc.workDir

	if err := svc.Uninstall(context.Background(), true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !ctrl.stopped {
		t.Fatal("卸载应先 Stop")
	}
	if !ctrl.uninstalled {
		t.Fatal("卸载应注销系统服务")
	}
	if _, err := os.Stat(bin); err == nil {
		t.Fatal("卸载后二进制应已删除")
	}
	if _, err := os.Stat(workDir); err == nil {
		t.Fatal("卸载后 workdir 应已删除")
	}
	if svc.ComponentEnabled() {
		t.Fatal("卸载后组件应强制关闭")
	}
}

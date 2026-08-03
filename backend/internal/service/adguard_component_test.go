package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/netcheck"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/updater"
)

func newTestAdGuardService(t *testing.T) (*AdGuardService, *repository.Database) {
	t.Helper()
	cfgSvc, db, _ := newTestConfigService(t)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "adguardhome")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aghYAML := []byte("bind_host: 127.0.0.1\ndns:\n  port: 1053\n  upstream_dns:\n    - 1.1.1.1\n")
	if err := os.WriteFile(filepath.Join(workDir, "AdGuardHome.yaml"), aghYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "bin", "AdGuardHome-fake")
	_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)

	mgr := adguard.NewManager(adguard.Config{BinaryPath: binPath, WorkDir: workDir, WebAddr: "127.0.0.1:3000"})
	upd := updater.New(updater.Config{DataDir: dir, AdGuardBinaryPath: binPath})
	transp := NewTransparentService(db, nil, nil, nil,
		func() (string, error) { return cfgSvc.GetBaseConfig() },
		func(c string) error { return cfgSvc.UpdateBaseConfig(c) },
	)
	svc := NewAdGuardService(db, upd, mgr, transp, cfgSvc, workDir, "127.0.0.1:3000")
	// 无 TProxy 的模式 2 依赖 DNS 重定向 applier；单测注入假实现
	svc.SetDNSRedirectApplier(&fakeDNSRedirectApplier{})
	return svc, db
}

// fakeDNSRedirectApplier 供单元测试下发/拆除 DNS 重定向。
type fakeDNSRedirectApplier struct {
	applied int
}

func (f *fakeDNSRedirectApplier) ApplyDNSRedirect(ctx context.Context, p netcheck.DNSRedirectParams) error {
	f.applied++
	return nil
}

func (f *fakeDNSRedirectApplier) TeardownDNSRedirect(ctx context.Context) error {
	f.applied = 0
	return nil
}

func TestComponentEnabled_DefaultFalse(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	if svc.ComponentEnabled() {
		t.Fatal("默认 component_enabled 应为 false")
	}
}

func TestComponentEnabled_ParseTruthy(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	for _, v := range []string{"true", "TRUE", "True", "1", "yes", "YES"} {
		if err := db.SetSetting(settingAdGuardComponent, v); err != nil {
			t.Fatal(err)
		}
		if !svc.ComponentEnabled() {
			t.Fatalf("值 %q 应视为 enabled", v)
		}
	}
	for _, v := range []string{"false", "0", "no", "", "off"} {
		if err := db.SetSetting(settingAdGuardComponent, v); err != nil {
			t.Fatal(err)
		}
		if svc.ComponentEnabled() {
			t.Fatalf("值 %q 应视为 disabled", v)
		}
	}
}

func TestSetComponentEnabled_TrueThenFalse(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	if err := svc.SetComponentEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !svc.ComponentEnabled() {
		t.Fatal("enable 后应为 true")
	}
	v, err := db.GetSetting(settingAdGuardComponent)
	if err != nil || v != "true" {
		t.Fatalf("settings 应为 true, got %q err=%v", v, err)
	}

	if err := svc.SetComponentEnabled(ctx, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if svc.ComponentEnabled() {
		t.Fatal("disable 后应为 false")
	}
	v, err = db.GetSetting(settingAdGuardComponent)
	if err != nil || v != "false" {
		t.Fatalf("settings 应为 false, got %q err=%v", v, err)
	}
}

func TestSetComponentEnabled_TrueDoesNotStart(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	if err := svc.SetComponentEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Running {
		t.Fatal("enable 不应自动启动进程")
	}
	if !st.ComponentEnabled {
		t.Fatal("Status.ComponentEnabled 应为 true")
	}
}

func TestStatus_ComponentEnabledField(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ComponentEnabled {
		t.Fatal("默认 Status.ComponentEnabled 应为 false")
	}
	_ = svc.SetComponentEnabled(context.Background(), true)
	st, err = svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.ComponentEnabled {
		t.Fatal("enable 后 Status.ComponentEnabled 应为 true")
	}
}

func TestShouldStartAtBoot_RequiresComponentEnabled(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	// boot 开 + 已安装，但 component 关 → 不自启
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	if svc.ShouldStartAtBoot() {
		t.Fatal("component 关闭时不应自启")
	}
	if err := svc.SetComponentEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !svc.ShouldStartAtBoot() {
		t.Fatal("component 与 boot 均开时应自启")
	}
}

func TestSetComponentEnabled_DisableWithWiring(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	// 最小可回滚快照：无 IO 动作，仅清 wiring 标记
	plan := WiringPlan{
		Actions:    []string{"test"},
		AGHDNSPort: 1053,
		WiringOn:   true,
	}
	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetComponentEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}

	if err := svc.SetComponentEnabled(ctx, false); err != nil {
		t.Fatalf("disable with wiring: %v", err)
	}
	if svc.ComponentEnabled() {
		t.Fatal("disable 后 component 应为 false")
	}
	if v, _ := db.GetSetting(settingAdGuardWiring); v == adguardWiringOn {
		t.Fatal("disable 应先解除 wiring")
	}
	if raw, _ := db.GetSetting(settingAdGuardSnapshot); strings.TrimSpace(raw) != "" {
		t.Fatalf("snapshot 应清空, got %q", raw)
	}
}

func TestUninstall_RequiresConfirm(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	err := svc.Uninstall(context.Background(), false)
	if err == nil {
		t.Fatal("confirm=false 应返回错误")
	}
	if !strings.Contains(err.Error(), "请确认卸载") {
		t.Fatalf("错误文案不符: %v", err)
	}
	// 未确认时不应删 workDir
	if _, statErr := os.Stat(svc.workDir); statErr != nil {
		t.Fatalf("confirm=false 不应删除 workDir: %v", statErr)
	}
}

func TestUninstall_RemovesBinaryWorkDirAndSettings(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	bin := svc.updater.AdGuardBinaryPath()
	bak := bin + ".bak"
	if err := os.WriteFile(bak, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.workDir, "extra.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 填充已知 settings + 一个自定义 adguard.* 键
	for k, v := range map[string]string{
		settingAdGuardComponent: "true",
		settingAdGuardBoot:      "true",
		settingAdGuardVersion:   "v0.107.0",
		settingAdGuardDNSPort:   "1053",
		settingAdGuardWebAddr:   "127.0.0.1:3000",
		settingAdGuardWiring:    adguardWiringOff,
		"adguard.sync_password": "true",
		"adguard.custom_flag":   "keep-me-gone",
	} {
		if err := db.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.Uninstall(ctx, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("二进制应已删除, err=%v", err)
	}
	if _, err := os.Stat(bak); !os.IsNotExist(err) {
		t.Fatalf(".bak 应已删除, err=%v", err)
	}
	if _, err := os.Stat(svc.workDir); !os.IsNotExist(err) {
		t.Fatalf("workDir 应已删除, err=%v", err)
	}
	if svc.ComponentEnabled() {
		t.Fatal("卸载后 component_enabled 应为 false")
	}
	if v, err := db.GetSetting(settingAdGuardComponent); err != nil || v != "false" {
		t.Fatalf("component_enabled 应为 false, got %q err=%v", v, err)
	}
	for _, k := range []string{
		settingAdGuardBoot, settingAdGuardVersion, settingAdGuardDNSPort,
		settingAdGuardWebAddr, "adguard.sync_password", "adguard.custom_flag",
	} {
		if v, _ := db.GetSetting(k); strings.TrimSpace(v) != "" {
			t.Fatalf("设置 %s 应已清空, got %q", k, v)
		}
	}
	if svc.BinaryPresent() {
		t.Fatal("卸载后 BinaryPresent 应为 false")
	}
}

func TestUninstall_WithWiringRollsBackFirst(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	plan := WiringPlan{
		Actions:    []string{"test"},
		AGHDNSPort: 1053,
		WiringOn:   true,
	}
	snap, err := marshalWiringSnapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardSnapshot, snap); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardComponent, "true"); err != nil {
		t.Fatal(err)
	}

	if err := svc.Uninstall(ctx, true); err != nil {
		t.Fatalf("Uninstall with wiring: %v", err)
	}
	if v, _ := db.GetSetting(settingAdGuardWiring); strings.TrimSpace(v) == adguardWiringOn {
		t.Fatal("卸载应先解除 wiring")
	}
	if _, err := os.Stat(svc.workDir); !os.IsNotExist(err) {
		t.Fatalf("workDir 应已删除, err=%v", err)
	}
	if svc.ComponentEnabled() {
		t.Fatal("卸载后 component 应为 false")
	}
}

func TestStartStop_PersistsDesiredRunning(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()
	if err := svc.SetComponentEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	// 新测服自带可执行的假脚本（#!/bin/sh），在 Linux 上 cmd.Start 会成功。
	// 验证「启动失败不写 desired」时临时换成不存在的路径；之后恢复，
	// 以便 ShouldStartAtBoot 仍能看到「已安装」的假二进制。
	goodMgr := svc.mgr
	svc.mgr = adguard.NewManager(adguard.Config{
		BinaryPath: filepath.Join(t.TempDir(), "AdGuardHome-missing"),
		WorkDir:    svc.workDir,
		WebAddr:    "127.0.0.1:3000",
	})
	if err := svc.Start(ctx); err == nil {
		t.Fatal("无二进制时应 Start 失败")
	}
	if svc.DesiredRunning() {
		t.Fatal("Start 失败不应标记 desiredRunning")
	}
	svc.mgr = goodMgr
	// 直接写 boot 模拟用户曾启动
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	if !svc.DesiredRunning() || !svc.ShouldStartAtBoot() {
		t.Fatal("应 desired+boot")
	}
	// Stop 幂等成功并清 desired
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if svc.DesiredRunning() {
		t.Fatal("Stop 后 desired 应为 false")
	}
	if svc.ShouldStartAtBoot() {
		t.Fatal("Stop 后不应自启")
	}
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.DesiredRunning {
		t.Fatal("Status.DesiredRunning 应为 false")
	}
}

func TestStopProcess_DoesNotClearDesiredRunning(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	if !svc.DesiredRunning() {
		t.Fatal("precondition")
	}
	if err := svc.StopProcess(ctx); err != nil {
		t.Fatalf("StopProcess: %v", err)
	}
	if !svc.DesiredRunning() {
		t.Fatal("StopProcess 不应清除 enabled_at_boot")
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if svc.DesiredRunning() {
		t.Fatal("用户 Stop 应清除 enabled_at_boot")
	}
}

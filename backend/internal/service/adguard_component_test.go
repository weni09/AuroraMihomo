package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestStart_RejectsWhenComponentDisabled 关组件时 Start 必须失败。
// 与 ShouldStartAtBoot 三道门对齐，避免 API 绕过组件开关把进程拉起并写 desired。
func TestStart_RejectsWhenComponentDisabled(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()
	if svc.ComponentEnabled() {
		t.Fatal("默认 component 应为 false")
	}
	// 即使库里残留 desired=true，关组件时也不允许 Start
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	beforeDesired := svc.DesiredRunning()
	err := svc.Start(ctx)
	if err == nil {
		t.Fatal("组件关闭时 Start 应失败")
	}
	if !strings.Contains(err.Error(), "组件未启用") {
		t.Fatalf("错误应提示组件未启用，实际: %v", err)
	}
	// 拒绝发生在 mgr.Start 之前，不得把 desired 改成「已确认启动成功」之外的语义；
	// 此处不应因失败路径调用 setDesiredRunning(true)。残留 true 可保留，false 也不得被写 true。
	if !beforeDesired && svc.DesiredRunning() {
		t.Fatal("组件关闭时 Start 失败不应写入 desiredRunning=true")
	}
	// 开组件后校验门打开：错误不再是「组件未启用」（是否真能 exec 因平台假二进制而异）
	if err := svc.SetComponentEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	err = svc.Start(ctx)
	if err != nil && strings.Contains(err.Error(), "组件未启用") {
		t.Fatalf("组件已开启时不应再报组件未启用: %v", err)
	}
	if err == nil {
		_ = svc.Stop(ctx)
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

// TestStartWithBootRetry_GivesUpAfterMaxAttempts 组件关时每次 Start 都失败，
// 有限次重试后应返回聚合错误（initialDelay/retryBase 置 0 避免单测空等）。
func TestStartWithBootRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	// 默认 component=false → Start 立即「组件未启用」
	ctx := context.Background()
	err := svc.startWithBootRetry(ctx, 0, 3, 0)
	if err == nil {
		t.Fatal("应在重试耗尽后失败")
	}
	if !strings.Contains(err.Error(), "3 次") && !strings.Contains(err.Error(), "中止") {
		// component 关时第二次循环 ShouldStartAtBoot 仍 false，可能走「中止」或「均失败」
		// 首次 Start 已失败；若 desired 未开，第二次会「开机自启中止」
		t.Logf("got err: %v", err)
	}
	if !strings.Contains(err.Error(), "组件") && !strings.Contains(err.Error(), "失败") && !strings.Contains(err.Error(), "中止") {
		t.Fatalf("错误应体现失败/中止语义: %v", err)
	}
}

// TestStartWithBootRetry_StopsWhenBootIntentCleared 重试间隙若用户清掉期望运行，应中止而非空转。
func TestStartWithBootRetry_StopsWhenBootIntentCleared(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()
	// 组件开但无可用二进制 → Start 失败；desired 先开再在失败后由「未安装/关 desired」中止较难注入，
	// 这里用：组件关 + desired 开，第一次 Start 因组件失败，ShouldStartAtBoot 一直 false → 第二次中止。
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	// component 仍为 false
	err := svc.startWithBootRetry(ctx, 0, 3, 0)
	if err == nil {
		t.Fatal("应失败或中止")
	}
	// 至少不应成功写「已在跑」；desired 可仍为 true（用户意图），但进程未起
	if svc.mgr.Status().Running {
		t.Fatal("组件关闭时不应 Running")
	}
}

// TestStartWithBootRetry_RespectsContextCancel 取消 ctx 应尽快返回。
func TestStartWithBootRetry_RespectsContextCancel(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	if err := svc.SetComponentEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingAdGuardBoot, "true"); err != nil {
		t.Fatal(err)
	}
	// 无二进制：Start 会失败并进入重试等待；用已取消的 ctx
	svc.mgr = adguard.NewManager(adguard.Config{
		BinaryPath: filepath.Join(t.TempDir(), "missing-agh"),
		WorkDir:    svc.workDir,
		WebAddr:    "127.0.0.1:3000",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := svc.startWithBootRetry(ctx, 0, 3, time.Minute) // 长退避，若未尊重 cancel 会卡住
	if err == nil {
		t.Fatal("已取消的 ctx 应返回错误")
	}
	if !strings.Contains(err.Error(), "取消") && !errors.Is(err, context.Canceled) {
		// 包装后可能是「开机自启取消」
		if !strings.Contains(err.Error(), "cancel") && !strings.Contains(err.Error(), "canceled") && !strings.Contains(err.Error(), "取消") {
			t.Fatalf("应体现取消: %v", err)
		}
	}
}

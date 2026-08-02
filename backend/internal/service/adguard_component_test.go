package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/adguard"
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
	return svc, db
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

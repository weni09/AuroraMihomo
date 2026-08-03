package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/updater"
)

func newAdGuardSettingsTestSvc(t *testing.T) (*AdGuardService, *updater.Manager, string) {
	t.Helper()
	cfgSvc, db, _ := newTestConfigService(t)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "adguardhome")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aghYAML := []byte("bind_host: 127.0.0.1\nhttp:\n  address: 127.0.0.1:3000\ndns:\n  port: 1053\n")
	if err := os.WriteFile(filepath.Join(workDir, "AdGuardHome.yaml"), aghYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "bin", "AdGuardHome-fake")
	_ = os.MkdirAll(filepath.Dir(binPath), 0o755)
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755)

	mgr := adguard.NewManager(adguard.Config{BinaryPath: binPath, WorkDir: workDir, WebAddr: "127.0.0.1:3000"})
	upd := updater.New(updater.Config{
		DataDir:           dir,
		AdGuardBinaryPath: binPath,
		CDNProviders:      []string{"https://global.example/"},
	})
	svc := NewAdGuardService(db, upd, mgr, nil, cfgSvc, workDir, "127.0.0.1:3000")
	return svc, upd, workDir
}

func TestSetWebPort_PersistsYamlAndSetting(t *testing.T) {
	svc, _, workDir := newAdGuardSettingsTestSvc(t)
	ctx := context.Background()

	if err := svc.SetWebPort(ctx, 4123); err != nil {
		t.Fatalf("SetWebPort: %v", err)
	}

	port, err := adguard.ReadWebPort(workDir)
	if err != nil || port != 4123 {
		t.Fatalf("yaml port=%d err=%v", port, err)
	}
	if v, _ := svc.db.GetSetting(settingAdGuardWebAddr); v != "127.0.0.1:4123" {
		t.Fatalf("setting web_addr=%q", v)
	}
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.WebAddr != "127.0.0.1:4123" {
		t.Fatalf("Status.WebAddr=%q", st.WebAddr)
	}
}

func TestSetWebPort_Invalid(t *testing.T) {
	svc, _, _ := newAdGuardSettingsTestSvc(t)
	if err := svc.SetWebPort(context.Background(), 0); err == nil {
		t.Fatal("port=0 应失败")
	}
	if err := svc.SetWebPort(context.Background(), 99999); err == nil {
		t.Fatal("port 越界应失败")
	}
}

func TestSetCDNProviders_PersistAndApply(t *testing.T) {
	svc, upd, _ := newAdGuardSettingsTestSvc(t)
	providers := []string{"https://cdn1.example/", "https://cdn2.example/"}
	if err := svc.SetCDNProviders(providers); err != nil {
		t.Fatalf("SetCDNProviders: %v", err)
	}

	raw, err := svc.db.GetSetting(settingAdGuardCDNProviders)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("json: %v raw=%s", err, raw)
	}
	if len(got) != 2 || got[0] != providers[0] {
		t.Fatalf("saved=%v", got)
	}

	listed := svc.CDNProviders()
	if len(listed) != 2 || listed[1] != providers[1] {
		t.Fatalf("CDNProviders()=%v", listed)
	}

	// Install 路径应把专用 CDN 推给 updater（不真下载，只检查 Apply）
	svc.applyAdGuardCDNToUpdater()
	eff := upd.EffectiveCDNProviders()
	if len(eff) < 2 || !strings.Contains(eff[0], "cdn1") {
		t.Fatalf("EffectiveCDNProviders=%v", eff)
	}
}

func TestSetCDNProviders_EmptyClears(t *testing.T) {
	svc, upd, _ := newAdGuardSettingsTestSvc(t)
	_ = svc.SetCDNProviders([]string{"https://x.example/"})
	if err := svc.SetCDNProviders(nil); err != nil {
		t.Fatal(err)
	}
	if len(svc.CDNProviders()) != 0 {
		t.Fatalf("应清空: %v", svc.CDNProviders())
	}
	svc.applyAdGuardCDNToUpdater()
	// 空列表回落到全局 CDN
	eff := upd.EffectiveCDNProviders()
	if len(eff) == 0 || !strings.Contains(eff[0], "global") {
		t.Fatalf("应回落全局 CDN, got %v", eff)
	}
}

func TestSetCredentials_WritesYamlAndUsername(t *testing.T) {
	svc, _, workDir := newAdGuardSettingsTestSvc(t)
	ctx := context.Background()
	if err := svc.SetCredentials(ctx, "aghuser", "test-pass-123"); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	name, err := adguard.ReadUsername(workDir)
	if err != nil || name != "aghuser" {
		t.Fatalf("yaml username=%q err=%v", name, err)
	}
	if v, _ := svc.db.GetSetting(settingAdGuardUsername); v != "aghuser" {
		t.Fatalf("setting username=%q", v)
	}
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Username != "aghuser" {
		t.Fatalf("Status.Username=%q", st.Username)
	}
}

func TestSetAutoUpdateSettings_CronAndToggle(t *testing.T) {
	svc, _, _ := newAdGuardSettingsTestSvc(t)
	if svc.AutoUpdateEnabled() {
		t.Fatal("默认 auto update 应关闭")
	}
	if got := svc.AutoUpdateCron(); got != defaultAdGuardAutoUpdateCron {
		t.Fatalf("默认 cron=%q", got)
	}
	on := true
	if err := svc.SetAutoUpdateSettings(&on, "30 4 * * *"); err != nil {
		t.Fatal(err)
	}
	if !svc.AutoUpdateEnabled() {
		t.Fatal("应已开启")
	}
	if got := svc.AutoUpdateCron(); got != "0 30 4 * * *" {
		t.Fatalf("cron 规范化=%q", got)
	}
	// 组件未启用时 ShouldRun=false
	if svc.ShouldRunAutoUpdate() {
		t.Fatal("组件未启用时不应调度")
	}
	if err := svc.SetComponentEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !svc.ShouldRunAutoUpdate() {
		t.Fatal("组件启用且开关开时应调度")
	}
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.AutoUpdate || st.AutoUpdateCron != "0 30 4 * * *" {
		t.Fatalf("Status auto=%v cron=%q", st.AutoUpdate, st.AutoUpdateCron)
	}
}

func TestSetAutoUpdateSettings_InvalidCron(t *testing.T) {
	svc, _, _ := newAdGuardSettingsTestSvc(t)
	on := true
	if err := svc.SetAutoUpdateSettings(&on, "not-a-cron"); err == nil {
		t.Fatal("非法 cron 应失败")
	}
}

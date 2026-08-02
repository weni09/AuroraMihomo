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

func TestSyncPasswordFromAurora_DisabledNoOp(t *testing.T) {
	svc, _, workDir := newAdGuardSettingsTestSvc(t)
	ctx := context.Background()
	// 先写入一个已知账号
	if err := svc.SetCredentials(ctx, "keepme", "original-pass"); err != nil {
		t.Fatal(err)
	}
	// sync 默认关闭
	if svc.PasswordSyncEnabled() {
		t.Fatal("默认应关闭 sync")
	}
	if err := svc.SyncPasswordFromAurora(ctx, "aurora-new-pass"); err != nil {
		t.Fatalf("sync off 应为 no-op nil: %v", err)
	}
	// yaml 用户名未变；密码仍能用 original（通过重新读 hash 不便，至少 users 仍是 keepme）
	name, _ := adguard.ReadUsername(workDir)
	if name != "keepme" {
		t.Fatalf("no-op 后用户名被改: %q", name)
	}
}

func TestSyncPasswordFromAurora_EnabledWrites(t *testing.T) {
	svc, _, workDir := newAdGuardSettingsTestSvc(t)
	ctx := context.Background()
	if err := svc.SetPasswordSync(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.SetSetting(settingAdGuardUsername, "synced"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncPasswordFromAurora(ctx, "aurora-synced-pass"); err != nil {
		t.Fatalf("SyncPasswordFromAurora: %v", err)
	}
	name, err := adguard.ReadUsername(workDir)
	if err != nil || name != "synced" {
		t.Fatalf("username=%q err=%v", name, err)
	}
	if !svc.PasswordSyncEnabled() {
		t.Fatal("sync 应仍为 true")
	}
	st, _ := svc.Status(ctx)
	if !st.PasswordSync || st.Username != "synced" {
		t.Fatalf("Status sync=%v user=%q", st.PasswordSync, st.Username)
	}
}

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

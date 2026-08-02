package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/internal/adguard"
)

func TestDNSMode_DefaultZero(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	if m := svc.DNSMode(); m != DNSModeNone {
		t.Fatalf("默认 DNSMode 应为 0, got %d", m)
	}
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.DnsMode != 0 {
		t.Fatalf("Status.DnsMode 默认应为 0, got %d", st.DnsMode)
	}
}

func TestDNSMode_MigrateWiringOnToMode2(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	if err := db.SetSetting(settingAdGuardWiring, adguardWiringOn); err != nil {
		t.Fatal(err)
	}
	// 未写 dns_mode 时 wiring=on 视为模式 2
	if m := svc.DNSMode(); m != DNSModeRedirect {
		t.Fatalf("wiring=on 迁移应为 mode 2, got %d", m)
	}
}

func TestSetDNSMode1_WhenPortFree(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	// mock：53 空闲
	prev := udpPortInUseFn
	udpPortInUseFn = func(port int) bool { return false }
	t.Cleanup(func() { udpPortInUseFn = prev })

	if err := svc.SetDNSMode(ctx, DNSModeBind53); err != nil {
		t.Fatalf("SetDNSMode(1): %v", err)
	}
	if svc.DNSMode() != DNSModeBind53 {
		t.Fatalf("DNSMode 应为 1, got %d", svc.DNSMode())
	}
	v, _ := db.GetSetting(settingAdGuardDNSMode)
	if v != "1" {
		t.Fatalf("settings dns_mode 应为 1, got %q", v)
	}
	port, err := adguard.ReadDNSPort(svc.workDir)
	if err != nil || port != 53 {
		t.Fatalf("yaml dns.port 应为 53, port=%d err=%v", port, err)
	}
	st, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.DnsMode != 1 || st.DNSPort != 53 {
		t.Fatalf("Status DnsMode/DNSPort = %d/%d", st.DnsMode, st.DNSPort)
	}
}

func TestSetDNSMode1_RejectsWhenPortBusy(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	prev := udpPortInUseFn
	udpPortInUseFn = func(port int) bool { return port == 53 }
	t.Cleanup(func() { udpPortInUseFn = prev })

	err := svc.SetDNSMode(context.Background(), DNSModeBind53)
	if err == nil {
		t.Fatal("53 占用时应失败")
	}
	if !strings.Contains(err.Error(), "53") {
		t.Fatalf("错误应提及 53: %v", err)
	}
	if !strings.Contains(err.Error(), "占用") {
		t.Fatalf("错误应为中文占用说明: %v", err)
	}
	if svc.DNSMode() != DNSModeNone {
		t.Fatalf("失败后模式应仍为 0, got %d", svc.DNSMode())
	}
}

func TestSetDNSMode0_AfterMode2(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	// 准备可对接的 mihomo DNS（与 AGH 同端口以触发冲突解决+上游补丁）
	base := `
mode: rule
dns:
  enable: true
  listen: 0.0.0.0:1053
proxies: []
`
	if err := svc.cfgSvc.UpdateBaseConfig(base); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.cfgSvc.ApplyLocalOnly(ctx); err != nil {
		t.Fatalf("ApplyLocalOnly: %v", err)
	}

	if err := svc.SetDNSMode(ctx, DNSModeRedirect); err != nil {
		t.Fatalf("SetDNSMode(2): %v", err)
	}
	if svc.DNSMode() != DNSModeRedirect {
		t.Fatalf("应为 mode 2, got %d", svc.DNSMode())
	}
	if v, _ := db.GetSetting(settingAdGuardWiring); v != adguardWiringOn {
		t.Fatalf("mode 2 后 wiring 应为 on, got %q", v)
	}
	if v, _ := db.GetSetting(settingAdGuardDNSMode); v != "2" {
		t.Fatalf("dns_mode 应为 2, got %q", v)
	}

	if err := svc.SetDNSMode(ctx, DNSModeNone); err != nil {
		t.Fatalf("SetDNSMode(0): %v", err)
	}
	if svc.DNSMode() != DNSModeNone {
		t.Fatalf("回 0 后 DNSMode 应为 0, got %d", svc.DNSMode())
	}
	if v, _ := db.GetSetting(settingAdGuardWiring); v == adguardWiringOn {
		t.Fatal("mode 0 应解除 wiring")
	}
	if v, _ := db.GetSetting(settingAdGuardDNSMode); v != "0" {
		t.Fatalf("dns_mode 应为 0, got %q", v)
	}
	if raw, _ := db.GetSetting(settingAdGuardSnapshot); strings.TrimSpace(raw) != "" {
		t.Fatalf("snapshot 应清空, got %q", raw)
	}
	// 上游应恢复
	raw, _ := os.ReadFile(filepath.Join(svc.workDir, "AdGuardHome.yaml"))
	if !strings.Contains(string(raw), "1.1.1.1") {
		t.Fatalf("rollback 后上游应恢复, got:\n%s", raw)
	}
}

func TestSetComponentEnabled_DisableSetsDNSMode0(t *testing.T) {
	svc, db := newTestAdGuardService(t)
	ctx := context.Background()

	if err := db.SetSetting(settingAdGuardDNSMode, "1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetComponentEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetComponentEnabled(ctx, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if v, _ := db.GetSetting(settingAdGuardDNSMode); v != "0" {
		t.Fatalf("关闭组件后 dns_mode 应为 0, got %q", v)
	}
	if svc.DNSMode() != DNSModeNone {
		t.Fatalf("DNSMode 应为 0, got %d", svc.DNSMode())
	}
}

func TestSetDNSMode_Invalid(t *testing.T) {
	svc, _ := newTestAdGuardService(t)
	if err := svc.SetDNSMode(context.Background(), DNSMode(9)); err == nil {
		t.Fatal("无效模式应报错")
	}
}

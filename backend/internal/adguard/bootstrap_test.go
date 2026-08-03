package adguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureBootstrapConfig_CreatesCompleteConfig(t *testing.T) {
	dir := t.TempDir()
	pass, err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", 1053, "admin", "secret123")
	if err != nil {
		t.Fatal(err)
	}
	if pass != "" {
		t.Fatalf("caller-supplied password should not return generated pass, got %q", pass)
	}
	if !IsBootstrapComplete(dir) {
		t.Fatal("should be complete")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "AdGuardHome.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"schema_version", "users", "http:", "dns:", "upstream_dns"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestEnsureBootstrapConfig_UpdatesHTTPAddress(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", 53, "admin", "first-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureBootstrapConfig(dir, "127.0.0.1:3001", 53, "admin", "ignored"); err != nil {
		t.Fatal(err)
	}
	m, _, err := loadConfigMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := asMap(m["http"])
	if h["address"] != "127.0.0.1:3001" {
		t.Fatalf("http address not updated: %v", h["address"])
	}
}

func TestEnsureBootstrapConfig_GeneratesRandomPassword(t *testing.T) {
	dir := t.TempDir()
	pass, err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", DefaultDNSPort, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pass) < 16 {
		t.Fatalf("generated pass too short: %q", pass)
	}
	// 不得再是固定弱口令
	if pass == "AuroraChangeMe" {
		t.Fatal("must not use fixed AuroraChangeMe")
	}
	b, err := os.ReadFile(filepath.Join(dir, "initial_admin_password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), pass) {
		t.Fatalf("initial file missing pass: %s", b)
	}
	// 第二次引导不应再生成
	pass2, err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", DefaultDNSPort, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if pass2 != "" {
		t.Fatalf("second bootstrap should not regenerate, got %q", pass2)
	}
}

func TestSanitizePollutionProneDNS(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", DefaultDNSPort, "admin", "x"); err != nil {
		t.Fatal(err)
	}
	// 故意写入污染源
	m, _, err := loadConfigMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	dns := asMap(m["dns"])
	dns["bootstrap_dns"] = []any{"8.8.8.8", "1.1.1.1", "223.5.5.5"}
	dns["fallback_dns"] = []any{"1.1.1.1", "127.0.0.1:1053"}
	m["dns"] = dns
	if err := saveConfigMap(dir, m); err != nil {
		t.Fatal(err)
	}
	if err := SanitizePollutionProneDNS(dir); err != nil {
		t.Fatal(err)
	}
	m2, _, _ := loadConfigMap(dir)
	dns2 := asMap(m2["dns"])
	boot := asStringList(dns2["bootstrap_dns"])
	for _, b := range boot {
		if b == "8.8.8.8" || b == "1.1.1.1" {
			t.Fatalf("bootstrap still has public DNS: %v", boot)
		}
	}
	fb := asStringList(dns2["fallback_dns"])
	for _, f := range fb {
		if f == "1.1.1.1" || f == "8.8.8.8" {
			t.Fatalf("fallback still has public DNS: %v", fb)
		}
	}
}

package adguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureBootstrapConfig_CreatesCompleteConfig(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", 1053, "admin", "secret123"); err != nil {
		t.Fatal(err)
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
	if err := EnsureBootstrapConfig(dir, "127.0.0.1:3000", 53, "admin", "first-pass"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBootstrapConfig(dir, "127.0.0.1:3001", 53, "admin", "ignored"); err != nil {
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

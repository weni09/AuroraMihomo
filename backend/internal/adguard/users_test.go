package adguard

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

func TestSetUserPassword_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "bind_host: 127.0.0.1\ndns:\n  port: 1053\n")

	if err := SetUserPassword(dir, "admin", "s3cret-pass"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "AdGuardHome.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	users, ok := m["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("users=%v", m["users"])
	}
	u := asMap(users[0])
	if u["name"] != "admin" {
		t.Fatalf("name=%v", u["name"])
	}
	hash, ok := u["password"].(string)
	if !ok || hash == "" {
		t.Fatalf("password missing: %v", u["password"])
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cret-pass")); err != nil {
		t.Fatalf("bcrypt verify: %v hash=%s", err, hash)
	}
	// 其它键应保留
	if asMap(m["dns"]) == nil {
		t.Fatal("dns section lost")
	}
}

func TestSetUserPassword_DefaultUsername(t *testing.T) {
	dir := t.TempDir()
	if err := SetUserPassword(dir, "  ", "plain-pwd-1"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	name, err := ReadUsername(dir)
	if err != nil || name != "admin" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

func TestSetUserPassword_EmptyPassword(t *testing.T) {
	dir := t.TempDir()
	if err := SetUserPassword(dir, "admin", ""); err == nil {
		t.Fatal("empty password should fail")
	}
}

func TestSetUserPassword_ReplacesUsers(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "users:\n  - name: old\n    password: x\n  - name: extra\n    password: y\n")
	if err := SetUserPassword(dir, "newadmin", "new-password-ok"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	m := readAGHMap(t, dir)
	users, _ := m["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want single user, got %d", len(users))
	}
	if asMap(users[0])["name"] != "newadmin" {
		t.Fatalf("name=%v", asMap(users[0])["name"])
	}
}

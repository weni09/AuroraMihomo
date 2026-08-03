package service

import (
	"path/filepath"
	"testing"

	"auroramihomo/backend/internal/repository"
)

func TestAGHCredStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := repository.NewDatabase(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := NewAGHCredStore(db, "test-master-key-for-agh-sso!!")
	if err := store.Save("ops", "s3cret-密码"); err != nil {
		t.Fatal(err)
	}
	// 密文不应明文落库
	raw, _ := db.GetSetting(settingAdGuardSSOPasswordEnc)
	if raw == "" || raw == "s3cret-密码" {
		t.Fatalf("unexpected sealed %q", raw)
	}
	u, p, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if u != "ops" || p != "s3cret-密码" {
		t.Fatalf("load u=%q p=%q", u, p)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	_, p2, err := store.Load()
	if err != nil || p2 != "" {
		t.Fatalf("after clear p=%q err=%v", p2, err)
	}
}

package auth

import "testing"

func TestSealOpenRoundTrip(t *testing.T) {
	master := "jwt-access-secret-for-test-32b!!"
	plain := "agh-admin-password-你好"
	sealed, err := SealString(master, plain)
	if err != nil {
		t.Fatal(err)
	}
	if sealed == plain || sealed == "" {
		t.Fatalf("sealed should differ: %q", sealed)
	}
	got, err := OpenString(master, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("got %q want %q", got, plain)
	}
	// 错误 master 应失败
	if _, err := OpenString("wrong-master-key-xxxxxxxxxxxx", sealed); err == nil {
		t.Fatal("wrong master should fail")
	}
}

func TestSealEmpty(t *testing.T) {
	if _, err := SealString("m", ""); err == nil {
		t.Fatal("empty plain should fail")
	}
}

package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// writeTarGz 写一个简单 tar.gz，entries 为 (name, content)。
func writeTarGz(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, body := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUntarGzExtractsBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "agh.tar.gz")
	payload := []byte("fake-adguardhome-binary")
	writeTarGz(t, src, map[string][]byte{
		"AdGuardHome/AdGuardHome": payload,
	})

	dest := filepath.Join(dir, "extract")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := untarGz(src, dest); err != nil {
		t.Fatalf("untarGz 失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "AdGuardHome", "AdGuardHome"))
	if err != nil {
		t.Fatalf("读解压结果失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("内容不符: got %q", got)
	}
}

// 路径穿越条目必须拒绝，防止恶意 release 写出提取目录之外
func TestUntarGzRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.tar.gz")
	writeTarGz(t, src, map[string][]byte{
		"../evil.bin": []byte("pwned"),
	})

	dest := filepath.Join(dir, "extract")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := untarGz(src, dest); err == nil {
		t.Fatal("含路径穿越的 tar.gz 应报错")
	}
	// 确认没有写出到 dest 的父目录
	if _, err := os.Stat(filepath.Join(dir, "evil.bin")); err == nil {
		t.Fatal("路径穿越成功写出了提取目录外的文件")
	}
}

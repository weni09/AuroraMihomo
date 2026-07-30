package updater

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Linux/macOS 的 mihomo 资产是单文件 gzip（不是 tar.gz），解出来直接就是
// 可执行二进制。这条路径与 Windows 的 zip 归档完全不同，需要单独覆盖。
func TestGunzipFileExtractsPayload(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("#!/bin/sh\necho fake-mihomo\n")

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	// 官方资产的 gzip 头里带原始文件名，解压时应忽略它、
	// 只按调用方给的目标路径落盘
	zw.Name = "mihomo-linux-amd64-v1.19.29"
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("写 gzip 失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败: %v", err)
	}

	src := filepath.Join(dir, "mihomo-linux-amd64-v1.19.29.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	dest := filepath.Join(dir, "out", "mihomo")
	if err := gunzipFile(src, dest); err != nil {
		t.Fatalf("gunzipFile 失败: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读解压结果失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("解压内容不符\n期望 %q\n实际 %q", payload, got)
	}

	// 内核二进制必须可执行。Windows 无 Unix 权限位，跳过。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("stat 失败: %v", err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("解压出的二进制缺少可执行位，实际权限 %v", info.Mode().Perm())
		}
	}
}

// 目标目录不存在时应自动创建（下载流程里 extract 子目录是新建的）
func TestGunzipFileCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte("x"))
	_ = zw.Close()

	src := filepath.Join(dir, "a.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "deep", "nested", "mihomo")
	if err := gunzipFile(src, dest); err != nil {
		t.Fatalf("应能自动建目录: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("目标文件未生成: %v", err)
	}
}

// 损坏的 gzip 要报错而不是静默产出空文件——空文件会被后续的
// `mihomo -v` 自检拦下，但报错点越早越好定位
func TestGunzipFileRejectsCorruptInput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.gz")
	if err := os.WriteFile(src, []byte("not gzip at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gunzipFile(src, filepath.Join(dir, "out")); err == nil {
		t.Error("损坏的 gzip 应报错")
	}
}

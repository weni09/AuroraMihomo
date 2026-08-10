package updater

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestUpdateMihomoBacksUpExistingBinary 验证 UpdateMihomo 在目标已存在时
// 先生成 .bak 再替换：升级路径上万一替换出错，还有一份旧内核可手工恢复。
//
// 完整走一遍"查 release → 下载 → 解压 → 临时校验 → 备份 → 替换"的真实链路。
// 资产用 gzip 压缩的可执行 shell 脚本冒充内核，校验阶段执行 `-v` 会成功。
// Windows 上无法构造 zip 内的可执行资产，跳过（CI 在 Linux 上覆盖）。
func TestUpdateMihomoBacksUpExistingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 资产是 zip 内 exe，测试资产不便构造")
	}

	// 资产内容：可执行 shell 脚本，-v 调用会输出并正常退出
	script := []byte("#!/bin/sh\necho mihomo-v1.2.3\n")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	gzBytes := gz.Bytes()

	assetName := fmt.Sprintf("mihomo-%s-%s-v1.2.3.gz", runtime.GOOS, runtime.GOARCH)
	dir := t.TempDir()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.3",
				"assets": []map[string]any{
					{
						"name":                 assetName,
						"browser_download_url": srv.URL + "/assets/" + assetName,
						"size":                 len(gzBytes),
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, ".gz"):
			_, _ = w.Write(gzBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	m := New(Config{
		DataDir:        dir,
		MihomoRepo:     "mihomo-repo/x",
		GitHubAPI:      srv.URL,
		CDNProviders:   []string{},
		UseMihomoProxy: false,
	})

	// 预置"旧内核"，内容要与新内核区分
	target := m.MihomoBinaryPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := m.UpdateMihomo(context.Background()); err != nil {
		t.Fatalf("UpdateMihomo 失败: %v", err)
	}

	// 旧内核应备份为 .bak 且内容保留
	bak, err := os.ReadFile(target + ".bak")
	if err != nil {
		t.Fatalf(".bak 备份未生成: %v", err)
	}
	if string(bak) != "OLD-BINARY" {
		t.Fatalf(".bak 内容应为旧内核，实际 %q", string(bak))
	}

	// 新内核已替换到位（内容为解压出的脚本而非旧内容）
	cur, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cur, []byte("mihomo-v1.2.3")) {
		t.Fatalf("目标二进制应被新内核替换，实际内容不含新内核特征")
	}
}

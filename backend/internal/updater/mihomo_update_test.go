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

// paddedShellScript 返回可执行 shell 脚本内容，并用递增数字注释行填充，
// 使 gzip 压缩后的归档体积 >= 1024 字节。
//
// 为什么需要填充：downloadWithCDN 对下载产物有 1024 字节的最小体积校验
// （防空文件/截断），真实内核二进制以 MB 计，测试伪造的小脚本压缩后往往
// 只有几十字节，在 Linux CI 上会被当成"无效文件"拒绝——而 Windows 上
// 这类测试要么跳过、要么走 zip/exe 分支（体积天然够大），掩盖了问题。
// 填充内容刻意用递增数字而非重复模式：重复内容 gzip 压缩率极高，
// 填了也白填。
func paddedShellScript(echoLine string) []byte {
	body := []byte("#!/bin/sh\n" + echoLine + "\n")
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		// 直接在 builder 上格式化：先 Sprintf 再 WriteString 是双份拷贝，
		// staticcheck QF1012 也要求合并成 Fprintf。
		fmt.Fprintf(&sb, "# pad %d %d %d %d %d\n", i, i*7, i*13, i*31, i*101)
	}
	return append(body, sb.String()...)
}

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

	// 资产内容：可执行 shell 脚本，-v 调用会输出并正常退出。
	// 用递增数字注释填充到压缩后 >=1024 字节（见 paddedShellScript）：
	// downloadWithCDN 对下载产物有 1024 字节的最小体积校验（防空文件/截断），
	// 真实内核二进制远超该值，测试伪造的小脚本若不填充会被当成无效文件。
	script := paddedShellScript("echo mihomo-v1.2.3")
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Content-Type", "application/json")
			// r.Host 即本 mock 服务器地址：assets 的下载地址指向自己。
			// 用请求 host 而非闭包引用 srv，避免 httptest.NewServer 的
			// 初始化表达式里引用自身变量（Go 的 := 左侧在右侧不可见）。
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.2.3",
				"assets": []map[string]any{
					{
						"name":                 assetName,
						"browser_download_url": "http://" + r.Host + "/assets/" + assetName,
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

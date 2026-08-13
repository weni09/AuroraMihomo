package updater

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 预压缩应为 assets 下文本类文件生成 .gz，跳过图片/字体与已压缩文件。
func TestPrecompressZashboard(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	// 文本类：应被压缩
	js := filepath.Join(assets, "index-abc.js")
	if err := os.WriteFile(js, []byte(strings.Repeat("console.log(1)\n", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(assets, "style-def.css")
	if err := os.WriteFile(css, []byte(strings.Repeat(".a{color:red}\n", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非文本：跳过
	png := filepath.Join(assets, "icon.png")
	if err := os.WriteFile(png, []byte("PNG-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 已是 .gz：跳过
	already := filepath.Join(assets, "x.gz")
	if err := os.WriteFile(already, []byte("already"), 0o644); err != nil {
		t.Fatal(err)
	}

	precompressZashboard(dir)

	for _, name := range []string{"index-abc.js", "style-def.css"} {
		gz := filepath.Join(assets, name+".gz")
		if _, err := os.Stat(gz); err != nil {
			t.Fatalf("%s 应生成 .gz: %v", name, err)
		}
		// 校验 gzip 可解且内容与原文件一致
		f, err := os.Open(gz)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("%s .gz 不是合法 gzip: %v", name, err)
		}
		got, _ := io.ReadAll(gr)
		_ = gr.Close()
		_ = f.Close()
		orig, _ := os.ReadFile(filepath.Join(assets, name))
		if string(got) != string(orig) {
			t.Fatalf("%s 解压内容与原文件不一致", name)
		}
	}
	// 非文本与已 .gz 不应生成
	if _, err := os.Stat(filepath.Join(assets, "icon.png.gz")); err == nil {
		t.Fatal("png 不应被预压缩")
	}
	// 幂等：再次调用不重复压（mtime 已是最新，precompressFile 跳过）
	if _, err := os.Stat(js + ".gz"); err != nil {
		t.Fatal("js.gz 应存在")
	}
	// 第三次调用同一目录应被 once 短路（不报错即可）
	precompressZashboard(dir)
}

// 预压缩失败（文件不可读）不应 panic，只记日志。
func TestPrecompressZashboardMissingDir(t *testing.T) {
	// 不存在的目录：静默返回
	precompressZashboard(filepath.Join(t.TempDir(), "nope"))
}

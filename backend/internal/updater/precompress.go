package updater

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// precompressOnce 保证同一目录只预压缩一次：zashboard 更新替换整目录，
// 第二次 UpdateZashboard 会整体换掉 assets/，旧 .gz 不复存在，无需重压。
var precompressOnce sync.Map // zashboardDir -> struct{}

// zashboardPrecompressExts 需要预压缩的文件扩展名。
// 与 api 包 gzippableContentType 保持一致：文本类可压，图片/字体已内压。
var zashboardPrecompressExts = map[string]bool{
	".js": true, ".css": true, ".html": true, ".json": true,
	".svg": true, ".wasm": true, ".txt": true,
}

// precompressZashboard 扫描 zashboard assets 目录，为文本类文件生成 .gz
// 预压缩副本，供静态服务直传（免每次请求运行时 gzip）。
//
// 为什么放在更新流程而非请求时惰性生成：
//   - 请求时首次命中要现压一次（和现在的运行时 gzip 无差别），并发下会
//     重复压同一文件；更新时一次压完，之后所有请求零 CPU。
//   - 更新是低频操作（手动或每天一次），压 4MB 文本耗时可忽略。
//
// 幂等：目录级 once 保证同一版本不重复压；.gz 已存在且比原文件新则跳过。
// 失败只记日志不阻断更新——预压缩是优化，缺失时请求侧回退运行时 gzip。
func precompressZashboard(dir string) {
	if dir == "" {
		return
	}
	if _, loaded := precompressOnce.LoadOrStore(dir, struct{}{}); loaded {
		return
	}
	assetsDir := filepath.Join(dir, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		// assets 目录不存在（异常安装）不是致命问题，运行时 gzip 兜底
		return
	}
	var n int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".gz") {
			continue
		}
		if !zashboardPrecompressExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		src := filepath.Join(assetsDir, name)
		if err := precompressFile(src); err != nil {
			logx.Errorf("zashboard 预压缩 %s 失败: %v", name, err)
			continue
		}
		n++
	}
	if n > 0 {
		logx.Infof("zashboard 预压缩完成: %d 个文本资源（%s）", n, assetsDir)
	}
}

// precompressFile 把单个文件 gzip(-9) 成 <file>.gz。
// .gz 已存在且比原文件新（mtime）时跳过——更新流程整体替换目录后
// 不会有旧 .gz，这里主要防同目录二次写入。
func precompressFile(src string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	dst := src + ".gz"
	if gzSt, gzErr := os.Stat(dst); gzErr == nil && gzSt.ModTime().After(st.ModTime()) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	gz.ModTime = st.ModTime()
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

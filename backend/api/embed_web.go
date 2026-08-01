package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:public
var embeddedWebFS embed.FS

// getWebFS 返回用于提供静态资源服务的 http.FileSystem。
// 优先检查本地磁盘目录（如 "./public" 或 "./frontend/dist"），如果其中存在 index.html，
// 则优先使用本地磁盘静态资源（方便开发调试或自定义静态文件）；
// 否则降级使用编译内嵌的 embeddedWebFS 资源。
func getWebFS() http.FileSystem {
	for _, dir := range []string{"./public", "./frontend/dist"} {
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			return http.Dir(dir)
		}
	}

	subFS, err := fs.Sub(embeddedWebFS, "public")
	if err != nil {
		return http.FS(embeddedWebFS)
	}
	return http.FS(subFS)
}

// spaFileSystemServer 返回一个 http.Handler，用于从 fsysProvider 提供静态文件服务。
// 对于请求的具体静态文件存在时正常响应；若文件不存在（例如前端 SPA 的路由路径），
// 则自动降级返回 index.html 内容，以支持前端单页应用（SPA）的客户端路由。
//
// fsysProvider 在每次请求时调用而不是启动时求值一次：getWebFS 的
// 磁盘优先/内嵌降级判断必须反映当前磁盘状态——部署后删掉磁盘 public/
// 目录应立即回退到内嵌资源，而不是继续指向已删除的目录（404）。
func spaFileSystemServer(routePrefix string, fsysProvider func() http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fsys := fsysProvider()
		rel := strings.TrimPrefix(r.URL.Path, routePrefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = "index.html"
		}

		cleanPath := filepath.ToSlash(filepath.Clean("/" + rel))
		cleanPath = strings.TrimPrefix(cleanPath, "/")

		// 检查文件系统中是否存在该路径对应的实体文件
		f, err := fsys.Open(cleanPath)
		if err == nil {
			stat, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !stat.IsDir() {
				http.StripPrefix(routePrefix, http.FileServer(fsys)).ServeHTTP(w, r)
				return
			}
		}

		// 如果文件不存在或为目录，则回退到 index.html 以支持 SPA 路由
		fIndex, err := fsys.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer fIndex.Close()

		stat, err := fIndex.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if rs, ok := fIndex.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
			return
		}

		http.NotFound(w, r)
	})
}

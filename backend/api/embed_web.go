package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed all:public
var embeddedWebFS embed.FS

// getWebFS 返回用于提供静态资源服务的 http.FileSystem。
// 固定使用编译期内嵌资源：前端产物由 make build-frontend 同步进
// backend/api/public（go:embed 源），运行时不再读磁盘 public/。
// 曾支持磁盘优先（./public、./frontend/dist 便于开发调试或自定义静态
// 文件），但双链路让「改了前端没进二进制也能生效」成为隐式行为——
// 单二进制方案下内嵌资源即唯一真相，升级时前端与后端永远同版。
func getWebFS() http.FileSystem {
	subFS, err := fs.Sub(embeddedWebFS, "public")
	if err != nil {
		return http.FS(embeddedWebFS)
	}
	return http.FS(subFS)
}

// cachedFileServer 包一层 http.FileServer：按路径设定缓存策略。
//
// hash 内容寻址的构建产物（/assets/）不随内容变化改名，可长缓存；
// 其余路径（index.html 等）必须可重验，让 SPA 升级后能拿到引用新
// hash 的新页面。FileServer 自带 Last-Modified 条件请求，no-cache
// 路径仍能命中 304 省流量。
func cachedFileServer(fsys http.FileSystem, path string) http.Handler {
	fs := http.FileServer(fsys)
	if strings.HasPrefix(path, "assets/") {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fs.ServeHTTP(w, r)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fs.ServeHTTP(w, r)
	})
}

// spaFileSystemServer 返回一个 http.Handler，用于从 fsysProvider 提供静态文件服务。
// 对于请求的具体静态文件存在时正常响应；若文件不存在（例如前端 SPA 的路由路径），
// 则自动降级返回 index.html 内容，以支持前端单页应用（SPA）的客户端路由。
//
// fsysProvider 在每次请求时调用而不是启动时求值一次：/ui 的 zashboard
// 目录可能被运行时替换（面板更新），按请求求值让它始终读到当前状态。
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
				// 命中真实文件：走带缓存策略的 FileServer。
				// /assets/ 下的构建产物文件名含内容 hash，内容寻址后
				// 可安全长缓存——这是「手机端反复白屏、刷新多次才出」的
				// 关键修复：此前无 Cache-Control，zashboard 30+ 个资源
				// 每次打开全部重新验证（同源 6 连接池 + 单核 gzip 排队），
				// 缓存热起来之前页面一直白屏。hash 文件名不变则内容不变，
				// 用 immutable 让浏览器跳过一切条件请求。
				http.StripPrefix(routePrefix, cachedFileServer(fsys, cleanPath)).ServeHTTP(w, r)
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
			// SPA 回退同样只给 index.html：必须可重验（no-cache），
			// 让前端升级后能拿到引用新 hash 资源的入口页
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
			return
		}

		http.NotFound(w, r)
	})
}

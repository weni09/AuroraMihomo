package main

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// 静态资源 gzip 压缩。
//
// 背景：Vite 产物主 bundle 约 1.16MB，此前静态服务裸传（无 Content-Encoding），
// 手机弱网首载要 10s+，且只有刷新命中浏览器缓存后才显得「快」——这是
// 「初次打开卡很久、刷新才行」的主因。API 响应与 /adguard-ui 反代不走这层：
// 前者是 JSON 小响应，后者上游是 AGH 自身。
//
// 实现要点：
//   - http.FileServer 在 WriteHeader 时才设置 Content-Type，外层无法提前
//     预判，因此延迟到首个 WriteHeader：文本类且客户端接受 gzip 才启用。
//   - 只压缩文本类（图片/字体本身已内压，再压徒增 CPU）。
//   - 304 无 body 不压缩（否则给 304 错加 Content-Encoding 会污染缓存）；
//     Range 请求不压缩（gzip 后字节偏移失去意义）。
//   - gzip 流收尾依赖 next handler 同步返回：FileServer/ServeContent 都在
//     ServeHTTP 返回前写完 body，defer close 恰好完成流结束标记
//     （EOF+CRC），浏览器才能解出完整内容。
func staticGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// acceptsGzip 解析 Accept-Encoding，只认显式 gzip；gzip;q=0 表示不接受。
func acceptsGzip(ae string) bool {
	for _, part := range strings.Split(ae, ",") {
		p := strings.TrimSpace(part)
		if p == "gzip" {
			return true
		}
		if strings.HasPrefix(p, "gzip;") {
			if strings.Contains(p, "q=0") && !strings.Contains(p, "q=0.") {
				return false
			}
			return true
		}
	}
	return false
}

// gzippableContentType 只压缩文本类；woff2/png/ico 等已在构建期内压，
// 再压只浪费 CPU 且几乎无收益。
func gzippableContentType(ct string) bool {
	base, _, _ := strings.Cut(ct, ";")
	base = strings.TrimSpace(strings.ToLower(base))
	switch base {
	case "text/html", "text/css", "text/plain", "text/javascript",
		"application/javascript", "application/x-javascript", "application/json",
		"application/manifest+json", "image/svg+xml", "application/wasm":
		return true
	}
	return false
}

// gzipResponseWriter 延迟启用 gzip 的 ResponseWriter 包装。
type gzipResponseWriter struct {
	http.ResponseWriter
	gw *gzip.Writer
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.gw == nil && code >= 200 && code < 300 {
		// 仅 2xx 压缩：304/4xx/5xx 无 body 或极小，不值得（也避免
		// 给 304 错加 Content-Encoding 破坏条件请求缓存语义）
		if gzippableContentType(g.Header().Get("Content-Type")) {
			g.Header().Del("Content-Length")
			g.Header().Set("Content-Encoding", "gzip")
			g.Header().Add("Vary", "Accept-Encoding")
			g.gw = gzip.NewWriter(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.gw != nil {
		return g.gw.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// close 收尾 gzip 流（写 EOF+CRC32）；未启用时为空操作。
func (g *gzipResponseWriter) close() {
	if g.gw != nil {
		_ = g.gw.Close()
	}
}

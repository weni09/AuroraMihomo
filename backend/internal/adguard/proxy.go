package adguard

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

const sessionCookieName = "aurora_session"
const adguardURLPrefix = "/adguard-ui"

// AuthorizeRequest 校验 AdGuard 反代请求：优先 aurora_session cookie，
// 其次 Authorization Bearer。JWT 校验方式与 aurora.verifyWSToken 一致（HMAC）。
func AuthorizeRequest(r *http.Request, secret string) bool {
	if r == nil || secret == "" {
		return false
	}
	raw := ""
	if c, err := r.Cookie(sessionCookieName); err == nil && c != nil {
		raw = strings.TrimSpace(c.Value)
	}
	if raw == "" {
		if h := r.Header.Get("Authorization"); h != "" {
			raw = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if raw == "" {
		return false
	}
	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	return err == nil && token.Valid
}

// NewProxyHandler 返回挂在 /adguard-ui 下的同源反代（与 SPA 路由 /adguard 分离，避免刷新整页变成裸 AGH）。
func NewProxyHandler(mgr *Manager, jwtSecret string, webAddrResolver func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !AuthorizeRequest(r, jwtSecret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if mgr == nil || !mgr.Status().Running {
			http.Error(w, "adguard not running", http.StatusServiceUnavailable)
			return
		}

		webAddr := ""
		if webAddrResolver != nil {
			webAddr = strings.TrimSpace(webAddrResolver())
		}
		if webAddr == "" {
			webAddr = strings.TrimSpace(mgr.Status().WebAddr)
		}
		if webAddr == "" {
			webAddr = "127.0.0.1:3000"
		}
		webAddr = strings.TrimPrefix(webAddr, "http://")
		webAddr = strings.TrimPrefix(webAddr, "https://")

		target, err := url.Parse("http://" + webAddr)
		if err != nil || target.Host == "" {
			http.Error(w, "invalid upstream", http.StatusBadGateway)
			return
		}
		if !isLoopbackHost(target.Hostname()) {
			http.Error(w, "upstream must be loopback", http.StatusBadGateway)
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = -1
		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = stripAdguardPrefix(req.URL.Path)
			req.URL.RawPath = ""

			if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				req.Header.Set("X-Real-IP", clientIP)
				if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
					req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
				} else {
					req.Header.Set("X-Forwarded-For", clientIP)
				}
			}
			proto := "http"
			if r.TLS != nil {
				proto = "https"
			} else if xp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xp != "" {
				proto = xp
			}
			req.Header.Set("X-Forwarded-Proto", proto)
			if host := r.Host; host != "" {
				req.Header.Set("X-Forwarded-Host", host)
			}
			req.Header.Set("X-Forwarded-Prefix", adguardURLPrefix)
		}
		proxy.ModifyResponse = modifyAdguardResponse
		proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func stripAdguardPrefix(path string) string {
	switch {
	case path == "/adguard-ui":
		return "/"
	case strings.HasPrefix(path, "/adguard-ui/"):
		p := strings.TrimPrefix(path, "/adguard-ui")
		if p == "" {
			return "/"
		}
		return p
	default:
		if path == "" {
			return "/"
		}
		return path
	}
}

func modifyAdguardResponse(resp *http.Response) error {
	if xfo := resp.Header.Get("X-Frame-Options"); xfo != "" {
		u := strings.ToUpper(strings.TrimSpace(xfo))
		if u == "DENY" || u == "SAMEORIGIN" {
			resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
		}
	} else {
		resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
	}

	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
		resp.Header.Del("Set-Cookie")
		for _, c := range cookies {
			resp.Header.Add("Set-Cookie", rewriteSetCookiePath(c))
		}
	}

	if loc := resp.Header.Get("Location"); loc != "" {
		if fixed := rewriteLocationUnderAdguard(loc); fixed != loc {
			resp.Header.Set("Location", fixed)
		}
	}

	ct := resp.Header.Get("Content-Type")
	if !shouldRewriteAdguardBody(ct) || resp.Body == nil {
		return nil
	}
	return rewriteAdguardResponseBody(resp)
}

func shouldRewriteAdguardBody(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "ecmascript") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "text/css")
}

func rewriteAdguardResponseBody(resp *http.Response) error {
	var reader io.Reader = resp.Body
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	switch enc {
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil
		}
		reader = gr
	case "br", "deflate", "zstd":
		return nil
	}

	body, err := io.ReadAll(reader)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}

	rewritten := rewriteAdguardAbsolutePaths(body)
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	resp.Header.Del("Content-Encoding")
	return nil
}

// rewriteAdguardAbsolutePaths 把 AGH 前端写死的根路径改到 /adguard 下。
func rewriteAdguardAbsolutePaths(body []byte) []byte {
	s := string(body)
	const mark = "\x00AGHPREFIX\x00"
	s = strings.ReplaceAll(s, adguardURLPrefix+"/", mark)
	replacements := [][2]string{
		{`"/control/`, `"` + adguardURLPrefix + `/control/`},
		{`'/control/`, `'` + adguardURLPrefix + `/control/`},
		{`"/control"`, `"` + adguardURLPrefix + `/control"`},
		{`'/control'`, `'` + adguardURLPrefix + `/control'`},
		{`"/assets/`, `"` + adguardURLPrefix + `/assets/`},
		{`'/assets/`, `'` + adguardURLPrefix + `/assets/`},
		{`"/login.`, `"` + adguardURLPrefix + `/login.`},
		{`'/login.`, `'` + adguardURLPrefix + `/login.`},
		{`"/login.html`, `"` + adguardURLPrefix + `/login.html`},
		{`"/install.`, `"` + adguardURLPrefix + `/install.`},
		{`'/install.`, `'` + adguardURLPrefix + `/install.`},
		{`"/install.html`, `"` + adguardURLPrefix + `/install.html`},
		{`href="/`, `href="` + adguardURLPrefix + `/`},
		{`src="/`, `src="` + adguardURLPrefix + `/`},
		{`action="/`, `action="` + adguardURLPrefix + `/`},
		{`url(/`, `url(` + adguardURLPrefix + `/`},
		{`url("/`, `url("` + adguardURLPrefix + `/`},
	}
	for _, pair := range replacements {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	s = strings.ReplaceAll(s, mark, adguardURLPrefix+"/")
	s = strings.ReplaceAll(s, adguardURLPrefix+adguardURLPrefix, adguardURLPrefix)
	return []byte(s)
}

func rewriteLocationUnderAdguard(loc string) string {
	if strings.HasPrefix(loc, adguardURLPrefix) {
		return loc
	}
	if strings.HasPrefix(loc, "/") {
		if loc == "/" {
			return adguardURLPrefix + "/"
		}
		return adguardURLPrefix + loc
	}
	return loc
}

func rewriteSetCookiePath(c string) string {
	lower := strings.ToLower(c)
	if strings.Contains(lower, "path=") {
		parts := strings.Split(c, ";")
		for i, p := range parts {
			pt := strings.TrimSpace(p)
			if len(pt) >= 5 && strings.EqualFold(pt[:5], "path=") {
				val := strings.TrimSpace(pt[5:])
				if val == "/" || val == "" {
					parts[i] = " Path=" + adguardURLPrefix
				}
			}
		}
		return strings.Join(parts, ";")
	}
	return c + "; Path=" + adguardURLPrefix
}

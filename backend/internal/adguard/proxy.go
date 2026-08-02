package adguard

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

const sessionCookieName = "aurora_session"

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

// NewProxyHandler 返回挂在 /adguard 下的同源反代。
// webAddrResolver 每次请求解析上游；为空时回退 Status().WebAddr，再默认 127.0.0.1:3000。
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

		proxy := httputil.NewSingleHostReverseProxy(target)
		// 流式/WebSocket：禁用缓冲刷新间隔
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
		}
		proxy.ModifyResponse = modifyAdguardResponse
		proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})
}

func stripAdguardPrefix(path string) string {
	switch {
	case path == "/adguard":
		return "/"
	case strings.HasPrefix(path, "/adguard/"):
		p := strings.TrimPrefix(path, "/adguard")
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
	// 上游若写 DENY，会阻止管理端同源 iframe 内嵌；统一为 SAMEORIGIN。
	if xfo := resp.Header.Get("X-Frame-Options"); xfo != "" {
		u := strings.ToUpper(strings.TrimSpace(xfo))
		if u == "DENY" || u == "SAMEORIGIN" {
			resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
		}
	} else {
		resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
	}

	if loc := resp.Header.Get("Location"); loc != "" {
		if fixed := rewriteLocationUnderAdguard(loc); fixed != loc {
			resp.Header.Set("Location", fixed)
		}
	}
	return nil
}

// rewriteLocationUnderAdguard 把指向站点根路径的绝对路径 Location 改写到 /adguard 前缀下。
func rewriteLocationUnderAdguard(loc string) string {
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "/adguard") {
		if loc == "/" {
			return "/adguard/"
		}
		return "/adguard" + loc
	}
	return loc
}

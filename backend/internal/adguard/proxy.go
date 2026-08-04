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

	"auroramihomo/backend/internal/auth"

	"github.com/golang-jwt/jwt/v4"
)

const sessionCookieName = "aurora_session"
const adguardURLPrefix = "/adguard-ui"

// AuthorizeRequest 校验 AdGuard 反代请求：优先 aurora_session cookie，
// 其次 Authorization Bearer。JWT 校验方式与 aurora.verifyWSToken 一致（HMAC）；
// ver 为口令版本闸门：改密后旧令牌即使签名有效也拒绝访问。
func AuthorizeRequest(r *http.Request, secret string, ver *auth.PasswordVer) bool {
	if r == nil || secret == "" {
		return false
	}
	raw := ""
	if c, err := r.Cookie(sessionCookieName); err == nil && c != nil {
		raw = strings.TrimSpace(c.Value)
	}
	if raw == "" {
		raw = auth.ExtractBearerToken(r)
	}
	if raw == "" {
		return false
	}
	claims, err := auth.ParseToken(raw, secret)
	if err != nil {
		return false
	}
	return auth.TokenVersionValid(claims, ver.Current())
}

// NewProxyHandler 返回挂在 /adguard-ui 下的同源反代（与 SPA 路由 /adguard 分离，避免刷新整页变成裸 AGH）。
// bridge 可为 nil；非 nil 时在已登录 Aurora 的前提下注入 agh_session，实现免密进 AGH。
// ver 为口令版本闸门（改密吊销），与 API / WS 共用同一计数器。
func NewProxyHandler(mgr *Manager, jwtSecret string, ver *auth.PasswordVer, webAddrResolver func() string, bridge *SessionBridge) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !AuthorizeRequest(r, jwtSecret, ver) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if mgr == nil || !mgr.Status().Running {
			http.Error(w, "adguard not running", http.StatusServiceUnavailable)
			return
		}

		userKey := UserKeyFromRequest(r, jwtSecret)

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

		// 访问登录页且已有 SSO 会话：直接进首页，避免再看 AGH 登录表单
		pathOnly := stripAdguardPrefix(r.URL.Path)
		if bridge != nil && userKey != "" {
			if pathOnly == "/login.html" || pathOnly == "/login" {
				if sess := bridge.SessionCookie(r.Context(), userKey); sess != "" {
					http.Redirect(w, r, adguardURLPrefix+"/", http.StatusFound)
					return
				}
			}
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

			// 免密：注入 AGH 会话；合并 Cookie，避免抹掉上游可能需要的其它 cookie
			if bridge != nil && userKey != "" {
				if sess := bridge.SessionCookie(r.Context(), userKey); sess != "" {
					req.Header.Set("Cookie", mergeCookieHeader(req.Header.Get("Cookie"), "agh_session", sess))
				}
			}
		}
		proxy.ModifyResponse = func(resp *http.Response) error {
			// 上游 401：会话失效，清缓存以便下次用密码重登
			if resp.StatusCode == http.StatusUnauthorized && bridge != nil && userKey != "" {
				bridge.InvalidateSession(userKey)
			}
			return modifyAdguardResponse(resp)
		}
		proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})
}

// mergeCookieHeader 在原有 Cookie 上设置/覆盖 name=value，保留其它 cookie。
func mergeCookieHeader(existing, name, value string) string {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" || value == "" {
		return existing
	}
	parts := strings.Split(existing, ";")
	out := make([]string, 0, len(parts)+1)
	prefix := name + "="
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, prefix) {
			continue
		}
		out = append(out, p)
	}
	out = append(out, prefix+value)
	return strings.Join(out, "; ")
}

// UserKeyFromRequest 从 JWT 提取稳定用户键（uid），供 SSO 会话索引。
func UserKeyFromRequest(r *http.Request, secret string) string {
	if r == nil || secret == "" {
		return ""
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
		return ""
	}
	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return ""
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "1"
	}
	switch v := claims["uid"].(type) {
	case float64:
		return fmt.Sprintf("%d", int64(v))
	case string:
		if v != "" {
			return v
		}
	}
	return "1"
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

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	var rewritten []byte
	switch {
	case strings.Contains(ct, "text/html"):
		rewritten = rewriteAdguardHTML(body)
	case strings.Contains(ct, "javascript") || strings.Contains(ct, "ecmascript"):
		// JS 用相对 API + HashRouter；绝不能改 Ze="/login.html"
		// （它配合 pathname.replace(/\/[^/]*$/, Ze)，改前缀会逃逸到站点根或双重前缀）。
		rewritten = rewriteAdguardJS(body)
	case strings.Contains(ct, "text/css"):
		rewritten = []byte(rewriteCommonAbsolutePaths(string(body)))
	default:
		rewritten = body
	}
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	resp.Header.Del("Content-Encoding")
	return nil
}

func rewriteCommonAbsolutePaths(s string) string {
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
	return s
}

// rewriteAdguardHTML：绝对路径 + <base> + history 补丁。
func rewriteAdguardHTML(body []byte) []byte {
	s := rewriteCommonAbsolutePaths(string(body))
	if strings.Contains(s, "<head") && (strings.Contains(s, "<html") || strings.Contains(s, "<!doctype") || strings.Contains(s, "<!DOCTYPE")) {
		needBase := !strings.Contains(s, `<base href="`+adguardURLPrefix+`/">`)
		needPatch := !strings.Contains(s, "/*agh-subpath-patch*/")
		var inject string
		if needBase {
			inject += `<base href="` + adguardURLPrefix + `/">`
		}
		if needPatch {
			inject += aghHistoryPatchScript()
		}
		if inject != "" {
			if i := strings.Index(s, "<head>"); i >= 0 {
				s = s[:i+6] + inject + s[i+6:]
			} else if i := strings.Index(s, "<head "); i >= 0 {
				if j := strings.Index(s[i:], ">"); j >= 0 {
					pos := i + j + 1
					s = s[:pos] + inject + s[pos:]
				}
			}
		}
	}
	return []byte(s)
}

// rewriteAdguardJS：只改静态 assets 根路径，保留 Ze="/login.html" 等常量。
func rewriteAdguardJS(body []byte) []byte {
	s := string(body)
	const mark = "\x00AGHPREFIX\x00"
	s = strings.ReplaceAll(s, adguardURLPrefix+"/", mark)
	s = strings.ReplaceAll(s, `"/assets/`, `"`+adguardURLPrefix+`/assets/`)
	s = strings.ReplaceAll(s, `'/assets/`, `'`+adguardURLPrefix+`/assets/`)
	s = strings.ReplaceAll(s, mark, adguardURLPrefix+"/")
	s = strings.ReplaceAll(s, adguardURLPrefix+adguardURLPrefix, adguardURLPrefix)
	return []byte(s)
}

// rewriteAdguardAbsolutePaths 兼容单测，按 HTML 规则处理。
func rewriteAdguardAbsolutePaths(body []byte) []byte {
	return rewriteAdguardHTML(body)
}

// aghHistoryPatchScript 拦截根路径导航，并强制 /adguard-ui 带尾斜杠，
// 避免 href.replace(/\/[^/]*$/, "/login.html") 把 /adguard-ui 整段换成 /login.html。
//
// 注意 Location.href setter 必须是 raw.set.call(this, fix(String(v)))——
// 少写 call 的右括号会在 HTML 内联脚本处直接 SyntaxError
// （浏览器报 missing ) after argument list at adguard-ui/:1:…），整页白屏。
func aghHistoryPatchScript() string {
	p := adguardURLPrefix
	return `<script>/*agh-subpath-patch*/(function(){var B="` + p + `";` +
		`function fix(u){if(typeof u!=="string"||!u)return u;` +
		`if(u.charAt(0)==="/"&&u.indexOf(B)!==0&&u.indexOf("//")!==0)return B+u;return u;}` +
		`var ps=history.pushState.bind(history);history.pushState=function(s,t,u){return ps(s,t,fix(u));};` +
		`var rs=history.replaceState.bind(history);history.replaceState=function(s,t,u){return rs(s,t,fix(u));};` +
		`try{var raw=Object.getOwnPropertyDescriptor(Location.prototype,"href");` +
		`if(raw&&raw.set){Object.defineProperty(Location.prototype,"href",{configurable:true,enumerable:true,` +
		`get:function(){return raw.get.call(this);},` +
		`set:function(v){raw.set.call(this,fix(String(v)));}});}}catch(e){}` +
		`try{if(location.pathname===B){history.replaceState(null,"",B+"/"+(location.search||"")+(location.hash||""));}}catch(e){}` +
		`})();</script>`
}

func rewriteLocationUnderAdguard(loc string) string {
	if strings.HasPrefix(loc, adguardURLPrefix) {
		return loc
	}
	if loc == "/" {
		return adguardURLPrefix + "/"
	}
	if loc == "/login.html" || loc == "/login" {
		return adguardURLPrefix + loc
	}
	if strings.HasPrefix(loc, "/") {
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

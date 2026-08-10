package adguard

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/auth"

	"github.com/golang-jwt/jwt/v4"
)

const testJWTSecret = "test-adguard-proxy-secret-32b!!"

func signTestJWT(t *testing.T, secret string, expOffset time.Duration, ver int64) string {
	t.Helper()
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"exp": now + int64(expOffset.Seconds()),
		"iat": now,
		"uid": 1,
		"ver": ver,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func TestAuthorizeRequest_NoCredentials(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	if auth.AuthorizeRequest(r, testJWTSecret, auth.NewPasswordVer(0)) {
		t.Fatal("expected unauthorized without cookie/bearer")
	}
}

func TestAuthorizeRequest_InvalidCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "not-a-jwt"})
	if auth.AuthorizeRequest(r, testJWTSecret, auth.NewPasswordVer(0)) {
		t.Fatal("expected unauthorized for invalid cookie")
	}
}

func TestAuthorizeRequest_ValidCookieAndBearer(t *testing.T) {
	token := signTestJWT(t, testJWTSecret, time.Hour, 0)

	rCookie := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rCookie.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	if !auth.AuthorizeRequest(rCookie, testJWTSecret, auth.NewPasswordVer(0)) {
		t.Fatal("valid cookie should authorize")
	}

	rBearer := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rBearer.Header.Set("Authorization", "Bearer "+token)
	if !auth.AuthorizeRequest(rBearer, testJWTSecret, auth.NewPasswordVer(0)) {
		t.Fatal("valid bearer should authorize")
	}
}

// 改密（口令版本 +1）后，签名有效但版本落后的令牌必须被拒绝；
// 版本匹配的令牌照常放行。
func TestAuthorizeRequest_StaleVersionRejected(t *testing.T) {
	stale := signTestJWT(t, testJWTSecret, time.Hour, 0)
	fresh := signTestJWT(t, testJWTSecret, time.Hour, 2)

	ver := auth.NewPasswordVer(2)

	rStale := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rStale.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: stale})
	if auth.AuthorizeRequest(rStale, testJWTSecret, ver) {
		t.Fatal("版本落后的旧令牌必须被拒绝")
	}

	rFresh := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rFresh.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: fresh})
	if !auth.AuthorizeRequest(rFresh, testJWTSecret, ver) {
		t.Fatal("版本匹配的令牌应放行")
	}
}

// 无 ver 声明的存量令牌（升级前签发）：未改密（版本 0）放行，
// 改密后（版本 ≥1）拒绝。
func TestAuthorizeRequest_MissingVersionClaim(t *testing.T) {
	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": now + 3600,
		"iat": now,
		"uid": 1,
	})
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: s})

	if !auth.AuthorizeRequest(r, testJWTSecret, auth.NewPasswordVer(0)) {
		t.Fatal("未改密时存量令牌应放行")
	}
	if auth.AuthorizeRequest(r, testJWTSecret, auth.NewPasswordVer(1)) {
		t.Fatal("改密后存量令牌必须被拒绝")
	}
}

func TestProxyHandler_NoCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, auth.NewPasswordVer(0), nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProxyHandler_InvalidCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, auth.NewPasswordVer(0), nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/control/status", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "garbage.token.value"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProxyHandler_ValidCookie_StripsPrefixAndProxies(t *testing.T) {
	var gotPath string
	var gotXFF, gotXFP, gotXFH, gotXRI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotXFF = r.Header.Get("X-Forwarded-For")
		gotXFP = r.Header.Get("X-Forwarded-Proto")
		gotXFH = r.Header.Get("X-Forwarded-Host")
		gotXRI = r.Header.Get("X-Real-IP")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok-from-upstream")
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	mgr := NewManager(Config{WebAddr: host})
	mgr.testForceRunning = true

	h := NewProxyHandler(mgr, testJWTSecret, auth.NewPasswordVer(0), func() string { return host }, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour, 0)

	req := httptest.NewRequest(http.MethodGet, "http://panel.example/adguard-ui/control/status?x=1", nil)
	req.Host = "panel.example"
	req.RemoteAddr = "203.0.113.10:54321"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/control/status" {
		t.Fatalf("upstream path = %q, want /control/status", gotPath)
	}
	if !strings.Contains(rec.Body.String(), "ok-from-upstream") {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if gotXFF == "" || gotXRI == "" {
		t.Fatalf("missing forward headers XFF=%q XRI=%q", gotXFF, gotXRI)
	}
	if gotXFP != "http" {
		t.Fatalf("X-Forwarded-Proto = %q", gotXFP)
	}
	if gotXFH != "panel.example" {
		t.Fatalf("X-Forwarded-Host = %q", gotXFH)
	}
	// DENY 应被改写为 SAMEORIGIN，便于同源 iframe
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
}

func TestProxyHandler_NotRunning_503(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, auth.NewPasswordVer(0), nil, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour, 0)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestStripAdguardPrefix(t *testing.T) {
	cases := map[string]string{
		"/adguard-ui":         "/",
		"/adguard-ui/":        "/",
		"/adguard-ui/foo":     "/foo",
		"/adguard-ui/foo/bar": "/foo/bar",
		"/other":              "/other",
		"":                    "/",
	}
	for in, want := range cases {
		if got := stripAdguardPrefix(in); got != want {
			t.Errorf("stripAdguardPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteLocationUnderAdguard(t *testing.T) {
	cases := map[string]string{
		"/":             "/adguard-ui/",
		"/login.html":   "/adguard-ui/login.html",
		"/adguard-ui/x": "/adguard-ui/x",
		"relative":      "relative",
		"https://x/y":   "https://x/y",
	}
	for in, want := range cases {
		if got := rewriteLocationUnderAdguard(in); got != want {
			t.Errorf("rewriteLocationUnderAdguard(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsLocalDialableHost 反代上游白名单（auth 包）：回环与域名别名放行，
// 公网 IP / 域名拒绝（防 SSRF）；本机接口 IP（若有）放行。
// 同源反代（/adguard-ui、/mihomo-api）共用同一策略，这里回归 auth 实现。
func TestIsLocalDialableHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":   true,
		"127.0.0.2":   true,
		"::1":         true,
		"localhost":   true,
		"LOCALHOST":   true,
		"0.0.0.0":     false,
		"8.8.8.8":     false,
		"example.com": false,
		"":            false,
	}
	for in, want := range cases {
		if got := auth.IsLocalDialableHost(in); got != want {
			t.Errorf("auth.IsLocalDialableHost(%q) = %v, want %v", in, got, want)
		}
	}
	// 动态取一个本机非回环接口 IP（测试环境网络各异，存在才断言）
	if ifIP := firstNonLoopbackInterfaceIP(t); ifIP != "" {
		if !auth.IsLocalDialableHost(ifIP) {
			t.Fatalf("本机接口 IP %q 应放行", ifIP)
		}
	}
}

func TestProxyHandler_RejectsNonLoopbackUpstream(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "8.8.8.8:53"})
	mgr.testForceRunning = true
	h := NewProxyHandler(mgr, testJWTSecret, auth.NewPasswordVer(0), func() string { return "8.8.8.8:53" }, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour, 0)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for non-local upstream", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "local IP") {
		t.Fatalf("body = %q, want local IP hint", rec.Body.String())
	}
}

func firstNonLoopbackInterfaceIP(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}
	return ""
}

func TestRewriteAdguardAbsolutePaths_InjectsBaseAndHistoryPatch(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>AdGuard Home</title></head><body><a href="/login.html">x</a></body></html>`)
	out := string(rewriteAdguardAbsolutePaths(in))
	if !strings.Contains(out, `<base href="/adguard-ui/">`) {
		t.Fatalf("missing base: %s", out)
	}
	if !strings.Contains(out, "/*agh-subpath-patch*/") {
		t.Fatalf("missing history patch: %s", out)
	}
	if !strings.Contains(out, `href="/adguard-ui/login.html"`) {
		t.Fatalf("login href not rewritten: %s", out)
	}
	// idempotent-ish: running twice should not double-prefix login beyond one level badly
	out2 := string(rewriteAdguardAbsolutePaths([]byte(out)))
	if strings.Contains(out2, "/adguard-ui/adguard-ui/") {
		t.Fatalf("double prefix: %s", out2)
	}
}

// 曾漏写 raw.set.call 的右括号，浏览器在 adguard-ui/ 文档内直接 SyntaxError 白屏。
func TestAghHistoryPatchScript_BalancedCallParens(t *testing.T) {
	s := aghHistoryPatchScript()
	if !strings.Contains(s, `raw.set.call(this,fix(String(v)));`) {
		t.Fatalf("Location.href setter 缺少 call 的闭合括号，脚本会 SyntaxError:\n%s", s)
	}
	// 旧错误写法不得再出现
	if strings.Contains(s, `raw.set.call(this,fix(String(v));}`) {
		t.Fatal("仍含有缺少 ) 的旧 setter 写法")
	}
	// 粗检：script 内 () 与 {} 成对（字符串里的括号忽略要求不高，此脚本无嵌套引号陷阱）
	depthParen, depthBrace := 0, 0
	body := s
	if i := strings.Index(body, ">"); i >= 0 {
		body = body[i+1:]
	}
	body = strings.TrimSuffix(body, "</script>")
	for _, r := range body {
		switch r {
		case '(':
			depthParen++
		case ')':
			depthParen--
			if depthParen < 0 {
				t.Fatal("多余的 )")
			}
		case '{':
			depthBrace++
		case '}':
			depthBrace--
			if depthBrace < 0 {
				t.Fatal("多余的 }")
			}
		}
	}
	if depthParen != 0 || depthBrace != 0 {
		t.Fatalf("括号不平衡 paren=%d brace=%d", depthParen, depthBrace)
	}
}

func TestRewriteAdguardJS_PreservesLoginConstant(t *testing.T) {
	in := []byte(`var Be=/\/[^/]*$/,Ze="/login.html";baseUrl="control";x="/assets/foo.png"`)
	out := string(rewriteAdguardJS(in))
	if !strings.Contains(out, `Ze="/login.html"`) {
		t.Fatalf("Ze login constant rewritten unexpectedly: %s", out)
	}
	if !strings.Contains(out, `"/adguard-ui/assets/foo.png"`) {
		t.Fatalf("assets path not rewritten: %s", out)
	}
}

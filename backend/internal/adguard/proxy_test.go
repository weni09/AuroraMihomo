package adguard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testJWTSecret = "test-adguard-proxy-secret-32b!!"

func signTestJWT(t *testing.T, secret string, expOffset time.Duration) string {
	t.Helper()
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"exp": now + int64(expOffset.Seconds()),
		"iat": now,
		"uid": 1,
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
	if AuthorizeRequest(r, testJWTSecret) {
		t.Fatal("expected unauthorized without cookie/bearer")
	}
}

func TestAuthorizeRequest_InvalidCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-jwt"})
	if AuthorizeRequest(r, testJWTSecret) {
		t.Fatal("expected unauthorized for invalid cookie")
	}
}

func TestAuthorizeRequest_ValidCookieAndBearer(t *testing.T) {
	token := signTestJWT(t, testJWTSecret, time.Hour)

	rCookie := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rCookie.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if !AuthorizeRequest(rCookie, testJWTSecret) {
		t.Fatal("valid cookie should authorize")
	}

	rBearer := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	rBearer.Header.Set("Authorization", "Bearer "+token)
	if !AuthorizeRequest(rBearer, testJWTSecret) {
		t.Fatal("valid bearer should authorize")
	}
}

func TestProxyHandler_NoCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProxyHandler_InvalidCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/control/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "garbage.token.value"})

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

	h := NewProxyHandler(mgr, testJWTSecret, func() string { return host }, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "http://panel.example/adguard-ui/control/status?x=1", nil)
	req.Host = "panel.example"
	req.RemoteAddr = "203.0.113.10:54321"
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

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
	h := NewProxyHandler(mgr, testJWTSecret, nil, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

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

func TestIsLoopbackHost(t *testing.T) {
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
		if got := isLoopbackHost(in); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestProxyHandler_RejectsNonLoopbackUpstream(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "8.8.8.8:53"})
	mgr.testForceRunning = true
	h := NewProxyHandler(mgr, testJWTSecret, func() string { return "8.8.8.8:53" }, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for non-loopback upstream", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "loopback") {
		t.Fatalf("body = %q, want loopback hint", rec.Body.String())
	}
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

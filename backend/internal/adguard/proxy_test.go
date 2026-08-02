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
	r := httptest.NewRequest(http.MethodGet, "/adguard/", nil)
	if AuthorizeRequest(r, testJWTSecret) {
		t.Fatal("expected unauthorized without cookie/bearer")
	}
}

func TestAuthorizeRequest_InvalidCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/adguard/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-jwt"})
	if AuthorizeRequest(r, testJWTSecret) {
		t.Fatal("expected unauthorized for invalid cookie")
	}
}

func TestAuthorizeRequest_ValidCookieAndBearer(t *testing.T) {
	token := signTestJWT(t, testJWTSecret, time.Hour)

	rCookie := httptest.NewRequest(http.MethodGet, "/adguard/", nil)
	rCookie.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if !AuthorizeRequest(rCookie, testJWTSecret) {
		t.Fatal("valid cookie should authorize")
	}

	rBearer := httptest.NewRequest(http.MethodGet, "/adguard/", nil)
	rBearer.Header.Set("Authorization", "Bearer "+token)
	if !AuthorizeRequest(rBearer, testJWTSecret) {
		t.Fatal("valid bearer should authorize")
	}
}

func TestProxyHandler_NoCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/adguard/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProxyHandler_InvalidCookie_401(t *testing.T) {
	mgr := NewManager(Config{WebAddr: "127.0.0.1:1"})
	h := NewProxyHandler(mgr, testJWTSecret, nil)

	req := httptest.NewRequest(http.MethodGet, "/adguard/control/status", nil)
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

	h := NewProxyHandler(mgr, testJWTSecret, func() string { return host })
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "http://panel.example/adguard/control/status?x=1", nil)
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
	h := NewProxyHandler(mgr, testJWTSecret, nil)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/adguard/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestStripAdguardPrefix(t *testing.T) {
	cases := map[string]string{
		"/adguard":           "/",
		"/adguard/":          "/",
		"/adguard/foo":       "/foo",
		"/adguard/foo/bar":   "/foo/bar",
		"/other":             "/other",
		"":                   "/",
	}
	for in, want := range cases {
		if got := stripAdguardPrefix(in); got != want {
			t.Errorf("stripAdguardPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteLocationUnderAdguard(t *testing.T) {
	cases := map[string]string{
		"/":            "/adguard/",
		"/login.html":  "/adguard/login.html",
		"/adguard/x":   "/adguard/x",
		"relative":     "relative",
		"https://x/y":  "https://x/y",
	}
	for in, want := range cases {
		if got := rewriteLocationUnderAdguard(in); got != want {
			t.Errorf("rewriteLocationUnderAdguard(%q) = %q, want %q", in, got, want)
		}
	}
}

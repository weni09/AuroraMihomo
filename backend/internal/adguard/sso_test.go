package adguard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionBridge_EstablishAndSessionCookie(t *testing.T) {
	var loginHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/control/login" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		loginHits++
		http.SetCookie(w, &http.Cookie{
			Name:     "agh_session",
			Value:    "sess-token-abc",
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(time.Hour),
		})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	bridge := NewSessionBridge(func() string { return host })
	bridge.SetUsername("admin")

	ctx := context.Background()
	if err := bridge.Establish(ctx, "1", "admin", "secret"); err != nil {
		t.Fatalf("Establish: %v", err)
	}
	if loginHits != 1 {
		t.Fatalf("loginHits=%d want 1", loginHits)
	}
	got := bridge.SessionCookie(ctx, "1")
	if got != "sess-token-abc" {
		t.Fatalf("SessionCookie=%q", got)
	}
	// 缓存命中，不再打 login
	_ = bridge.SessionCookie(ctx, "1")
	if loginHits != 1 {
		t.Fatalf("cached SessionCookie should not re-login, hits=%d", loginHits)
	}

	bridge.InvalidateSession("1")
	// 有密码缓存时应重登
	got = bridge.SessionCookie(ctx, "1")
	if got != "sess-token-abc" {
		t.Fatalf("after invalidate re-login cookie=%q", got)
	}
	if loginHits != 2 {
		t.Fatalf("re-login hits=%d want 2", loginHits)
	}

	bridge.Clear("1")
	if bridge.SessionCookie(ctx, "1") != "" {
		t.Fatal("Clear should drop session and password")
	}
}

// memCredStore 仅用于单测的内存 CredStore。
type memCredStore struct {
	user, pass string
}

func (m *memCredStore) Save(username, password string) error {
	m.user, m.pass = username, password
	return nil
}
func (m *memCredStore) Load() (string, string, error) { return m.user, m.pass, nil }
func (m *memCredStore) Clear() error {
	m.user, m.pass = "", ""
	return nil
}

func TestSessionBridge_WrongMemoryFallsBackToStore(t *testing.T) {
	var sawPass []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/control/login" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sawPass = append(sawPass, body.Password)
		if body.Password != "agh-real" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "agh_session", Value: "ok-cookie", Path: "/",
			Expires: time.Now().Add(time.Hour),
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	host := strings.TrimPrefix(upstream.URL, "http://")
	bridge := NewSessionBridge(func() string { return host })
	store := &memCredStore{user: "admin", pass: "agh-real"}
	bridge.SetCredStore(store)
	// 模拟 changePassword 误写入的 Aurora 新密码
	bridge.RememberPassword("1", "aurora-new", time.Hour)
	bridge.InvalidateSession("1")

	got := bridge.SessionCookie(context.Background(), "1")
	if got != "ok-cookie" {
		t.Fatalf("cookie=%q want ok-cookie; attempts=%v", got, sawPass)
	}
	if len(sawPass) < 2 || sawPass[0] != "aurora-new" || sawPass[1] != "agh-real" {
		t.Fatalf("login attempts=%v want [aurora-new, agh-real]", sawPass)
	}
}

func TestProxyHandler_InjectsAghSession(t *testing.T) {
	var gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")

	// 假 AGH 登录端：用同一 upstream 路径分流
	// 简化：预先塞 session，不走 loginAGH
	bridge := NewSessionBridge(func() string { return host })
	bridge.mu.Lock()
	bridge.sessions["1"] = bridgeSession{
		cookie: "preseeded",
		expiry: time.Now().Add(time.Hour),
	}
	bridge.mu.Unlock()

	mgr := NewManager(Config{WebAddr: host})
	mgr.testForceRunning = true
	h := NewProxyHandler(mgr, testJWTSecret, func() string { return host }, bridge)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/control/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotCookie, "agh_session=preseeded") {
		t.Fatalf("Cookie=%q want contain agh_session=preseeded", gotCookie)
	}
}

func TestProxyHandler_LoginHTMLRedirectWithSSO(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be hit for login redirect, path=%s", r.URL.Path)
	}))
	defer upstream.Close()
	host := strings.TrimPrefix(upstream.URL, "http://")

	bridge := NewSessionBridge(func() string { return host })
	bridge.mu.Lock()
	bridge.sessions["1"] = bridgeSession{cookie: "x", expiry: time.Now().Add(time.Hour)}
	bridge.mu.Unlock()

	mgr := NewManager(Config{WebAddr: host})
	mgr.testForceRunning = true
	h := NewProxyHandler(mgr, testJWTSecret, func() string { return host }, bridge)
	token := signTestJWT(t, testJWTSecret, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/adguard-ui/login.html", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/adguard-ui/" {
		t.Fatalf("Location=%q", loc)
	}
}

func TestUserKeyFromRequest(t *testing.T) {
	token := signTestJWT(t, testJWTSecret, time.Hour)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if got := UserKeyFromRequest(r, testJWTSecret); got != "1" {
		t.Fatalf("uid=%q want 1", got)
	}
	if got := UserKeyFromRequest(httptest.NewRequest(http.MethodGet, "/", nil), testJWTSecret); got != "" {
		t.Fatalf("empty=%q", got)
	}
}

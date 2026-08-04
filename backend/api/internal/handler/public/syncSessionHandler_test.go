package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/svc"
)

// Android Edge 等会长期持有过期 aurora_session：有 Bearer 时必须 Set-Cookie 覆盖，不能短路。
func TestSyncSessionOverwritesStaleCookie(t *testing.T) {
	cfg := config.Config{}
	cfg.Auth.AccessExpire = 7200
	cfg.Auth.AccessSecret = "test-secret"
	svcCtx := &svc.ServiceContext{Config: cfg}

	h := SyncSessionHandler(svcCtx)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", nil)
	req.Header.Set("Authorization", "Bearer fresh-token-value")
	// 模拟浏览器仍带着旧 cookie
	req.AddCookie(&http.Cookie{Name: "aurora_session", Value: "stale-or-expired-token"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	setCookie := rec.Header().Values("Set-Cookie")
	if len(setCookie) == 0 {
		t.Fatal("有 Bearer 时必须 Set-Cookie 覆盖旧会话，不能因已有 cookie 而短路")
	}
	joined := strings.Join(setCookie, "\n")
	if !strings.Contains(joined, "aurora_session=fresh-token-value") {
		t.Fatalf("Set-Cookie 应写入 Bearer 值，实际 %q", joined)
	}
	if !strings.Contains(joined, "HttpOnly") {
		t.Fatalf("cookie 应 HttpOnly，实际 %q", joined)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("应对齐响应禁缓存，实际 %q", rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Body.String(), `"source":"bearer"`) {
		t.Fatalf("source 应为 bearer，body=%s", rec.Body.String())
	}
}

func TestSyncSessionWithoutBearerUsesExistingCookie(t *testing.T) {
	cfg := config.Config{}
	svcCtx := &svc.ServiceContext{Config: cfg}
	h := SyncSessionHandler(svcCtx)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: "aurora_session", Value: "already-there"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	// 无 Bearer 且已有 cookie：幂等成功，不必再 Set-Cookie
	if !strings.Contains(rec.Body.String(), `"source":"cookie"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

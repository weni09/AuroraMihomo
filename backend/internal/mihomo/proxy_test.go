package mihomo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/auth"

	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
)

const testProxyJWTSecret = "test-kernel-proxy-secret-32b!!"

func signProxyTestJWT(t *testing.T, ver int64) string {
	t.Helper()
	now := time.Now().Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": now + 3600,
		"iat": now,
		"uid": 1,
		"ver": ver,
	})
	s, err := tok.SignedString([]byte(testProxyJWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func authCookie(t *testing.T, ver int64) *http.Cookie {
	return &http.Cookie{Name: auth.SessionCookieName, Value: signProxyTestJWT(t, ver)}
}

// 普通 API 转发：路径剥掉 /mihomo-api 前缀、注入内核 secret（Bearer + token query）、
// 补齐 X-Forwarded-*，移除上游 Set-Cookie，并强制 no-store。
func TestKernelAPIProxy_ProxiesWithSecretAndForwardHeaders(t *testing.T) {
	var gotPath, gotAuth, gotToken, gotXFP, gotXFH string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotToken = r.URL.Query().Get("token")
		gotXFP = r.Header.Get("X-Forwarded-Proto")
		gotXFH = r.Header.Get("X-Forwarded-Host")
		w.Header().Set("Set-Cookie", "evil=1; Path=/")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hello":"mihomo"}`))
	}))
	defer upstream.Close()

	addr := strings.TrimPrefix(upstream.URL, "http://")
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(1),
		func() (string, string) { return addr, "kernel-secret-123" }, nil)

	req := httptest.NewRequest(http.MethodGet, "http://panel.example/mihomo-api/version?x=1", nil)
	req.Host = "panel.example"
	req.RemoteAddr = "203.0.113.10:54321"
	req.AddCookie(authCookie(t, 1))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/version" {
		t.Fatalf("upstream path = %q, want /version", gotPath)
	}
	if !strings.Contains(rec.Body.String(), `"hello":"mihomo"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if want := "Bearer kernel-secret-123"; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotToken != "kernel-secret-123" {
		t.Fatalf("query token = %q, want kernel-secret-123", gotToken)
	}
	if gotXFP != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", gotXFP)
	}
	if gotXFH != "panel.example" {
		t.Fatalf("X-Forwarded-Host = %q, want panel.example", gotXFH)
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("上游 Set-Cookie 应被移除，实际 %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestKernelAPIProxy_NoCredential_401(t *testing.T) {
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
		func() (string, string) { return "127.0.0.1:9090", "" }, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestKernelAPIProxy_InvalidCookie_401(t *testing.T) {
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
		func() (string, string) { return "127.0.0.1:9090", "" }, nil)
	req := httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "garbage.token"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// 改密后旧令牌（版本落后）拒绝访问内核 API，与 /ws、/adguard-ui 同一闸门。
func TestKernelAPIProxy_StaleToken_401(t *testing.T) {
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(2),
		func() (string, string) { return "127.0.0.1:9090", "" }, nil)
	req := httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil)
	req.AddCookie(authCookie(t, 0)) // 版本 0，落后于当前 2
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// 内核未启用 external-controller（resolver 返回空）时统一 503，
// 前端据此展示「内核未启用外部控制接口」而不是报错。
func TestKernelAPIProxy_Unavailable_503(t *testing.T) {
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
		func() (string, string) { return "", "" }, nil)
	req := httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil)
	req.AddCookie(authCookie(t, 0))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// SSRF 防线：external-controller 被改写成公网地址时必须拒绝，
// 与 /adguard-ui 的反代白名单同源（公网 IP 与域名拒绝）。
func TestKernelAPIProxy_RejectsPublicUpstream_502(t *testing.T) {
	for _, upstream := range []string{"8.8.8.8:9090", "example.com:9090"} {
		h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
			func() (string, string) { return upstream, "" }, nil)
		req := httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil)
		req.AddCookie(authCookie(t, 0))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("upstream %q: status = %d, want 502", upstream, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "local or private") {
			t.Fatalf("upstream %q: body = %q, want local/private hint", upstream, rec.Body.String())
		}
	}
}

// 分机部署：内核配置成其它机器的私网 IP（如 nginx/内核在 192.168.1.252、
// 管理端在别处）。白名单必须放行私网地址——本用例只断言「未被白名单拒绝」，
// 不对 dial 结果做假设（本机若恰好可达该 mihomo 会返回 200，否则 502）。
func TestKernelAPIProxy_AllowsPrivateUpstream(t *testing.T) {
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
		func() (string, string) { return "192.168.1.252:9090", "" }, nil)
	req := httptest.NewRequest(http.MethodGet, "/mihomo-api/version", nil)
	req.AddCookie(authCookie(t, 0))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("私网上游不应在鉴权/可用性阶段被拒，status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "local or private") {
		t.Fatalf("私网上游不应被白名单拒绝，body = %q", rec.Body.String())
	}
}

// 面板的 WebSocket 隧道（/traffic、/connections、/logs 等）必须能穿越反代：
// 客户端向 /mihomo-api/traffic 建立 ws，经同源反代转发到内核，双向消息可达。
func TestKernelAPIProxy_WebSocketTunnel(t *testing.T) {
	// 内核侧：WebSocket 回显服务器（验证 Bearer + token query 与路径剥除）
	var gotWSBearer, gotWSToken bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" {
			t.Errorf("upstream ws path = %q, want /traffic", r.URL.Path)
		}
		gotWSBearer = r.Header.Get("Authorization") == "Bearer kernel-secret-123"
		gotWSToken = r.URL.Query().Get("token") == "kernel-secret-123"
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, append([]byte("echo:"), msg...))
		}
	}))
	defer upstream.Close()

	addr := strings.TrimPrefix(upstream.URL, "http://")
	h := NewKernelAPIProxyHandler(testProxyJWTSecret, auth.NewPasswordVer(0),
		func() (string, string) { return addr, "kernel-secret-123" }, nil)

	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	wsURL := "ws" + strings.TrimPrefix(proxySrv.URL, "http") + "/mihomo-api/traffic?token=stale"
	header := http.Header{}
	header.Add("Cookie", auth.SessionCookieName+"="+signProxyTestJWT(t, 0))

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, reply, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if string(reply) != "echo:hi" {
		t.Fatalf("ws reply = %q, want echo:hi", string(reply))
	}
	if !gotWSBearer {
		t.Fatal("内核应收到 Bearer secret 的 ws 握手")
	}
	if !gotWSToken {
		t.Fatal("内核应收到被覆盖为内核 secret 的 token query")
	}
}

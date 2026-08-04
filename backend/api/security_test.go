package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/handler"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/model"

	"github.com/zeromicro/go-zero/rest"
)

// newSecurityTestServer 起一个真实 HTTP 服务，返回上下文与 base URL
func newSecurityTestServer(t *testing.T, port int, dbName string) (*svc.ServiceContext, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Config{}
	cfg.DataSource = filepath.Join(dir, dbName)
	cfg.Mihomo.ConfigDir = dir
	cfg.Auth.AccessSecret = "secret32345678901234567890123456"
	cfg.Auth.AccessExpire = 3600
	cfg.Host = "127.0.0.1"
	cfg.Port = port

	ctx := svc.NewServiceContext(cfg)
	t.Cleanup(func() { _ = ctx.Database.Close() })

	server := rest.MustNewServer(cfg.RestConf)
	t.Cleanup(server.Stop)
	handler.RegisterHandlers(server, ctx)
	// 与生产 main() 一致的认证加固：口令版本闸门（改密后旧令牌失效）。
	// 不加这一行，TestChangePassword 的吊销断言就测不到真实路径。
	applyAuthHardening(server, ctx)

	go server.Start()
	time.Sleep(time.Second)

	return ctx, "http://127.0.0.1:" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func loginForToken(t *testing.T, ctx *svc.ServiceContext, baseURL, pwd string) string {
	t.Helper()
	hashed, err := auth.HashPassword(pwd)
	if err != nil {
		t.Fatalf("生成口令哈希失败: %v", err)
	}
	if err := ctx.Database.SetSetting("admin_password", hashed); err != nil {
		t.Fatalf("写入口令失败: %v", err)
	}
	ctx.LoginLimiter.Reset("127.0.0.1")

	body, _ := json.Marshal(map[string]string{"password": pwd})
	resp, err := http.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["token"] == "" {
		t.Fatal("未取到登录令牌")
	}
	return out["token"]
}

// 核心安全回归：文件直链端点是公开的（供内核拉取，无法带 JWT），
// 因此绝不能按用户可枚举的文件名寻址。按名访问必须失败，
// 只有持有随机 token 才能读到内容。
func TestFileEndpointNotEnumerableByName(t *testing.T) {
	ctx, baseURL := newSecurityTestServer(t, 8893, "sec_file.db")

	const secret = "top-secret-subscription-template"
	f := &model.SubFile{
		Name:       "rules.yaml",
		Content:    secret,
		ShareToken: "s3cr3ttoken",
	}
	if err := ctx.Database.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}

	// 按文件名访问必须拿不到内容
	resp, err := http.Get(baseURL + "/api/v1/file/rules.yaml")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("按文件名访问不应成功，实际 %d，正文:\n%s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("按文件名访问泄露了文件内容")
	}

	// 持 token 访问应成功
	respOK, err := http.Get(baseURL + "/api/v1/file/s3cr3ttoken")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	okBody, _ := io.ReadAll(respOK.Body)
	_ = respOK.Body.Close()
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("持 token 访问应成功，实际 %d", respOK.StatusCode)
	}
	if string(okBody) != secret {
		t.Fatalf("内容不符，实际 %q", okBody)
	}
}

// 改密接口必须存在、需要鉴权、且必须校验旧口令
func TestChangePassword(t *testing.T) {
	ctx, baseURL := newSecurityTestServer(t, 8894, "sec_pwd.db")
	const oldPwd = "original-pass-1"
	token := loginForToken(t, ctx, baseURL, oldPwd)

	post := func(withAuth bool, payload map[string]string) *http.Response {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/password", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		if withAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		return resp
	}

	// 未鉴权必须被拒
	r1 := post(false, map[string]string{"oldPassword": oldPwd, "newPassword": "brand-new-pass"})
	_ = r1.Body.Close()
	if r1.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未鉴权改密应返回 401，实际 %d", r1.StatusCode)
	}

	// 旧口令错误必须被拒（防止会话劫持直接升级为账户接管）
	r2 := post(true, map[string]string{"oldPassword": "wrong-old", "newPassword": "brand-new-pass"})
	_ = r2.Body.Close()
	if r2.StatusCode == http.StatusOK {
		t.Fatal("旧口令错误时改密不应成功")
	}

	// 过短的新口令必须被拒
	r3 := post(true, map[string]string{"oldPassword": oldPwd, "newPassword": "short"})
	_ = r3.Body.Close()
	if r3.StatusCode == http.StatusOK {
		t.Fatal("过短的新口令不应被接受")
	}

	// 改密前旧令牌访问受保护端点应成功（前置校验：确认令牌在改密前可用，
	// 排除「本来就不通」的假阳性）
	req := func(bearer string) *http.Response {
		r, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/system/status", nil)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		return resp
	}
	if resp := req(token); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("改密前旧令牌访问受保护端点应成功（前置校验），实际 %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 正常改密
	r4 := post(true, map[string]string{"oldPassword": oldPwd, "newPassword": "brand-new-pass"})
	_ = r4.Body.Close()
	if r4.StatusCode != http.StatusOK {
		t.Fatalf("改密应成功，实际 %d", r4.StatusCode)
	}

	// 新口令应可登录，旧口令应失效
	stored, err := ctx.Database.GetSetting("admin_password")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.VerifyPassword(stored, "brand-new-pass"); !ok {
		t.Fatal("改密后新口令应可校验通过")
	}
	if ok, _ := auth.VerifyPassword(stored, oldPwd); ok {
		t.Fatal("改密后旧口令必须失效")
	}
	// 落库的必须是哈希，不能是明文
	if stored == "brand-new-pass" {
		t.Fatal("新口令不得以明文形式存储")
	}

	// —— 口令版本吊销回归 ——
	// 改密后同一旧令牌必须 401
	if resp := req(token); resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		t.Fatalf("改密后旧令牌应 401，实际 %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 未携带令牌同样 401（rest.WithJwt 行为不变）
	if resp := req(""); resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		t.Fatalf("无令牌应 401，实际 %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// 新口令重新登录后，新令牌携带新版本，访问受保护端点应恢复 200。
	// loginForToken 会覆盖 admin_password（同值哈希），不影响版本计数。
	newToken := loginForToken(t, ctx, baseURL, "brand-new-pass")
	if resp := req(newToken); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("新令牌访问受保护端点应 200，实际 %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

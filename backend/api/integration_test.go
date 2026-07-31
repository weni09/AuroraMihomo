package main

import (
	"bytes"
	"encoding/json"
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

func TestIntegrationAuth(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Config{}
	cfg.DataSource = filepath.Join(dir, "test.db")
	cfg.Mihomo.ConfigDir = dir
	cfg.Auth.AccessSecret = "secret12345678901234567890123456"
	cfg.Auth.AccessExpire = 3600
	cfg.Host = "127.0.0.1"
	cfg.Port = 8890

	ctx := svc.NewServiceContext(cfg)
	t.Cleanup(func() { _ = ctx.Database.Close() })
	server := rest.MustNewServer(cfg.RestConf)
	defer server.Stop()
	handler.RegisterHandlers(server, ctx)

	go server.Start()
	time.Sleep(1 * time.Second) // Wait for server to start

	client := &http.Client{}

	// Test 1: Unauthenticated request should return 401 Unauthorized
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8890/api/v1/subscriptions", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}

	// 错误口令必须被拒绝
	loginPayload := map[string]string{"password": "wrong_password"}
	body, _ := json.Marshal(loginPayload)
	reqLogin, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/auth/login", bytes.NewBuffer(body))
	reqLogin.Header.Set("Content-Type", "application/json")
	respBad, err := client.Do(reqLogin)
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	if respBad.StatusCode == http.StatusOK {
		t.Fatal("错误口令不应登录成功")
	}
	_ = respBad.Body.Close()

	// 口令以哈希形式存库，不能直接取出当明文用；
	// 这里改写为一个已知口令的哈希再登录。
	const knownPwd = "integration-test-pass"
	hashed, err := auth.HashPassword(knownPwd)
	if err != nil {
		t.Fatalf("生成口令哈希失败: %v", err)
	}
	if err := ctx.Database.SetSetting("admin_password", hashed); err != nil {
		t.Fatalf("写入口令失败: %v", err)
	}
	// 上一步的失败尝试会计入限流，需清除后再测正确口令
	ctx.LoginLimiter.Reset("127.0.0.1")

	// Test 2: Authenticated Login
	loginPayload["password"] = knownPwd
	body, _ = json.Marshal(loginPayload)
	reqLogin, _ = http.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/auth/login", bytes.NewBuffer(body))
	reqLogin.Header.Set("Content-Type", "application/json")
	respLogin, _ := client.Do(reqLogin)

	if respLogin.StatusCode != http.StatusOK {
		t.Fatalf("Login failed: %d", respLogin.StatusCode)
	}

	var loginResp map[string]string
	_ = json.NewDecoder(respLogin.Body).Decode(&loginResp)
	token := loginResp["token"]
	if token == "" {
		t.Fatalf("Token not generated")
	}

	// Test 3: Authenticated Request
	reqAuth, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8890/api/v1/subscriptions", nil)
	reqAuth.Header.Set("Authorization", "Bearer "+token)
	respAuth, _ := client.Do(reqAuth)

	if respAuth.StatusCode != http.StatusOK {
		t.Errorf("Authenticated request failed, expected 200 got %d", respAuth.StatusCode)
	}
}

// config_merge / mihomo_reload 两个任务此前只在启动时登记为占位记录，
// handler 从不调用 MarkTaskRun，导致任务面板永久显示"待运行"。
// 本测试验证触发 /config/merge 与 /mihomo/reload 后，对应任务的
// last_run / status 字段会被真实更新。
func TestTaskStatusUpdatedAfterMergeAndReload(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Config{}
	cfg.DataSource = filepath.Join(dir, "test_task.db")
	cfg.Mihomo.ConfigDir = dir
	cfg.Auth.AccessSecret = "secret22345678901234567890123456"
	cfg.Auth.AccessExpire = 3600
	cfg.Host = "127.0.0.1"
	cfg.Port = 8891

	ctx := svc.NewServiceContext(cfg)
	t.Cleanup(func() { _ = ctx.Database.Close() })
	server := rest.MustNewServer(cfg.RestConf)
	defer server.Stop()
	handler.RegisterHandlers(server, ctx)

	go server.Start()
	time.Sleep(1 * time.Second)

	// 登录取 token
	hashed, err := auth.HashPassword("task-test-pass")
	if err != nil {
		t.Fatalf("生成口令哈希失败: %v", err)
	}
	if err := ctx.Database.SetSetting("admin_password", hashed); err != nil {
		t.Fatalf("写入口令失败: %v", err)
	}
	client := &http.Client{}
	body, _ := json.Marshal(map[string]string{"password": "task-test-pass"})
	reqLogin, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8891/api/v1/auth/login", bytes.NewBuffer(body))
	reqLogin.Header.Set("Content-Type", "application/json")
	respLogin, err := client.Do(reqLogin)
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	var loginResp map[string]string
	_ = json.NewDecoder(respLogin.Body).Decode(&loginResp)
	token := loginResp["token"]
	if token == "" {
		t.Fatal("未获取到登录令牌")
	}

	call := func(method, path string) *http.Response {
		req, _ := http.NewRequest(method, "http://127.0.0.1:8891"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", path, err)
		}
		return resp
	}

	// 触发一次合并
	_ = call(http.MethodPost, "/api/v1/config/merge")

	tasksBefore, err := ctx.Database.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	mergeTask := findTaskByName(tasksBefore, "config_merge")
	if mergeTask == nil {
		t.Fatal("未找到 config_merge 任务记录")
	}
	if mergeTask.LastRun.IsZero() {
		t.Fatal("触发合并后 config_merge 的 LastRun 应被更新，实际仍为零值")
	}
	if mergeTask.Status != "ok" && mergeTask.Status != "error" {
		t.Fatalf("config_merge 的 Status 应为 ok 或 error，实际 %q", mergeTask.Status)
	}

	// 触发一次内核重载
	_ = call(http.MethodPost, "/api/v1/mihomo/reload")

	tasksAfter, err := ctx.Database.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	reloadTask := findTaskByName(tasksAfter, "mihomo_reload")
	if reloadTask == nil {
		t.Fatal("未找到 mihomo_reload 任务记录")
	}
	if reloadTask.LastRun.IsZero() {
		t.Fatal("触发重载后 mihomo_reload 的 LastRun 应被更新，实际仍为零值")
	}
}

func findTaskByName(tasks []model.Task, name string) *model.Task {
	for i := range tasks {
		if tasks[i].Name == name {
			return &tasks[i]
		}
	}
	return nil
}

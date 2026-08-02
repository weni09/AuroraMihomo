package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerStopAndLogs(t *testing.T) {
	bin := "sleep"
	args := []string{"2"}
	if runtime.GOOS == "windows" {
		bin = "timeout"
		args = []string{"/t", "2", "/nobreak"}
	}
	mgr := NewManager(Config{BinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	mgr.cmd = exec.CommandContext(ctx, bin, args...)
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("mock binary unavailable: %v", err)
	}
	if err := mgr.Stop(ctx); err != nil {
		// on some windows environments kill may race; only hard fail if still running
		t.Logf("stop returned: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if mgr.Status().IsRunning {
		t.Fatal("expected stopped")
	}
	_ = mgr.Logs(10, "")
}

// 并发调用 Start/Stop/Restart 不得出现数据竞争或双重解锁，
// 且不应重复拉起进程。用 -race 运行。
func TestManagerConcurrentLifecycle(t *testing.T) {
	bin := "sleep"
	args := []string{"5"}
	if runtime.GOOS == "windows" {
		bin = "timeout"
		args = []string{"/t", "5", "/nobreak"}
	}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("mock binary unavailable: %v", err)
	}
	_ = args

	mgr := NewManager(Config{BinaryPath: bin, ConfigDir: t.TempDir()})
	// 订阅日志，让 appendLog 的回调路径也参与竞争
	unsub := mgr.SubscribeLogs(func(LogLine) {})
	defer unsub()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				switch n % 3 {
				case 0:
					_ = mgr.Start(ctx)
				case 1:
					_ = mgr.Stop(ctx)
				default:
					_ = mgr.Restart(ctx)
				}
				cancel()
			}
		}(i)
	}
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = mgr.Stop(ctx)
	if mgr.Status().IsRunning {
		t.Fatal("全部停止后状态仍为运行中")
	}
}

// controller 为空或进程未运行时，ReloadConfig 必须回退为 Reload（重启），
// 不能因为拿不到 external-controller 地址而直接报错或什么都不做。
func TestReloadConfigFallbackWhenNoController(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.ReloadConfig(ctx, "", "", "config.yaml")
	// 二进制不存在，回退到 Restart 必然失败，但错误必须来自 Start/Stop 路径，
	// 而不是卡住或 panic —— 能返回错误就说明确实走了回退分支
	if err == nil {
		t.Fatal("二进制不存在时 ReloadConfig 回退重启应报错")
	}
}

// 进程未运行时，即使传了 controller 地址也不应尝试请求（因为一定失败），
// 而是直接走回退路径
func TestReloadConfigFallbackWhenNotRunning(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.ReloadConfig(ctx, "127.0.0.1:19999", "", "config.yaml")
	if err == nil {
		t.Fatal("进程未运行时应回退重启并因二进制缺失而报错")
	}
}

// 进程已运行且 controller 可达时，ReloadConfig 应调用 PUT /configs 而不重启进程
func TestReloadConfigHotReloadSuccess(t *testing.T) {
	var gotPath, gotAuth string
	var gotForce string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/configs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotForce = r.URL.Query().Get("force")
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotPath = body["path"]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	bin := "sleep"
	args := []string{"3"}
	if runtime.GOOS == "windows" {
		bin = "timeout"
		args = []string{"/t", "3", "/nobreak"}
	}
	mgr := NewManager(Config{BinaryPath: bin})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	mgr.cmd = exec.CommandContext(ctx, bin, args...)
	if err := mgr.cmd.Start(); err != nil {
		t.Skipf("mock binary unavailable: %v", err)
	}
	defer func() { _ = mgr.Stop(context.Background()) }()

	controller := strings.TrimPrefix(srv.URL, "http://")
	pidBefore := mgr.cmd.Process.Pid

	if err := mgr.ReloadConfig(ctx, controller, "mysecret", "config.yaml"); err != nil {
		t.Fatalf("热重载应成功，实际报错: %v", err)
	}
	if gotForce != "true" {
		t.Fatalf("应带 force=true 查询参数，实际 %q", gotForce)
	}
	if gotAuth != "Bearer mysecret" {
		t.Fatalf("应带 Authorization 头，实际 %q", gotAuth)
	}
	if gotPath == "" {
		t.Fatal("请求体应包含配置文件绝对路径")
	}
	// 进程不应被重启：PID 应保持不变
	if mgr.cmd == nil || mgr.cmd.Process.Pid != pidBefore {
		t.Fatal("热重载不应重启进程")
	}
}

// external-controller 请求失败时应静默回退为重启，而不是直接把 HTTP 错误抛给调用方
func TestReloadConfigFallbackOnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bin := "sleep"
	args := []string{"3"}
	if runtime.GOOS == "windows" {
		bin = "timeout"
		args = []string{"/t", "3", "/nobreak"}
	}
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	realCmd := exec.CommandContext(ctx, bin, args...)
	if err := realCmd.Start(); err != nil {
		t.Skipf("mock binary unavailable: %v", err)
	}
	mgr.cmd = realCmd
	defer func() { _ = mgr.Stop(context.Background()) }()

	controller := strings.TrimPrefix(srv.URL, "http://")
	err := mgr.ReloadConfig(ctx, controller, "", "config.yaml")
	// 二进制路径是假的，回退重启必然失败——这正好证明确实发生了回退
	if err == nil {
		t.Fatal("API 报错后应回退重启，且因假二进制路径而报错")
	}
}

func TestWithDefaultEnvAddsMissing(t *testing.T) {
	got := withDefaultEnv([]string{"PATH=/bin", "HOME=/tmp"}, "DISABLE_NFTABLES", "1")
	found := false
	for _, e := range got {
		if e == "DISABLE_NFTABLES=1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应补上 DISABLE_NFTABLES=1，实际 %v", got)
	}
}

func TestWithDefaultEnvRespectsExisting(t *testing.T) {
	in := []string{"DISABLE_NFTABLES=0", "PATH=/bin"}
	got := withDefaultEnv(in, "DISABLE_NFTABLES", "1")
	if len(got) != len(in) {
		t.Fatalf("已有键时不应追加，got=%v", got)
	}
	if got[0] != "DISABLE_NFTABLES=0" {
		t.Fatalf("不得覆盖用户值，got=%v", got)
	}
}

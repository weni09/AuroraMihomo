package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

// 进程未运行时即使传了 controller 地址，ReloadConfig 会先尝试 API；
// 连接被拒绝（说明确实没有内核在听）后回退为 Restart 拉起内核。
func TestReloadConfigFallbackWhenNotRunning(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.ReloadConfig(ctx, "127.0.0.1:19999", "", "config.yaml")
	if err == nil {
		t.Fatal("进程未运行且 API 不可达时应回退重启并因二进制缺失而报错")
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

// 未托管任何进程但 controller 可达时，ReloadConfig 必须热重载成功且绝不
// Restart。这是自升级后的典型时序：旧主进程故意不杀内核（避免 TProxy
// 断网），新主进程尚未 Attach 就开始合并。旧实现把「未托管」当「未运行」
// 直接 Restart，会再 spawn 一份 mihomo 与旧内核抢代理/DNS/controller 端口，
// 表现为面板闪断、网络不可用（v0.10.0 自升级后的症状）。
func TestReloadConfigHotReloadWithoutAttach(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/configs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// 管理器不托管任何进程，且二进制路径是假的：若误走 Restart 必然因
	// 二进制缺失而报错，返回 nil 即证明没有触发 Restart。
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	controller := strings.TrimPrefix(srv.URL, "http://")
	if err := mgr.ReloadConfig(ctx, controller, "secret", "config.yaml"); err != nil {
		t.Fatalf("未托管但 API 可达时应热重载成功且不重启，实际报错: %v", err)
	}
	if st := mgr.Status(); st.IsRunning {
		t.Fatal("未托管任何进程，Status 不应显示运行中")
	}
}

// 未托管且 API 不可达（连接被拒绝）时，ReloadConfig 必须回退 Restart：
// 说明确实没有内核在听，需要拉起一份。与 TestReloadConfigHotReloadWithoutAttach
// 形成对照——回退与否取决于 API 是否可达，而不是是否已托管。
func TestReloadConfigRestartWhenUnmanagedAndAPIDown(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "definitely-not-a-real-binary-xyz"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.ReloadConfig(ctx, "127.0.0.1:19999", "", "config.yaml")
	if err == nil {
		t.Fatal("未托管且 API 不可达时应回退重启并因二进制缺失而报错")
	}
}

// external-controller 请求失败（已托管进程）时应静默回退为重启，而不是直接把 HTTP 错误抛给调用方
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

// DiscoverAndAttach 应在 Start 前接管已在运行的 mihomo 进程（自升级遗留 /
// 孤儿内核），避免 Start 双开抢端口。Linux 上通过 symlink 伪装成 mihomo 的
// sleep 进程验证 /proc cmdline 匹配与托管接管。
func TestDiscoverAndAttachFindsRunningMihomo(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("进程表扫描仅 Linux /proc 实现")
	}
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skipf("无 /bin/sleep，跳过: %v", err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "mihomo")
	if err := os.Symlink("/bin/sleep", link); err != nil {
		t.Skipf("无法创建 symlink: %v", err)
	}
	cmd := exec.Command(link, "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动模拟进程: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	mgr := NewManager(Config{BinaryPath: link})
	ok, err := mgr.DiscoverAndAttach()
	if err != nil {
		t.Fatalf("DiscoverAndAttach 不应报错: %v", err)
	}
	if !ok {
		t.Fatal("应发现并接管正在运行的 mihomo 进程")
	}
	st := mgr.Status()
	if !st.IsRunning {
		t.Fatal("接管后 Status 应显示运行中")
	}
}

// 进程表里没有 mihomo 时 DiscoverAndAttach 应返回 false，由调用方退回 Start。
func TestDiscoverAndAttachNothingRunning(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "mihomo-nonexistent-name-xyz"})
	ok, err := mgr.DiscoverAndAttach()
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if ok {
		t.Fatal("不存在匹配进程时应返回 false")
	}
}

// PIDMatchesBinary 在 Linux 上按 cmdline 首参核对进程身份，防止数据库 PID
// 被系统复用后误托管无关进程（自升级关停保存的 PID → 新进程接管之间，
// 旧 PID 可能被回收分配给别的程序）。
func TestPIDMatchesBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PID 身份校验仅 Linux /proc 实现")
	}
	if _, err := os.Stat("/bin/sleep"); err != nil {
		t.Skipf("无 /bin/sleep，跳过: %v", err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "mihomo")
	if err := os.Symlink("/bin/sleep", link); err != nil {
		t.Skipf("无法创建 symlink: %v", err)
	}
	cmd := exec.Command(link, "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动模拟进程: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	if !PIDMatchesBinary(cmd.Process.Pid, "mihomo") {
		t.Fatal("cmdline 首参与二进制名一致时应匹配")
	}
	if PIDMatchesBinary(cmd.Process.Pid, "other-name") {
		t.Fatal("二进制名不一致时应判为不匹配（PID 复用保护）")
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

func TestAttachExternalRejectsInvalidPID(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "mihomo", ConfigDir: t.TempDir()})
	ok, err := mgr.AttachExternal(0, "")
	if err != nil || ok {
		t.Fatalf("pid=0 应返回 false,nil，实际 ok=%v err=%v", ok, err)
	}
	ok, err = mgr.AttachExternal(-1, "")
	if err != nil || ok {
		t.Fatalf("负 pid 应返回 false,nil，实际 ok=%v err=%v", ok, err)
	}
	// 极大且通常不存在的 PID
	ok, err = mgr.AttachExternal(1<<30, "v-test")
	if err != nil {
		t.Fatalf("不存在的 PID 不应报错: %v", err)
	}
	if ok {
		t.Fatal("不存在的 PID 不应被判定为接管成功")
	}
}

func TestAttachExternalCurrentProcess(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "mihomo", ConfigDir: t.TempDir()})
	pid := os.Getpid()
	ok, err := mgr.AttachExternal(pid, "attached-version")
	if err != nil {
		t.Fatalf("接管当前进程失败: %v", err)
	}
	if !ok {
		t.Fatal("当前进程 PID 应可接管")
	}
	// 给轮询 goroutine 一点时间：旧实现会对非亲子进程 Wait→ECHILD 并立刻
	// 清空托管，这里 sleep 后仍应显示 running。
	time.Sleep(50 * time.Millisecond)
	st := mgr.Status()
	if !st.IsRunning || st.PID != pid {
		t.Fatalf("Status 应显示已运行 pid=%d，实际 %+v", pid, st)
	}
	if st.Version != "attached-version" {
		t.Fatalf("版本应写入，实际 %q", st.Version)
	}
	// 幂等：再次 Attach 同一管理器应直接成功
	ok, err = mgr.AttachExternal(pid, "ignored")
	if err != nil || !ok {
		t.Fatalf("重复 Attach 应幂等成功，实际 ok=%v err=%v", ok, err)
	}
	// 再等一轮，确认没有被错误地清掉
	time.Sleep(50 * time.Millisecond)
	st = mgr.Status()
	if !st.IsRunning || st.PID != pid {
		t.Fatalf("轮询后仍应托管 pid=%d，实际 %+v", pid, st)
	}
}

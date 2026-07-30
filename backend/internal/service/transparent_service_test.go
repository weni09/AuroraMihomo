package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

// fakeStore 内存版设置存储。
type fakeStore struct {
	mu sync.Mutex
	kv map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{kv: map[string]string{}} }

func (f *fakeStore) GetSetting(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.kv[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (f *fakeStore) SetSetting(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kv[key] = value
	return nil
}

// fakeApplier 记录调用，可注入失败。
type fakeApplier struct {
	mu         sync.Mutex
	applied    int
	tornDown   int
	snapshots  int
	applyErr   error
	lastParams netcheck.TProxyParams
}

func (f *fakeApplier) Apply(_ context.Context, p netcheck.TProxyParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied++
	f.lastParams = p
	return f.applyErr
}

func (f *fakeApplier) Teardown(_ context.Context, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tornDown++
	return nil
}

func (f *fakeApplier) Snapshot(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots++
	return "/tmp/snap", nil
}

func (f *fakeApplier) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applied, f.tornDown, f.snapshots
}

// 构造一个可用性可控的 report
func reportWith(os string, available ...netcheck.Mode) *netcheck.Report {
	r := &netcheck.Report{OS: os}
	for _, m := range []netcheck.Mode{netcheck.ModeTUN, netcheck.ModeTProxy} {
		ok := false
		for _, a := range available {
			if a == m {
				ok = true
			}
		}
		st := netcheck.ModeStatus{Mode: m, Available: ok}
		if !ok {
			st.Reason = "缺少 " + string(m) + " 所需条件"
			st.InstallHint = "apk add nftables"
		}
		r.Modes = append(r.Modes, st)
	}
	return r
}

func newSvc(t *testing.T, report *netcheck.Report) (*TransparentService, *fakeStore, *fakeApplier) {
	t.Helper()
	store := newFakeStore()
	app := &fakeApplier{}
	reloads := 0
	s := NewTransparentService(store, app, logx.WithContext(context.Background()),
		func(context.Context) error { reloads++; return nil })
	s.detect = func() *netcheck.Report { return report }
	return s, store, app
}

// 环境不支持时必须拒绝写入，并把原因与修复命令告诉用户
func TestUpdateRejectsUnavailableMode(t *testing.T) {
	s, store, _ := newSvc(t, reportWith("linux")) // 两种模式都不可用

	err := s.Update(context.Background(), true, "tproxy", 0, "")
	if err == nil {
		t.Fatal("环境不支持时应拒绝启用")
	}
	// 报错要有可操作性
	if !strings.Contains(err.Error(), "缺少") || !strings.Contains(err.Error(), "apk add") {
		t.Errorf("报错应包含原因与修复命令，实际: %v", err)
	}
	// 不能留下"已启用"的状态
	if v, _ := store.GetSetting(settingTransparentEnabled); v == "true" {
		t.Error("拒绝后不应把 enabled 写成 true")
	}
}

// 关闭必须在任何环境下都可用，否则用户会陷入"开着但关不掉"
func TestDisableAlwaysAllowedEvenWhenUnsupported(t *testing.T) {
	s, store, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	// 环境突然变得不支持（例如依赖被卸载）
	s.detect = func() *netcheck.Report { return reportWith("linux") }

	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Errorf("关闭应始终允许，实际报错: %v", err)
	}
	if v, _ := store.GetSetting(settingTransparentEnabled); v != "false" {
		t.Errorf("关闭后 enabled 应为 false，实际 %q", v)
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("关闭时应拆除规则")
	}
}

// 启用后必须落下"待确认"记录，且要先记录再下发规则
func TestEnableWritesPendingBeforeApplying(t *testing.T) {
	s, store, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))

	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	v, err := store.GetSetting(settingTransparentPendingUntil)
	if err != nil || v == "" {
		t.Fatal("启用后应记录确认截止时间，否则面板崩溃后不会回滚")
	}
	sec, _ := strconv.ParseInt(v, 10, 64)
	left := time.Until(time.Unix(sec, 0))
	if left <= 0 || left > ConfirmWindow+time.Second {
		t.Errorf("确认窗口异常: 剩余 %v，期望约 %v", left, ConfirmWindow)
	}
	if applied, _, snaps := app.counts(); applied != 1 || snaps != 1 {
		t.Errorf("TProxy 启用应先快照再下发，实际 applied=%d snapshots=%d", applied, snaps)
	}
}

// 规则下发失败要回到关闭状态，不能留下"待确认"
func TestEnableRollsBackWhenApplyFails(t *testing.T) {
	s, store, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	app.applyErr = errors.New("nft 执行失败")

	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err == nil {
		t.Fatal("下发失败时应报错")
	}
	if v, _ := store.GetSetting(settingTransparentEnabled); v == "true" {
		t.Error("下发失败后不应保持 enabled")
	}
	if v, _ := store.GetSetting(settingTransparentPendingUntil); v != "" {
		t.Errorf("下发失败后不应留待确认记录，实际 %q", v)
	}
}

// TUN 模式由 mihomo 自管防火墙，面板不该去动规则
func TestEnableTUNDoesNotTouchFirewall(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTUN))

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}
	if applied, _, snaps := app.counts(); applied != 0 || snaps != 0 {
		t.Errorf("TUN 模式不该下发防火墙规则，实际 applied=%d snapshots=%d", applied, snaps)
	}
}

// 确认后清掉待确认记录，回滚不再发生
func TestConfirmClearsPending(t *testing.T) {
	s, store, _ := newSvc(t, reportWith("linux", netcheck.ModeTUN))
	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if v, _ := store.GetSetting(settingTransparentPendingUntil); v != "" {
		t.Errorf("确认后应清掉待确认记录，实际 %q", v)
	}
	st, _ := s.Status()
	if st.PendingConfirm {
		t.Error("确认后 PendingConfirm 应为 false")
	}
	if !st.Enabled {
		t.Error("确认后应保持启用")
	}
}

func TestConfirmWithoutPendingFails(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux", netcheck.ModeTUN))
	if err := s.Confirm(context.Background()); err == nil {
		t.Error("没有待确认操作时确认应报错")
	}
}

// 核心保护：面板崩溃重启后，超时未确认的启用必须被回滚。
// 只靠进程内定时器做不到这一点——进程一死，规则会永久留在宿主上。
func TestRecoverPendingRollsBackExpiredEnable(t *testing.T) {
	store := newFakeStore()
	app := &fakeApplier{}
	// 模拟上次启用后崩溃：数据库里留着已过期的待确认记录
	_ = store.SetSetting(settingTransparentEnabled, "true")
	_ = store.SetSetting(settingTransparentMode, "tproxy")
	_ = store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10))

	s := NewTransparentService(store, app, logx.WithContext(context.Background()), nil)
	s.detect = func() *netcheck.Report { return reportWith("linux", netcheck.ModeTProxy) }

	s.RecoverPending(context.Background())

	if v, _ := store.GetSetting(settingTransparentEnabled); v != "false" {
		t.Errorf("超时未确认应回滚为 false，实际 %q", v)
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("回滚时应拆除规则")
	}
}

// 窗口未过期时不该回滚，而是继续等待
func TestRecoverPendingKeepsUnexpiredEnable(t *testing.T) {
	store := newFakeStore()
	app := &fakeApplier{}
	_ = store.SetSetting(settingTransparentEnabled, "true")
	_ = store.SetSetting(settingTransparentMode, "tproxy")
	_ = store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

	s := NewTransparentService(store, app, logx.WithContext(context.Background()), nil)
	s.detect = func() *netcheck.Report { return reportWith("linux", netcheck.ModeTProxy) }

	s.RecoverPending(context.Background())

	if v, _ := store.GetSetting(settingTransparentEnabled); v != "true" {
		t.Errorf("窗口未过期不应回滚，实际 enabled=%q", v)
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("窗口未过期不该拆除规则")
	}
}

// 没有待确认记录时 RecoverPending 应无副作用
func TestRecoverPendingNoopWhenNothingPending(t *testing.T) {
	store := newFakeStore()
	app := &fakeApplier{}
	s := NewTransparentService(store, app, logx.WithContext(context.Background()), nil)
	s.detect = func() *netcheck.Report { return reportWith("linux", netcheck.ModeTProxy) }

	s.RecoverPending(context.Background())

	if _, down, _ := app.counts(); down != 0 {
		t.Error("无待确认记录时不该做任何拆除")
	}
}

// 规则里的放行端口必须覆盖 SSH 与面板，否则可能把操作者关在门外
func TestApplyKeepsManagementPorts(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	for _, want := range []int{22, 8899} {
		found := false
		for _, p := range ports {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("放行端口应包含 %d（SSH/面板），实际 %v", want, ports)
		}
	}
}

func TestUpdateRejectsInvalidMode(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux", netcheck.ModeTUN))
	if err := s.Update(context.Background(), true, "bogus", 0, ""); err == nil {
		t.Error("非法模式应被拒绝")
	}
}

// 注入参数要跟随开关状态：关闭时必须给出 off，否则配置里会残留上次的 tun
func TestInjectOptionsFollowsState(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux", netcheck.ModeTUN))

	if opt := s.InjectOptions(); opt.Mode != netcheck.ModeOff {
		t.Errorf("未启用时应返回 off，实际 %s", opt.Mode)
	}

	if err := s.Update(context.Background(), true, "tun", 0, "system"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	opt := s.InjectOptions()
	if opt.Mode != netcheck.ModeTUN {
		t.Errorf("启用后应返回 tun，实际 %s", opt.Mode)
	}
	if opt.TUNStack != "system" {
		t.Errorf("应沿用指定的 stack，实际 %q", opt.TUNStack)
	}
	// Linux 上才开 auto-redirect
	if !opt.AutoRedirect {
		t.Error("Linux 上应启用 auto-redirect 让 mihomo 自管防火墙")
	}
}

func TestInjectOptionsNoAutoRedirectOnDarwin(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("darwin", netcheck.ModeTUN))
	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if s.InjectOptions().AutoRedirect {
		t.Error("macOS 上 auto-redirect 无效，不应开启")
	}
}

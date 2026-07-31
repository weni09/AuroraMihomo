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
	// failOn 令指定键的写入失败，用于验证中途失败时是否正确回滚
	failOn string
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
	if f.failOn != "" && f.failOn == key {
		return errors.New("模拟落库失败: " + key)
	}
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
	// rulesActive 与 rulesActiveErr 控制 RulesActive 的返回值，
	// 默认 rulesActive=true：多数测试不关心这个探测，默认"规则还在"
	// 才不会意外触发 ReconcileState 的回滚路径
	rulesActive    bool
	rulesActiveErr error
}

func (f *fakeApplier) Apply(_ context.Context, p netcheck.TProxyParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied++
	f.lastParams = p
	return f.applyErr
}

func (f *fakeApplier) Teardown(_ context.Context) error {
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

func (f *fakeApplier) RulesActive(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rulesActive, f.rulesActiveErr
}

// newFakeApplier 默认 rulesActive=true：多数测试场景里规则理应还在，
// 只有专门测 ReconcileState 的用例才需要显式改成 false
func newFakeApplier() *fakeApplier {
	return &fakeApplier{rulesActive: true}
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

// fakeBase 内存版 base.yaml。
//
// 开关状态现在落在 base.yaml 而不是 settings 表，断言必须能看到这份文本，
// 否则测试只能验证"调用没报错"，看不出到底写了什么。
type fakeBase struct {
	mu      sync.Mutex
	content string
	// getErr / setErr 用于模拟读写失败，验证失败时是否正确回滚
	getErr error
	setErr error
	// writes 记录写入次数，用来确认回滚确实回写了一次
	writes int
}

func newFakeBase(initial string) *fakeBase { return &fakeBase{content: initial} }

func (f *fakeBase) get() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.content, nil
}

func (f *fakeBase) set(c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.content = c
	f.writes++
	return nil
}

func (f *fakeBase) text() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content
}

func (f *fakeBase) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// tunEnabled / tproxyPort 复用生产代码的读取逻辑，避免测试自己写一套
// 解析而与实现漂移
func (f *fakeBase) tunEnabled(t *testing.T) bool {
	t.Helper()
	on, _, _, err := readBaseSwitchState(f.text())
	if err != nil {
		t.Fatalf("解析 base 配置失败: %v", err)
	}
	return on
}

func (f *fakeBase) tproxyPort(t *testing.T) int {
	t.Helper()
	_, _, p, err := readBaseSwitchState(f.text())
	if err != nil {
		t.Fatalf("解析 base 配置失败: %v", err)
	}
	return p
}

func newSvc(t *testing.T, report *netcheck.Report) (*TransparentService, *fakeStore, *fakeApplier) {
	t.Helper()
	s, store, app, _ := newSvcWithBase(t, report, "")
	return s, store, app
}

// newSvcWithBase 额外暴露 base.yaml 假实现，供需要检查落点的用例使用
func newSvcWithBase(t *testing.T, report *netcheck.Report, initialBase string) (
	*TransparentService, *fakeStore, *fakeApplier, *fakeBase) {
	t.Helper()
	store := newFakeStore()
	app := newFakeApplier()
	base := newFakeBase(initialBase)
	reloads := 0
	s := NewTransparentService(store, app, logx.WithContext(context.Background()),
		func(context.Context) error { reloads++; return nil },
		base.get, base.set)
	s.detect = func() *netcheck.Report { return report }
	return s, store, app, base
}

// 环境不支持时必须拒绝写入，并把原因与修复命令告诉用户
func TestUpdateRejectsUnavailableMode(t *testing.T) {
	s, _, _, base := newSvcWithBase(t, reportWith("linux"), "") // 两种模式都不可用

	err := s.Update(context.Background(), true, "tproxy", 0, "")
	if err == nil {
		t.Fatal("环境不支持时应拒绝启用")
	}
	// 报错要有可操作性
	if !strings.Contains(err.Error(), "缺少") || !strings.Contains(err.Error(), "apk add") {
		t.Errorf("报错应包含原因与修复命令，实际: %v", err)
	}
	// 被拒绝的启用不该在用户配置里留下任何痕迹
	if base.writeCount() != 0 {
		t.Errorf("拒绝后不该改写 base 配置，实际写入 %d 次", base.writeCount())
	}
}

// 关闭必须在任何环境下都可用，否则用户会陷入"开着但关不掉"
func TestDisableAlwaysAllowedEvenWhenUnsupported(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	// 环境突然变得不支持（例如依赖被卸载）
	s.detect = func() *netcheck.Report { return reportWith("linux") }

	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Errorf("关闭应始终允许，实际报错: %v", err)
	}
	if base.tproxyPort(t) != 0 {
		t.Errorf("关闭后 base.yaml 里的 tproxy-port 应被清掉，实际 %d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("关闭时应拆除规则")
	}
}

// 关闭 TUN 必须显式写 enable: false，不能只把键删掉。
//
// 删键等于"本地未声明"，订阅里带 tun.enable: true 时合并会把它放回来，
// 用户点了关闭却关不掉——这是开关状态迁移到 base.yaml 后新出现的风险点。
func TestDisableWritesExplicitFalseForTUN(t *testing.T) {
	s, _, _, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN), "")
	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	text := base.text()
	if !strings.Contains(text, "enable: false") {
		t.Errorf("关闭 TUN 必须在 base.yaml 里显式写 enable: false，"+
			"否则订阅带的 tun.enable: true 会把它盖回来。实际内容:\n%s", text)
	}
	if base.tunEnabled(t) {
		t.Error("关闭后读回的状态仍是开启")
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
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), userBaseYAML)
	app.applyErr = errors.New("nft 执行失败")

	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err == nil {
		t.Fatal("下发失败时应报错")
	}
	if base.tproxyPort(t) != 0 {
		t.Errorf("下发失败后不应保持启用，实际 tproxy-port=%d", base.tproxyPort(t))
	}
	if v, _ := store.GetSetting(settingTransparentPendingUntil); v != "" {
		t.Errorf("下发失败后不应留待确认记录，实际 %q", v)
	}
	// 回滚必须还原成原文，而不是"再改一次"的结果——后者会在每次失败时
	// 都把用户的注释与键顺序磨掉一层
	if base.text() != userBaseYAML {
		t.Errorf("回滚应还原 base.yaml 原文。\n期望:\n%s\n实际:\n%s", userBaseYAML, base.text())
	}
}

// 启用过程中落库失败也要还原 base.yaml，否则磁盘上会留下一个没能生效的开关
func TestEnableRestoresBaseWhenStoreFails(t *testing.T) {
	store := newFakeStore()
	app := newFakeApplier()
	base := newFakeBase(userBaseYAML)
	s := NewTransparentService(store, app, logx.WithContext(context.Background()),
		nil, base.get, base.set)
	s.detect = func() *netcheck.Report { return reportWith("linux", netcheck.ModeTUN) }
	store.failOn = settingTransparentMode

	if err := s.Update(context.Background(), true, "tun", 0, ""); err == nil {
		t.Fatal("落库失败时应报错")
	}
	if base.text() != userBaseYAML {
		t.Errorf("落库失败后应还原 base.yaml 原文，实际:\n%s", base.text())
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

// 确认后清掉待确认记录，回滚不再发生。
// 用 TProxy：只有它会开确认窗口（见下一个用例）。
func TestConfirmClearsPending(t *testing.T) {
	s, store, _, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
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

// TUN 不开确认窗口：它由 mihomo 自管路由与防火墙并在退出时清理，
// 不存在"改错了却关不掉"的处境，没必要让用户每次都去点一次确认。
// 这条与前端提示文案必须一致——界面若仍宣称"90 秒内不确认会自动回滚"，
// 用户会等一个永远不会出现的按钮。
func TestEnableTUNOpensNoConfirmWindow(t *testing.T) {
	s, store, _, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN), "")

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}

	if v, _ := store.GetSetting(settingTransparentPendingUntil); v != "" {
		t.Errorf("TUN 不该记录确认截止时间，实际 %q", v)
	}
	st, _ := s.Status()
	if st.PendingConfirm {
		t.Error("TUN 启用后不该进入待确认状态")
	}
	if !st.Enabled || st.Mode != string(netcheck.ModeTUN) {
		t.Errorf("TUN 应直接生效，实际 enabled=%v mode=%q", st.Enabled, st.Mode)
	}
	if !base.tunEnabled(t) {
		t.Error("base.yaml 里应已写入 tun.enable: true")
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
	// 模拟上次启用后崩溃：base.yaml 里 TProxy 还开着，
	// 数据库里留着已过期的待确认记录
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	_ = store.SetSetting(settingTransparentMode, "tproxy")
	_ = store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10))

	s.RecoverPending(context.Background())

	// 回滚必须落到 base.yaml 上：只改 settings 表的话，随后的合并
	// 仍会把 tproxy-port 写进最终配置
	if base.tproxyPort(t) != 0 {
		t.Errorf("超时未确认应清掉 base.yaml 里的 tproxy-port，实际 %d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("回滚时应拆除规则")
	}
}

// 窗口未过期时不该回滚，而是继续等待
func TestRecoverPendingKeepsUnexpiredEnable(t *testing.T) {
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	_ = store.SetSetting(settingTransparentMode, "tproxy")
	_ = store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

	s.RecoverPending(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("窗口未过期不应回滚，实际 tproxy-port=%d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("窗口未过期不该拆除规则")
	}
}

// 没有待确认记录时 RecoverPending 应无副作用
func TestRecoverPendingNoopWhenNothingPending(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")

	s.RecoverPending(context.Background())

	if _, down, _ := app.counts(); down != 0 {
		t.Error("无待确认记录时不该做任何拆除")
	}
	if base.writeCount() != 0 {
		t.Error("无待确认记录时不该改写 base 配置")
	}
}

// 真机测试发现的问题：TProxy 规则不持久化到宿主重启，但"已确认启用"的
// 记录会。ReconcileState 探测到规则已消失时应回落为关闭，而不是让界面
// 一直显示"已开启"却与宿主实际状态不符。
func TestReconcileStateDisablesWhenRulesGoneAfterReboot(t *testing.T) {
	// base.yaml 里 TProxy 开着（"已确认启用"的状态，没有 pending_until），
	// 但宿主重启后规则已被内核清空
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	app.rulesActive = false
	_ = store.SetSetting(settingTransparentMode, "tproxy")

	s.ReconcileState(context.Background())

	// 这条断言是本次改动的回归防线：启用状态一旦改从 base.yaml 判定，
	// 而这里仍读 settings 表的 enabled，整段检测就会变成永不触发的死代码
	if base.tproxyPort(t) != 0 {
		t.Errorf("规则已消失时应清掉 base.yaml 里的 tproxy-port，实际 %d", base.tproxyPort(t))
	}
	if v, _ := store.GetSetting(settingTransparentMode); v != string(netcheck.ModeOff) {
		t.Errorf("回落后 mode 应为 off，实际 %q", v)
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("回落状态时应调用 Teardown（幂等，即便规则已经不存在）")
	}
}

// ReconcileState 只在启动流程里被调用，紧随其后就有一次合并，
// 因此它自己不该再触发一次配置重新下发——否则每次"带 TProxy 开关的
// 宿主重启"都会白拉一遍所有订阅。
func TestReconcileStateDoesNotTriggerRedundantReload(t *testing.T) {
	store := newFakeStore()
	app := newFakeApplier()
	app.rulesActive = false
	base := newFakeBase("tproxy-port: 7893\n")
	_ = store.SetSetting(settingTransparentMode, "tproxy")

	reloads := 0
	s := NewTransparentService(store, app, logx.WithContext(context.Background()),
		func(context.Context) error { reloads++; return nil }, base.get, base.set)
	s.detect = func() *netcheck.Report { return reportWith("linux", netcheck.ModeTProxy) }

	s.ReconcileState(context.Background())

	if base.tproxyPort(t) != 0 {
		t.Fatalf("前置条件：应已回落，实际 tproxy-port=%d", base.tproxyPort(t))
	}
	if reloads != 0 {
		t.Errorf("不该触发配置重新下发（启动流程随后会合并一次），实际调用 %d 次", reloads)
	}
}

// 规则确实还在时不该做任何改动
func TestReconcileStateNoopWhenRulesStillActive(t *testing.T) {
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	app.rulesActive = true
	_ = store.SetSetting(settingTransparentMode, "tproxy")

	s.ReconcileState(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("规则还在时不该改动 base 配置，实际 tproxy-port=%d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("规则还在时不该拆除")
	}
}

// 待确认状态（尚未走到 Confirm）应交给 RecoverPending 处理，
// ReconcileState 不该在这个阶段介入
func TestReconcileStateSkipsWhenStillPending(t *testing.T) {
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	app.rulesActive = false
	_ = store.SetSetting(settingTransparentMode, "tproxy")
	_ = store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

	s.ReconcileState(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("待确认状态下不该被 ReconcileState 提前改动，实际 tproxy-port=%d",
			base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("待确认状态下不该拆除，应交给 RecoverPending 处理")
	}
}

// TUN 模式不受影响：路由与防火墙完全由 mihomo 自管，不存在"记录说开着、
// 内核没跟上"的缺口，ReconcileState 不该碰它
func TestReconcileStateIgnoresTUNMode(t *testing.T) {
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN), "tun:\n  enable: true\n")
	app.rulesActive = false
	_ = store.SetSetting(settingTransparentMode, "tun")

	s.ReconcileState(context.Background())

	if !base.tunEnabled(t) {
		t.Error("TUN 模式不该被 ReconcileState 关掉")
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("TUN 模式下不该调用 Teardown")
	}
}

// 探测本身失败时（如 nft 命令不可执行）不能贸然断言"规则已失效"，
// 应保持现状，只留下日志
func TestReconcileStateKeepsStateWhenProbeFails(t *testing.T) {
	s, store, app, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")
	app.rulesActiveErr = errors.New("nft: command not found")
	_ = store.SetSetting(settingTransparentMode, "tproxy")

	s.ReconcileState(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("探测失败时不该断言规则已失效，实际 tproxy-port=%d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Error("探测失败时不该拆除")
	}
}

// 规则里的放行端口必须覆盖 SSH 与面板，否则可能把操作者关在门外
func TestApplyKeepsManagementPorts(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	s.SetManagementPorts(8899, func() int { return 9090 })
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	for _, want := range []int{22, 8899, 9090} {
		found := false
		for _, p := range ports {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("放行端口应包含 %d（SSH/面板/内核 API），实际 %v", want, ports)
		}
	}
}

// 管理端口必须取运行时的真实配置。硬编码 8899/9090 的话，用户把面板
// 改到别的端口后启用 TProxy 就会失去面板访问——与本文件通篇在防的
// "锁死自己"是同一类事故。
func TestApplyUsesConfiguredManagementPorts(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	s.SetManagementPorts(8443, func() int { return 19090 })
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	for _, want := range []int{22, 8443, 19090} {
		if !containsPort(ports, want) {
			t.Errorf("放行端口应包含实际配置的 %d，实际 %v", want, ports)
		}
	}
	// 旧的硬编码值不该再出现，否则说明取的仍是写死的那一组
	for _, unwanted := range []int{8899, 9090} {
		if containsPort(ports, unwanted) {
			t.Errorf("放行端口不应含硬编码的 %d，实际 %v", unwanted, ports)
		}
	}
}

// 内核 API 端口用户随时可改，所以必须每次现取而不是启动时缓存一次。
func TestControllerPortReadFreshOnEachApply(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	port := 9090
	s.SetManagementPorts(8899, func() int { return port })

	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("首次启用失败: %v", err)
	}
	// 模拟用户改掉 external-controller 端口后重新启用
	port = 19090
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("再次启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	if !containsPort(ports, 19090) {
		t.Errorf("应放行改动后的内核 API 端口 19090，实际 %v", ports)
	}
	if containsPort(ports, 9090) {
		t.Errorf("不应继续放行改动前的 9090（说明端口被缓存了），实际 %v", ports)
	}
}

// 端口取不到时（未配置 external-controller、配置还没生成）返回 0，
// 不该把 0 写进规则——那会生成一条永不匹配的规则并掩盖配置问题。
func TestZeroManagementPortsAreOmitted(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	s.SetManagementPorts(0, func() int { return 0 })
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	if containsPort(ports, 0) {
		t.Errorf("规则里不应出现 0 端口，实际 %v", ports)
	}
	// SSH 兜底必须还在：它不由本程序配置，是最后的救命通道
	if !containsPort(ports, 22) {
		t.Errorf("SSH 22 必须始终放行，实际 %v", ports)
	}
}

// 面板与内核 API 配到同一端口时不该产生重复规则：重复条目无害但会让
// 排障的人以为规则被下发了两次。
func TestDuplicateManagementPortsDeduped(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTProxy))
	s.SetManagementPorts(22, func() int { return 22 })
	if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	app.mu.Lock()
	ports := app.lastParams.KeepPorts
	app.mu.Unlock()

	if len(ports) != 1 || ports[0] != 22 {
		t.Errorf("重复端口应被去重为单个 22，实际 %v", ports)
	}
}

func containsPort(ports []int, want int) bool {
	for _, p := range ports {
		if p == want {
			return true
		}
	}
	return false
}

// v6 规则与 v6 策略路由必须同时有或同时没有。只下发规则而没有路由，
// v6 包会被打上标记却无路可走，从"不分流"恶化成"不通"——所以这里
// 严格跟随宿主的实际 v6 出网能力，而不是无条件开或无条件关。
func TestEnableIPv6FollowsHostEgressCapability(t *testing.T) {
	for _, tc := range []struct {
		name   string
		egress bool
	}{
		{"宿主有 v6 出网能力时下发 v6 规则", true},
		{"宿主无 v6 出网能力时不下发", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := reportWith("linux", netcheck.ModeTProxy)
			report.HasIPv6Egress = tc.egress
			s, _, app := newSvc(t, report)
			if err := s.Update(context.Background(), true, "tproxy", 0, ""); err != nil {
				t.Fatalf("启用失败: %v", err)
			}

			app.mu.Lock()
			got := app.lastParams.EnableIPv6
			app.mu.Unlock()

			if got != tc.egress {
				t.Errorf("EnableIPv6 应为 %v（宿主 v6 出网=%v），实际 %v",
					tc.egress, tc.egress, got)
			}
		})
	}
}

func TestUpdateRejectsInvalidMode(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux", netcheck.ModeTUN))
	if err := s.Update(context.Background(), true, "bogus", 0, ""); err == nil {
		t.Error("非法模式应被拒绝")
	}
}

// 开关状态与 base.yaml 必须始终一致：这是"两个界面读同一份数据"的根基。
// 早先这里测的是 InjectOptions()——那条路径已被移除，因为它构成了
// 与 base.yaml 并列的第二个事实来源。
func TestStateFollowsBaseConfig(t *testing.T) {
	s, _, _, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN), "")

	if st, _ := s.Status(); st.Enabled {
		t.Error("base.yaml 里没开时状态不该是启用")
	}

	if err := s.Update(context.Background(), true, "tun", 0, "system"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	st, _ := s.Status()
	if !st.Enabled || st.Mode != string(netcheck.ModeTUN) {
		t.Errorf("启用后状态应为 tun，实际 enabled=%v mode=%q", st.Enabled, st.Mode)
	}
	// 用户选的 stack 要真正落到配置里，而不是只记在内存或 settings 表
	if st.TUNStack != "system" {
		t.Errorf("应沿用指定的 stack，实际 %q", st.TUNStack)
	}
	if !strings.Contains(base.text(), "stack: system") {
		t.Errorf("base.yaml 里应写入 stack: system，实际:\n%s", base.text())
	}
}

// 用户在配置中心直接改 base.yaml 时，系统设置那边的开关必须跟着变——
// 这是双向同步的另一半，没有它两个界面又会各说各话
func TestStateReflectsExternalBaseEdit(t *testing.T) {
	s, _, _, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN), "")

	// 模拟用户在配置中心手工开启 TUN
	_ = base.set("tun:\n  enable: true\n  stack: gvisor\n")

	st, _ := s.Status()
	if !st.Enabled || st.Mode != string(netcheck.ModeTUN) {
		t.Errorf("应识别出用户手工开启的 TUN，实际 enabled=%v mode=%q", st.Enabled, st.Mode)
	}
	if st.TUNStack != "gvisor" {
		t.Errorf("应读出用户填的 stack，实际 %q", st.TUNStack)
	}
}

// 两种模式在 base.yaml 里同时存在时以 TUN 为先，且状态判定要稳定，
// 否则界面会在两个模式间跳变
func TestStatePrefersTUNWhenBothConfigured(t *testing.T) {
	s, _, _, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN),
		"tun:\n  enable: true\ntproxy-port: 7893\n")

	st, _ := s.Status()
	if st.Mode != string(netcheck.ModeTUN) {
		t.Errorf("两者并存时应以 TUN 为先，实际 %q", st.Mode)
	}
}

// 切换模式必须把另一种清掉，否则最终配置里会两个模式同时开着
func TestSwitchingModeClearsTheOther(t *testing.T) {
	s, _, _, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy), "")

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("切到 TProxy 失败: %v", err)
	}

	if base.tunEnabled(t) {
		t.Error("切到 TProxy 后 TUN 应被关掉")
	}
	if base.tproxyPort(t) != 7893 {
		t.Errorf("TProxy 端口未写入，实际 %d", base.tproxyPort(t))
	}

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("切回 TUN 失败: %v", err)
	}
	if base.tproxyPort(t) != 0 {
		t.Errorf("切回 TUN 后 tproxy-port 应被清掉，实际 %d", base.tproxyPort(t))
	}
	if !base.tunEnabled(t) {
		t.Error("切回 TUN 后应为开启")
	}
}

// 反复切换开关不该持续磨损用户的 base.yaml：注释必须一直在
func TestTogglingPreservesUserComments(t *testing.T) {
	s, _, _, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN), userBaseYAML)

	for i := 0; i < 3; i++ {
		if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
			t.Fatalf("第 %d 次启用失败: %v", i, err)
		}
		if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
			t.Fatalf("第 %d 次关闭失败: %v", i, err)
		}
	}

	text := base.text()
	if !strings.Contains(text, "# 我的机场配置，请勿删除注释") {
		t.Errorf("反复切换后用户注释丢失，实际:\n%s", text)
	}
	if !strings.Contains(text, "香港节点") {
		t.Errorf("反复切换后用户节点配置丢失，实际:\n%s", text)
	}
	// 结构体往返会凭空写出这些零值字段，定点改写不该出现
	if strings.Contains(text, "ipv6: false") {
		t.Errorf("反复切换后凭空出现了用户未设置的字段，实际:\n%s", text)
	}
}

// ---- 环境准备 ----

// fakeProvisioner 记录调用并可注入失败
type fakeProvisioner struct {
	calls    int
	lastOpts netcheck.ProvisionOptions
	res      *netcheck.ProvisionResult
	err      error
}

func (f *fakeProvisioner) Provision(_ context.Context, _ *netcheck.Report,
	opts netcheck.ProvisionOptions) (*netcheck.ProvisionResult, error) {
	f.calls++
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	return &netcheck.ProvisionResult{Success: true, Message: "ok"}, nil
}

// 未注入 provisioner（非 Linux）时要给出能自解释的拒绝，而不是空指针崩溃
func TestProvisionRejectedWhenUnavailable(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("windows"))

	_, env, err := s.Provision(context.Background(),
		netcheck.ProvisionOptions{InstallPackages: true})
	if err == nil {
		t.Fatal("未注入 provisioner 时应拒绝")
	}
	if !strings.Contains(err.Error(), "Linux") {
		t.Errorf("报错应说明仅 Linux 可用，实际: %v", err)
	}
	// 即便拒绝，环境报告也要照常返回，否则界面上"缺什么"的提示会整块消失
	if env == nil {
		t.Error("拒绝时也应返回环境报告")
	}
}

// 装完包后工具才出现在 PATH 上，必须重新探测，否则界面仍显示"不可用"
func TestProvisionRedetectsAfterSuccess(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux"))
	p := &fakeProvisioner{}
	s.SetProvisioner(p)

	// 模拟"准备之后环境变可用"：第二次探测返回 TProxy 可用
	detectCount := 0
	s.detect = func() *netcheck.Report {
		detectCount++
		if detectCount == 1 {
			return reportWith("linux") // 准备前：都不可用
		}
		return reportWith("linux", netcheck.ModeTProxy)
	}

	res, env, err := s.Provision(context.Background(),
		netcheck.ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Success {
		t.Error("应成功")
	}
	if !env.ModeStatusOf(netcheck.ModeTProxy).Available {
		t.Error("应返回准备之后重新探测的报告（TProxy 已可用）")
	}
	if detectCount < 2 {
		t.Errorf("准备前后都要探测，实际只探测了 %d 次", detectCount)
	}
}

// 开关状态与防火墙规则都不该被这个操作碰到——它只管系统依赖
func TestProvisionDoesNotTouchSwitchOrFirewall(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	s.SetProvisioner(&fakeProvisioner{})

	if _, _, err := s.Provision(context.Background(),
		netcheck.ProvisionOptions{InstallPackages: true, ApplySysctl: true}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}

	if base.writeCount() != 0 {
		t.Error("准备环境不该改写用户的 base 配置")
	}
	if st, _ := s.Status(); st.Enabled {
		t.Error("准备环境不该顺手把开关打开")
	}
	if applied, down, snaps := app.counts(); applied != 0 || down != 0 || snaps != 0 {
		t.Errorf("准备环境不该动防火墙，实际 applied=%d down=%d snaps=%d",
			applied, down, snaps)
	}
}

// 选项要如实透传：用户只勾了装包就不该顺带改 sysctl
func TestProvisionPassesOptionsThrough(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux"))
	p := &fakeProvisioner{}
	s.SetProvisioner(p)

	if _, _, err := s.Provision(context.Background(),
		netcheck.ProvisionOptions{InstallPackages: true}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !p.lastOpts.InstallPackages || p.lastOpts.ApplySysctl {
		t.Errorf("选项透传有误，实际 %+v", p.lastOpts)
	}
}

// 准备失败时环境报告仍要返回，让界面继续显示缺什么
func TestProvisionReturnsEnvOnFailure(t *testing.T) {
	s, _, _ := newSvc(t, reportWith("linux"))
	s.SetProvisioner(&fakeProvisioner{err: errors.New("需要以 root 运行")})

	_, env, err := s.Provision(context.Background(),
		netcheck.ProvisionOptions{InstallPackages: true})
	if err == nil {
		t.Fatal("应把 provisioner 的错误透传出来")
	}
	if env == nil {
		t.Error("失败时也应返回环境报告")
	}
}

package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"auroramihomo/backend/internal/engine"
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
	mu        sync.Mutex
	applied   int
	tornDown  int
	snapshots int
	// mihomoCleaned 记录 CleanupMihomoAutoRedirect 调用次数
	mihomoCleaned int
	applyErr      error
	lastParams    netcheck.TProxyParams
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

func (f *fakeApplier) Teardown(_ context.Context, _ ...[]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tornDown++
	return nil
}

func (f *fakeApplier) DumpRules(_ context.Context) (string, error) {
	return "table inet aurora_tproxy { }", nil
}

func (f *fakeApplier) CleanupMihomoAutoRedirect(_ context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mihomoCleaned++
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

func (f *fakeApplier) cleanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mihomoCleaned
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

// seedManagedTProxy 造出"面板此前经开关启用了 TProxy"这一初始状态。
//
// 光在 base.yaml 里写 tproxy-port 是不够的：那只表达"内核监听某端口"，
// 与"用户在配置中心自己填了个端口"无从区分。面板托管着规则这件事记在
// settings 里（见 settingTProxyManaged），构造初始状态时必须一并给上，
// 否则用例实际测的是"用户手填端口"那条完全不同的路径。
//
// 收成一个 helper 而不是在每个用例里各写两行，是为了让这个前提只有一处定义：
// 将来托管关系的表示方式若再变，用例不必逐个跟改。
func seedManagedTProxy(t *testing.T, store *fakeStore) {
	t.Helper()
	if err := store.SetSetting(settingTransparentMode, string(netcheck.ModeTProxy)); err != nil {
		t.Fatalf("构造初始状态失败（mode）: %v", err)
	}
	if err := store.SetSetting(settingTProxyManaged, "1"); err != nil {
		t.Fatalf("构造初始状态失败（managed）: %v", err)
	}
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

// TUN 模式由 mihomo 自管防火墙，面板不该去动 aurora_tproxy 规则
func TestEnableTUNDoesNotTouchFirewall(t *testing.T) {
	s, _, app := newSvc(t, reportWith("linux", netcheck.ModeTUN))

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}
	if applied, _, snaps := app.counts(); applied != 0 || snaps != 0 {
		t.Errorf("TUN 模式不该下发防火墙规则，实际 applied=%d snapshots=%d", applied, snaps)
	}
}

// 从 TUN 切到 TProxy 必须先清 mihomo auto-redirect 残留，再下发 aurora 规则。
// 否则 REDIRECT + TProxy 两套劫持叠在一起，旁路由手机会直接断网。
func TestSwitchFromTUNToTProxyCleansMihomoRedirect(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tun", 0, "mixed"); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}
	if app.cleanCount() != 0 {
		t.Fatalf("启用 TUN 不该清理 mihomo 残留，实际 clean=%d", app.cleanCount())
	}
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("切换到 TProxy 失败: %v", err)
	}
	if app.cleanCount() < 1 {
		t.Fatal("切到 TProxy 前应调用 CleanupMihomoAutoRedirect")
	}
	if applied, _, _ := app.counts(); applied < 1 {
		t.Fatal("切到 TProxy 后应下发 aurora 防火墙规则")
	}
	text := base.text()
	if !strings.Contains(text, "enable: false") {
		t.Errorf("切换后 base 应显式关闭 tun.enable，实际: %s", text)
	}
	if !strings.Contains(text, "tproxy-port:") {
		t.Errorf("切换后 base 应有 tproxy-port，实际: %s", text)
	}
}

// 关闭透明代理时，即使之前不是面板托管的 TProxy，也要清 mihomo auto-redirect 残留。
func TestDisableCleansMihomoAutoRedirect(t *testing.T) {
	s, _, app, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN), "")
	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("启用 TUN 失败: %v", err)
	}
	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if app.cleanCount() < 1 {
		t.Fatal("关闭时应清理 mihomo auto-redirect 残留")
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
	seedManagedTProxy(t, store)
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
	seedManagedTProxy(t, store)
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
	seedManagedTProxy(t, store)

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
	seedManagedTProxy(t, store)

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
	seedManagedTProxy(t, store)

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
	seedManagedTProxy(t, store)
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
	seedManagedTProxy(t, store)

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

// ---- TProxy 的"配置里有端口"与"规则已由面板下发"是两件事 ----
//
// 这一组是本次改动的核心回归防线。TProxy 生效需要两半：tproxy-port 让内核
// 监听（配置能表达），以及防火墙规则与策略路由把流量引过去（只有面板能放上去，
// 配置里没有痕迹）。早先两者被混为一谈——只要 tproxy-port > 0 就算已启用。

// 用户在「配置中心」手填 tproxy-port 不构成"面板已接管"。
//
// 手填端口 + 自己写防火墙规则是本项目明确支持的用法（见前端 redir-port /
// tproxy-port 的帮助文案）。把它误判成已启用会让界面谎报"流量已接管"，
// 而实际上没有任何流量被引到那个端口。
func TestHandWrittenTProxyPortIsNotEnabled(t *testing.T) {
	// 只有端口，没有托管标记——即用户自己在配置中心填的
	s, _, _, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy),
		"tproxy-port: 7893\n")

	st, _ := s.Status()
	if st.Enabled {
		t.Error("仅配置了端口、规则不是面板下发的，不该报告为已启用")
	}
	// 但这个事实要如实带给界面，否则用户无从判断自己是不是漏了一步
	if !st.PortConfiguredOnly {
		t.Error("应报告『端口已配置但未托管』，供界面提示用户")
	}
	// 端口本身仍要读出来展示，否则界面上是个空值
	if st.TProxyPort != 7893 {
		t.Errorf("应读出配置里的端口，实际 %d", st.TProxyPort)
	}
}

// 面板经开关启用后，同一份配置必须报告为已启用。
// 与上一个用例的唯一差别就是托管标记，用来钉住判据确实是它。
func TestPanelManagedTProxyIsEnabled(t *testing.T) {
	s, _, _, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	st, _ := s.Status()
	if !st.Enabled || st.Mode != string(netcheck.ModeTProxy) {
		t.Errorf("经开关启用后应报告已启用，实际 enabled=%v mode=%q", st.Enabled, st.Mode)
	}
	if st.PortConfiguredOnly {
		t.Error("已托管时不该再报告『仅配置了端口』")
	}
	if base.tproxyPort(t) != 7893 {
		t.Errorf("端口应写入 base.yaml，实际 %d", base.tproxyPort(t))
	}
}

// 关闭"面板的透明代理"不得删掉用户手填的 tproxy-port。
//
// 这是旧判据最有破坏性的后果：面板会把一份自己从未接管过的配置当作残留状态
// 抹掉，而用户可能正靠它和自己写的规则工作。
func TestDisableKeepsHandWrittenTProxyPort(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy),
		"tproxy-port: 7893\n")

	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if base.tproxyPort(t) != 7893 {
		t.Errorf("未托管的端口是用户自己填的，关闭时不该删，实际 %d", base.tproxyPort(t))
	}
	// 也不该去拆用户自己管的规则
	if _, down, _ := app.counts(); down != 0 {
		t.Errorf("未托管时不该拆除规则，实际拆除 %d 次", down)
	}
}

// 面板自己写进去的端口，关闭时必须删掉：规则刚拆，留着端口只会让内核继续
// 监听一个没有任何流量被引过来的端口。
func TestDisableRemovesManagedTProxyPort(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if base.tproxyPort(t) != 0 {
		t.Errorf("面板托管的端口关闭时应删掉，实际 %d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down == 0 {
		t.Error("已托管时关闭应拆除规则")
	}
}

// ReconcileState 不得碰用户手填的端口。
//
// 旧代码在这里最危险：它探到"规则不存在"就认为是宿主重启后的残留状态，
// 把端口删掉。而对手填端口的用户来说，规则本来就不该由面板管，
// 每次重启面板都会吃掉他的配置。
func TestReconcileStateKeepsHandWrittenTProxyPort(t *testing.T) {
	s, _, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy),
		"tproxy-port: 7893\n")
	app.rulesActive = false // 宿主上没有本面板的 nft 表

	s.ReconcileState(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("未托管的端口不该被 ReconcileState 删掉，实际 %d", base.tproxyPort(t))
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Errorf("未托管时不该拆除规则，实际拆除 %d 次", down)
	}
}

// 从引入托管标记之前的版本升级上来时，要能认领宿主上残留的规则。
//
// nft 表名 aurora_tproxy 为本项目独有，它存在就只能是面板下发的。
// 不认领的话，用户点关闭会因为标记为空而跳过 Teardown，规则永久留在宿主上。
func TestReconcileStateClaimsOrphanRulesOnUpgrade(t *testing.T) {
	// 老版本的状态：base.yaml 里有端口，规则还在宿主上跑着，但没有托管标记
	s, store, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy),
		"tproxy-port: 7893\n")
	app.rulesActive = true

	s.ReconcileState(context.Background())

	if v, _ := store.GetSetting(settingTProxyManaged); v != "1" {
		t.Errorf("应认领宿主上残留的本面板规则，实际标记 %q", v)
	}
	// 认领后规则与配置一致，不该被拆，端口也不该动
	if _, down, _ := app.counts(); down != 0 {
		t.Errorf("规则与配置一致时不该拆除，实际拆除 %d 次", down)
	}
	if base.tproxyPort(t) != 7893 {
		t.Errorf("认领后端口不该改动，实际 %d", base.tproxyPort(t))
	}
	// 认领之后状态才该显示为已启用
	if st, _ := s.Status(); !st.Enabled {
		t.Error("认领后应报告已启用")
	}
}

// 认领的前提是表确实存在：宿主上没有本面板的规则时，不能把用户手填的端口
// 算到面板头上（否则又回到"面板删用户配置"那条老路）。
func TestReconcileStateDoesNotClaimWhenNoRules(t *testing.T) {
	s, store, app, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy),
		"tproxy-port: 7893\n")
	// fakeApplier 默认 rulesActive=true，这里要的正是相反的前提
	app.rulesActive = false

	s.ReconcileState(context.Background())

	if v, _ := store.GetSetting(settingTProxyManaged); v == "1" {
		t.Error("宿主上没有本面板的规则时不该认领")
	}
}

// 用户在「配置中心」把 tproxy-port 删掉（或改开 TUN）时，那条路径不经过本服务，
// 面板下发的规则不会被拆。留下的规则把流量引向已无人监听的端口 = 彻底断网，
// 而界面显示的是另一回事，完全指不到原因。
func TestReconcileStateTearsDownOrphanRulesAfterExternalEdit(t *testing.T) {
	s, store, app, base := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 确认掉，否则会走 RecoverPending 那条路径
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	// 模拟用户在配置中心直接删掉端口
	if err := base.set("mode: rule\n"); err != nil {
		t.Fatalf("改写 base 失败: %v", err)
	}
	app.rulesActive = true // 规则还在宿主上

	s.ReconcileState(context.Background())

	if _, down, _ := app.counts(); down == 0 {
		t.Error("配置里已无 tproxy-port 时应拆除孤儿规则")
	}
	if v, _ := store.GetSetting(settingTProxyManaged); v == "1" {
		t.Error("拆除成功后应清掉托管标记")
	}
}

// 从 TProxy 切到 TUN 必须先拆掉 TProxy 的规则。
//
// 切换时 tproxy-port 会被从配置里删掉，内核随之停止监听；但 nftables 规则与
// 策略路由不在配置里，不会跟着消失。两者不同步的后果是所有流量被引向一个
// 已无人监听的端口——比没开透明代理更糟，那是彻底断网，且界面显示"TUN 已启用"。
func TestSwitchingFromTProxyToTUNTearsDownRules(t *testing.T) {
	s, store, app, _ := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy), "")
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用 TProxy 失败: %v", err)
	}

	if err := s.Update(context.Background(), true, "tun", 0, ""); err != nil {
		t.Fatalf("切到 TUN 失败: %v", err)
	}

	if _, down, _ := app.counts(); down == 0 {
		t.Error("切到 TUN 时应拆除 TProxy 规则，否则流量会被引向已无人监听的端口")
	}
	if v, _ := store.GetSetting(settingTProxyManaged); v == "1" {
		t.Error("拆除后应清掉托管标记")
	}
}

// 下发规则失败时不能留下托管标记：Apply 内部失败时自己已经 Teardown 过，
// 宿主上不该有本面板的规则，标记留着会让后续逻辑误以为还托管着。
func TestFailedApplyLeavesNoManagedMark(t *testing.T) {
	s, store, app, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTProxy), "")
	app.applyErr = errors.New("nft: 语法错误")

	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err == nil {
		t.Fatal("下发失败时应报错")
	}

	if v, _ := store.GetSetting(settingTProxyManaged); v == "1" {
		t.Error("下发失败后不该留下托管标记")
	}
	if st, _ := s.Status(); st.Enabled {
		t.Error("下发失败后不该报告为已启用")
	}
}

// 非 Linux 平台启动时不得 panic。
//
// 复现的是一个真实崩溃：servicecontext 里曾用 `var tpApplier *netcheck.Applier`
// 声明变量，非 Linux 上不赋值就直接传进构造函数。值为 nil 的具体类型指针一旦
// 装进接口，接口本身就不等于 nil（它带着类型信息），于是所有
// `s.applier == nil` 守卫全部失效，方法照常被调用并在解引用字段时崩掉——
// Windows 上启动即 panic 在 ReconcileState。
//
// 构造方那侧已改成声明为接口类型（真 nil），这里再从服务侧钉一遍：
// 即便将来又有调用方传进 typed nil，也不该把整个进程带崩。
func TestReconcileStateSurvivesTypedNilApplier(t *testing.T) {
	// 关键：具体类型的 nil 指针，而不是 nil 字面量
	var typedNil *fakeApplier
	store := newFakeStore()
	base := newFakeBase("tproxy-port: 7893\n")
	s := NewTransparentService(store, typedNil, logx.WithContext(context.Background()),
		nil, base.get, base.set)
	s.detect = func() *netcheck.Report { return reportWith("windows") }

	// 不 panic 即为通过；顺带确认它没去动用户的配置
	s.ReconcileState(context.Background())
	s.RecoverPending(context.Background())

	if base.tproxyPort(t) != 7893 {
		t.Errorf("不该改动配置，实际 tproxy-port=%d", base.tproxyPort(t))
	}
}

// 关闭路径同样不能被 typed nil 带崩：用户在任何平台都必须能关掉开关。
func TestDisableSurvivesTypedNilApplier(t *testing.T) {
	var typedNil *fakeApplier
	store := newFakeStore()
	base := newFakeBase("tproxy-port: 7893\n")
	_ = store.SetSetting(settingTProxyManaged, "1") // 托管标记在，会走到拆规则那步
	s := NewTransparentService(store, typedNil, logx.WithContext(context.Background()),
		nil, base.get, base.set)
	s.detect = func() *netcheck.Report { return reportWith("windows") }

	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭应始终可用: %v", err)
	}
}

// 切到 TProxy 必须把 TUN 显式写成 false，不能只删键。
//
// 「配置中心」的「开启虚拟网卡」开关读的就是 tun.enable。删键后它靠"读不到"
// 显示为关，那只是碰巧正确：删键等于"本地未声明"，订阅里带着
// tun: {enable: true} 时合并会把它补回来，最终配置里两种模式同时开着。
// 与 disable() 的做法保持一致（见 TestDisableWritesExplicitFalseForTUN）。
func TestSwitchingToTProxyWritesExplicitFalseForTUN(t *testing.T) {
	s, _, _, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy),
		"tun:\n  enable: true\n  stack: gvisor\n")

	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("切到 TProxy 失败: %v", err)
	}

	text := base.text()
	// 必须是显式 false，而不是键消失
	if !strings.Contains(text, "enable: false") {
		t.Errorf("切到 TProxy 后应显式写 tun.enable: false，实际:\n%s", text)
	}
	// 用户其余的 tun 配置不该被牵连
	if !strings.Contains(text, "stack: gvisor") {
		t.Errorf("不该动用户的 tun.stack，实际:\n%s", text)
	}
	if base.tproxyPort(t) != 7893 {
		t.Errorf("tproxy-port 应写入，实际 %d", base.tproxyPort(t))
	}
}

// 订阅里带着 tun.enable: true 时，切到 TProxy 后的本地声明要能压住它。
//
// 这是上一个用例真正要防的后果：两种模式同时出现在最终配置里。
func TestSwitchingToTProxySurvivesSubscriptionCarryingTUN(t *testing.T) {
	s, _, _, base := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy),
		"tun:\n  enable: true\n")

	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("切到 TProxy 失败: %v", err)
	}

	// 用合并引擎复现"订阅带 TUN"的场景（默认 Local First）
	eng := engine.NewMergeEngine()
	local, err := eng.LoadAndParse([]byte(base.text()))
	if err != nil {
		t.Fatalf("解析本地配置失败: %v", err)
	}
	remote, err := eng.LoadAndParse([]byte("tun:\n  enable: true\n"))
	if err != nil {
		t.Fatalf("解析远程配置失败: %v", err)
	}
	res := eng.MergeDetailed(local, remote, nil, nil)

	if res.Config.TUN.Enable {
		t.Error("订阅带着 tun.enable: true 时，本地的显式 false 应压住它，" +
			"否则最终配置里 TUN 与 TProxy 同时开着")
	}
	if res.Config.TProxyPort != 7893 {
		t.Errorf("合并后应保留 tproxy-port，实际 %d", res.Config.TProxyPort)
	}
}

// ---- 配置变更后规则要跟上（Resync） ----
//
// 规则里烧进了 tproxy-port / DNS 端口 / 内核 API 端口，而这些值用户随时能在
// 「配置中心」改。改完点「保存并应用」只会重新生成 config.yaml 并让内核热重载，
// 防火墙规则不会跟着变——内核听在新端口、规则还往旧端口投，界面却提示"已生效"。

// newResyncSvc 造一个"合并流程会回调 Resync"的服务，贴近真实链路。
func newResyncSvc(t *testing.T) (*TransparentService, *fakeStore, *fakeApplier, *fakeBase) {
	t.Helper()
	store := newFakeStore()
	app := newFakeApplier()
	base := newFakeBase("")
	var svc *TransparentService
	svc = NewTransparentService(store, app, logx.WithContext(context.Background()),
		func(ctx context.Context) error { svc.Resync(ctx); return nil },
		base.get, base.set)
	svc.detect = func() *netcheck.Report {
		return reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy)
	}
	return svc, store, app, base
}

// 启用时 reload 会触发合并、合并末尾又会调 Resync。
// 指纹在 Apply 之后立刻落库，所以 Resync 应命中比对、不重复下发。
// 重复下发不只是浪费：Apply 先删表再建，每次都有瞬时丢包。
func TestResyncDoesNotReapplyRightAfterEnable(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)

	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}

	if applied, _, _ := app.counts(); applied != 1 {
		t.Errorf("启用时应只下发一次规则，实际 %d 次", applied)
	}
}

// 配置没漂移时 Resync 是空操作。
// 绝大多数合并（改节点、换订阅、定时拉取）都不影响规则，
// 无条件重下发等于每次拉订阅都抖一次网络。
func TestResyncSkipsWhenConfigUnchanged(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	s.Resync(context.Background())
	s.Resync(context.Background())

	if applied, _, _ := app.counts(); applied != 1 {
		t.Errorf("配置未变时不该重下发，实际 %d 次", applied)
	}
}

// 改 DNS 端口后规则必须跟上，且用的是新端口。
// 不跟上的话 53 的查询会被重定向到一个没人监听的端口，DNS 全部失效。
func TestResyncReappliesWhenDNSPortChanges(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	// 用户在配置中心把 dns.listen 改成 5353
	s.SetDNSPortFn(func() int { return 5353 })
	s.Resync(context.Background())

	applied, _, _ := app.counts()
	if applied != 2 {
		t.Fatalf("DNS 端口变更后应重下发，实际下发 %d 次", applied)
	}
	if app.lastParams.DNSPort != 5353 {
		t.Errorf("重下发应使用新的 DNS 端口 5353，实际 %d", app.lastParams.DNSPort)
	}
}

// 改内核 API 端口后规则必须跟上。
//
// 这条最危险：规则若仍只放行旧端口，新端口的流量会被 TPROXY 捕获，
// 面板从此无法访问内核 API——正是"锁死自己"那类问题。
func TestResyncReappliesWhenControllerPortChanges(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	s.SetManagementPorts(8899, func() int { return 9090 })
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	// 用户把 external-controller 端口改成 19090
	s.SetManagementPorts(8899, func() int { return 19090 })
	s.Resync(context.Background())

	if applied, _, _ := app.counts(); applied != 2 {
		t.Fatalf("内核 API 端口变更后应重下发，实际 %d 次", applied)
	}
	var found bool
	for _, p := range app.lastParams.KeepPorts {
		if p == 19090 {
			found = true
		}
	}
	if !found {
		t.Errorf("重下发应放行新的内核 API 端口 19090，实际放行 %v", app.lastParams.KeepPorts)
	}
}

// Resync 不走 90 秒确认窗口。
//
// 它是用户保存配置的直接结果，而非独立的"启用"操作；定时拉取也会走合并流程，
// 那时没人在界面前，开窗口只会等来一次误回滚。
func TestResyncDoesNotOpenConfirmWindow(t *testing.T) {
	s, _, _, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	s.SetDNSPortFn(func() int { return 5353 })
	s.Resync(context.Background())

	if st, _ := s.Status(); st.PendingConfirm {
		t.Error("Resync 不该进入待确认状态，否则定时拉取会触发误回滚")
	}
}

// 待确认期间不重下发：用户正在验证网络，重下发会打断验证，
// 且回滚逻辑依赖当前那套规则。
func TestResyncSkipsWhilePendingConfirm(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	// 刻意不 Confirm，保持待确认

	s.SetDNSPortFn(func() int { return 5353 })
	s.Resync(context.Background())

	if applied, _, _ := app.counts(); applied != 1 {
		t.Errorf("待确认期间不该重下发，实际 %d 次", applied)
	}
}

// 未托管时 Resync 什么都不做：用户手填 tproxy-port、自己维护规则的情形，
// 那些规则不属于面板。

func TestResyncIgnoresUnmanagedTProxy(t *testing.T) {
	s, _, app, _ := newSvcWithBase(t,
		reportWith("linux", netcheck.ModeTProxy), "tproxy-port: 7893\n")

	s.Resync(context.Background())

	if applied, _, _ := app.counts(); applied != 0 {
		t.Errorf("未托管时不该下发规则，实际 %d 次", applied)
	}
}

// 重下发失败时保留旧指纹，让下次合并还能再试。
// 也不拆旧规则——旧规则至少还能工作（只是端口对不上），拆了就是彻底断网。
func TestResyncKeepsOldSignatureOnFailure(t *testing.T) {
	s, store, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	sigBefore, _ := store.GetSetting(settingTProxyAppliedSig)

	app.applyErr = errors.New("nft: 下发失败")
	s.SetDNSPortFn(func() int { return 5353 })
	s.Resync(context.Background())

	sigAfter, _ := store.GetSetting(settingTProxyAppliedSig)
	if sigAfter != sigBefore {
		t.Errorf("下发失败时不该更新指纹（否则下次合并不会再试），before=%q after=%q",
			sigBefore, sigAfter)
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Errorf("下发失败不该拆旧规则（那会彻底断网），实际拆除 %d 次", down)
	}
}

// 把 dns.listen 改成一个没人监听的端口（典型是 53）后，Resync 的重下发会被
// Applier 的端口探测拒绝。此时必须让界面看到"规则与配置不一致"。
//
// 这是这条链的最后一环：拒绝下发保住了 DNS（旧规则仍然有效），但配置与规则
// 从此不一致，用户需要知道自己刚保存的改动没有生效。
func TestResyncFailureSurfacesAsOutOfSync(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	if st, _ := s.Status(); st.RulesOutOfSync {
		t.Fatal("前置条件：刚启用时规则应与配置一致")
	}

	// 用户把 dns.listen 改成 53，而 53 上没有监听者 —— Applier 拒绝下发
	app.applyErr = errors.New("未检测到有程序在 UDP 端口 53 上监听")
	s.SetDNSPortFn(func() int { return 53 })
	s.Resync(context.Background())

	st, _ := s.Status()
	if !st.RulesOutOfSync {
		t.Error("重下发被拒绝后应报告规则与配置不一致，否则用户不知道改动没生效")
	}
	// 旧规则仍在，透明代理还是启用状态——不能因为一次重下发失败就报未启用
	if !st.Enabled {
		t.Error("重下发失败不该让状态变成未启用（旧规则仍在生效）")
	}
	if _, down, _ := app.counts(); down != 0 {
		t.Errorf("不该拆旧规则（那会让 DNS 彻底不可用），实际拆除 %d 次", down)
	}
}

// ---------- 自定义防火墙规则 ----------

// 非法规则（sudo 前缀、既非命令也非参数）必须在保存时拒绝，且不落库。
func TestSaveCustomRulesRejectsInvalid(t *testing.T) {
	s, store, _, _ := newResyncSvc(t)

	if err := s.SaveCustomRules(context.Background(), "sudo iptables -A INPUT -j ACCEPT"); err == nil {
		t.Fatal("sudo 前缀应被拒绝")
	}
	if err := s.SaveCustomRules(context.Background(), "随便写的行"); err == nil {
		t.Fatal("非命令行应被拒绝")
	}
	if _, err := store.GetSetting(settingCustomRules); err == nil {
		t.Fatal("校验失败时不应落库")
	}
}

// 保存自定义规则后指纹变化，Resync 必须重下发，且新规则进入下发参数。
func TestSaveCustomRulesTriggersResync(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	// 裸参数形式：保存后 customRules() 会补 iptables 前缀
	ruleText := "# 放行内网回程\n-t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN\n"
	if err := s.SaveCustomRules(context.Background(), ruleText); err != nil {
		t.Fatalf("保存规则失败: %v", err)
	}

	if applied, _, _ := app.counts(); applied != 2 {
		t.Fatalf("保存规则后应立即重下发，实际下发 %d 次", applied)
	}
	got := app.lastParams.CustomRules
	if len(got) != 1 || got[0] != "iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN" {
		t.Fatalf("重下发应携带规范化后的自定义规则，实际 %v", got)
	}
}

// 保存与当前内容一致的规则时不重下发（指纹相同，幂等空转）。
func TestSaveCustomRulesSameContentSkipsResync(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}

	ruleText := "iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN\n"
	if err := s.SaveCustomRules(context.Background(), ruleText); err != nil {
		t.Fatalf("保存规则失败: %v", err)
	}
	if applied, _, _ := app.counts(); applied != 2 {
		t.Fatalf("首次保存应重下发，实际 %d 次", applied)
	}

	// 相同内容再保存：指纹没变，不应重下发
	if err := s.SaveCustomRules(context.Background(), ruleText); err != nil {
		t.Fatalf("重复保存失败: %v", err)
	}
	if applied, _, _ := app.counts(); applied != 2 {
		t.Errorf("内容未变的保存不应重下发，实际 %d 次", applied)
	}
}

// Apply 成功后应落库已应用自定义规则快照；关闭托管后快照清空。
func TestAppliedCustomRulesSnapshotLifecycle(t *testing.T) {
	s, store, _, _ := newResyncSvc(t)
	ruleText := "iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN\n"
	if err := store.SetSetting(settingCustomRules, ruleText); err != nil {
		t.Fatalf("预置规则失败: %v", err)
	}
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	applied, err := store.GetSetting(settingCustomRulesApplied)
	if err != nil || !strings.Contains(applied, "iptables -t nat -A") {
		t.Fatalf("启用后应落库已应用快照, got %q err=%v", applied, err)
	}
	if err := s.Update(context.Background(), false, "off", 0, ""); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if v := store.kv[settingCustomRulesApplied]; v != "" {
		t.Fatalf("关闭托管后应清空已应用快照, got %q", v)
	}
}

// SaveCustomRules 在重应用失败时必须返回 error，不能谎报成功。
func TestSaveCustomRulesReturnsResyncError(t *testing.T) {
	s, _, app, _ := newResyncSvc(t)
	if err := s.Update(context.Background(), true, "tproxy", 7893, ""); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := s.Confirm(context.Background()); err != nil {
		t.Fatalf("确认失败: %v", err)
	}
	app.applyErr = errors.New("模拟下发失败")
	err := s.SaveCustomRules(context.Background(), "iptables -A INPUT -j ACCEPT\n")
	if err == nil {
		t.Fatal("重应用失败时应返回 error")
	}
	if !strings.Contains(err.Error(), "重新应用") && !strings.Contains(err.Error(), "重新下发") {
		t.Fatalf("错误应说明重应用失败: %v", err)
	}
}

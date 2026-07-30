package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// 透明代理相关的设置键。沿用 KV settings 表，无需迁移。
const (
	settingTransparentEnabled = "transparent.enabled"
	settingTransparentMode    = "transparent.mode"
	// settingTransparentPendingUntil 存"必须在此刻之前确认网络正常"的时间戳。
	// 持久化到数据库而非只放内存，是为了让面板自身崩溃/重启后仍能发现
	// 有一次未确认的启用并把它回滚掉——只靠进程内定时器，进程一死
	// 规则就会永久留在宿主上，而此时网络可能已经不通。
	settingTransparentPendingUntil = "transparent.pending_until"
	settingTransparentTProxyPort   = "transparent.tproxy_port"
	settingTransparentTUNStack     = "transparent.tun_stack"
)

// ConfirmWindow 启用后必须确认的时限。
//
// 90 秒的取舍：太短则用户来不及切到别的设备验证，太长则真出问题时
// 断网持续过久。TProxy 配错时用户往往需要换一台机器才能访问面板。
const ConfirmWindow = 90 * time.Second

// TransparentState 对外暴露的当前状态。
type TransparentState struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	// PendingConfirm 为 true 表示刚启用、等待用户确认网络正常。
	// 超时未确认会自动回滚。
	PendingConfirm bool `json:"pendingConfirm"`
	// SecondsLeft 距自动回滚的剩余秒数，仅 PendingConfirm 时有意义
	SecondsLeft int    `json:"secondsLeft"`
	TProxyPort  int    `json:"tproxyPort"`
	TUNStack    string `json:"tunStack"`
}

// transparentStore 抽象设置读写，便于测试。
type transparentStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
}

// transparentApplier 抽象规则下发，TProxy 模式用。
type transparentApplier interface {
	Apply(ctx context.Context, p netcheck.TProxyParams) error
	Teardown(ctx context.Context, enableIPv6 bool) error
	Snapshot(ctx context.Context) (string, error)
}

// TransparentService 管理透明代理开关。
//
// 两条不可妥协的约束：
//   - 环境不具备条件时开关强制关闭且拒绝写入。检测结论由 netcheck 给出，
//     这里只做判定与拒绝，不尝试"绕过"。
//   - 启用后必须显式确认，否则自动回滚。规则写错会让操作者失去对机器的
//     访问，而此时他已经无法通过面板关掉开关。
type TransparentService struct {
	store   transparentStore
	applier transparentApplier
	logger  logx.Logger
	// reloadFn 触发配置重新合并下发，使开关立即生效
	reloadFn func(ctx context.Context) error
	// detect 返回环境检测结论，测试可替换
	detect func() *netcheck.Report
	// now 便于测试控制时间
	now func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewTransparentService 构造。reloadFn 可为 nil（此时改开关只落库不即时生效）。
func NewTransparentService(store transparentStore, applier transparentApplier,
	logger logx.Logger, reloadFn func(ctx context.Context) error) *TransparentService {
	return &TransparentService{
		store:    store,
		applier:  applier,
		logger:   logger,
		reloadFn: reloadFn,
		detect:   netcheck.Detect,
		now:      time.Now,
	}
}

// Status 返回当前状态与环境检测结论。
func (s *TransparentService) Status() (*TransparentState, *netcheck.Report) {
	return s.state(), s.detect()
}

func (s *TransparentService) state() *TransparentState {
	st := &TransparentState{
		Mode:       s.getString(settingTransparentMode, string(netcheck.ModeOff)),
		Enabled:    s.getBool(settingTransparentEnabled),
		TProxyPort: s.getInt(settingTransparentTProxyPort, netcheck.DefaultTProxyPort),
		TUNStack:   s.getString(settingTransparentTUNStack, "mixed"),
	}
	if until := s.pendingUntil(); !until.IsZero() {
		left := int(until.Sub(s.now()).Seconds())
		if left > 0 {
			st.PendingConfirm = true
			st.SecondsLeft = left
		}
	}
	return st
}

// Update 修改开关与模式。
//
// 环境不支持时直接拒绝：把"不可用"做成写入失败而不是静默忽略，
// 用户才能知道为什么开不起来。
func (s *TransparentService) Update(ctx context.Context, enabled bool, mode string,
	tproxyPort int, tunStack string) error {
	if !netcheck.ValidMode(mode) {
		return fmt.Errorf("未知的透明代理模式: %s", mode)
	}

	// 关闭是任何环境下都允许的操作——包括环境已经变得不支持的情况，
	// 否则用户会陷入"开着但关不掉"。
	if !enabled || mode == string(netcheck.ModeOff) {
		return s.disable(ctx)
	}

	status := s.detect().ModeStatusOf(netcheck.Mode(mode))
	if !status.Available {
		// 直接把检测给出的原因回给用户：他需要知道缺什么才能补齐，
		// 泛泛的"不支持"没有可操作性
		msg := status.Reason
		if msg == "" {
			msg = "当前环境不支持该模式"
		}
		if status.InstallHint != "" {
			msg += "。可执行：" + status.InstallHint
		}
		return fmt.Errorf("无法启用 %s 模式: %s", mode, msg)
	}

	return s.enable(ctx, mode, tproxyPort, tunStack)
}

func (s *TransparentService) enable(ctx context.Context, mode string,
	tproxyPort int, tunStack string) error {
	if tproxyPort == 0 {
		tproxyPort = netcheck.DefaultTProxyPort
	}
	if tunStack == "" {
		tunStack = "mixed"
	}

	// TProxy 需要我们自己动宿主的防火墙与路由，先快照再下发。
	// TUN 由 mihomo 自己管规则，面板不碰防火墙。
	if mode == string(netcheck.ModeTProxy) {
		if s.applier == nil {
			return errors.New("当前平台不支持 TProxy 模式")
		}
		if _, err := s.applier.Snapshot(ctx); err != nil {
			// 快照失败不阻断：它是出问题后的手工兜底，
			// 而真正的自动保护是下面的确认窗口
			s.logger.Errorf("防火墙快照失败（继续启用）: %v", err)
		}
	}

	if err := s.persist(map[string]string{
		settingTransparentEnabled:    "true",
		settingTransparentMode:       mode,
		settingTransparentTProxyPort: strconv.Itoa(tproxyPort),
		settingTransparentTUNStack:   tunStack,
	}); err != nil {
		return err
	}

	// 先记确认截止时间再下发规则：反序的话，规则生效后若进程立刻崩溃，
	// 数据库里没有待确认记录，重启后就不会回滚。
	until := s.now().Add(ConfirmWindow)
	if err := s.store.SetSetting(settingTransparentPendingUntil,
		strconv.FormatInt(until.Unix(), 10)); err != nil {
		return fmt.Errorf("记录确认截止时间失败（未启用）: %w", err)
	}

	if err := s.applyMode(ctx, mode, tproxyPort); err != nil {
		// 下发失败立即回滚，不留待确认状态
		_ = s.clearPending()
		_ = s.persist(map[string]string{settingTransparentEnabled: "false"})
		return err
	}

	s.startRollbackTimer(until)
	s.logger.Infof("透明代理已启用（%s），需在 %.0f 秒内确认网络正常，否则自动回滚",
		mode, ConfirmWindow.Seconds())
	return nil
}

func (s *TransparentService) applyMode(ctx context.Context, mode string, tproxyPort int) error {
	if mode == string(netcheck.ModeTProxy) {
		p := netcheck.TProxyParams{
			TProxyPort: tproxyPort,
			DNSPort:    netcheck.DefaultDNSPort,
			KeepPorts:  s.keepPorts(),
		}
		if err := s.applier.Apply(ctx, p); err != nil {
			return err
		}
	}
	// 配置注入在合并流程里完成，这里触发一次重新下发使其生效
	return s.reload(ctx)
}

// keepPorts 必须直连的端口。
//
// 这是防"锁死自己"的核心：SSH 断了还能从别的设备连面板，面板断了还能
// SSH 进去关闭。两个都被劫持就只剩物理接触主机一条路。
func (s *TransparentService) keepPorts() []int {
	// 22: SSH；8899: 面板；9090: mihomo 外部控制器
	return []int{22, 8899, 9090}
}

// Confirm 确认网络正常，取消自动回滚。
func (s *TransparentService) Confirm(_ context.Context) error {
	if s.pendingUntil().IsZero() {
		return errors.New("当前没有待确认的启用操作")
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()

	if err := s.clearPending(); err != nil {
		return err
	}
	s.logger.Info("透明代理已确认，自动回滚已取消")
	return nil
}

func (s *TransparentService) disable(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()

	mode := s.getString(settingTransparentMode, string(netcheck.ModeOff))
	// 无论此前是什么模式都尝试拆一次规则：模式记录可能与实际状态不一致
	// （例如上次关闭时拆除失败），拆除本身是幂等的。
	if s.applier != nil && mode == string(netcheck.ModeTProxy) {
		if err := s.applier.Teardown(ctx, false); err != nil {
			s.logger.Errorf("拆除透明代理规则失败: %v", err)
		}
	}

	if err := s.persist(map[string]string{
		settingTransparentEnabled: "false",
		settingTransparentMode:    string(netcheck.ModeOff),
	}); err != nil {
		return err
	}
	_ = s.clearPending()

	if err := s.reload(ctx); err != nil {
		s.logger.Errorf("关闭透明代理后重新下发配置失败: %v", err)
		return err
	}
	s.logger.Info("透明代理已关闭")
	return nil
}

// RecoverPending 在进程启动时检查是否有未确认的启用。
//
// 面板崩溃或宿主重启后，数据库里可能留着一条待确认记录。此时规则要么还在
// （宿主没重启）要么已随重启失效，两种情况都应回到关闭状态：用户没能确认
// 网络正常，就不该让这套规则继续生效。
func (s *TransparentService) RecoverPending(ctx context.Context) {
	until := s.pendingUntil()
	if until.IsZero() {
		return
	}
	if s.now().Before(until) {
		// 窗口还没过，继续等剩余时间
		s.logger.Infof("发现未确认的透明代理启用，继续等待确认（剩余 %.0f 秒）",
			until.Sub(s.now()).Seconds())
		s.startRollbackTimer(until)
		return
	}
	s.logger.Info("发现已超时未确认的透明代理启用，执行回滚")
	if err := s.disable(ctx); err != nil {
		s.logger.Errorf("回滚透明代理失败: %v", err)
	}
}

// startRollbackTimer 启动内存中的回滚定时器。
// 它只是"及时性"保障；真正的兜底是持久化的 pending_until +
// RecoverPending，进程死掉也不会漏掉回滚。
func (s *TransparentService) startRollbackTimer(until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	d := until.Sub(s.now())
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
		s.logger.Error("透明代理未在时限内确认，自动回滚")
		if err := s.disable(context.Background()); err != nil {
			s.logger.Errorf("自动回滚失败: %v", err)
		}
	}()
}

func (s *TransparentService) reload(ctx context.Context) error {
	if s.reloadFn == nil {
		return nil
	}
	return s.reloadFn(ctx)
}

// InjectOptions 供配置合并流程取当前开关对应的注入参数。
func (s *TransparentService) InjectOptions() netcheck.InjectOptions {
	st := s.state()
	if !st.Enabled {
		return netcheck.InjectOptions{Mode: netcheck.ModeOff}
	}
	report := s.detect()
	return netcheck.InjectOptions{
		Mode:       netcheck.Mode(st.Mode),
		TProxyPort: st.TProxyPort,
		TUNStack:   st.TUNStack,
		// auto-redirect 让 mihomo 自管防火墙，仅 Linux 有效
		AutoRedirect: report.OS == "linux",
	}
}

// ---- 设置读写辅助 ----

func (s *TransparentService) persist(kv map[string]string) error {
	for k, v := range kv {
		if err := s.store.SetSetting(k, v); err != nil {
			return fmt.Errorf("保存 %s 失败: %w", k, err)
		}
	}
	return nil
}

func (s *TransparentService) clearPending() error {
	return s.store.SetSetting(settingTransparentPendingUntil, "")
}

func (s *TransparentService) pendingUntil() time.Time {
	v, err := s.store.GetSetting(settingTransparentPendingUntil)
	if err != nil || v == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func (s *TransparentService) getString(key, def string) string {
	v, err := s.store.GetSetting(key)
	if err != nil || v == "" {
		if errors.Is(err, gorm.ErrRecordNotFound) || v == "" {
			return def
		}
		return def
	}
	return v
}

func (s *TransparentService) getBool(key string) bool {
	v, err := s.store.GetSetting(key)
	if err != nil {
		return false
	}
	return v == "true" || v == "1"
}

func (s *TransparentService) getInt(key string, def int) int {
	v, err := s.store.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

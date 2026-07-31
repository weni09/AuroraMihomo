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
//
// 开关状态（是否启用、TProxy 端口、TUN 协议栈）已迁移到 base.yaml，
// 由 readBaseSwitchState 读取——「配置中心」编辑的就是同一份文件，
// 两处状态才不会各说各话。这里只剩两个纯运行时记录：
const (
	// settingTransparentMode 记住用户上次选择的模式。
	// base.yaml 里两种模式都没开时，界面需要它来决定下拉框默认停在哪一项。
	settingTransparentMode = "transparent.mode"
	// settingTransparentPendingUntil 存"必须在此刻之前确认网络正常"的时间戳。
	// 持久化到数据库而非只放内存，是为了让面板自身崩溃/重启后仍能发现
	// 有一次未确认的启用并把它回滚掉——只靠进程内定时器，进程一死
	// 规则就会永久留在宿主上，而此时网络可能已经不通。
	settingTransparentPendingUntil = "transparent.pending_until"
)

// ConfirmWindow 启用后必须确认的时限。
//
// 90 秒的取舍：太短则用户来不及切到别的设备验证，太长则真出问题时
// 断网持续过久。TProxy 配错时用户往往需要换一台机器才能访问面板。
const ConfirmWindow = 90 * time.Second

// defaultTUNStackName 与 netcheck 的默认协议栈保持一致：
// TCP 走内核栈（开销低）、UDP 走 gvisor（兼容性好）。
const defaultTUNStackName = "mixed"

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
	Teardown(ctx context.Context) error
	Snapshot(ctx context.Context) (string, error)
	// RulesActive 探测宿主上是否还存在本项目下发的防火墙规则。
	// 用于 ReconcileState 核实"已确认启用"的记录是否仍与实际状态一致。
	RulesActive(ctx context.Context) (bool, error)
}

// transparentProvisioner 补齐系统条件（装包、写 sysctl）。
//
// 与 transparentApplier 分开而非塞进同一个接口：前者动的是"系统层"
// （软件包、内核参数），后者动的是防火墙规则，生命周期与风险都不同——
// 防火墙改动要 90 秒确认窗口兜着，装包不需要也不适用。
// 合成一个接口会让两边的假实现都被迫实现对方的方法。
type transparentProvisioner interface {
	Provision(ctx context.Context, report *netcheck.Report,
		opts netcheck.ProvisionOptions) (*netcheck.ProvisionResult, error)
}

// TransparentService 管理透明代理开关。
//
// 两条不可妥协的约束：
//   - 环境不具备条件时开关强制关闭且拒绝写入。检测结论由 netcheck 给出，
//     这里只做判定与拒绝，不尝试"绕过"。
//   - 启用后必须显式确认，否则自动回滚。规则写错会让操作者失去对机器的
//     访问，而此时他已经无法通过面板关掉开关。
//
// 设计变更：开关状态现在存储在 base.yaml 中（tun.enable / tproxy-port），
// 而不是 settings 表。settings 表只保留 transparent.mode 用于记录用户上次选择。
type TransparentService struct {
	store   transparentStore
	applier transparentApplier
	// provisioner 补齐系统依赖；非 Linux 或未注入时为 nil，Provision 据此拒绝
	provisioner transparentProvisioner
	logger      logx.Logger
	// reloadFn 触发配置重新合并下发，使开关立即生效
	reloadFn func(ctx context.Context) error
	// detect 返回环境检测结论，测试可替换
	detect func() *netcheck.Report
	// now 便于测试控制时间
	now func() time.Time

	// panelPort 是面板自身的监听端口，来自 aurora-api.yaml，进程生命周期内不变。
	panelPort int
	// controllerPortFn 取 mihomo external-controller 的端口。
	//
	// 用函数而不是取值一次：external-controller 在 config.yaml 里，用户随时
	// 能在界面上改。启动时取一次会让"改完端口再启用 TProxy"放行到旧端口上，
	// 而那正是会锁死面板与内核 API 的情形。为 nil 时不放行该项。
	controllerPortFn func() int

	// 新增：用于读写 base.yaml
	getBaseFn    func() (string, error)
	updateBaseFn func(content string) error

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewTransparentService 构造。
//
// reloadFn 可为 nil（此时改开关只落库不即时生效）。
// getBaseFn 和 updateBaseFn 用于读写 base.yaml，不可为 nil。
func NewTransparentService(
	store transparentStore,
	applier transparentApplier,
	logger logx.Logger,
	reloadFn func(ctx context.Context) error,
	getBaseFn func() (string, error),
	updateBaseFn func(content string) error,
) *TransparentService {
	return &TransparentService{
		store:        store,
		applier:      applier,
		logger:       logger,
		reloadFn:     reloadFn,
		getBaseFn:    getBaseFn,
		updateBaseFn: updateBaseFn,
		detect:       netcheck.Detect,
		now:          time.Now,
	}
}

// SetProvisioner 注入环境准备器。
//
// 单独用 setter 而不加进构造函数参数：它只在 Linux 上存在，且是后加的可选
// 能力，塞进构造函数会让所有调用方（含既有测试）都得传一个 nil。
func (s *TransparentService) SetProvisioner(p transparentProvisioner) {
	s.provisioner = p
}

// SetManagementPorts 注入必须在防火墙规则里放行的管理端口。
//
// panelPort 取自 aurora-api.yaml，固定值即可；controllerPortFn 是函数，
// 因为内核 API 端口用户随时可改（理由见字段注释）。
// 任一项为 0/nil 时该端口不放行——宁可少放一个也不要往规则里写 0 端口，
// 那会生成一条永不匹配的规则，反而掩盖了配置问题。
// SSH 22 不走这里，它是 keepPorts() 里的固定兜底。
func (s *TransparentService) SetManagementPorts(panelPort int, controllerPortFn func() int) {
	s.panelPort = panelPort
	s.controllerPortFn = controllerPortFn
}

// Status 返回当前状态与环境检测结论。
func (s *TransparentService) Status() (*TransparentState, *netcheck.Report) {
	return s.state(), s.detect()
}

func (s *TransparentService) state() *TransparentState {
	// 开关的真实状态以 base.yaml 为准：它同时是「配置中心」的编辑对象，
	// 两个界面读同一份数据才不会各说各话。settings 表里的 mode 只作为
	// base.yaml 未表达意图时的兜底（例如两者都没开，用于记住用户上次选了哪个模式）。
	enabled := false
	mode := s.getString(settingTransparentMode, string(netcheck.ModeOff))
	tproxyPort := netcheck.DefaultTProxyPort
	tunStack := defaultTUNStackName

	if s.getBaseFn != nil {
		baseYAML, err := s.getBaseFn()
		if err != nil {
			// 读不到 base 配置时不能假装开关是关的：那会让界面显示"已关闭"
			// 而实际规则可能还在生效。留痕并回落到 settings 表的记录。
			s.logger.Errorf("读取 base 配置失败，透明代理状态可能不准确: %v", err)
		} else {
			tunOn, stack, port, perr := readBaseSwitchState(baseYAML)
			if perr != nil {
				s.logger.Errorf("解析 base 配置失败，透明代理状态可能不准确: %v", perr)
			} else if tunOn {
				enabled = true
				mode = string(netcheck.ModeTUN)
				if stack != "" {
					tunStack = stack
				}
			} else if port > 0 {
				enabled = true
				mode = string(netcheck.ModeTProxy)
				tproxyPort = port
			}
		}
	}

	st := &TransparentState{
		Mode:       mode,
		Enabled:    enabled,
		TProxyPort: tproxyPort,
		TUNStack:   tunStack,
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

// Provision 尝试补齐透明代理所需的系统条件（装依赖、写 sysctl）。
//
// 与开关本身正交：它不改任何开关状态，也不下发防火墙规则，只把环境从
// "不可用"推向"可用"。因此不需要 90 秒确认窗口那套保护——装个包和写一份
// sysctl drop-in 都不会让操作者失去对机器的访问。
//
// 返回的第二个值是执行后重新探测的环境报告：用户点完按钮要立刻看到"现在
// 到底可用了没有"，让他自己再刷一次很别扭，也容易出现"提示成功但模式仍显示
// 不可用"的割裂感。
func (s *TransparentService) Provision(ctx context.Context, opts netcheck.ProvisionOptions) (
	*netcheck.ProvisionResult, *netcheck.Report, error) {
	if s.provisioner == nil {
		// 非 Linux 平台不构造 provisioner。这里的措辞要能自解释，
		// 否则用户只会看到一个没有上下文的"不支持"
		return nil, s.detect(), errors.New("当前平台不支持自动准备环境（仅 Linux 可用）")
	}

	before := s.detect()
	res, err := s.provisioner.Provision(ctx, before, opts)
	if err != nil {
		// 前置校验不通过（非 root、非 Linux、没指定动作等）。
		// 环境报告照常返回，界面上那些"缺什么"的提示不该因此消失。
		return nil, before, err
	}

	// 装完包后 nft/iptables 才出现在 PATH 上，必须重新探测才能反映真实状态
	after := s.detect()
	s.logger.Infof("透明代理环境准备完成: %s", res.Message)
	return res, after, nil
}

func (s *TransparentService) enable(ctx context.Context, mode string,
	tproxyPort int, tunStack string) error {
	if tproxyPort == 0 {
		tproxyPort = netcheck.DefaultTProxyPort
	}
	if tunStack == "" {
		tunStack = defaultTUNStackName
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

	if s.getBaseFn == nil || s.updateBaseFn == nil {
		return errors.New("base 配置读写函数未初始化")
	}

	// 原文必须留着：下面任何一步失败都要把用户的 base.yaml 原样放回去。
	// 用改写后的内容再改一次做"回滚"是不行的——那样每次失败都会在用户文件上
	// 叠加一次格式化，注释和键顺序照样被磨掉。
	originalYAML, err := s.getBaseFn()
	if err != nil {
		return fmt.Errorf("读取基础配置失败: %w", err)
	}

	// 两种模式互斥，所以每次都把另一种显式清掉（传 nil 即删除该键）。
	// 定点改写而非整份结构体往返，理由见 base_yaml_patch.go 顶部注释。
	var patches map[string]interface{}
	switch mode {
	case string(netcheck.ModeTUN):
		patches = map[string]interface{}{
			"tun.enable":  true,
			"tun.stack":   tunStack,
			"tproxy-port": nil,
		}
	case string(netcheck.ModeTProxy):
		patches = map[string]interface{}{
			"tun.enable":  nil,
			"tproxy-port": tproxyPort,
		}
	default:
		return fmt.Errorf("未知的透明代理模式: %s", mode)
	}

	patchedYAML, err := patchBaseYAMLMulti(originalYAML, patches)
	if err != nil {
		return fmt.Errorf("改写基础配置失败: %w", err)
	}
	if err := s.updateBaseFn(patchedYAML); err != nil {
		return fmt.Errorf("保存基础配置失败: %w", err)
	}

	// restoreBase 把 base.yaml 还原成本次操作前的原文
	restoreBase := func() {
		if rerr := s.updateBaseFn(originalYAML); rerr != nil {
			// 还原失败要显眼：此时磁盘上的 base.yaml 带着一个没能生效的开关，
			// 用户在配置中心会看到与实际不符的状态
			s.logger.Errorf("回滚 base 配置失败，配置中心显示的开关状态可能与实际不符: %v", rerr)
		}
	}

	// 模式记录留给 state()：当 base.yaml 两种模式都没开时，
	// 它用来记住用户上次选的是哪个
	if err := s.store.SetSetting(settingTransparentMode, mode); err != nil {
		restoreBase()
		return fmt.Errorf("保存模式失败: %w", err)
	}

	// 只有 TProxy 需要确认窗口：它由面板下发 nftables 规则与策略路由，
	// 配错会让操作者失去 SSH 与面板两条通道。TUN 由 mihomo 自管规则并在
	// 退出时清理，进程一停就恢复，不存在"改错了却关不掉"的处境。
	if mode == string(netcheck.ModeTProxy) {
		// 先记确认截止时间再下发规则：反序的话，规则生效后若进程立刻崩溃，
		// 数据库里没有待确认记录，重启后就不会回滚。
		until := s.now().Add(ConfirmWindow)
		if err := s.store.SetSetting(settingTransparentPendingUntil,
			strconv.FormatInt(until.Unix(), 10)); err != nil {
			restoreBase()
			return fmt.Errorf("记录确认截止时间失败（未启用）: %w", err)
		}

		if err := s.applyMode(ctx, mode, tproxyPort); err != nil {
			// 下发失败立即回滚，不留待确认状态
			_ = s.clearPending()
			restoreBase()
			return err
		}

		s.startRollbackTimer(until)
		s.logger.Infof("透明代理已启用（%s），需在 %.0f 秒内确认网络正常，否则自动回滚",
			mode, ConfirmWindow.Seconds())
		return nil
	}

	if err := s.applyMode(ctx, mode, tproxyPort); err != nil {
		restoreBase()
		return err
	}
	s.logger.Infof("透明代理已启用（%s）", mode)
	return nil
}

func (s *TransparentService) applyMode(ctx context.Context, mode string, tproxyPort int) error {
	if mode == string(netcheck.ModeTProxy) {
		p := netcheck.TProxyParams{
			TProxyPort: tproxyPort,
			KeepPorts:  s.keepPorts(),
			// 只在宿主确实有 IPv6 出网能力时下发 v6 规则。没有能力却下发
			// 等于建了一条通往空路由的路；有能力却不下发则会让 v6 包被打标
			// 后无处可去（兜底规则的家族限定处理了这一侧，见 BuildNFTRules）。
			EnableIPv6: s.detect().HasIPv6Egress,
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
//
// 面板与内核 API 的端口都是可配置的（前者来自 aurora-api.yaml 的 Port，
// 后者来自 config.yaml 的 external-controller），所以必须取运行时的真实值。
// 早先这里硬编码 8899/9090，用户把面板改到别的端口后启用 TProxy 就会
// 失去面板访问——与本文件通篇在防的那类锁死是同一回事。
func (s *TransparentService) keepPorts() []int {
	// 22 是 SSH 的固定兜底：它不由本程序配置，但正是最后的救命通道。
	ports := []int{22}
	if s.panelPort > 0 {
		ports = append(ports, s.panelPort)
	}
	if s.controllerPortFn != nil {
		if p := s.controllerPortFn(); p > 0 {
			ports = append(ports, p)
		}
	}
	return dedupPorts(ports)
}

// dedupPorts 去重并保持原有顺序。
//
// 面板与内核 API 可能被配到同一端口（或与 22 撞上），重复的 return 规则
// 虽然无害但会让 `nft list table` 的输出变得可疑——排障时看到重复条目
// 第一反应会是"规则被下发了两次"，白白浪费时间。
func dedupPorts(in []int) []int {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, p := range in {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
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
		if err := s.applier.Teardown(ctx); err != nil {
			s.logger.Errorf("拆除透明代理规则失败: %v", err)
		}
	}

	// 关闭要写进 base.yaml，且必须显式写 tun.enable: false，不能只把键删掉。
	//
	// 删键等于"未设置"，一旦订阅里带着 tun: {enable: true}，合并时（remote 优先，
	// 或 local 未声明该键时的补齐）它就会被放回来——用户点了关闭却关不掉。
	// 显式 false 才是一个能参与合并、能压住远程的本地声明。
	// tproxy-port 则相反：它是端口值，0 不是合法端口，删键才是"不监听"。
	if s.getBaseFn != nil && s.updateBaseFn != nil {
		baseYAML, err := s.getBaseFn()
		if err != nil {
			// 读不到就没法安全改写：宁可只拆规则并如实报错，也不要凭空写一份
			// base.yaml 覆盖用户的真实配置
			s.logger.Errorf("读取 base 配置失败，未能写入关闭状态: %v", err)
			return fmt.Errorf("读取基础配置失败: %w", err)
		}
		patched, perr := patchBaseYAMLMulti(baseYAML, map[string]interface{}{
			"tun.enable":  false,
			"tproxy-port": nil,
		})
		if perr != nil {
			s.logger.Errorf("改写 base 配置失败，未能写入关闭状态: %v", perr)
			return fmt.Errorf("改写基础配置失败: %w", perr)
		}
		if err := s.updateBaseFn(patched); err != nil {
			s.logger.Errorf("保存 base 配置失败，未能写入关闭状态: %v", err)
			return fmt.Errorf("保存基础配置失败: %w", err)
		}
	}

	// 更新模式为 off
	if err := s.store.SetSetting(settingTransparentMode, string(netcheck.ModeOff)); err != nil {
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

// ReconcileState 核实"已确认启用"的 TProxy 记录是否仍与宿主上的真实
// 规则一致。
//
// 真机测试发现的问题（见 AuroraMihomo-Transparent-Proxy-Test-Report.md
// 第 6.3 节）：TProxy 的 nftables 规则与策略路由不会持久化到宿主重启，
// 但数据库里"已确认启用"的记录会。RecoverPending 只处理"启用后还没
// 确认"的场景，对这种"已经确认过，规则却已经因为宿主重启而消失"的
// 情况完全没有覆盖——面板会一直显示"已开启"，用户没有任何信号能察觉
// 网络实际上根本没被接管，直到自己手动碰一下开关。
//
// 只在 TProxy 模式下需要这一步：TUN 模式的路由与防火墙完全由 mihomo
// 自己在每次启动时按 config.yaml 里的 tun.enable 重建，不存在"面板记录
// 说开着、内核那边却没跟上"的缺口。
//
// 修正方式是回落到关闭，而不是尝试静默重新下发规则：重新下发等于绕过了
// 启用时本该有的 90 秒确认窗口，与"规则变更必须经用户确认"这条设计原则
// 相悖。用户如果仍然需要 TProxy，重新走一次正常的启用流程即可。
//
// 只能在启动流程里调用：它省掉了 disable() 末尾的配置重新下发，
// 依赖调用方紧随其后会做一次合并。
func (s *TransparentService) ReconcileState(ctx context.Context) {
	if s.applier == nil {
		return
	}
	// 待确认状态由 RecoverPending 处理，这里只关心"已确认"的情况——
	// pending_until 非空时说明还没走到确认这一步，不该在这里介入
	if !s.pendingUntil().IsZero() {
		return
	}
	// 启用状态必须走 state()：它以 base.yaml 为准。
	// 早先这里读的是 settings 表的 transparent.enabled，而开关状态迁移到
	// base.yaml 之后那个键已经没人写 "true" 了，判断会永远为假，
	// 使这整段宿主重启后的规则失效检测变成死代码。
	st := s.state()
	if !st.Enabled || st.Mode != string(netcheck.ModeTProxy) {
		return
	}

	active, err := s.applier.RulesActive(ctx)
	if err != nil {
		// 探测本身失败（如 nft 命令不可执行），无法判断真实状态，
		// 不能就此断言规则已失效——保守起见维持现状，只记录异常
		s.logger.Errorf("核实透明代理规则状态失败，暂不处理: %v", err)
		return
	}
	if active {
		return
	}

	s.logger.Error("检测到透明代理记录为已启用，但宿主上的防火墙规则已不存在" +
		"（通常是宿主重启导致），回落为关闭状态。如需继续使用请重新启用")

	// 刻意不走 disable()：它末尾会调 reloadFn 触发一次带远程刷新的合并，
	// 而本函数只在启动流程里被调用，紧随其后就有一次同样的合并。
	// 走 disable() 会让每次"带 TProxy 开关的宿主重启"都白拉一遍所有订阅，
	// 且那一次合并用的是没有超时约束的 rootCtx。
	// 这里只做"拆规则 + 落状态"，配置由后续那次合并按新状态重新生成。
	if err := s.applier.Teardown(ctx); err != nil {
		// 规则本来就不存在，Teardown 是幂等的；失败只记录不阻断落状态，
		// 否则状态会卡在"记录说开着、规则确实没有"的原样
		s.logger.Errorf("拆除透明代理规则失败（继续回落状态）: %v", err)
	}
	// 状态落在 base.yaml 上：随后那次合并读的是它，只改 settings 表
	// 不会让最终 config.yaml 里的 tproxy-port 消失。
	if s.getBaseFn != nil && s.updateBaseFn != nil {
		baseYAML, err := s.getBaseFn()
		if err != nil {
			s.logger.Errorf("回落透明代理状态失败（读取 base 配置）: %v", err)
			return
		}
		patched, perr := patchBaseYAMLMulti(baseYAML, map[string]interface{}{
			"tun.enable":  false,
			"tproxy-port": nil,
		})
		if perr != nil {
			s.logger.Errorf("回落透明代理状态失败（改写 base 配置）: %v", perr)
			return
		}
		if err := s.updateBaseFn(patched); err != nil {
			s.logger.Errorf("回落透明代理状态失败（保存 base 配置）: %v", err)
			return
		}
	}
	if err := s.store.SetSetting(settingTransparentMode, string(netcheck.ModeOff)); err != nil {
		s.logger.Errorf("回落透明代理状态失败: %v", err)
		return
	}
	s.logger.Info("透明代理已回落为关闭，配置将由随后的合并按新状态重新生成")
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

// ---- 设置读写辅助 ----

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

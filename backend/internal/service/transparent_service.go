package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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
// 两处状态才不会各说各话。这里只剩三个纯运行时记录：
const (
	// settingTransparentMode 记住用户上次选择的模式。
	// base.yaml 里两种模式都没开时，界面需要它来决定下拉框默认停在哪一项。
	settingTransparentMode = "transparent.mode"
	// settingTransparentPendingUntil 存"必须在此刻之前确认网络正常"的时间戳。
	// 持久化到数据库而非只放内存，是为了让面板自身崩溃/重启后仍能发现
	// 有一次未确认的启用并把它回滚掉——只靠进程内定时器，进程一死
	// 规则就会永久留在宿主上，而此时网络可能已经不通。
	settingTransparentPendingUntil = "transparent.pending_until"
	// settingTProxyManaged 记录"宿主上的 TProxy 防火墙规则与策略路由由本面板
	// 下发"。值为 "1" 表示已托管，其余（含空/键不存在）表示未托管。
	//
	// 这不是与 base.yaml 并列的第二个开关状态，而是 base.yaml 根本表达不了的
	// 另一个事实。TProxy 生效需要两半：
	//   - tproxy-port 让内核监听某个端口 —— 配置能表达；
	//   - nftables 规则与策略路由把流量引到该端口 —— 只有面板能放上去，
	//     在配置文件里没有任何痕迹。
	// 所以"配置里有端口"并不等于"流量已被接管"。
	//
	// TUN 不需要这个标记：它的两半都由 mihomo 按 tun.enable 自己完成
	// （建网卡、改路由、写并清理规则），配置就是机制，配置中心改它即真的开关。
	//
	// 缺了这个标记，tproxy-port > 0 会被直接当成"已启用"，后果有两层：
	// 界面对用户手填的端口谎报"已接管"，而流量并未被引走；更糟的是
	// ReconcileState 会探到"规则不存在"，进而把用户手填的端口当成宿主重启后的
	// 残留状态删掉——而"自己填端口、自己写防火墙规则"是本项目明确支持的用法
	// （见前端 redir-port / tproxy-port 的帮助文案）。
	settingTProxyManaged = "transparent.tproxy_managed"
	// settingTProxyAppliedSig 记录上次下发规则时用的参数指纹。
	//
	// 规则里烧进了若干运行时值（tproxy-port、DNS 端口、内核 API 端口、是否下发
	// v6 规则），而这些值用户随时能在「配置中心」改。改完只会重新生成 config.yaml
	// 并让内核热重载，防火墙规则不会跟着变——两者从此不一致，且没有任何信号。
	// 后果按严重度：改 external-controller 端口会失去面板对内核的访问（规则仍放行
	// 旧端口）；改 tproxy-port 或 DNS 端口会让对应流量投向无人监听的端口。
	//
	// 存指纹而不是每次合并都无条件重下发：重下发有瞬时丢包，定时拉取也会走合并
	// 流程，无条件重写等于每次拉订阅都抖一次网络。只有指纹变了才动。
	settingTProxyAppliedSig = "transparent.tproxy_applied_sig"
	// settingCustomRules 用户自定义防火墙规则（iptables 语法，多行文本）。
	//
	// 与内置 nft 规则是两个通道：内置由面板生成，自定义由用户书写，在
	// TProxy 规则生效后逐条追加执行、拆除时逆序 -D（仅 -A/-I 形式）。
	// 保存后若 TProxy 正在运行会立即重新应用（指纹变化触发 Resync）。
	settingCustomRules = "transparent.custom_rules"
	// settingCustomRulesApplied 上次成功应用到宿主的自定义规则（规范化后、
	// 每行一条）。与 settingCustomRules（用户编辑原文）分开存：
	// Apply 重入时必须先按本键拆除旧批，再按新目标追加，否则 iptables -A
	// 会叠规则、改 A→B 会留下 A 的孤儿。
	settingCustomRulesApplied = "transparent.custom_rules_applied"
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
	// PortConfiguredOnly 为 true 表示 base.yaml 里配了 tproxy-port，但接管流量
	// 所需的防火墙规则与策略路由不是本面板下发的（Enabled 因此为 false）。
	//
	// 单独报出来而不是并进 Enabled：这是一个用户需要知道、且只有他能判断对错的
	// 中间状态。可能是他自己在管规则（本项目支持的用法，此时一切正常），也可能
	// 是他以为填了端口就等于开启（此时内核在监听但没有任何流量被引过去）。
	// 面板无从区分这两者，只能如实呈现"端口配了、规则不是我下的"，把判断交给用户。
	PortConfiguredOnly bool `json:"portConfiguredOnly"`
	// RulesOutOfSync 为 true 表示宿主上的防火墙规则与当前配置不一致
	// （规则里烧进的端口已经不是配置里的值）。
	//
	// 正常情况下合并流程末尾的 Resync 会自动消除这种不一致，所以这个字段为 true
	// 只发生在重下发失败时（如 nft 报错）。必须报出来：此时内核听在新端口、
	// 规则还往旧端口投，流量会进黑洞，而用户刚看到的是"配置已生效"。
	// 静默不同步比报错糟得多——用户完全没有线索。
	RulesOutOfSync bool `json:"rulesOutOfSync"`
}

// transparentStore 抽象设置读写，便于测试。
type transparentStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
}

// TransparentApplier 抽象规则下发，TProxy 模式用。
//
// 导出是为了让调用方能把变量声明成这个接口类型。构造方必须区分 Linux 与
// 其它平台（非 Linux 不构造 Applier），若用 `var a *netcheck.Applier` 再传进来，
// 得到的是一个"带类型但值为 nil"的接口——它不等于 nil，本文件里所有
// `s.applier == nil` 的守卫会全部失效，方法照常被调用并在解引用字段时 panic。
// 声明为接口类型，未赋值时才是真正的 nil 接口。
type TransparentApplier interface {
	Apply(ctx context.Context, p netcheck.TProxyParams) error
	Teardown(ctx context.Context, customRules ...[]string) error
	Snapshot(ctx context.Context) (string, error)
	// RulesActive 探测宿主上是否还存在本项目下发的防火墙规则。
	// 用于 ReconcileState 核实"已确认启用"的记录是否仍与实际状态一致。
	RulesActive(ctx context.Context) (bool, error)
	// DumpRules 输出本面板 nft 表的当前规则集，供界面展示内置规则。
	// TProxy 未开启时返回空字符串。
	DumpRules(ctx context.Context) (string, error)
}

// transparentProvisioner 补齐系统条件（装包、写 sysctl）。
//
// 与 TransparentApplier 分开而非塞进同一个接口：前者动的是"系统层"
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
	applier TransparentApplier
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

	// dnsPortFn 取 mihomo 实际的 DNS 监听端口（config.yaml 的 dns.listen）。
	//
	// 防火墙规则要把 53 端口的查询重定向到这个端口，而不是 tproxy-port——
	// TPROXY 保留原始目的端口，送错门时 mihomo 不会按 DNS 应答，
	// 表现就是"域名解析没被接管"。
	// 同样用函数：dns.listen 用户随时能在配置中心改，取值一次会让规则
	// 指向旧端口。为 nil 或返回 0 时回落到 netcheck 的默认端口。
	dnsPortFn func() int

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
	applier TransparentApplier,
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

// hasApplier 判断规则下发能力是否真的可用。
//
// 不能直接写 `s.applier == nil`：调用方若把一个值为 nil 的具体类型指针
// （如 `var a *netcheck.Applier` 未赋值）传进来，装进接口后接口不等于 nil，
// 那个比较会通过，紧接着调用方法就在解引用字段时 panic。这不是假设——
// 非 Linux 平台启动时曾因此崩在 ReconcileState。
//
// 构造方那侧已改为声明接口类型（见 TransparentApplier 的注释），
// 这里再用反射兜一层：本服务的每个使用点都靠这个判断决定"能不能碰防火墙"，
// 判错的代价是进程崩溃或规则残留，值得多这一次检查。
func (s *TransparentService) hasApplier() bool {
	if s.applier == nil {
		return false
	}
	// 只有指针/接口等可为 nil 的种类需要看值；其它种类（如结构体值实现接口）
	// 一律视为可用
	v := reflect.ValueOf(s.applier)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !v.IsNil()
	default:
		return true
	}
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

// SetDNSPortFn 注入 mihomo DNS 监听端口的来源。
//
// 单独一个 setter 而不塞进 SetManagementPorts：那个方法的语义是"必须放行的
// 管理端口"，而 DNS 端口是重定向目标，性质完全不同，混在一起会让调用方
// 误以为 DNS 端口也会被放行直连（那恰好是我们不想要的）。
func (s *TransparentService) SetDNSPortFn(fn func() int) {
	s.dnsPortFn = fn
}

// dnsPort 返回下发规则时使用的 DNS 重定向目标端口。
//
// 取不到时回落到 netcheck 的默认值：规则里必须有一个确定的端口，
// 写 0 会让整份规则被 nft 拒绝、一条都不生效（连带整个 TProxy 失效），
// 那比"重定向到默认端口可能不对"糟得多。
func (s *TransparentService) dnsPort() int {
	if s.dnsPortFn != nil {
		if p := s.dnsPortFn(); p > 0 {
			return p
		}
	}
	return netcheck.DefaultDNSPort
}

// Status 返回当前状态与环境检测结论。
func (s *TransparentService) Status() (*TransparentState, *netcheck.Report) {
	return s.state(), s.detect()
}

// state 汇总当前开关状态。
//
// 两种模式的判据不同，因为两者的"启用"由不同的东西构成：
//
//   - TUN：base.yaml 里 tun.enable 为真即已启用。mihomo 按这个字段自己建网卡、
//     改路由、写并清理防火墙规则，配置就是机制。
//   - TProxy：必须 tproxy-port > 0 **且** 规则由本面板下发（见
//     settingTProxyManaged）。配置只表达"内核监听哪个端口"，不表达"流量是否
//     被引到这个端口"，后者只有面板知道。
//
// 早先两者共用"配置里有值就算开"的判据，于是用户在「配置中心」手填 tproxy-port
// 会被误判成"面板已接管"——界面谎报状态，且 ReconcileState 会把那个端口当成
// 残留状态删掉。
//
// 两种模式在 base.yaml 里同时出现时以 TUN 为先，与 netcheck.Inject 的注入顺序
// 一致：判定与生成必须用同一套优先级，否则界面说的模式和内核跑的模式会不一样。
func (s *TransparentService) state() *TransparentState {
	// 开关的真实状态以 base.yaml 为准：它同时是「配置中心」的编辑对象，
	// 两个界面读同一份数据才不会各说各话。settings 表里的 mode 只作为
	// base.yaml 未表达意图时的兜底（例如两者都没开，用于记住用户上次选了哪个模式）。
	enabled := false
	portConfiguredOnly := false
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
				tproxyPort = port
				if s.tproxyManaged() {
					enabled = true
					mode = string(netcheck.ModeTProxy)
				} else {
					// 端口配了但规则不是面板下的。不报"已启用"，也不去改用户的
					// 配置，只把这个事实带给界面由用户判断（见字段注释）。
					portConfiguredOnly = true
				}
			}
		}
	}

	// 规则是否已与配置脱节：拿"按当前配置该下发什么"与"上次实际下发了什么"
	// 比对得出，而不是另存一个布尔。派生值不会与事实漂移——
	// 独立的标志位一旦漏更新就会长期骗人。
	outOfSync := false
	if enabled && mode == string(netcheck.ModeTProxy) {
		applied := s.getString(settingTProxyAppliedSig, "")
		// 没有记录时不报不一致：老版本升级上来、或 ReconcileState 刚认领了
		// 残留规则，都属于"不知道"而非"已知不一致"。谎报会让用户去查一个
		// 不存在的问题。
		if applied != "" && applied != paramsSignature(s.tproxyParams(tproxyPort)) {
			outOfSync = true
		}
	}

	st := &TransparentState{
		Mode:               mode,
		Enabled:            enabled,
		TProxyPort:         tproxyPort,
		TUNStack:           tunStack,
		PortConfiguredOnly: portConfiguredOnly,
		RulesOutOfSync:     outOfSync,
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
		prev := s.state()
		s.logger.Infof("透明代理关闭请求: 当前 enabled=%v mode=%s tproxyPort=%d managed=%v",
			prev.Enabled, prev.Mode, prev.TProxyPort, s.tproxyManaged())
		if err := s.disable(ctx); err != nil {
			s.logger.Errorf("透明代理关闭失败: %v", err)
			return err
		}
		return nil
	}

	s.logger.Infof("透明代理开启/切换请求: mode=%s tproxyPort=%d tunStack=%q managed=%v",
		mode, tproxyPort, tunStack, s.tproxyManaged())

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
		err := fmt.Errorf("无法启用 %s 模式: %s", mode, msg)
		s.logger.Errorf("透明代理开启被拒绝: %v", err)
		return err
	}

	if err := s.enable(ctx, mode, tproxyPort, tunStack); err != nil {
		s.logger.Errorf("透明代理开启失败 mode=%s tproxyPort=%d: %v", mode, tproxyPort, err)
		return err
	}
	return nil
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
		if !s.hasApplier() {
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

	// 两种模式互斥，所以每次都把另一种关掉。
	// 定点改写而非整份结构体往返，理由见 base_yaml_patch.go 顶部注释。
	//
	// 两个字段的"关掉"写法不同，且必须不同（与 disable() 保持一致）：
	//   - tun.enable 写显式 false，不能删键。「配置中心」的 TUN 开关读的就是
	//     这个键，删键后它靠"读不到"显示为关，那只是碰巧正确；而删键等于
	//     "本地未声明"，一旦订阅里带着 tun: {enable: true}，合并时它就会被
	//     补回来，最终配置里两种模式同时开着。显式 false 才是一个能参与合并、
	//     意图明确的本地声明。
	//   - tproxy-port 删键（传 nil）。它是端口值，0 不是合法端口，
	//     "不监听"的唯一表达就是这个键不存在。
	var patches map[string]interface{}
	switch mode {
	case string(netcheck.ModeTUN):
		// auto-redirect 默认写 false 并落进 base：合并注入对已声明键只补不覆盖，
		// 避免 Linux 上无条件注入 true 时，在部分 Alpine/virt 环境把整个 TUN
		// 静默打挂（runtime enable=false、看不见 Meta 网卡）。
		// 需要网关级劫持的用户可在配置中心改回 true。
		patches = map[string]interface{}{
			"tun.enable":        true,
			"tun.stack":         tunStack,
			"tun.auto-redirect": false,
			"tproxy-port":       nil,
		}
	case string(netcheck.ModeTProxy):
		patches = map[string]interface{}{
			"tun.enable":  false,
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

		// 托管标记必须在 Apply 之前落库，与上面记确认截止时间同理：
		// Apply 成功后进程若立刻崩溃，规则已经在宿主上，而数据库里没有标记，
		// 重启后就没人认领这套规则——既不会被 ReconcileState 校准，
		// 用户点关闭时也会因为标记为空而跳过 Teardown，规则永久留在宿主上。
		// 反过来标记先落、Apply 失败，下面会把标记清掉，只是多写一次库。
		if err := s.setTProxyManaged(true); err != nil {
			_ = s.clearPending()
			restoreBase()
			return fmt.Errorf("记录规则托管状态失败（未启用）: %w", err)
		}

		if err := s.applyMode(ctx, mode, tproxyPort); err != nil {
			// 下发失败立即回滚，不留待确认状态，也不留托管标记：
			// Apply 内部失败时自己已经 Teardown 过（见 netcheck.Applier.Apply），
			// 宿主上不该有本面板的规则，标记必须跟着清掉
			_ = s.clearPending()
			if merr := s.setTProxyManaged(false); merr != nil {
				s.logger.Errorf("清理规则托管标记失败: %v", merr)
			}
			restoreBase()
			return err
		}

		s.startRollbackTimer(until)
		s.logger.Infof("透明代理已启用（%s），需在 %.0f 秒内确认网络正常，否则自动回滚",
			mode, ConfirmWindow.Seconds())
		return nil
	}

	// 切到 TUN 时必须先把 TProxy 的规则拆掉。
	//
	// 上面的 patches 已经把 tproxy-port 从配置里删了，内核随之停止监听该端口；
	// 但 nftables 规则与策略路由不在配置里，不会因此消失。两者不同步的后果是
	// 所有流量被规则引向一个已经没人监听的端口——比单纯没开透明代理更糟，
	// 那是彻底断网，且用户看到的界面显示"TUN 已启用"，完全指不到原因。
	//
	// 只在确实托管过时拆：没托管说明规则（如果有）是用户自己管的，不该替他动。
	if s.hasApplier() && s.tproxyManaged() {
		if err := s.applier.Teardown(ctx, s.customRulesForTeardown()); err != nil {
			// 拆除失败不阻断切换：TUN 那边的配置已经写好，继续下发让它先生效。
			// 但标记要留着，这样用户下次关闭时还会再拆一次。
			s.logger.Errorf("切换到 TUN 前拆除 TProxy 规则失败，宿主上可能残留规则: %v", err)
		} else if merr := s.setTProxyManaged(false); merr != nil {
			s.logger.Errorf("清理规则托管标记失败: %v", merr)
		}
	}

	if err := s.applyMode(ctx, mode, tproxyPort); err != nil {
		restoreBase()
		return err
	}
	s.logger.Infof("透明代理已启用（%s）", mode)
	return nil
}

// tproxyParams 组装下发规则所需的运行时参数。
//
// 抽成一处是必需的：enable() 与 Resync() 必须用同一套取值逻辑，
// 否则"启用时算出的规则"与"重同步时算出的规则"会有分歧，
// 而那种分歧表现为规则莫名其妙地变来变去，极难排查。
//
// 每个值都现取而不是缓存：它们全都来自用户随时可改的配置。
func (s *TransparentService) tproxyParams(tproxyPort int) netcheck.TProxyParams {
	return netcheck.TProxyParams{
		TProxyPort: tproxyPort,
		// DNS 必须重定向到 mihomo 的 DNS 端口，不是 tproxy-port，
		// 理由见 TProxyParams.DNSPort 的注释
		DNSPort:   s.dnsPort(),
		KeepPorts: s.keepPorts(),
		// 只在宿主确实有 IPv6 出网能力时下发 v6 规则。没有能力却下发
		// 等于建了一条通往空路由的路；有能力却不下发则会让 v6 包被打标
		// 后无处可去（兜底规则的家族限定处理了这一侧，见 BuildNFTRules）。
		EnableIPv6: s.detect().HasIPv6Egress,
		// 自定义规则读库现取：SaveCustomRules 保存后立即重应用依赖指纹
		// 变化，这里必须与应用时的取值来自同一份数据。
		CustomRules: s.customRules(),
		// 上一批已成功应用的规则：Apply 在追加本批前先拆掉它们，
		// 防止 iptables -A 叠规则、改内容时留下孤儿。
		PreviousCustomRules: s.appliedCustomRules(),
	}
}

// customRules 读取并规范化用户自定义防火墙规则。
//
// 读取或规范化失败时返回 nil：规则有问题不该让 TProxy 整体起不来
// （保存时已校验过格式，这里只是兜底防御）。
func (s *TransparentService) customRules() []string {
	raw := s.getString(settingCustomRules, "")
	rules, err := netcheck.NormalizeCustomRules(raw)
	if err != nil {
		s.logger.Errorf("自定义防火墙规则解析失败（本批规则不生效）: %v", err)
		return nil
	}
	return rules
}

// appliedCustomRules 读取"上次成功应用到宿主"的自定义规则快照。
//
// 存的是规范化后的完整命令（每行一条），与 NormalizeCustomRules 输出同形，
// 可直接交给 removeCustomRules / Teardown，无需再规范化。
func (s *TransparentService) appliedCustomRules() []string {
	raw := s.getString(settingCustomRulesApplied, "")
	if raw == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// customRulesForTeardown 合并"已应用快照"与"当前目标"，用于关闭/回滚/失败清理。
// 只拆当前目标会漏掉改 A→B 后仍在链上的 A；只拆快照会漏掉本批已追加的几条。
func (s *TransparentService) customRulesForTeardown() []string {
	return netcheck.MergeCustomRuleLists(s.appliedCustomRules(), s.customRules())
}

// persistAppliedCustomRules 在 Apply 成功后落库本批自定义规则快照。
// 失败只记日志：规则已经在宿主上，缺快照最坏是下次多拆几次（幂等）。
func (s *TransparentService) persistAppliedCustomRules(rules []string) {
	value := strings.Join(rules, "\n")
	if err := s.store.SetSetting(settingCustomRulesApplied, value); err != nil {
		s.logger.Errorf("记录已应用自定义规则快照失败: %v", err)
	}
}

// clearAppliedCustomRules 清除已应用快照（Teardown 成功后调用）。
func (s *TransparentService) clearAppliedCustomRules() {
	if err := s.store.SetSetting(settingCustomRulesApplied, ""); err != nil {
		s.logger.Errorf("清除已应用自定义规则快照失败: %v", err)
	}
}

// paramsSignature 把影响规则内容的运行时值压成一个可比较的指纹。
//
// 只包含真正会改变规则文本的字段。LAN 网段不在其中：它由 Normalize 补默认值，
// 目前没有用户可改的入口，纳入只会让指纹无谓地变长。
// 自定义规则必须纳入：它直接追加进规则集，内容变了而指纹不变，
// Resync 就不会重下发，用户保存的规则等于没生效。
func paramsSignature(p netcheck.TProxyParams) string {
	return fmt.Sprintf("tp=%d;dns=%d;keep=%v;v6=%t;rules=%s",
		p.TProxyPort, p.DNSPort, p.KeepPorts, p.EnableIPv6, rulesHash(p.CustomRules))
}

// rulesHash 把自定义规则列表压成指纹片段（sha256 前 4 字节的 hex）。
// 空列表返回空串，让"没有自定义规则"在指纹里一目了然。
func rulesHash(rules []string) string {
	if len(rules) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(rules, "\n")))
	return hex.EncodeToString(sum[:4])
}

func (s *TransparentService) applyMode(ctx context.Context, mode string, tproxyPort int) error {
	if mode == string(netcheck.ModeTProxy) {
		p := s.tproxyParams(tproxyPort)
		if err := s.applier.Apply(ctx, p); err != nil {
			return err
		}
		// 记下这次用的参数，供 Resync 判断配置是否已经漂移。
		// 失败只记录不阻断：规则已经生效了，指纹只是用于后续比对，
		// 缺了它最坏的结果是下次合并多做一次幂等的重下发。
		if err := s.store.SetSetting(settingTProxyAppliedSig, paramsSignature(p)); err != nil {
			s.logger.Errorf("记录规则参数指纹失败（不影响本次生效）: %v", err)
		}
		// 同步"已应用自定义规则"快照：下次重应用/改内容时据此先拆旧批。
		s.persistAppliedCustomRules(p.CustomRules)
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
	until := s.pendingUntil()
	if until.IsZero() {
		s.logger.Error("透明代理确认请求被拒绝：当前没有待确认的启用操作")
		return errors.New("当前没有待确认的启用操作")
	}
	s.logger.Infof("透明代理确认请求：取消自动回滚（原截止 %s）", until.Format(time.RFC3339))
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()

	if err := s.clearPending(); err != nil {
		s.logger.Errorf("透明代理确认失败（清除 pending 失败）: %v", err)
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

	// 拆规则的门禁是托管标记，而不是 settings 表里的 mode。
	//
	// mode 只在用户经「透明代理」页操作时才写，它回答的是"用户上次选了什么"，
	// 不是"宿主上现在有没有本面板的规则"。用它做门禁会漏拆：任何让 mode 与
	// 实际托管状态不一致的路径（关闭时拆除失败、mode 落库失败、旧版本升级）
	// 都会让规则永久留在宿主上，而用户已经点过关闭、界面也显示已关闭。
	// 托管标记是专门记录这件事的，且 Teardown 本身幂等，多拆一次无害。
	tproxyManaged := s.tproxyManaged()
	s.logger.Infof("透明代理关闭执行: managed=%v hasApplier=%v customRules=%d",
		tproxyManaged, s.hasApplier(), len(s.customRulesForTeardown()))
	if s.hasApplier() && tproxyManaged {
		if err := s.applier.Teardown(ctx, s.customRulesForTeardown()); err != nil {
			// 拆除失败时刻意不清标记：规则可能还在宿主上，标记留着才能让
			// 下一次关闭（或启动时的 ReconcileState）再尝试拆一次
			s.logger.Errorf("拆除透明代理规则失败: %v", err)
		} else if merr := s.setTProxyManaged(false); merr != nil {
			s.logger.Errorf("清理规则托管标记失败: %v", merr)
		} else {
			s.logger.Info("透明代理防火墙规则与托管标记已清除")
		}
	} else if !tproxyManaged {
		s.logger.Info("透明代理关闭：未托管，跳过防火墙拆除（仅落关闭状态）")
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
		patches := map[string]interface{}{
			"tun.enable": false,
		}
		// 只删本面板托管过的 tproxy-port。
		//
		// 未托管说明这个端口是用户自己填的（配置中心手填 + 自己写防火墙规则是
		// 本项目支持的用法），关闭"面板的透明代理"不该顺手删掉一份面板从未
		// 接管过的配置——那是在用户没要求的情况下改他的文件。
		// 已托管则必须删：端口是本面板写进去的，规则也刚拆掉，留着端口只会让
		// 内核继续监听一个没有任何流量被引过来的端口。
		if tproxyManaged {
			patches["tproxy-port"] = nil
		}
		patched, perr := patchBaseYAMLMulti(baseYAML, patches)
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

// ReconcileState 核实"面板认为自己托管着 TProxy"与宿主上的真实规则是否一致。
//
// 两种方向的不一致都要处理，它们的成因不同：
//
//  1. 标记说托管、宿主上规则却没了。真机测试发现的问题（见
//     AuroraMihomo-Transparent-Proxy-Test-Report.md 第 6.3 节）：TProxy 的
//     nftables 规则与策略路由不持久化到宿主重启，但数据库记录会。
//     RecoverPending 只覆盖"启用后还没确认"，对"已经确认过、规则却随宿主重启
//     消失"完全没有覆盖——面板会一直显示"已开启"，用户没有任何信号能察觉
//     网络实际根本没被接管，直到自己手动碰一下开关。
//  2. 标记说托管、但配置里已经不是"面板托管的 TProxy"了。用户可以在
//     「配置中心」直接删掉 tproxy-port 或改开 tun.enable，那条路径不经过本
//     服务，规则不会被拆。留下的是一套把流量引向已无人监听端口的规则，
//     即彻底断网，而界面显示的是 TUN 已启用，完全指不到原因。
//
// 只有 TProxy 需要这一步：TUN 的路由与防火墙由 mihomo 每次启动时按
// config.yaml 里的 tun.enable 自己重建，不存在"面板记录说开着、内核没跟上"。
//
// 情况 1 的修正是回落到关闭，而不是静默重新下发规则：重新下发等于绕过了启用时
// 本该有的 90 秒确认窗口，与"规则变更必须经用户确认"相悖。用户仍需要 TProxy 时
// 重新走一次正常启用流程即可。
//
// 只能在启动流程里调用：它省掉了 disable() 末尾的配置重新下发，
// 依赖调用方紧随其后会做一次合并。
func (s *TransparentService) ReconcileState(ctx context.Context) {
	if !s.hasApplier() {
		return
	}
	// 待确认状态由 RecoverPending 处理，这里只关心"已确认"的情况——
	// pending_until 非空时说明还没走到确认这一步，不该在这里介入
	if !s.pendingUntil().IsZero() {
		return
	}

	// 标记缺失时先尝试认领宿主上已有的规则。
	//
	// 这一步靠的是 nft 表名 aurora_tproxy 为本项目独有（见 netcheck.NFTTableName）：
	// 那张表存在，就只能是本面板下发的，不必猜。因此托管标记在启动时是可推导的，
	// 不是一份只能相信的记录。
	//
	// 覆盖两种标记缺失的情形：
	//   - 从引入该标记之前的版本升级上来，规则还在宿主上跑着（宿主未重启）。
	//     不认领的话，用户点关闭会因为标记为空而跳过 Teardown，规则永久残留。
	//   - enable() 里标记落库成功但随后进程崩溃之类的窗口。
	//
	// 认领只发生在"表确实存在"时，所以不会把用户自己写的规则算到面板头上——
	// 用户手写规则不会用这个表名。
	if !s.tproxyManaged() {
		active, err := s.applier.RulesActive(ctx)
		if err != nil {
			// 探测失败无法判断，保持未托管：这一侧是安全的，面板不会去动
			// 用户的配置，代价只是残留规则要等下次启动再认领
			s.logger.Errorf("核实宿主上是否存在本面板的透明代理规则失败，暂不处理: %v", err)
			return
		}
		if !active {
			// 宿主上没有本面板的规则，也没有托管记录，两边一致。
			// 用户自己填 tproxy-port、自己写规则的情形正落在这里：
			// 面板从未接管过，不该去改一份不属于它的配置。
			return
		}
		s.logger.Info("发现宿主上残留本面板下发的 TProxy 规则但无托管记录" +
			"（通常是升级或异常退出导致），已认领，后续按正常流程核实")
		if err := s.setTProxyManaged(true); err != nil {
			// 认领失败就没法安全地继续：后面的回落路径会删 base.yaml 里的
			// tproxy-port，而那个删除的前提正是"这个端口是面板写的"，
			// 该前提由标记承载。宁可留着规则等下次启动，也不要在依据不牢时
			// 改用户的配置。
			s.logger.Errorf("认领残留 TProxy 规则失败，本次不处理: %v", err)
			return
		}
	}

	// 启用状态必须走 state()：它以 base.yaml 为准（TProxy 还要求托管标记）。
	// 早先这里读的是 settings 表的 transparent.enabled，而开关状态迁移到
	// base.yaml 之后那个键已经没人写 "true" 了，判断会永远为假，
	// 使这整段宿主重启后的规则失效检测变成死代码。
	st := s.state()
	if !st.Enabled || st.Mode != string(netcheck.ModeTProxy) {
		// 上面那条情况 2：标记说托管，但配置已经不再是面板托管的 TProxy
		// （用户在配置中心删了端口，或改开了 TUN）。规则成了孤儿，拆掉。
		// 不需要改 base.yaml——用户的意图已经写在里面了，这里只是让宿主状态
		// 跟上它。
		s.logger.Error("检测到本面板下发的 TProxy 规则已与基础配置不符" +
			"（配置里已无 tproxy-port 或已切到 TUN），拆除残留规则")
		if err := s.applier.Teardown(ctx, s.customRulesForTeardown()); err != nil {
			// 拆除失败时保留标记，下次启动或用户点关闭时还会再试一次
			s.logger.Errorf("拆除残留 TProxy 规则失败: %v", err)
			return
		}
		if err := s.setTProxyManaged(false); err != nil {
			s.logger.Errorf("清理规则托管标记失败: %v", err)
		}
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
	if err := s.applier.Teardown(ctx, s.customRulesForTeardown()); err != nil {
		// 规则本来就不存在，Teardown 是幂等的；失败只记录不阻断落状态，
		// 否则状态会卡在"记录说开着、规则确实没有"的原样
		s.logger.Errorf("拆除透明代理规则失败（继续回落状态）: %v", err)
	}
	// 托管关系到此结束：下面会把 base.yaml 里的 tproxy-port 一并清掉，
	// 标记留着会让后续的 disable() 再徒劳地拆一次已经不存在的规则
	if err := s.setTProxyManaged(false); err != nil {
		s.logger.Errorf("清理规则托管标记失败: %v", err)
	}
	// 状态落在 base.yaml 上：随后那次合并读的是它，只改 settings 表
	// 不会让最终 config.yaml 里的 tproxy-port 消失。
	//
	// 这里删 tproxy-port 是安全的：能走到这一步说明标记为已托管，
	// 也就是这个端口本来就是面板自己写进去的，不是用户手填的。
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

// Resync 在配置变更后让防火墙规则跟上新配置。
//
// 为什么需要它：规则里烧进了 tproxy-port、DNS 端口、内核 API 端口、是否下发 v6
// 规则这几个运行时值，而它们全都来自用户随时能在「配置中心」改的配置。改完点
// 「保存并应用」只会重新生成 config.yaml 并让内核热重载——防火墙规则不会变。
// 于是内核听在新端口、规则还往旧端口投，界面却提示"已生效"。
// 具体后果：改 external-controller 端口会失去面板对内核的访问；改 tproxy-port
// 或 dns.listen 会让对应流量投向无人监听的端口。
//
// 刻意不走 90 秒确认窗口（与 enable() 不同）。三个理由：
//   - 这是用户主动保存配置的直接结果，不是一次独立的"启用"操作；
//   - 定时拉取也会走合并流程，那时没人在界面前，开窗口只会等来一次误回滚；
//   - 新规则是按新配置算出来的，理应比旧规则更正确。
//
// 确认窗口真正要保护的是"别把自己锁在外面"，这一点由 keepPorts() 现取实时值来
// 保证：即便用户把配置写错，SSH 与面板两条通道仍然放行，他还能改回来。
//
// 只在已托管 TProxy 时动作。未托管（用户手填 tproxy-port、自己维护规则）时
// 什么都不做——那些规则不属于面板。
//
// 返回 error 供 SaveCustomRules 等需要如实上报"是否已立即生效"的调用方使用；
// 合并流程末尾的 resyncTransparent 仍可忽略返回值（配置已落盘，规则同步
// 失败不该让"保存配置"报失败）。
func (s *TransparentService) Resync(ctx context.Context) error {
	if !s.hasApplier() || !s.tproxyManaged() {
		return nil
	}
	st := s.state()
	if !st.Enabled || st.Mode != string(netcheck.ModeTProxy) {
		// 配置已经不是"面板托管的 TProxy"了。这属于状态不一致，由
		// ReconcileState 在启动时处理（它会拆掉孤儿规则）；
		// 这里不越权拆规则——合并流程每次都跑，误判的代价太大。
		return nil
	}
	// 待确认状态下不介入：此时用户正在验证网络，重下发会打断他的验证，
	// 且回滚逻辑依赖当前那套规则。
	if !s.pendingUntil().IsZero() {
		return nil
	}

	want := s.tproxyParams(st.TProxyPort)
	sig := paramsSignature(want)
	if sig == s.getString(settingTProxyAppliedSig, "") {
		// 配置没漂移。这是绝大多数合并的情形（改节点、换订阅都不影响规则），
		// 直接返回避免无谓的重下发——Apply 会先删表再建，期间有瞬时丢包。
		return nil
	}

	s.logger.Infof("检测到透明代理相关配置已变更，重新下发防火墙规则（%s -> %s）",
		s.getString(settingTProxyAppliedSig, "(无记录)"), sig)
	if err := s.applier.Apply(ctx, want); err != nil {
		// 下发失败时保留旧指纹与旧 applied 快照：下次合并还会再试一次。
		// 不拆旧规则——旧规则至少还能工作（只是端口对不上新配置），
		// 拆了就是彻底断网。Apply 内部失败时已自行清理到一致状态。
		s.logger.Errorf("重新下发防火墙规则失败，规则仍是变更前的状态，"+
			"透明代理可能与当前配置不一致: %v", err)
		return fmt.Errorf("重新下发防火墙规则失败: %w", err)
	}
	if err := s.store.SetSetting(settingTProxyAppliedSig, sig); err != nil {
		s.logger.Errorf("记录规则参数指纹失败: %v", err)
	}
	s.persistAppliedCustomRules(want.CustomRules)
	s.logger.Info("防火墙规则已与当前配置同步")
	return nil
}

// GetCustomRules 返回用户自定义防火墙规则原文。
//
// 返回未规范化的原始文本（保留注释与空行），用户重新编辑时看到的是
// 自己写的样子，而不是被格式化过的版本。
func (s *TransparentService) GetCustomRules() string {
	return s.getString(settingCustomRules, "")
}

// SaveCustomRules 校验并保存用户自定义防火墙规则。
//
// 存原文（非规范化结果）：应用时才规范化，用户再次编辑时保留自己的
// 排版。校验失败时返回带行号的错误，由接口原样透出给界面。
//
// 保存后若 TProxy 正在运行，立即重新应用让新规则生效。走 Resync 而非
// 90 秒确认窗口：规则只是增量变化（内置规则未动），风险远低于首次启用；
// Resync 按指纹判断，规则没变时是幂等空转。
//
// Resync 失败会向上返回：库已写入新文本，但宿主可能仍是旧规则——
// 绝不能对调用方谎报"已立即重新应用"。
func (s *TransparentService) SaveCustomRules(ctx context.Context, text string) error {
	normalized, err := netcheck.NormalizeCustomRules(text)
	if err != nil {
		s.logger.Errorf("保存自定义防火墙规则被拒绝（格式校验失败）: %v", err)
		return err
	}
	s.logger.Infof("保存自定义防火墙规则: lines=%d tproxyManaged=%v",
		len(normalized), s.tproxyManaged())
	if err := s.store.SetSetting(settingCustomRules, text); err != nil {
		s.logger.Errorf("保存自定义防火墙规则失败: %v", err)
		return fmt.Errorf("保存自定义防火墙规则失败: %w", err)
	}
	if err := s.Resync(ctx); err != nil {
		s.logger.Errorf("自定义防火墙规则已落库，但重新应用失败: %v", err)
		return fmt.Errorf("规则已保存到数据库，但重新应用到宿主失败（请检查系统设置里的「规则不同步」提示后重试）: %w", err)
	}
	if s.tproxyManaged() {
		s.logger.Info("自定义防火墙规则已保存并已尝试同步到宿主")
	} else {
		s.logger.Info("自定义防火墙规则已保存（当前未托管 TProxy，仅落库）")
	}
	return nil
}

// BuiltinRules 返回面板内置 nft 规则文本与策略路由命令（按当前参数生成）。
//
// 供界面"查看内置规则"使用——注意这是"按当前配置该下发的样子"，
// 与实际生效的规则（ActiveRules）可能不同：用户改过端口但还没合并时，
// 两者会有短暂差异。
func (s *TransparentService) BuiltinRules() (string, []string, error) {
	p := s.tproxyParams(s.state().TProxyPort)
	rules, err := netcheck.BuildNFTRules(p)
	if err != nil {
		return "", nil, err
	}
	cmds := make([]string, 0, len(netcheck.PolicyRouteCommands(p.EnableIPv6)))
	for _, c := range netcheck.PolicyRouteCommands(p.EnableIPv6) {
		cmds = append(cmds, strings.Join(c, " "))
	}
	return rules, cmds, nil
}

// ActiveRules 返回宿主上实际生效的面板 nft 规则文本。
// TProxy 未开启或表不存在时返回空字符串（界面据此提示"未开启"）。
func (s *TransparentService) ActiveRules(ctx context.Context) (string, error) {
	if !s.hasApplier() {
		return "", nil
	}
	return s.applier.DumpRules(ctx)
}

// IPTablesBackend 返回 iptables 命令的后端类型：nf_tables / legacy / 空。
//
// 自定义防火墙规则按 iptables 语法执行，用户需要知道规则最终落到了哪套
// 后端：legacy 与 nftables 两套规则互不可见，写错地方等于没写。
func (s *TransparentService) IPTablesBackend() string {
	return s.detect().IPTablesBackend
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

// TProxyManaged 报告宿主上的 TProxy 防火墙规则与策略路由是否由本面板下发。
//
// 导出给合并流程用：它据此决定是否注入 TProxy 的技术参数（routing-mark）。
// 这个事实只有本服务知道——配置文件里没有任何痕迹。
func (s *TransparentService) TProxyManaged() bool {
	return s.tproxyManaged()
}

// tproxyManaged 报告宿主上的 TProxy 规则是否由本面板下发。
//
// 读不到（键不存在、或本次是从旧版本升级上来）一律按 false：把"不确定"
// 当作"未托管"是安全的一侧——面板因此不会去动用户的 tproxy-port，
// 也不会谎报已接管；代价只是用户需要重新点一次开关。反过来把不确定当已托管，
// 就会让 ReconcileState 删掉用户手写的配置。
func (s *TransparentService) tproxyManaged() bool {
	return s.getString(settingTProxyManaged, "") == "1"
}

// setTProxyManaged 落库托管标记。
// 关闭托管时同步清空"已应用自定义规则"快照：宿主规则已拆（或即将视为
// 不再由面板托管），快照留着只会让下次启用误以为有一批旧规则要先拆。
func (s *TransparentService) setTProxyManaged(managed bool) error {
	v := ""
	if managed {
		v = "1"
	} else {
		s.clearAppliedCustomRules()
	}
	return s.store.SetSetting(settingTProxyManaged, v)
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

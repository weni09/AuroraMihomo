package netcheck

import (
	"fmt"

	"auroramihomo/backend/internal/domain"
)

// 透明代理默认端口。挑高位端口避免与常见服务冲突，
// 与 mihomo 文档示例保持一致便于用户对照。
const (
	DefaultTProxyPort = 7893
	DefaultDNSPort    = 1053
)

// InjectOptions 注入透明代理技术参数所需的参数。
//
// 这里没有 Mode 字段：启用哪种模式由 base.yaml 里的 tun.enable /
// tproxy-port 决定（见 Inject），外部再传一个模式进来只会形成第二个
// 事实来源，两者不一致时无从判断该信谁。
type InjectOptions struct {
	// TUNStack 用户没在配置里指定 stack 时使用的默认值；
	// 已指定则以用户的为准，不覆盖。空表示用 defaultTUNStack。
	TUNStack string
	// AutoRedirect 仅 Linux 有效：让 mihomo 自己写并清理防火墙规则。
	// macOS 上 mihomo 会静默忽略该字段。
	AutoRedirect bool
	// TProxyManaged 表示宿主上的 TProxy 防火墙规则与策略路由由本面板下发。
	//
	// 与上面 InjectOptions 不设 Mode 的理由不冲突：模式确实该从配置里读，
	// 但"规则是不是面板下发的"在配置里根本没有表达（那是 nftables 与策略路由
	// 的状态），只能由调用方从托管标记传入。
	//
	// TProxy 唯一要注入的 routing-mark 只在配合面板下发的规则时才有意义
	// （见 injectTProxyTechnicalParams），所以未托管时不该注入——
	// 用户在「配置中心」把 tproxy-port 当普通端口设置填上，不构成启用 TProxy。
	TProxyManaged bool
}

// defaultTUNStack 选 mixed：TCP 走内核栈（开销低），UDP 走 gvisor
// （兼容性好）。mihomo 文档推荐这一组合。
const defaultTUNStack = "mixed"

// Inject 注入透明代理的技术参数。
//
// 设计变更：不再覆盖 tun.enable 和 tproxy-port，改为只注入技术参数。
// 透明代理开关现在直接修改 base.yaml，通过配置合并流程生效。
//
// 注入内容：
//   - TUN：stack（默认值）、auto-route、auto-detect-interface、dns-hijack、auto-redirect
//   - TProxy：routing-mark（防止自环）
//
// 根据合并后配置的实际值决定是否注入：
//   - cfg.TUN.Enable == true → 注入 TUN 技术参数
//   - cfg.TProxyPort > 0 且 opt.TProxyManaged → 注入 TProxy 技术参数
//
// TProxy 那侧多一个托管条件，因为两种模式的"启用"由不同的东西构成：
// tun.enable 一写、配置一下发，mihomo 自己就把网卡、路由、防火墙都建好了，
// 配置即机制；而 tproxy-port 只让内核监听一个端口，把流量引过去的规则不在
// 配置里，只有面板能放上去。所以"配了端口"不等于"启用了 TProxy"——用户在
// 「配置中心 → 端口设置」里把它当普通端口填上是完全正常的用法。
func Inject(cfg *domain.Config, opt InjectOptions) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}

	// 根据合并后配置的实际值决定注入内容
	if cfg.TUN.Enable {
		// TUN 已开启（来自 base 或 remote），注入技术参数
		return injectTUNTechnicalParams(cfg, opt)
	}

	if cfg.TProxyPort > 0 && opt.TProxyManaged {
		// TProxy 端口已配置，且规则确实由本面板下发，注入技术参数
		return injectTProxyTechnicalParams(cfg, opt)
	}

	// 未启用透明代理（含"仅配置了 tproxy-port 但规则不是面板下发"）。
	// 不注入，并且要把此前注入过的 routing-mark 清掉——否则那个值会永久留在
	// 生成的配置里（理由见 clearInjectedRoutingMark）。
	clearInjectedRoutingMark(cfg)
	return nil
}

// injectTUNTechnicalParams 补齐 TUN 的技术参数。
//
// 一律"只补不覆盖"：这些字段在「配置中心」里都有对应表单项，用户显式填过的值
// 必须留下。早先这里无条件赋值，结果用户特意选的 stack: system 或
// auto-route: false 会在每次合并时被静默改回默认值——正是本次改动要消除的
// "开关覆盖用户配置"那类问题，只是换了个位置发生。
func injectTUNTechnicalParams(cfg *domain.Config, opt InjectOptions) error {
	// TUN 是值类型字段，取址直接改
	t := &cfg.TUN

	// stack 只在用户没填时补默认值；填了就按他的来（含从订阅合并进来的值）
	if t.Stack == "" {
		stack := opt.TUNStack
		if stack == "" {
			stack = defaultTUNStack
		}
		t.Stack = stack
	}
	// 校验最终取值而不是只校验入参：用户在配置中心填了错的 stack 时，
	// 与其让 mihomo 拒绝整份配置，不如在这里就把原因说清楚
	switch t.Stack {
	case "system", "gvisor", "mixed":
	default:
		return fmt.Errorf("不支持的 TUN stack: %s（可选 system / gvisor / mixed）", t.Stack)
	}

	// auto-route / auto-detect-interface 缺省时补 true：不接管路由的 TUN
	// 等于没开，而 mihomo 不会为此报错，只是静默不生效。
	// 但用户显式写了 false 就尊重他——TUNConfig 用 *bool 正是为了能区分
	// "没配"和"特意关掉"（见 TUNConfig 字段注释），无条件覆盖会让那个
	// 类型设计失去意义。
	yes := true
	if t.AutoRoute == nil {
		t.AutoRoute = &yes
	}
	if t.AutoDetectInterface == nil {
		t.AutoDetectInterface = &yes
	}

	// DNS 劫持是分流生效的前提：不劫持的话客户端直接问上游 DNS，
	// 域名类规则拿不到查询。any:53 同时覆盖 UDP 与 TCP。
	if len(t.DNSHijack) == 0 {
		t.DNSHijack = []string{"any:53"}
	}

	if t.Extra == nil {
		t.Extra = map[string]interface{}{}
	}
	// auto-redirect：旁路由/网关下让 mihomo 用 REDIRECT 接管转发流量。
	//
	// 只补不覆盖：
	//   - 未声明 + opt.AutoRedirect：写入 true（配置中心只开 tun.enable 的常见路径）
	//   - 显式 false：尊重用户，不改回 true
	//   - 显式 true：保持
	// Alpine 上 nft 后端 file exists 由 mihomo 启动时 DISABLE_NFTABLES=1 解决。
	if _, declared := t.Extra["auto-redirect"]; !declared && opt.AutoRedirect {
		t.Extra["auto-redirect"] = true
	}

	// auto-detect-interface 与 interface-name 互斥：两者同时存在时
	// mihomo 的行为取决于实现细节，不该让用户碰到这种不确定性。
	//
	// 只在 auto-detect-interface 确实生效时才清：用户显式关掉自动探测、
	// 手工指定出口网卡是完全合理的配置，那种情况下 interface-name 必须保留。
	if cfg.InterfaceName != "" && t.AutoDetectInterface != nil && *t.AutoDetectInterface {
		cfg.InterfaceName = ""
	}

	// TUN 不需要 routing-mark：mihomo 自管防火墙规则，没有"内核自身流量被
	// 自己的规则捕获"那个自环问题。从 TProxy 切过来时它会残留在配置里，
	// 顺手清掉（只清本包注入的那个值）。
	clearInjectedRoutingMark(cfg)

	return nil
}

// injectTProxyTechnicalParams 注入 TProxy 技术参数，不覆盖 TProxyPort 字段。
func injectTProxyTechnicalParams(cfg *domain.Config, opt InjectOptions) error {
	// 注意：不再设置 cfg.TProxyPort，由 base.yaml 控制

	// routing-mark 让内核自身的出站流量带上标记，防火墙规则据此放行，
	// 否则 mihomo 发出的包会被自己的 TPROXY 规则再次捕获形成自环。
	// 值必须与 firewall.go 里的 KernelMark 一致。
	cfg.RoutingMark = KernelMark

	return nil
}

// clearInjectedRoutingMark 清掉本包注入过的 routing-mark。
//
// 只清等于 KernelMark 的值：那是本包注入的特征值，别的值说明是用户自己配的
// （routing-mark 是 mihomo 的通用字段，有人会用它做策略路由），不能动。
//
// 需要这一步是因为注入是"每次合并重新生成"，而清理不是自动的：一旦某次合并
// 注入了 routing-mark，它就会一直留在生成的 config.yaml 里，即使后来
// 透明代理已经关掉、或（本次修正的情形）当初根本就不该注入。
// 而 routing-mark 的唯一用途是配合面板下发的防火墙规则放行内核自身流量，
// 规则不存在时它对用户毫无意义，还会让排障的人以为透明代理是开着的。
func clearInjectedRoutingMark(cfg *domain.Config) {
	if cfg.RoutingMark == KernelMark {
		cfg.RoutingMark = 0
	}
}

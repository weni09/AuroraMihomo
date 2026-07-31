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
//   - cfg.TProxyPort > 0 → 注入 TProxy 技术参数
func Inject(cfg *domain.Config, opt InjectOptions) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}

	// 根据合并后配置的实际值决定注入内容
	if cfg.TUN.Enable {
		// TUN 已开启（来自 base 或 remote），注入技术参数
		return injectTUNTechnicalParams(cfg, opt)
	}

	if cfg.TProxyPort > 0 {
		// TProxy 已配置端口（来自 base 或 remote），注入技术参数
		return injectTProxyTechnicalParams(cfg, opt)
	}

	// 两者都未启用，不注入
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
	// auto-redirect 让 mihomo 自己管防火墙规则并在退出时清理，
	// 面板就不必碰 nftables。仅 Linux 生效，macOS 上 mihomo 会忽略。
	if opt.AutoRedirect {
		t.Extra["auto-redirect"] = true
	} else {
		delete(t.Extra, "auto-redirect")
	}

	// auto-detect-interface 与 interface-name 互斥：两者同时存在时
	// mihomo 的行为取决于实现细节，不该让用户碰到这种不确定性。
	//
	// 只在 auto-detect-interface 确实生效时才清：用户显式关掉自动探测、
	// 手工指定出口网卡是完全合理的配置，那种情况下 interface-name 必须保留。
	if cfg.InterfaceName != "" && t.AutoDetectInterface != nil && *t.AutoDetectInterface {
		cfg.InterfaceName = ""
	}

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

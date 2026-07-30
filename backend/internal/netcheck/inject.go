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

// InjectOptions 注入透明代理配置所需的参数。
type InjectOptions struct {
	Mode Mode
	// TProxyPort 仅 TProxy 模式使用，0 表示用默认值
	TProxyPort int
	// TUNStack system / gvisor / mixed，空表示用 mixed
	TUNStack string
	// AutoRedirect 仅 Linux 有效：让 mihomo 自己写并清理防火墙规则。
	// macOS 上 mihomo 会静默忽略该字段。
	AutoRedirect bool
}

// defaultTUNStack 选 mixed：TCP 走内核栈（开销低），UDP 走 gvisor
// （兼容性好）。mihomo 文档推荐这一组合。
const defaultTUNStack = "mixed"

// Inject 按模式改写最终配置。
//
// 这是"透明代理开关"真正落到内核配置上的地方。放在配置生成的最后一步
// （GenerateYAML 之前），而不是写进用户的 base 配置：后者用户可以随手改掉，
// 开关状态与实际配置就会不一致。
//
// 关闭模式下同样要动手——必须显式把 tun.enable 置 false。只是"不写 tun 段"
// 不够：上一次开启时写进去的配置还在磁盘上，不覆盖就等于没关掉。
func Inject(cfg *domain.Config, opt InjectOptions) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}

	switch opt.Mode {
	case ModeOff, "":
		disableAll(cfg)
		return nil

	case ModeTUN:
		disableTProxy(cfg)
		return enableTUN(cfg, opt)

	case ModeTProxy:
		disableTUN(cfg)
		return enableTProxy(cfg, opt)

	default:
		return fmt.Errorf("未知的透明代理模式: %s", opt.Mode)
	}
}

func enableTUN(cfg *domain.Config, opt InjectOptions) error {
	stack := opt.TUNStack
	if stack == "" {
		stack = defaultTUNStack
	}
	switch stack {
	case "system", "gvisor", "mixed":
	default:
		return fmt.Errorf("不支持的 TUN stack: %s（可选 system / gvisor / mixed）", stack)
	}

	// TUN 是值类型字段，取址直接改
	t := &cfg.TUN
	t.Enable = true
	t.Stack = stack

	// auto-route 必须显式为 true：不接管路由的 TUN 等于没开，
	// 且 mihomo 不会为此报错（指针语义的用意见 TUNConfig 注释）。
	yes := true
	t.AutoRoute = &yes
	t.AutoDetectInterface = &yes

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
	if cfg.InterfaceName != "" {
		cfg.InterfaceName = ""
	}

	return nil
}

func enableTProxy(cfg *domain.Config, opt InjectOptions) error {
	port := opt.TProxyPort
	if port == 0 {
		port = DefaultTProxyPort
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("tproxy 端口非法: %d", port)
	}
	cfg.TProxyPort = port

	// routing-mark 让内核自身的出站流量带上标记，防火墙规则据此放行，
	// 否则 mihomo 发出的包会被自己的 TPROXY 规则再次捕获形成自环。
	// 值必须与 firewall.go 里的 KernelMark 一致。
	cfg.RoutingMark = KernelMark

	return nil
}

func disableTUN(cfg *domain.Config) {
	cfg.TUN.Enable = false
}

func disableTProxy(cfg *domain.Config) {
	cfg.TProxyPort = 0
	// 只在等于我们注入的值时清掉，避免抹掉用户自己配的 routing-mark
	if cfg.RoutingMark == KernelMark {
		cfg.RoutingMark = 0
	}
}

func disableAll(cfg *domain.Config) {
	disableTUN(cfg)
	disableTProxy(cfg)
}

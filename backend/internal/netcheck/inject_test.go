package netcheck

import (
	"testing"

	"auroramihomo/backend/internal/domain"
)

// 启用状态来自 base.yaml（这里用 cfg.TUN.Enable 模拟），
// 注入负责把"开了但不完整"的配置补成真正能工作的一份
func TestInjectTUNFillsAutoRouteWhenUnset(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true

	if err := Inject(cfg, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if !cfg.TUN.Enable {
		t.Error("tun.enable 不应被修改")
	}
	// auto-route 缺省时必须补 true：不接管路由的 TUN 等于没开，
	// 而 mihomo 对此不报错，只是静默不生效
	if cfg.TUN.AutoRoute == nil || !*cfg.TUN.AutoRoute {
		t.Errorf("auto-route 缺省时应补为 true，实际 %v", cfg.TUN.AutoRoute)
	}
	if cfg.TUN.AutoDetectInterface == nil || !*cfg.TUN.AutoDetectInterface {
		t.Errorf("auto-detect-interface 缺省时应补为 true，实际 %v", cfg.TUN.AutoDetectInterface)
	}
	// 不劫持 DNS 的话客户端直接问上游，域名分流拿不到查询
	if len(cfg.TUN.DNSHijack) == 0 {
		t.Error("应设置 dns-hijack，否则域名类规则失效")
	}
	if cfg.TUN.Stack != defaultTUNStack {
		t.Errorf("默认 stack 应为 %s，实际 %s", defaultTUNStack, cfg.TUN.Stack)
	}
}

// 自动探测生效时 interface-name 必须让位，否则 mihomo 的取舍取决于实现细节
func TestInjectTUNClearsConflictingInterfaceName(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true
	cfg.InterfaceName = "eth0"

	if err := Inject(cfg, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.InterfaceName != "" {
		t.Errorf("interface-name 与 auto-detect-interface 互斥，应被清空，实际 %q", cfg.InterfaceName)
	}
}

func TestInjectTUNAutoRedirectOptional(t *testing.T) {
	on := &domain.Config{}
	on.TUN.Enable = true
	if err := Inject(on, InjectOptions{AutoRedirect: true}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if v, ok := on.TUN.Extra["auto-redirect"]; !ok || v != true {
		t.Errorf("开启 AutoRedirect 应写入 auto-redirect: true，实际 %v", on.TUN.Extra)
	}

	off := &domain.Config{}
	off.TUN.Enable = true
	if err := Inject(off, InjectOptions{AutoRedirect: false}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if _, ok := off.TUN.Extra["auto-redirect"]; ok {
		t.Errorf("未开启时不应写 auto-redirect，实际 %v", off.TUN.Extra)
	}
}

func TestInjectTUNAcceptsAllValidStacks(t *testing.T) {
	for _, s := range []string{"system", "gvisor", "mixed"} {
		c := &domain.Config{}
		c.TUN.Enable = true
		if err := Inject(c, InjectOptions{TUNStack: s}); err != nil {
			t.Errorf("stack %q 应被接受: %v", s, err)
		}
		if c.TUN.Stack != s {
			t.Errorf("stack 应为 %q，实际 %q", s, c.TUN.Stack)
		}
	}
	// 兜底默认值本身非法时也要拦住，否则会把坏值写进最终配置
	c := &domain.Config{}
	c.TUN.Enable = true
	if err := Inject(c, InjectOptions{TUNStack: "nonsense"}); err == nil {
		t.Error("应拒绝非法的默认 stack")
	}
}

// routing-mark 必须与防火墙规则里的 KernelMark 一致，
// 不一致的话内核出站不会被放行，形成自环
func TestInjectTProxySetsMatchingRoutingMark(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TProxyPort = DefaultTProxyPort // 模拟 base.yaml 已设置端口
	// TProxyManaged 是必需的前提：routing-mark 的作用就是配合面板下发的防火墙
	// 规则放行内核自身流量，规则不存在时注入它没有任何意义
	if err := Inject(cfg, InjectOptions{TProxyManaged: true}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.TProxyPort != DefaultTProxyPort {
		t.Errorf("tproxy-port 不应被修改，实际 %d", cfg.TProxyPort)
	}
	if cfg.RoutingMark != KernelMark {
		t.Errorf("routing-mark 必须等于防火墙规则里的 KernelMark(0x%x)，实际 0x%x，"+
			"不一致会导致内核出站自环", KernelMark, cfg.RoutingMark)
	}
}

// 核心不变量：开关状态由 base.yaml 决定，注入永远不许改动 enable / 端口。
// 这条一旦被破坏，界面上的开关与最终配置就会重新走向不一致。
func TestInjectNeverTouchesEnableOrPort(t *testing.T) {
	t.Run("TUN 开启时不改 enable", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = true
		if err := Inject(cfg, InjectOptions{AutoRedirect: true}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if !cfg.TUN.Enable {
			t.Error("注入不该把 tun.enable 改掉")
		}
	})

	t.Run("TUN 关闭时不被打开", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = false
		if err := Inject(cfg, InjectOptions{TUNStack: "mixed", AutoRedirect: true}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.TUN.Enable {
			t.Error("注入不该擅自开启 TUN")
		}
		// 未启用时连技术参数都不该补，否则会在用户配置里留下一堆
		// 与"没开 TUN"矛盾的残留字段
		if cfg.TUN.Stack != "" || cfg.TUN.AutoRoute != nil {
			t.Errorf("未启用时不该补技术参数，stack=%q autoRoute=%v",
				cfg.TUN.Stack, cfg.TUN.AutoRoute)
		}
	})

	t.Run("TProxy 端口不被改写", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TProxyPort = 7999
		if err := Inject(cfg, InjectOptions{}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.TProxyPort != 7999 {
			t.Errorf("注入不该改动 tproxy-port，实际 %d", cfg.TProxyPort)
		}
	})

	t.Run("两者都未启用时完全不动配置", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.RoutingMark = 0x1234 // 用户自己的值
		if err := Inject(cfg, InjectOptions{}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.TUN.Enable || cfg.TProxyPort != 0 {
			t.Error("两者都未启用时不该有任何写入")
		}
		if cfg.RoutingMark != 0x1234 {
			t.Errorf("不该抹掉用户自己配的 routing-mark，实际 0x%x", cfg.RoutingMark)
		}
	})
}

// 用户在配置中心显式填过的值必须留下——这正是本次改动想解决的问题，
// 若注入仍无条件覆盖，只是把"开关压用户配置"搬了个地方
func TestInjectDoesNotOverrideExplicitUserValues(t *testing.T) {
	t.Run("保留用户选的 stack", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = true
		cfg.TUN.Stack = "system" // 用户特意选的
		if err := Inject(cfg, InjectOptions{TUNStack: "mixed"}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.TUN.Stack != "system" {
			t.Errorf("用户选的 stack 被覆盖成了 %q", cfg.TUN.Stack)
		}
	})

	t.Run("保留用户显式关掉的 auto-route", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = true
		no := false
		cfg.TUN.AutoRoute = &no // 用户特意关掉
		if err := Inject(cfg, InjectOptions{}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.TUN.AutoRoute == nil || *cfg.TUN.AutoRoute {
			t.Error("用户显式关掉的 auto-route 被改回 true")
		}
	})

	t.Run("保留用户自定义的 dns-hijack", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = true
		cfg.TUN.DNSHijack = []string{"tcp://1.1.1.1:53"}
		if err := Inject(cfg, InjectOptions{}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if len(cfg.TUN.DNSHijack) != 1 || cfg.TUN.DNSHijack[0] != "tcp://1.1.1.1:53" {
			t.Errorf("用户自定义的 dns-hijack 被覆盖: %v", cfg.TUN.DNSHijack)
		}
	})

	// 手工指定出口网卡 + 关掉自动探测是合理组合，不该被清掉
	t.Run("关掉自动探测时保留 interface-name", func(t *testing.T) {
		cfg := &domain.Config{}
		cfg.TUN.Enable = true
		no := false
		cfg.TUN.AutoDetectInterface = &no
		cfg.InterfaceName = "eth0"
		if err := Inject(cfg, InjectOptions{}); err != nil {
			t.Fatalf("注入失败: %v", err)
		}
		if cfg.InterfaceName != "eth0" {
			t.Errorf("自动探测已关闭，interface-name 不该被清空，实际 %q", cfg.InterfaceName)
		}
	})
}

// 用户在配置中心填了非法 stack 时，与其让 mihomo 拒绝整份配置，
// 不如在这里报错把原因说清楚
func TestInjectRejectsBadStackFromBaseConfig(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true
	cfg.TUN.Stack = "nonsense"
	if err := Inject(cfg, InjectOptions{}); err == nil {
		t.Error("应拒绝 base 配置里的非法 stack")
	}
}

// TUN 启用时不该顺手给 TProxy 打标记：routing-mark 是 TProxy 专用的
// 防自环手段，出现在 TUN 配置里只会让排障时多一条无来由的线索
func TestInjectTUNDoesNotSetRoutingMark(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true
	if err := Inject(cfg, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.RoutingMark == KernelMark {
		t.Error("TUN 模式不该注入 TProxy 专用的 routing-mark")
	}
}

// 两种模式在 base.yaml 里同时存在时（用户手改或订阅带入），
// 以 TUN 优先并且不去动 TProxy 的端口——清理互斥是 transparent_service
// 的职责，注入阶段擅自改端口会掩盖配置本身的问题
func TestInjectPrefersTUNWhenBothPresent(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true
	cfg.TProxyPort = 7893
	if err := Inject(cfg, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.TUN.Stack == "" {
		t.Error("应按 TUN 分支补齐技术参数")
	}
	if cfg.TProxyPort != 7893 {
		t.Errorf("不该改动 tproxy-port，实际 %d", cfg.TProxyPort)
	}
}

func TestInjectRejectsNilConfig(t *testing.T) {
	if err := Inject(nil, InjectOptions{}); err == nil {
		t.Error("配置为 nil 应报错")
	}
}

// 仅配置了 tproxy-port 不构成启用 TProxy，不该注入任何技术参数。
//
// tproxy-port 只让内核监听一个端口；把流量引到该端口的防火墙规则与策略路由
// 不在配置文件里，只有面板能放上去。用户在「配置中心 → 端口设置」里把它当成
// 普通端口填上是正常用法（本项目的默认端口布局就带一个 tproxy-port）。
// 此时注入 routing-mark 会在他的配置里留下一个无用字段：那个值的唯一作用是
// 配合面板下发的规则放行内核自身流量，规则不存在时它什么也不做，
// 却会让排障的人以为透明代理是开着的。
func TestInjectSkipsTProxyWhenNotManaged(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TProxyPort = 7894 // 用户自己填的端口

	if err := Inject(cfg, InjectOptions{TProxyManaged: false}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if cfg.RoutingMark != 0 {
		t.Errorf("规则不是面板下发时不该注入 routing-mark，实际 0x%x", cfg.RoutingMark)
	}
	// 端口是用户的配置，注入阶段一律不动
	if cfg.TProxyPort != 7894 {
		t.Errorf("不该改动用户填的 tproxy-port，实际 %d", cfg.TProxyPort)
	}
}

// 托管关系结束后，此前注入的 routing-mark 必须被清掉。
//
// 注入是"每次合并重新生成"，但清理不是自动的：一旦某次注入写进了
// routing-mark，它会一直留在生成的 config.yaml 里，哪怕透明代理已经关掉。
// 这正是本次修正前的实际状态——面板对一个手填端口注入了 routing-mark: 0xff，
// 而宿主上从来没有对应的规则。
func TestInjectClearsStaleRoutingMarkWhenNoLongerManaged(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TProxyPort = 7894
	cfg.RoutingMark = KernelMark // 上一次合并注入留下的

	if err := Inject(cfg, InjectOptions{TProxyManaged: false}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if cfg.RoutingMark != 0 {
		t.Errorf("不再托管时应清掉此前注入的 routing-mark，实际 0x%x", cfg.RoutingMark)
	}
}

// 清理只针对本包注入的特征值：routing-mark 是 mihomo 的通用字段，
// 用户可能用它做自己的策略路由，那种值不能动。
func TestInjectKeepsUserRoutingMarkWhenNotManaged(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TProxyPort = 7894
	cfg.RoutingMark = 0x1234 // 用户自己配的，与 KernelMark 不同

	if err := Inject(cfg, InjectOptions{TProxyManaged: false}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if cfg.RoutingMark != 0x1234 {
		t.Errorf("不该抹掉用户自己配的 routing-mark，实际 0x%x", cfg.RoutingMark)
	}
}

// 从 TProxy 切到 TUN 后，TProxy 的 routing-mark 不该残留：
// TUN 由 mihomo 自管防火墙，没有内核出站自环那个问题，这个字段纯属遗留。
func TestInjectClearsRoutingMarkWhenSwitchedToTUN(t *testing.T) {
	cfg := &domain.Config{}
	cfg.TUN.Enable = true
	cfg.RoutingMark = KernelMark // TProxy 时期注入的

	if err := Inject(cfg, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if cfg.RoutingMark != 0 {
		t.Errorf("切到 TUN 后应清掉 TProxy 的 routing-mark，实际 0x%x", cfg.RoutingMark)
	}
	// 但用户自己的值同样要留着
	cfg2 := &domain.Config{}
	cfg2.TUN.Enable = true
	cfg2.RoutingMark = 0x1234
	if err := Inject(cfg2, InjectOptions{}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg2.RoutingMark != 0x1234 {
		t.Errorf("不该抹掉用户自己配的 routing-mark，实际 0x%x", cfg2.RoutingMark)
	}
}

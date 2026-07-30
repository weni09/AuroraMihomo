package netcheck

import (
	"testing"

	"auroramihomo/backend/internal/domain"
)

func TestInjectTUNEnablesAutoRoute(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTUN}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}

	if !cfg.TUN.Enable {
		t.Error("tun.enable 应为 true")
	}
	// auto-route 必须显式为 true：不接管路由的 TUN 等于没开，
	// 而 mihomo 对此不报错，只是静默不生效
	if cfg.TUN.AutoRoute == nil || !*cfg.TUN.AutoRoute {
		t.Errorf("auto-route 必须显式为 true，实际 %v", cfg.TUN.AutoRoute)
	}
	if cfg.TUN.AutoDetectInterface == nil || !*cfg.TUN.AutoDetectInterface {
		t.Errorf("auto-detect-interface 必须显式为 true，实际 %v", cfg.TUN.AutoDetectInterface)
	}
	// 不劫持 DNS 的话客户端直接问上游，域名分流拿不到查询
	if len(cfg.TUN.DNSHijack) == 0 {
		t.Error("应设置 dns-hijack，否则域名类规则失效")
	}
	if cfg.TUN.Stack != defaultTUNStack {
		t.Errorf("默认 stack 应为 %s，实际 %s", defaultTUNStack, cfg.TUN.Stack)
	}
}

// auto-detect-interface 与 interface-name 互斥，注入时要清掉后者
func TestInjectTUNClearsConflictingInterfaceName(t *testing.T) {
	cfg := &domain.Config{}
	cfg.InterfaceName = "eth0"

	if err := Inject(cfg, InjectOptions{Mode: ModeTUN}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.InterfaceName != "" {
		t.Errorf("interface-name 与 auto-detect-interface 互斥，应被清空，实际 %q", cfg.InterfaceName)
	}
}

func TestInjectTUNAutoRedirectOptional(t *testing.T) {
	on := &domain.Config{}
	if err := Inject(on, InjectOptions{Mode: ModeTUN, AutoRedirect: true}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if v, ok := on.TUN.Extra["auto-redirect"]; !ok || v != true {
		t.Errorf("开启 AutoRedirect 应写入 auto-redirect: true，实际 %v", on.TUN.Extra)
	}

	off := &domain.Config{}
	if err := Inject(off, InjectOptions{Mode: ModeTUN, AutoRedirect: false}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if _, ok := off.TUN.Extra["auto-redirect"]; ok {
		t.Errorf("未开启时不应写 auto-redirect，实际 %v", off.TUN.Extra)
	}
}

func TestInjectTUNRejectsBadStack(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTUN, TUNStack: "nonsense"}); err == nil {
		t.Error("应拒绝非法 stack")
	}
	for _, s := range []string{"system", "gvisor", "mixed"} {
		c := &domain.Config{}
		if err := Inject(c, InjectOptions{Mode: ModeTUN, TUNStack: s}); err != nil {
			t.Errorf("stack %q 应被接受: %v", s, err)
		}
	}
}

// routing-mark 必须与防火墙规则里的 KernelMark 一致，
// 不一致的话内核出站不会被放行，形成自环
func TestInjectTProxySetsMatchingRoutingMark(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTProxy}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.TProxyPort != DefaultTProxyPort {
		t.Errorf("tproxy-port 应为默认 %d，实际 %d", DefaultTProxyPort, cfg.TProxyPort)
	}
	if cfg.RoutingMark != KernelMark {
		t.Errorf("routing-mark 必须等于防火墙规则里的 KernelMark(0x%x)，实际 0x%x，"+
			"不一致会导致内核出站自环", KernelMark, cfg.RoutingMark)
	}
}

func TestInjectTProxyCustomPort(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTProxy, TProxyPort: 7999}); err != nil {
		t.Fatalf("注入失败: %v", err)
	}
	if cfg.TProxyPort != 7999 {
		t.Errorf("应使用指定端口 7999，实际 %d", cfg.TProxyPort)
	}
}

func TestInjectTProxyRejectsBadPort(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTProxy, TProxyPort: 70000}); err == nil {
		t.Error("应拒绝越界端口")
	}
}

// 关闭时必须主动写 false，而不是"不写 tun 段"——
// 上次开启留在磁盘上的配置不覆盖就等于没关掉
func TestInjectOffActivelyDisablesPreviousState(t *testing.T) {
	cfg := &domain.Config{}
	// 先开起来，模拟上一次的状态
	if err := Inject(cfg, InjectOptions{Mode: ModeTUN}); err != nil {
		t.Fatalf("预置 TUN 失败: %v", err)
	}
	if err := Inject(cfg, InjectOptions{Mode: ModeTProxy}); err != nil {
		t.Fatalf("预置 TProxy 失败: %v", err)
	}

	if err := Inject(cfg, InjectOptions{Mode: ModeOff}); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if cfg.TUN.Enable {
		t.Error("关闭后 tun.enable 必须为 false")
	}
	if cfg.TProxyPort != 0 {
		t.Errorf("关闭后 tproxy-port 应清零，实际 %d", cfg.TProxyPort)
	}
	if cfg.RoutingMark != 0 {
		t.Errorf("关闭后应清掉我们注入的 routing-mark，实际 0x%x", cfg.RoutingMark)
	}
}

// 空模式等同于关闭，不能当成"不处理"
func TestInjectEmptyModeMeansOff(t *testing.T) {
	cfg := &domain.Config{}
	_ = Inject(cfg, InjectOptions{Mode: ModeTUN})
	if err := Inject(cfg, InjectOptions{Mode: ""}); err != nil {
		t.Fatalf("空模式应视为关闭: %v", err)
	}
	if cfg.TUN.Enable {
		t.Error("空模式应关闭 TUN")
	}
}

// 不能抹掉用户自己配的 routing-mark
func TestInjectOffPreservesUserRoutingMark(t *testing.T) {
	cfg := &domain.Config{}
	cfg.RoutingMark = 0x1234 // 用户自己的值，不是我们注入的

	if err := Inject(cfg, InjectOptions{Mode: ModeOff}); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if cfg.RoutingMark != 0x1234 {
		t.Errorf("不该抹掉用户自己配的 routing-mark，实际变成 0x%x", cfg.RoutingMark)
	}
}

// 两种模式互斥：切到 TProxy 要关掉 TUN，反之亦然
func TestInjectModesAreMutuallyExclusive(t *testing.T) {
	cfg := &domain.Config{}
	if err := Inject(cfg, InjectOptions{Mode: ModeTUN}); err != nil {
		t.Fatalf("注入 TUN 失败: %v", err)
	}
	if err := Inject(cfg, InjectOptions{Mode: ModeTProxy}); err != nil {
		t.Fatalf("切到 TProxy 失败: %v", err)
	}
	if cfg.TUN.Enable {
		t.Error("切到 TProxy 后 TUN 应关闭")
	}

	if err := Inject(cfg, InjectOptions{Mode: ModeTUN}); err != nil {
		t.Fatalf("切回 TUN 失败: %v", err)
	}
	if cfg.TProxyPort != 0 {
		t.Errorf("切回 TUN 后 tproxy-port 应清零，实际 %d", cfg.TProxyPort)
	}
}

func TestInjectRejectsNilConfigAndBadMode(t *testing.T) {
	if err := Inject(nil, InjectOptions{Mode: ModeTUN}); err == nil {
		t.Error("配置为 nil 应报错")
	}
	if err := Inject(&domain.Config{}, InjectOptions{Mode: Mode("bogus")}); err == nil {
		t.Error("未知模式应报错")
	}
}

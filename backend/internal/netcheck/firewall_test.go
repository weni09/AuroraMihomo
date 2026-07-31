package netcheck

import (
	"strings"
	"testing"
)

// 规则顺序就是安全边界，顺序错了会把操作者关在门外。
// 这些用例专门盯住顺序与豁免项，防止后续"整理规则"时被打乱。

func sampleParams() TProxyParams {
	return TProxyParams{
		TProxyPort: 7893,
		KeepPorts:  []int{22, 8899, 9090},
		EnableIPv6: false,
	}
}

// chainOf 截出指定链的规则文本。
//
// prerouting 与 output 两条链的规则在方向语义上不同（一个看 dport、
// 一个看 sport），只在整份输出里搜字符串会让"写在错误的链里"这类问题
// 照样通过——这正是 D1 那个会断 SSH 的缺陷能长期存在的原因。
func chainOf(t *testing.T, rules, chain string) string {
	t.Helper()
	start := strings.Index(rules, "chain "+chain+" {")
	if start < 0 {
		t.Fatalf("规则里没有 %s 链:\n%s", chain, rules)
	}
	rest := rules[start:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("%s 链没有正常闭合:\n%s", chain, rest)
	}
	return rest[:end]
}

// 管理端口必须在 TPROXY 之前放行。若顺序颠倒，规则生效瞬间
// SSH 与面板连接就会被劫持，操作者无法再关掉透明代理。
func TestNFTRulesExemptManagementPortsBeforeTProxy(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}

	firstTProxy := strings.Index(out, "tproxy to")
	if firstTProxy < 0 {
		t.Fatalf("规则里没有 tproxy 动作:\n%s", out)
	}
	for _, port := range []int{22, 8899, 9090} {
		needle := "tcp dport " + itoa(port) + " return"
		idx := strings.Index(out, needle)
		if idx < 0 {
			t.Errorf("端口 %d 未被放行:\n%s", port, out)
			continue
		}
		if idx > firstTProxy {
			t.Errorf("端口 %d 的放行规则出现在 tproxy 之后（位置 %d > %d），会导致连接被劫持",
				port, idx, firstTProxy)
		}
	}
}

// 内核自身出站必须放行，否则 mihomo 发出的包又被 TPROXY 抓回来，
// 形成自环——这对应一个已知的高 CPU 故障。
func TestNFTRulesExemptKernelMark(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if !strings.Contains(out, "meta mark 0xff return") {
		t.Errorf("缺少内核出站放行规则（meta mark 0x%x return）:\n%s", KernelMark, out)
	}
	// prerouting 与 output 两条链都要有
	if n := strings.Count(out, "meta mark 0xff return"); n < 2 {
		t.Errorf("内核出站放行应同时出现在 prerouting 与 output 链，实际 %d 处", n)
	}
}

// DNS 必须在局域网放行之前劫持：否则同网段的 DNS 查询直接放行，
// 分流规则失效，域名类规则形同虚设。
func TestNFTRulesHijackDNSBeforeLANReturn(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	dns := strings.Index(out, "udp dport 53")
	lan := strings.Index(out, "ip daddr 192.168.0.0/16 return")
	if dns < 0 || lan < 0 {
		t.Fatalf("缺少 DNS 劫持或局域网放行规则:\n%s", out)
	}
	if dns > lan {
		t.Errorf("DNS 劫持(%d)出现在局域网放行(%d)之后，域名分流会失效", dns, lan)
	}
}

// output 链必须是 route hook，否则改了 mark 不会重新查路由，
// 本机自身的 UDP 流量出不去。
func TestNFTOutputChainUsesRouteHook(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if !strings.Contains(out, "type route hook output") {
		t.Errorf("output 链必须用 route hook，否则本机 UDP 无法出网:\n%s", out)
	}
}

// 只操作自己的表，绝不整体 flush——宿主上还有 Docker、fail2ban 的规则。
func TestNFTRulesUseDedicatedTableOnly(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if !strings.Contains(out, "table inet "+NFTTableName) {
		t.Errorf("应使用专用表 %s:\n%s", NFTTableName, out)
	}
	for _, forbidden := range []string{"flush ruleset", "iptables -F", "delete table inet filter"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("规则里出现危险操作 %q，会抹掉宿主其它规则:\n%s", forbidden, out)
		}
	}
	// 拆除命令同样只针对自己的表
	td := strings.Join(NFTTeardownCommand(), " ")
	if td != "nft delete table inet "+NFTTableName {
		t.Errorf("拆除命令应只删专用表，实际 %q", td)
	}
}

// 重复应用要幂等：先 add 再 delete 再建，避免残留旧规则叠加
func TestNFTRulesAreIdempotent(t *testing.T) {
	out, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	addIdx := strings.Index(out, "add table inet "+NFTTableName)
	delIdx := strings.Index(out, "delete table inet "+NFTTableName)
	if addIdx < 0 || delIdx < 0 {
		t.Fatalf("缺少 add/delete 前置语句，重复应用会叠加规则:\n%s", out)
	}
	if addIdx > delIdx {
		t.Errorf("应先 add 再 delete（表不存在时 delete 会失败），实际 add=%d delete=%d", addIdx, delIdx)
	}
}

func TestNormalizeRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		p    TProxyParams
	}{
		{"tproxy 端口为 0", TProxyParams{TProxyPort: 0, KeepPorts: []int{22}}},
		{"tproxy 端口越界", TProxyParams{TProxyPort: 70000, KeepPorts: []int{22}}},
		// 空 KeepPorts 必须拒绝：没有豁免端口就等于放弃唯一的补救通道
		{"KeepPorts 为空", TProxyParams{TProxyPort: 7893}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.p
			if err := p.Normalize(); err == nil {
				t.Errorf("应拒绝该参数，实际通过了")
			}
		})
	}
}

func TestNormalizeFillsDefaultLANCIDRs(t *testing.T) {
	p := sampleParams()
	if err := p.Normalize(); err != nil {
		t.Fatalf("Normalize 失败: %v", err)
	}
	if len(p.LANCIDRs) == 0 {
		t.Fatal("未填充默认局域网网段")
	}
	// 三个私有网段与本机回环都要在内
	for _, want := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8"} {
		found := false
		for _, got := range p.LANCIDRs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("默认网段缺少 %s", want)
		}
	}
}

// 策略路由的 local 路由类型是 TProxy 能工作的前提
func TestPolicyRouteUsesLocalRouteType(t *testing.T) {
	cmds := PolicyRouteCommands(false)
	joined := make([]string, 0, len(cmds))
	for _, c := range cmds {
		joined = append(joined, strings.Join(c, " "))
	}
	all := strings.Join(joined, "\n")

	if !strings.Contains(all, "ip route add local 0.0.0.0/0 dev lo table 100") {
		t.Errorf("缺少 local 类型默认路由，打标包不会被本机接收:\n%s", all)
	}
	if !strings.Contains(all, "ip rule add fwmark 1 table 100") {
		t.Errorf("缺少 fwmark 策略规则:\n%s", all)
	}
	// 未开 IPv6 时不应下发 v6 命令
	if strings.Contains(all, "-6") {
		t.Errorf("未启用 IPv6 却生成了 v6 命令:\n%s", all)
	}
}

func TestPolicyRouteIPv6Optional(t *testing.T) {
	cmds := PolicyRouteCommands(true)
	var hasV6 bool
	for _, c := range cmds {
		if strings.Join(c, " ") == "ip -6 route add local ::/0 dev lo table 100" {
			hasV6 = true
		}
	}
	if !hasV6 {
		t.Error("启用 IPv6 时应生成 v6 的 local 默认路由")
	}
}

// ---- 本机流量接管（output 链）的回归用例 ----
//
// 本机流量在 TProxy 下由 output 链 + 策略路由发夹接管。下面这组用例盯住的
// 是曾经真实存在过的缺陷，每一条都对应一种"规则看着有、实际不起作用"的情形。

// output 链必须按 sport 放行管理端口。
//
// 这是曾经会断 SSH 的缺陷：output 链处理出站包，sshd 对入站 SSH 的回包是
// sport=22、dport=客户端随机端口，只按 dport 放行匹配不到它，回包会被打标
// 经 local 路由当作本机投递而永远出不去。此前 SSH 没断只是因为后面的局域网
// 网段 return 兜住了同网段客户端——一旦 SSH 来源不在那几个私有网段
// （公网跳板、VPN 段），启用 TProxy 的瞬间就会失联。
func TestOutputChainExemptsManagementPortsBySourcePort(t *testing.T) {
	rules, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	out := chainOf(t, rules, "output")

	firstMark := strings.Index(out, "meta mark set")
	if firstMark < 0 {
		t.Fatalf("output 链里没有打标动作:\n%s", out)
	}
	for _, port := range []int{22, 8899, 9090} {
		needle := "tcp sport " + itoa(port) + " return"
		idx := strings.Index(out, needle)
		if idx < 0 {
			t.Errorf("output 链未按源端口放行 %d，本机上该服务的回包会被打标后无法送出:\n%s",
				port, out)
			continue
		}
		if idx > firstMark {
			t.Errorf("端口 %d 的 sport 放行出现在打标之后（%d > %d），回包仍会被劫持",
				port, idx, firstMark)
		}
	}
}

// prerouting 侧相反：入站包的管理端口在目的端口上，必须按 dport 放行。
// 两条链的方向语义不同，这条用例与上一条互为对照，防止"统一"成同一种写法。
func TestPreroutingChainExemptsManagementPortsByDestPort(t *testing.T) {
	rules, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	pre := chainOf(t, rules, "prerouting")
	for _, port := range []int{22, 8899, 9090} {
		if !strings.Contains(pre, "tcp dport "+itoa(port)+" return") {
			t.Errorf("prerouting 链未按目的端口放行 %d:\n%s", port, pre)
		}
	}
}

// 本机 DNS 必须被打标，否则本机流量只能按 IP 分流、域名类规则全部失效。
// 位置有两个硬约束：在局域网 return 之前（否则指向局域网 DNS 的查询被放行），
// 且在 mihomo/面板的 mark return 之后（否则它们自己的查询自环）。
func TestOutputChainHijacksLocalDNS(t *testing.T) {
	rules, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	out := chainOf(t, rules, "output")

	dns := strings.Index(out, "th dport 53")
	if dns < 0 {
		t.Fatalf("output 链没有劫持本机 DNS，域名类规则对本机流量不会生效:\n%s", out)
	}
	lan := strings.Index(out, "ip daddr 192.168.0.0/16 return")
	if lan < 0 {
		t.Fatalf("output 链缺少局域网放行:\n%s", out)
	}
	if dns > lan {
		t.Errorf("DNS 劫持(%d)出现在局域网放行(%d)之后，指向局域网 DNS 的查询会被直接放行",
			dns, lan)
	}
	kernelMark := strings.Index(out, "meta mark 0xff return")
	panelMark := strings.Index(out, "meta mark 0xfe return")
	if kernelMark < 0 || panelMark < 0 {
		t.Fatalf("output 链缺少内核/面板放行:\n%s", out)
	}
	if dns < kernelMark || dns < panelMark {
		t.Errorf("DNS 劫持(%d)必须在内核(%d)与面板(%d)放行之后，否则它们自己的查询会自环",
			dns, kernelMark, panelMark)
	}
	// 回环目标必须排除：mihomo 自己的 DNS 就监听在回环上
	if !strings.Contains(out, "ip daddr != 127.0.0.0/8") {
		t.Errorf("本机 DNS 劫持未排除回环目标，会与 mihomo 自身的 DNS 形成自环:\n%s", out)
	}
}

// 面板自身出站必须放行。不放行的话面板拉订阅、下载内核都会被 mihomo 接管，
// 既绕过了用户对出网方式的显式选择，也让 mihomo 故障时失去恢复手段。
func TestRulesExemptPanelMark(t *testing.T) {
	rules, err := BuildNFTRules(sampleParams())
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if PanelMark == KernelMark {
		t.Fatal("面板与内核的 mark 不能相同，否则无法区分两者的流量")
	}
	for _, chain := range []string{"prerouting", "output"} {
		got := chainOf(t, rules, chain)
		if !strings.Contains(got, "meta mark 0xfe return") {
			t.Errorf("%s 链缺少面板出站放行（meta mark 0x%x return）:\n%s",
				chain, PanelMark, got)
		}
	}
}

// 未启用 v6 时兜底规则必须限定 IPv4。
//
// 表是 inet 家族，不限定的话 v6 包也会被打上 FirewallMark，而此时并没有
// 下发 v6 的 ip rule 与 local ::/0 路由——包被标记后无路可走，成了静默
// 丢包的黑洞。这比"v6 不走代理"糟得多：后者只是不分流，前者是不通。
func TestCatchAllRestrictedToIPv4WhenIPv6Disabled(t *testing.T) {
	p := sampleParams()
	p.EnableIPv6 = false
	rules, err := BuildNFTRules(p)
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	for _, chain := range []string{"prerouting", "output"} {
		got := chainOf(t, rules, chain)
		for _, line := range strings.Split(got, "\n") {
			if !strings.Contains(line, "meta mark set") ||
				!strings.Contains(line, "meta l4proto { tcp, udp }") {
				continue
			}
			// DNS 那几条自带 `ip daddr` / `ip6 daddr`，家族已由地址匹配隐含限定，
			// 不需要也不该再加 nfproto。这里只校验没有任何地址匹配的兜底规则。
			if strings.Contains(line, "dport 53") {
				continue
			}
			if !strings.Contains(line, "meta nfproto ipv4") {
				t.Errorf("%s 链的兜底规则未限定 IPv4，v6 包会被打标却无路由可走:\n%s",
					chain, line)
			}
		}
	}
}

// 启用 v6 时反过来：既要放行 v6 的本地网段，也不该再限定 IPv4，
// 否则 v6 策略路由建了却没有规则会把流量导向它。
func TestIPv6RulesEmittedWhenEnabled(t *testing.T) {
	p := sampleParams()
	p.EnableIPv6 = true
	rules, err := BuildNFTRules(p)
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if strings.Contains(rules, "meta nfproto ipv4") {
		t.Errorf("启用 IPv6 后兜底规则不应再限定 IPv4:\n%s", rules)
	}
	// 回环、链路本地、ULA、组播都要放行，缺了会把邻居发现、DHCPv6、mDNS
	// 这些本地协议一起代理掉
	for _, cidr := range []string{"::1/128", "fe80::/10", "fc00::/7", "ff00::/8"} {
		if !strings.Contains(rules, "ip6 daddr "+cidr+" return") {
			t.Errorf("启用 IPv6 后缺少本地网段放行 %s:\n%s", cidr, rules)
		}
	}
	out := chainOf(t, rules, "output")
	if !strings.Contains(out, "ip6 daddr != ::1/128") {
		t.Errorf("启用 IPv6 后 output 链缺少 v6 DNS 劫持:\n%s", out)
	}
}

// 未启用 v6 时不该出现 v6 规则：多写几条无用规则会让排障时误以为
// v6 已经被接管了。
func TestNoIPv6RulesWhenDisabled(t *testing.T) {
	p := sampleParams()
	p.EnableIPv6 = false
	rules, err := BuildNFTRules(p)
	if err != nil {
		t.Fatalf("生成规则失败: %v", err)
	}
	if strings.Contains(rules, "ip6 daddr") {
		t.Errorf("未启用 IPv6 却生成了 v6 规则:\n%s", rules)
	}
}

// 拆除命令必须与建立命令对应，否则会留下孤立的 ip rule。
//
// 关键在于 v4 与 v6 一律清理，不看"当初有没有启用 v6"：真机测试里，
// 按参数决定清不清 v6 的旧实现在有 IPv6 出网能力的宿主上留下了残留的
// v6 ip rule 与 local ::/0 路由，而日志照样报告"规则已拆除"
// （调用方传的是硬编码的 false）。拆除路径不该依赖记住启用时的参数：
// 进程重启后那个参数根本无从得知。
func TestPolicyRouteTeardownAlwaysCoversBothFamilies(t *testing.T) {
	td := PolicyRouteTeardownCommands()
	all := ""
	for _, c := range td {
		all += strings.Join(c, " ") + "\n"
	}
	for _, want := range []string{
		"ip rule del fwmark 1 table 100",
		"ip route flush table 100",
		"ip -6 rule del fwmark 1 table 100",
		"ip -6 route flush table 100",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("拆除命令缺少 %q（会留下残留的策略路由）:\n%s", want, all)
		}
	}
}

// itoa 避免为了一次转换引入 strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

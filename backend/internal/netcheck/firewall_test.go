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
		DNSPort:    1053,
		KeepPorts:  []int{22, 8899, 9090},
		EnableIPv6: false,
	}
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
		{"tproxy 端口为 0", TProxyParams{TProxyPort: 0, DNSPort: 1053, KeepPorts: []int{22}}},
		{"tproxy 端口越界", TProxyParams{TProxyPort: 70000, DNSPort: 1053, KeepPorts: []int{22}}},
		{"DNS 端口为 0", TProxyParams{TProxyPort: 7893, DNSPort: 0, KeepPorts: []int{22}}},
		// 空 KeepPorts 必须拒绝：没有豁免端口就等于放弃唯一的补救通道
		{"KeepPorts 为空", TProxyParams{TProxyPort: 7893, DNSPort: 1053}},
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

// 拆除命令必须与建立命令对应，否则会留下孤立的 ip rule
func TestPolicyRouteTeardownMatchesSetup(t *testing.T) {
	td := PolicyRouteTeardownCommands(true)
	all := ""
	for _, c := range td {
		all += strings.Join(c, " ") + "\n"
	}
	for _, want := range []string{
		"ip rule del fwmark 1 table 100",
		"ip route flush table 100",
		"ip -6 rule del fwmark 1 table 100",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("拆除命令缺少 %q:\n%s", want, all)
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

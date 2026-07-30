package netcheck

import (
	"fmt"
	"strings"
)

// TProxy 模式所需的防火墙与策略路由参数。
//
// 这些值同时出现在规则文本与 mihomo 配置里，必须一致，因此集中定义：
//   - FirewallMark 打给待代理流量的 fwmark，配合 ip rule 把包引到本机
//   - RouteTable   策略路由表号，放一条 local 默认路由
//   - KernelMark   mihomo 自身出站流量的标记（对应配置里的 routing-mark），
//     规则里必须为它放行，否则内核出站会再次被 TPROXY 捕获形成自环
const (
	FirewallMark = 1
	RouteTable   = 100
	KernelMark   = 0xff

	// NFTTableName 是本项目专用的 nftables 表名。
	//
	// 用独立表（而不是往内置链里插规则）是拆除阶段的关键：删表即完整回收，
	// 不需要逐条比对"哪条是我加的"。绝不执行 `nft flush ruleset` 或
	// `iptables -F`——那会把宿主上 Docker、fail2ban、k8s 的规则一起抹掉。
	NFTTableName = "aurora_tproxy"

	// IPTChainPrefix 是 iptables 回退路径下自建链的前缀，作用同上。
	IPTChainPrefix = "AURORA_TP"
)

// TProxyParams 生成规则所需的运行时参数。
type TProxyParams struct {
	// TProxyPort mihomo 的 tproxy-port
	TProxyPort int
	// DNSPort mihomo DNS 监听端口，用于劫持 UDP/53
	DNSPort int
	// LANCIDRs 局域网网段，这些地址之间的互访不该被代理
	LANCIDRs []string
	// KeepPorts 必须直连的本机端口（SSH、面板、内核 API）。
	// 这是防"锁死自己"的第一道保障：规则里最先为它们放行，
	// 顺序错了就可能在 SSH 会话中把自己关在门外。
	KeepPorts []int
	// EnableIPv6 是否同时下发 IPv6 规则
	EnableIPv6 bool
}

// defaultLANCIDRs 私有网段。用户未指定时用这一组。
var defaultLANCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"255.255.255.255/32",
}

// Normalize 补齐缺省值并做基本校验。
func (p *TProxyParams) Normalize() error {
	if p.TProxyPort <= 0 || p.TProxyPort > 65535 {
		return fmt.Errorf("tproxy 端口非法: %d", p.TProxyPort)
	}
	if p.DNSPort <= 0 || p.DNSPort > 65535 {
		return fmt.Errorf("DNS 端口非法: %d", p.DNSPort)
	}
	if len(p.LANCIDRs) == 0 {
		p.LANCIDRs = append([]string(nil), defaultLANCIDRs...)
	}
	// 面板与内核 API 端口即使调用方没传也要放行：这两个通道断了
	// 就无法在界面上关掉透明代理，只能物理接触主机。
	if len(p.KeepPorts) == 0 {
		return fmt.Errorf("KeepPorts 不能为空，至少需放行 SSH 与面板端口")
	}
	return nil
}

// BuildNFTRules 生成 nftables 规则脚本。
//
// 规则顺序即安全边界，不可调整：
//
//  1. 放行必须直连的管理端口（SSH / 面板 / 内核 API）
//  2. 放行 mihomo 自身出站（按 KernelMark），否则自环
//  3. 放行局域网网段，但 UDP/53 除外（DNS 仍需劫持才能分流）
//  4. 其余 TCP/UDP 交给 TPROXY
//
// 输出可直接喂给 `nft -f -`，也可先用 `nft --check -f -` 干跑校验。
func BuildNFTRules(p TProxyParams) (string, error) {
	if err := p.Normalize(); err != nil {
		return "", err
	}

	var b strings.Builder
	w := func(format string, args ...interface{}) {
		fmt.Fprintf(&b, format+"\n", args...)
	}

	w("# AuroraMihomo 透明代理规则（TProxy）")
	w("# 本表由面板管理，拆除时整表删除。请勿手工往表内添加规则。")
	// 先删同名表再建，使重复应用具备幂等性；表不存在时 delete 会报错，
	// 所以用 add 兜一次——nft 对已存在的 add 是空操作。
	w("add table inet %s", NFTTableName)
	w("delete table inet %s", NFTTableName)
	w("table inet %s {", NFTTableName)

	// prerouting：处理来自局域网其它设备的流量
	w("  chain prerouting {")
	w("    type filter hook prerouting priority mangle; policy accept;")
	for _, port := range p.KeepPorts {
		w("    meta l4proto tcp tcp dport %d return", port)
	}
	w("    meta mark 0x%x return", KernelMark)
	// 已建立的连接由 socket 匹配直接接管，避免重复进入 TPROXY
	w("    socket transparent 1 meta mark set 0x%x accept", FirewallMark)
	w("    udp dport 53 meta mark set 0x%x tproxy to :%d accept", FirewallMark, p.TProxyPort)
	for _, cidr := range p.LANCIDRs {
		if strings.Contains(cidr, ":") {
			continue
		}
		w("    ip daddr %s return", cidr)
	}
	w("    meta l4proto { tcp, udp } meta mark set 0x%x tproxy to :%d accept",
		FirewallMark, p.TProxyPort)
	w("  }")

	// output：处理本机自身发出的流量。
	// hook type 必须是 route，否则改了 mark 也不会重新查路由，
	// 本机 UDP 出站会走不通。
	w("  chain output {")
	w("    type route hook output priority mangle; policy accept;")
	for _, port := range p.KeepPorts {
		w("    meta l4proto tcp tcp dport %d return", port)
	}
	w("    meta mark 0x%x return", KernelMark)
	for _, cidr := range p.LANCIDRs {
		if strings.Contains(cidr, ":") {
			continue
		}
		w("    ip daddr %s return", cidr)
	}
	w("    meta l4proto { tcp, udp } meta mark set 0x%x accept", FirewallMark)
	w("  }")
	w("}")

	return b.String(), nil
}

// PolicyRouteCommands 返回建立策略路由所需的命令（按顺序执行）。
//
// `ip route add local` 里的 local 类型是关键：它让内核把打了标记的包
// 当作发往本机的流量投递，而不是继续转发出去。
func PolicyRouteCommands(enableIPv6 bool) [][]string {
	cmds := [][]string{
		{"ip", "rule", "add", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", fmt.Sprint(RouteTable)},
	}
	if enableIPv6 {
		cmds = append(cmds,
			[]string{"ip", "-6", "rule", "add", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
			[]string{"ip", "-6", "route", "add", "local", "::/0", "dev", "lo", "table", fmt.Sprint(RouteTable)},
		)
	}
	return cmds
}

// PolicyRouteTeardownCommands 返回拆除策略路由的命令。
//
// 全部允许失败：拆除要幂等（可能本来就没加上，或已被别处删掉），
// 任何一条报错都不该阻止后续清理。
func PolicyRouteTeardownCommands(enableIPv6 bool) [][]string {
	cmds := [][]string{
		{"ip", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "route", "flush", "table", fmt.Sprint(RouteTable)},
	}
	if enableIPv6 {
		cmds = append(cmds,
			[]string{"ip", "-6", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
			[]string{"ip", "-6", "route", "flush", "table", fmt.Sprint(RouteTable)},
		)
	}
	return cmds
}

// NFTTeardownCommand 返回删除本项目专用表的命令。
// 只删自己那张表，宿主上其它规则不受影响。
func NFTTeardownCommand() []string {
	return []string{"nft", "delete", "table", "inet", NFTTableName}
}

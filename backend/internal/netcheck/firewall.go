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
//   - PanelMark    面板自身出站流量的标记，理由见下方注释
const (
	FirewallMark = 1
	RouteTable   = 100
	KernelMark   = 0xff

	// PanelMark 是面板（本程序）自身出站流量的 fwmark。
	//
	// 为什么面板需要单独放行：本机流量被接管后，面板拉订阅、查版本、下载
	// 内核这些请求也会被 TPROXY 捕获，于是
	//   - 「优先经由本地 Mihomo 代理出网」那个显式设置被无声地绕过了：
	//     用户明确关掉它想直连，实际却仍然走了 mihomo；
	//   - 更糟的是 mihomo 自己挂掉时面板一并失去出网能力，连重新下载内核
	//     都做不到，等于把恢复手段也一起赔进去了。
	// mihomo 用 routing-mark(KernelMark) 解决同样的问题，这里沿用同一手法。
	//
	// 取 0xfe 紧挨 KernelMark(0xff)：两者语义相邻（都是「本程序族的流量，
	// 不要再抓一遍」），数值相邻便于在 nft 输出里一眼对照。
	PanelMark = 0xfe

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
	// LANCIDRs 局域网网段，这些地址之间的互访不该被代理
	LANCIDRs []string
	// LANCIDRs6 IPv6 侧的免代理网段，仅 EnableIPv6 时使用
	LANCIDRs6 []string
	// KeepPorts 必须直连的本机端口（SSH、面板、内核 API）。
	// 这是防"锁死自己"的第一道保障：规则里最先为它们放行，
	// 顺序错了就可能在 SSH 会话中把自己关在门外。
	KeepPorts []int
	// EnableIPv6 是否同时下发 IPv6 规则。
	//
	// 由调用方按宿主是否真的有 IPv6 出网能力决定（见 Report.HasIPv6Egress）。
	// 为 false 时兜底规则会被限定为仅 IPv4，避免 v6 包被打了标却没有对应的
	// v6 策略路由可走——那会变成静默丢包的黑洞。
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

// defaultLANCIDRs6 IPv6 侧的免代理网段。
//
// 与 v4 的那组一一对应：回环、链路本地、唯一本地地址（v4 私有网段的对应物）、
// 组播。缺了这些会把邻居发现、DHCPv6、mDNS 之类的本地协议一起代理掉，
// 局域网基础功能会出现难以定位的故障。
var defaultLANCIDRs6 = []string{
	"::1/128",
	"fe80::/10",
	"fc00::/7",
	"ff00::/8",
}

// Normalize 补齐缺省值并做基本校验。
func (p *TProxyParams) Normalize() error {
	if p.TProxyPort <= 0 || p.TProxyPort > 65535 {
		return fmt.Errorf("tproxy 端口非法: %d", p.TProxyPort)
	}
	if len(p.LANCIDRs) == 0 {
		p.LANCIDRs = append([]string(nil), defaultLANCIDRs...)
	}
	if len(p.LANCIDRs6) == 0 {
		p.LANCIDRs6 = append([]string(nil), defaultLANCIDRs6...)
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
// 规则顺序即安全边界，不可调整。两条链的共同骨架：
//
//  1. 放行必须直连的管理端口（SSH / 面板 / 内核 API）
//  2. 放行 mihomo 与面板自身的出站（按 KernelMark / PanelMark），否则自环
//  3. 劫持 DNS，必须在局域网放行之前，否则域名类分流失效
//  4. 放行局域网网段
//  5. 其余 TCP/UDP 交给 TPROXY
//
// prerouting 与 output 在第 1、3 步上有本质差异，见各自的注释——
// 这里曾经因为两条链共用同一份"放行 dport"的写法而留下一个会断 SSH 的缺陷。
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

	// prerouting：处理来自局域网其它设备的流量。
	// 这里的包是"进来的"，管理端口出现在目的端口上，所以按 dport 放行。
	w("  chain prerouting {")
	w("    type filter hook prerouting priority mangle; policy accept;")
	for _, port := range p.KeepPorts {
		w("    meta l4proto tcp tcp dport %d return", port)
	}
	w("    meta mark 0x%x return", KernelMark)
	w("    meta mark 0x%x return", PanelMark)
	// 已建立的连接由 socket 匹配直接接管，避免重复进入 TPROXY
	w("    socket transparent 1 meta mark set 0x%x accept", FirewallMark)
	w("    udp dport 53 meta mark set 0x%x tproxy to :%d accept", FirewallMark, p.TProxyPort)
	p.writeLANReturns(w)
	p.writeCatchAll(w, true)
	w("  }")

	// output：处理本机自身发出的流量。
	//
	// hook type 必须是 route，否则改了 mark 也不会重新查路由，
	// 本机 UDP 出站会走不通。
	w("  chain output {")
	w("    type route hook output priority mangle; policy accept;")
	// 这条链上的包是"出去的"，我们自己服务的回包里管理端口是**源**端口
	// （sshd 回包为 sport=22、dport=客户端随机端口）。只按 dport 放行匹配
	// 不到它们，回包会被打标经 local 路由当作本机投递、永远出不去——
	// 此前 SSH 没断只是因为后面的局域网网段 return 兜住了同网段的客户端，
	// 一旦 SSH 来源不在那几个私有网段（公网跳板、VPN 段）就会当场失联。
	for _, port := range p.KeepPorts {
		w("    meta l4proto tcp tcp sport %d return", port)
	}
	// dport 侧同时保留：本机主动连出去的管理端口（例如从本机 ssh 到别的
	// 机器）维持直连，与改动前的行为一致。
	for _, port := range p.KeepPorts {
		w("    meta l4proto tcp tcp dport %d return", port)
	}
	w("    meta mark 0x%x return", KernelMark)
	w("    meta mark 0x%x return", PanelMark)
	// 本机 DNS 也要劫持，否则本机流量只能按 IP 分流、域名类规则全部失效。
	// 必须在局域网放行之前（下一步就会 return 掉指向局域网 DNS 的查询），
	// 也必须在上面两条 mark return 之后（否则 mihomo 与面板自己的查询自环）。
	//
	// 排除回环目标：mihomo 自己的 DNS 就监听在回环上，劫持它等于自环。
	// 代价是 systemd-resolved 那种把 nameserver 指向 127.0.0.53 的机器上
	// 本机域名分流仍不生效——这一点由 Detect() 单独探测并告警，
	// 而不是在这里悄悄地"看着劫持了其实没有"。
	p.writeDNSHijack(w)
	p.writeLANReturns(w)
	p.writeCatchAll(w, false)
	w("  }")
	w("}")

	return b.String(), nil
}

// writeLANReturns 放行局域网网段。
// v6 段只在启用 v6 时写：没有 v6 策略路由的情况下 v6 包压根不会被打标
// （见 writeCatchAll），多写这几条只会让规则更难读。
func (p TProxyParams) writeLANReturns(w func(string, ...interface{})) {
	for _, cidr := range p.LANCIDRs {
		if strings.Contains(cidr, ":") {
			continue
		}
		w("    ip daddr %s return", cidr)
	}
	if !p.EnableIPv6 {
		return
	}
	for _, cidr := range p.LANCIDRs6 {
		if !strings.Contains(cidr, ":") {
			continue
		}
		w("    ip6 daddr %s return", cidr)
	}
}

// writeDNSHijack 打标本机发出的 DNS 查询，交给下游的策略路由送进 mihomo。
//
// 只在 output 链用：prerouting 那边已经有一条直接 tproxy 的 DNS 规则，
// 而 output 链上不能用 tproxy 动作（它只在 prerouting 可用），
// 靠打标 + `ip rule fwmark` 发夹回 prerouting 才能到达 mihomo。
func (p TProxyParams) writeDNSHijack(w func(string, ...interface{})) {
	w("    meta l4proto { tcp, udp } th dport 53 ip daddr != 127.0.0.0/8 "+
		"meta mark set 0x%x accept", FirewallMark)
	if p.EnableIPv6 {
		w("    meta l4proto { tcp, udp } th dport 53 ip6 daddr != ::1/128 "+
			"meta mark set 0x%x accept", FirewallMark)
	}
}

// writeCatchAll 写兜底规则：其余 TCP/UDP 全部交给代理。
//
// 未启用 v6 时必须限定 nfproto ipv4。表是 inet 家族、规则不限定家族的话
// v6 包也会被打上 FirewallMark，而此时并没有下发 v6 的 ip rule 与
// local ::/0 路由——包被标记后无路可走，直接变成静默丢包的黑洞。
// 这比"v6 不走代理"糟糕得多：后者只是不分流，前者是网络不通。
func (p TProxyParams) writeCatchAll(w func(string, ...interface{}), tproxy bool) {
	family := ""
	if !p.EnableIPv6 {
		family = "meta nfproto ipv4 "
	}
	if tproxy {
		w("    %smeta l4proto { tcp, udp } meta mark set 0x%x tproxy to :%d accept",
			family, FirewallMark, p.TProxyPort)
		return
	}
	w("    %smeta l4proto { tcp, udp } meta mark set 0x%x accept", family, FirewallMark)
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
//
// 刻意**不**接受 enableIPv6 参数：拆除永远同时清理 v4 与 v6。
//
// 真机测试发现的问题：原先按 enableIPv6 决定清不清 v6，而调用方
// （TransparentService.disable）传的是硬编码的 false，于是在有 IPv6 出网
// 能力的宿主上，启用时下发的 v6 ip rule 与 local ::/0 路由在关闭后仍然
// 残留，日志却报告"规则已拆除"。这类"拆除依赖记住启用时的参数"的设计
// 天生易错——进程重启、数据库与实际状态不一致时那个参数根本无从得知。
// 既然 v6 的清理命令在没有 v6 规则时只会返回"不存在"（已被视为成功），
// 无条件执行没有代价，却消除了整类残留。
func PolicyRouteTeardownCommands() [][]string {
	return [][]string{
		{"ip", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "route", "flush", "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "route", "flush", "table", fmt.Sprint(RouteTable)},
	}
}

// NFTTeardownCommand 返回删除本项目专用表的命令。
// 只删自己那张表，宿主上其它规则不受影响。
func NFTTeardownCommand() []string {
	return []string{"nft", "delete", "table", "inet", NFTTableName}
}

// NFTRulesCheckCommand 返回探测本项目专用表是否存在的命令。
// 用于启动时核实"数据库记录已启用"与"宿主上规则确实还在"是否一致——
// 见 Applier.RulesActive 的说明。
func NFTRulesCheckCommand() []string {
	return []string{"nft", "list", "table", "inet", NFTTableName}
}

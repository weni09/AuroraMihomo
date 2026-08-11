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
	// DNSPort mihomo 的 DNS 监听端口（config.yaml 里的 dns.listen）。
	//
	// 必须与 TProxyPort 分开：TPROXY 会保留原始目的端口，把 53 的查询投到
	// tproxy-port 上，mihomo 收到的是"目的端口 53 的普通流量"，不会按 DNS 应答，
	// 于是域名解析看起来被劫持了、其实从未被接管（真机实测的现象）。
	// DNS 必须重定向到 mihomo 真正的 DNS 监听端口。
	//
	// 为 0 时回落到 DefaultDNSPort，理由见 Normalize。
	DNSPort int
	// LANCIDRs 局域网网段，这些地址之间的互访不该被代理
	LANCIDRs []string
	// LANCIDRs6 IPv6 侧的免代理网段，仅 EnableIPv6 时使用
	LANCIDRs6 []string
	// KeepPorts 必须直连的本机端口（SSH、面板、内核 API）。
	// 这是防"锁死自己"的第一道保障：规则里最先为它们放行，
	// 顺序错了就可能在 SSH 会话中把自己关在门外。
	// 仅 TCP：这些管理通道都是 TCP，不写 UDP 以免无谓放宽。
	KeepPorts []int
	// ExemptPorts 用户配置的免代理端口（目的端口放行）。
	//
	// 与 KeepPorts 分开而不是混进同一列表：管理端口只放行 TCP，用户端口
	// 必须同时覆盖 TCP 与 UDP（QUIC、游戏、DoQ 等）。混在一起会让 SSH/面板
	// 也写成 UDP return，或让用户端口只剩 TCP——两种都是错的。
	// 规则位置仍在 catch-all 之前（与 KeepPorts 同级），才能真正生效。
	ExemptPorts []int
	// EnableIPv6 是否同时下发 IPv6 规则。
	//
	// 由调用方按宿主是否真的有 IPv6 出网能力决定（见 Report.HasIPv6Egress）。
	// 为 false 时兜底规则会被限定为仅 IPv4，避免 v6 包被打了标却没有对应的
	// v6 策略路由可走——那会变成静默丢包的黑洞。
	EnableIPv6 bool
	// CustomRules 用户自定义防火墙规则（iptables 语法，已由
	// NormalizeCustomRules 规范化为完整命令）。在内置 nft 规则生效后
	// 逐条追加执行；Teardown 时逆序 -D 拆除（仅 -A/-I 形式）。
	CustomRules []string
	// PreviousCustomRules 宿主上"上一批已成功应用"的自定义规则。
	//
	// iptables -A 不幂等：重复 Apply 会叠规则；改 A→B 时若只按新列表
	// 追加，旧 A 会永久留在链里。Apply 在追加 CustomRules 前会先逆序
	// 拆除本列表。首次启用时为空。
	PreviousCustomRules []string
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
	// DNS 端口缺省回落而不是报错：调用方未显式给出时，用本包的默认值
	// （与 Inject 注入 dns.listen 时用的是同一个常量）比拒绝下发更有用。
	// 校验上界仍要做，非法值会让整份规则被 nft 拒绝、一条都不生效。
	if p.DNSPort == 0 {
		p.DNSPort = DefaultDNSPort
	}
	if p.DNSPort < 0 || p.DNSPort > 65535 {
		return fmt.Errorf("dns 端口非法: %d", p.DNSPort)
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
	// 用户免代理端口：TCP+UDP 目的端口都放行（见 ExemptPorts 字段注释）
	p.writeExemptPortReturns(w, true)
	w("    meta mark 0x%x return", KernelMark)
	w("    meta mark 0x%x return", PanelMark)
	// DNS 劫持必须排在 socket 匹配与局域网放行之前。
	//
	// 排在 socket 之前：客户端反复向同一台 DNS 服务器查询时，socket 匹配会
	// 认出那条已有的 UDP "连接"并直接 accept，后面的 DNS 规则就再也见不到
	// 这些包——表现正是"只有第一次查询被接管，之后全部漏出去"。
	//
	// 排在局域网放行之前：客户端的 DNS 通常指向路由器/局域网内的服务器
	// （如 192.168.1.1），而 writeLANReturns 会把去往私有网段的包直接 return。
	// 顺序反了的话，最常见的那种配置下 DNS 劫持完全不生效。
	p.writeDNSHijackPrerouting(w)
	// 已建立的连接由 socket 匹配直接接管，避免重复进入 TPROXY
	w("    socket transparent 1 meta mark set 0x%x accept", FirewallMark)
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
	// 用户免代理端口：本机出站 sport/dport 的 TCP+UDP 一并放行
	p.writeExemptPortReturns(w, false)
	w("    meta mark 0x%x return", KernelMark)
	w("    meta mark 0x%x return", PanelMark)
	// 本机自身的 DNS 查询在这条链上必须**放行**，交给下面的 nat 链改写目的端口。
	//
	// 这里曾经是"打标"，那是错的：打标后包经 `ip rule fwmark` 命中 table 100 的
	// local 路由被本机投递，而目的端口仍是 53，本机没有任何进程监听 53
	// （mihomo 的 DNS 在 dns.listen 指定的高位端口上），于是查询原地超时。
	// 真机现象就是 `communications error to <上游DNS>#53: timed out`。
	//
	// output 链上不能用 tproxy 动作（仅 prerouting 可用），要把包送到另一个端口
	// 只能靠 nat 改写目的地址端口。mangle(-150) 先于 nat dstnat(-100) 执行，
	// 所以这里 return 之后，nat 链才有机会看到这些包。
	p.writeDNSPassthroughForNAT(w)
	p.writeLANReturns(w)
	p.writeCatchAll(w, false)
	w("  }")

	// nat output：把本机自身的 DNS 查询改写到 mihomo 的 DNS 端口。
	//
	// 为什么必须单独一条 nat 链：output 链上没有 tproxy 动作可用，而"送到另一个
	// 端口"只有改写目的地址端口一条路，那是 nat 的能力。mangle 那条链只负责
	// 把 DNS 包放过来（见 writeDNSPassthroughForNAT）。
	//
	// 用 redirect 而不是 dnat：redirect 等价于"改写到本机"，无需写出具体地址，
	// 也就不会因为宿主主 IP 变化而失效。
	w("  chain nat_output {")
	w("    type nat hook output priority dstnat; policy accept;")
	// 管理端口与自身流量的放行必须重复一遍：nat 链是独立的钩子，
	// mangle 链里的 return 对它没有任何约束力。漏掉这些会把 mihomo 自己的
	// 上游 DNS 查询也改写回本机，形成自环——mihomo 查上游、被改写回自己、
	// 再查上游，DNS 彻底不可用。
	w("    meta mark 0x%x return", KernelMark)
	w("    meta mark 0x%x return", PanelMark)
	// 排除回环目标：mihomo 的 DNS 本身就监听在回环上，改写它等于自环。
	// 代价是 systemd-resolved 那种把 nameserver 指向 127.0.0.53 的机器上
	// 本机域名分流仍不生效——这一点由 Detect() 单独探测并告警，
	// 而不是在这里悄悄地"看着劫持了其实没有"。
	w("    meta l4proto { tcp, udp } th dport 53 ip daddr != 127.0.0.0/8 "+
		"redirect to :%d", p.DNSPort)
	if p.EnableIPv6 {
		w("    meta l4proto { tcp, udp } th dport 53 ip6 daddr != ::1/128 "+
			"redirect to :%d", p.DNSPort)
	}
	w("  }")
	w("}")

	return b.String(), nil
}

// writeExemptPortReturns 为用户配置的免代理端口写 return 规则。
//
// prerouting=true：只按 dport 放行（局域网设备访问该端口的流量不进 TPROXY）。
// prerouting=false（output 链）：sport 与 dport 都写——本机服务回包是 sport，
// 本机主动访问对端端口是 dport，与 KeepPorts 在 output 链上的双写一致。
//
// 用 th dport/sport 一次覆盖 TCP+UDP：与 DNS 劫持写法同构，避免 tcp/udp 各写
// 一条导致规则膨胀、读起来像漏写。
//
// 注意端口 53 的不对称：prerouting 里免代理 return 排在 DNS 劫持之前，
// 局域网设备的 53 查询会被直接放行；但本机出站 53 在 mangle output 放行后，
// nat_output 链仍会把它 redirect 到 mihomo 的 DNS 端口——本机豁免 53 并不
// 真正生效（也正好避免用户把面板的 DNS 劫持误关掉）。
func (p TProxyParams) writeExemptPortReturns(w func(string, ...interface{}), prerouting bool) {
	for _, port := range p.ExemptPorts {
		if port <= 0 || port > 65535 {
			continue
		}
		if prerouting {
			w("    meta l4proto { tcp, udp } th dport %d return", port)
			continue
		}
		w("    meta l4proto { tcp, udp } th sport %d return", port)
		w("    meta l4proto { tcp, udp } th dport %d return", port)
	}
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

// writeDNSHijackPrerouting 把局域网设备发来的 DNS 查询直接投给 mihomo。
//
// TCP 与 UDP 都要覆盖。早先这里只有 `udp dport 53`，于是走 TCP 的 DNS 查询
// （响应超过 512 字节被截断后客户端会重试 TCP、DoT 之外的 zone transfer、
// 以及部分把 DNS 固定走 TCP 的客户端）原样漏到上游，域名类分流对它们失效。
// output 链那侧一直是 `{ tcp, udp }`，两条链的口径本就该一致。
//
// 用 th dport 而不是 udp dport：th（transport header）对 TCP/UDP 通用，
// 与 output 链的写法保持一致。
//
// 与 output 链的区别在动作：这里能直接用 tproxy（该动作仅在 prerouting 可用），
// 而 output 链只能打标，再靠 `ip rule fwmark` 发夹回 prerouting。
// 目的端口必须是 mihomo 的 DNS 端口，不是 tproxy-port。
//
// TPROXY 保留原始目的端口：投到 tproxy-port 时 mihomo 看到的是"目的端口 53 的
// 普通 TCP/UDP 流量"，它不会按 DNS 协议应答，于是查询既没被代理也没被解析。
// 真机现象是 nslookup 仍从上游 DNS 拿到被污染的结果——看着像"劫持没生效"，
// 实际是劫持了但送错了门。
func (p TProxyParams) writeDNSHijackPrerouting(w func(string, ...interface{})) {
	w("    meta l4proto { tcp, udp } th dport 53 meta mark set 0x%x tproxy to :%d accept",
		FirewallMark, p.DNSPort)
}

// writeDNSPassthroughForNAT 在 mangle output 链上放行本机的 DNS 查询，
// 把它们留给 nat_output 链改写目的端口。
//
// 这里必须是 return 而不是打标。打标的话包会经 `ip rule fwmark` 命中 table 100
// 的 local 路由被本机投递，但目的端口仍是 53，而本机没有进程监听 53
// （mihomo 的 DNS 在 dns.listen 指定的高位端口上），查询原地超时——
// 真机实测报 `communications error to <上游DNS>#53: timed out`。
//
// 位置同样关键：必须在局域网放行之前（否则指向局域网 DNS 的查询会先被
// return 掉、连 nat 链都到不了），也必须在 KernelMark/PanelMark 之后
// （否则 mihomo 与面板自己的查询会被卷进来）。
func (p TProxyParams) writeDNSPassthroughForNAT(w func(string, ...interface{})) {
	w("    meta l4proto { tcp, udp } th dport 53 ip daddr != 127.0.0.0/8 return")
	if p.EnableIPv6 {
		w("    meta l4proto { tcp, udp } th dport 53 ip6 daddr != ::1/128 return")
	}
}

// writeCatchAll 写兜底规则：其余 TCP/UDP 全部交给代理。
//
// 未启用 v6 时必须限定 nfproto ipv4。表是 inet 家族、规则不限定家族的话
// v6 包也会被打上 FirewallMark，而此时并没有下发 v6 的 ip rule 与
// local ::/0 路由——包被标记后无路可走，直接变成静默丢包的黑洞。
// 这比"v6 不走代理"糟糕得多：后者只是不分流，前者是网络不通。
//
// 一旦限定了 nfproto，tproxy 动作也必须显式写出家族（`tproxy ip to`）。
// nft 会把 "nfproto ipv4 + 不带家族的 tproxy" 判为协议冲突并拒绝**整份**规则：
//
//	Error: conflicting protocols specified: ip vs. unknown.
//	You must specify ip or ip6 family in tproxy statement
//
// 而 Apply 是先 `nft --check` 再下发，校验失败就一条规则都不会生效——
// 表现为"TProxy 开着但完全没接管流量"（真机实测于 Alpine 3.24 / nftables 1.1.6）。
// 启用 v6 时不限定 nfproto，此时 `tproxy to` 不带家族才是合法的。
func (p TProxyParams) writeCatchAll(w func(string, ...interface{}), tproxy bool) {
	family := ""
	// tproxyFamily 与 family 必须同时给或同时不给，理由见上面的注释
	tproxyFamily := ""
	if !p.EnableIPv6 {
		family = "meta nfproto ipv4 "
		tproxyFamily = "ip "
	}
	if tproxy {
		w("    %smeta l4proto { tcp, udp } meta mark set 0x%x tproxy %sto :%d accept",
			family, FirewallMark, tproxyFamily, p.TProxyPort)
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
//
// 注意：Linux 允许同一 selector（fwmark+table）有多条不同 priority 的
// ip rule；`ip rule del` 一次只删一条。调用方应循环执行 del 直到失败
// （见 Applier.Teardown / 开机脚本 purge），本函数只给出单次 del 命令。
func PolicyRouteTeardownCommands() [][]string {
	return [][]string{
		{"ip", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "route", "flush", "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "route", "flush", "table", fmt.Sprint(RouteTable)},
	}
}

// PolicyRouteRuleDeleteCommands 返回需要循环执行直到失败的 ip rule del。
// route flush 不在其中（执行一次即可）。
func PolicyRouteRuleDeleteCommands() [][]string {
	return [][]string{
		{"ip", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
		{"ip", "-6", "rule", "del", "fwmark", fmt.Sprint(FirewallMark), "table", fmt.Sprint(RouteTable)},
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

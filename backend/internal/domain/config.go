package domain

// Config represents the complete structure of a Mihomo configuration file (config.yaml).
//
// 字段选取原则（对应需求「本地支持官方所有参数的表单式配置」）：
//   - 参与合并语义判断（同一性比较、冲突检测、Local/Remote First 策略）的字段，
//     必须显式建模为强类型字段，例如 Proxies/ProxyGroups/Rules/RuleProviders/DNS/TUN/Sniffer。
//   - 其余官方参数（如 listeners、proxy-providers、sub-rules、tls、experimental、ntp 等长尾配置）
//     只需要「本地表单可编辑 + 原样落盘」，不需要合并期的语义处理，因此归入 General 兜底 map
//     （yaml:",inline"）。前端通过「高级参数(YAML)」文本域直接编辑这部分内容。
//   - 常用的系统级标量参数（如 find-process-mode、tcp-concurrent 等）单独建模，
//     便于表单渲染与 Local First 合并（见 engine.MergeDetailed）。
type Config struct {
	Mode       string `yaml:"mode,omitempty"`
	Port       int    `yaml:"port,omitempty"`
	SocksPort  int    `yaml:"socks-port,omitempty"`
	MixedPort  int    `yaml:"mixed-port,omitempty"`
	RedirPort  int    `yaml:"redir-port,omitempty"`
	TProxyPort int    `yaml:"tproxy-port,omitempty"`

	AllowLan         bool     `yaml:"allow-lan,omitempty"`
	BindAddress      string   `yaml:"bind-address,omitempty"`
	Authentication   []string `yaml:"authentication,omitempty"`
	SkipAuthPrefixes []string `yaml:"skip-auth-prefixes,omitempty"`
	LanAllowedIPs    []string `yaml:"lan-allowed-ips,omitempty"`
	LanDisallowedIPs []string `yaml:"lan-disallowed-ips,omitempty"`
	// ipv6 在 mihomo 里默认为 true。必须用指针 + omitempty 区分「显式关闭」
	// 与「未配置」：普通 bool 的 false 会被 omitempty 丢掉，开箱 base 里写的
	// ipv6: false 经合并往返后从运行配置中消失，内核仍按默认开启 IPv6。
	IPv6              *bool  `yaml:"ipv6,omitempty"`
	LogLevel          string `yaml:"log-level,omitempty"`
	FindProcessMode   string `yaml:"find-process-mode,omitempty"`
	GlobalClientFP    string `yaml:"global-client-fingerprint,omitempty"`
	TCPConcurrent     bool   `yaml:"tcp-concurrent,omitempty"`
	UnifiedDelay      bool   `yaml:"unified-delay,omitempty"`
	InterfaceName     string `yaml:"interface-name,omitempty"`
	RoutingMark       int    `yaml:"routing-mark,omitempty"`
	DisableKeepAlive  bool   `yaml:"disable-keep-alive,omitempty"`
	KeepAliveIdle     int    `yaml:"keep-alive-idle,omitempty"`
	KeepAliveInterval int    `yaml:"keep-alive-interval,omitempty"`

	// geodata-mode 官方默认 false；同样用指针保留「显式 false」落盘，
	// 与 IPv6 同一类问题（bool + omitempty 会把显式关闭吞掉）。
	GeodataMode       *bool             `yaml:"geodata-mode,omitempty"`
	GeodataLoader     string            `yaml:"geodata-loader,omitempty"`
	GeositeMatcher    string            `yaml:"geosite-matcher,omitempty"`
	GeoAutoUpdate     bool              `yaml:"geo-auto-update,omitempty"`
	GeoUpdateInterval int               `yaml:"geo-update-interval,omitempty"`
	GeoXURL           map[string]string `yaml:"geox-url,omitempty"`

	ExternalController     string `yaml:"external-controller,omitempty"`
	ExternalControllerTLS  string `yaml:"external-controller-tls,omitempty"`
	ExternalControllerUnix string `yaml:"external-controller-unix,omitempty"`
	ExternalControllerPipe string `yaml:"external-controller-pipe,omitempty"`
	ExternalUI             string `yaml:"external-ui,omitempty"`
	ExternalUIName         string `yaml:"external-ui-name,omitempty"`
	ExternalUIURL          string `yaml:"external-ui-url,omitempty"`
	ExternalDohServer      string `yaml:"external-doh-server,omitempty"`
	Secret                 string `yaml:"secret,omitempty"`

	Profile ProfileConfig          `yaml:"profile,omitempty"`
	Hosts   map[string]interface{} `yaml:"hosts,omitempty"`

	DNS           DNSConfig               `yaml:"dns,omitempty"`
	TUN           TUNConfig               `yaml:"tun,omitempty"`
	Sniffer       SnifferConfig           `yaml:"sniffer,omitempty"`
	Proxies       []Proxy                 `yaml:"proxies,omitempty"`
	ProxyGroups   []ProxyGroup            `yaml:"proxy-groups,omitempty"`
	RuleProviders map[string]RuleProvider `yaml:"rule-providers,omitempty"`
	Rules         []string                `yaml:"rules,omitempty"`

	// General 是兜底 map，承载所有未显式建模的官方参数
	// （listeners / proxy-providers / sub-rules / tls / experimental / ntp / tunnels 等）。
	// 合并时遵循 Local First：本地缺失的键才由远程补齐（见 engine.MergeDetailed）。
	General map[string]interface{} `yaml:",inline"`
}

// ProfileConfig 对应官方 profile 段，控制 select 记忆与 fake-ip 持久化。
type ProfileConfig struct {
	StoreSelected bool `yaml:"store-selected,omitempty"`
	StoreFakeIP   bool `yaml:"store-fake-ip,omitempty"`
}

type Proxy struct {
	Name   string                 `yaml:"name"`
	Type   string                 `yaml:"type"`
	Server string                 `yaml:"server"`
	Port   int                    `yaml:"port"`
	Extra  map[string]interface{} `yaml:",inline"` // captures all other proxy-specific settings
}

type ProxyGroup struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies,omitempty"`
	Use      []string `yaml:"use,omitempty"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
	Strategy string   `yaml:"strategy,omitempty"`

	// Extra 承载官方其余策略组参数（include-all / include-all-proxies /
	// include-all-providers / filter / exclude-filter / exclude-type / lazy /
	// disable-udp / tolerance / timeout / max-failed-times / expected-status /
	// empty-fallback / hidden / icon 等）。
	// 这个兜底是必需的：include-all 或 filter 型策略组一旦丢掉这些字段，
	// 该组就既无 proxies 也无 use，mihomo 会直接拒绝加载整份配置。
	Extra map[string]interface{} `yaml:",inline"`
}

type RuleProvider struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	URL      string `yaml:"url,omitempty"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval,omitempty"`

	// Extra 承载 format / proxy / size-limit / payload / header /
	// path-in-bundle 等官方字段。丢掉 format: mrs 会让内核拿 YAML 解析器
	// 去读二进制规则集；type: inline 丢掉 payload 则规则集直接变空。
	Extra map[string]interface{} `yaml:",inline"`
}

// DNSConfig 建模 dns 段的常用字段。
//
// Extra（yaml 内联）承载所有未显式建模的官方 dns 子字段，例如
// nameserver-policy / proxy-server-nameserver / respect-rules / use-hosts /
// prefer-h3 / direct-nameserver 等。没有它，这些字段会在
// 「解析 -> 合并 -> 生成」的往返中被静默丢弃，直接违背
// 「支持官方所有参数」的需求。
//
// EnhancedMode 等字符串字段带 omitempty：未设置时不应写出
// `enhanced-mode: ""` 这类空值，mihomo 会因非法枚举值拒绝加载。
type DNSConfig struct {
	Enable            bool     `yaml:"enable"`
	IPv6              bool     `yaml:"ipv6"`
	EnhancedMode      string   `yaml:"enhanced-mode,omitempty"`
	FakeIPRange       string   `yaml:"fake-ip-range,omitempty"`
	FakeIPFilter      []string `yaml:"fake-ip-filter,omitempty"`
	Nameserver        []string `yaml:"nameserver,omitempty"`
	Fallback          []string `yaml:"fallback,omitempty"`
	DefaultNameserver []string `yaml:"default-nameserver,omitempty"`

	Extra map[string]interface{} `yaml:",inline"`
}

// TUNConfig 建模 tun 段的常用字段。
// Extra 承载 device / mtu / strict-route / inet4-address /
// route-exclude-address / endpoint-independent-nat 等未建模的官方字段。
type TUNConfig struct {
	Enable bool   `yaml:"enable"`
	Stack  string `yaml:"stack,omitempty"`
	// auto-route / auto-detect-interface 在 mihomo 里默认为 true。
	// 用指针 + omitempty 才能区分「用户显式关掉」与「用户没配」：
	// 若用普通 bool，只开 tun.enable 就会写出 auto-route: false，
	// 结果 TUN 起来了却不接管路由，且不报任何错。
	AutoRoute           *bool    `yaml:"auto-route,omitempty"`
	AutoDetectInterface *bool    `yaml:"auto-detect-interface,omitempty"`
	DNSHijack           []string `yaml:"dns-hijack,omitempty"`

	Extra map[string]interface{} `yaml:",inline"`
}

// SnifferConfig 建模 sniffer 段的常用字段。
// Extra 承载 skip-domain / override-destination / sniffing /
// force-domain / skip-src-address 等未建模的官方字段。
type SnifferConfig struct {
	Enable bool `yaml:"enable"`
	// 同 TUN：这两项在 mihomo 里默认为 true，必须用指针区分
	// 「显式关闭」与「未配置」，否则会静默覆盖掉内核默认值
	ForceDNSMapping *bool                 `yaml:"force-dns-mapping,omitempty"`
	ParsePureIP     *bool                 `yaml:"parse-pure-ip,omitempty"`
	Sniff           map[string]SniffPorts `yaml:"sniff,omitempty"`

	Extra map[string]interface{} `yaml:",inline"`
}

type SniffPorts struct {
	Ports []string `yaml:"ports,omitempty"`
}

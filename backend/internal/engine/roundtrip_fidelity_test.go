package engine

import (
	"strings"
	"testing"
)

// 前端表单写出的字段必须能被 domain.Config 完整接住并原样写回。
// 任何一个字段名对不上（或子结构缺 inline 兜底）都会导致用户配了却不生效，
// 而且不报错 —— 这类静默失效很难被发现，因此用往返测试锁住。
func TestConfigRoundTripKeepsAllFormFields(t *testing.T) {
	e := NewMergeEngine()

	// 覆盖前端表单各分组里代表性的字段，含各子结构里未显式建模、
	// 依赖 inline 兜底的官方参数
	raw := []byte(`mode: rule
log-level: debug
allow-lan: true
bind-address: '*'
ipv6: true
find-process-mode: strict
global-client-fingerprint: chrome
tcp-concurrent: true
unified-delay: true
interface-name: eth0
routing-mark: 6666
disable-keep-alive: true
keep-alive-idle: 15
keep-alive-interval: 30
authentication:
    - user:pass
skip-auth-prefixes:
    - 127.0.0.1/32
lan-allowed-ips:
    - 192.168.1.0/24
lan-disallowed-ips:
    - 192.168.1.5/32
port: 7890
socks-port: 7891
mixed-port: 7892
redir-port: 7893
tproxy-port: 7894
external-controller: 127.0.0.1:9090
external-controller-tls: 127.0.0.1:9443
external-ui: /ui
external-ui-name: zashboard
external-doh-server: /dns-query
secret: s3cret
geodata-mode: true
geodata-loader: standard
geo-auto-update: true
geo-update-interval: 24
geox-url:
    geoip: http://example.com/geoip.dat
    geosite: http://example.com/geosite.dat
profile:
    store-selected: true
    store-fake-ip: true
dns:
    enable: true
    ipv6: true
    enhanced-mode: fake-ip
    fake-ip-range: 198.18.0.1/16
    nameserver:
        - 1.1.1.1
    fallback:
        - 8.8.8.8
    direct-nameserver:
        - 223.5.5.5
    use-hosts: true
    use-system-hosts: true
    prefer-h3: true
    cache-algorithm: lru
    nameserver-policy:
        +.example.com: 1.1.1.1
tun:
    enable: true
    stack: gvisor
    auto-route: true
    auto-detect-interface: true
    dns-hijack:
        - any:53
    mtu: 9000
    endpoint-independent-nat: true
    strict-route: true
sniffer:
    enable: true
    force-dns-mapping: true
    parse-pure-ip: true
    override-destination: true
    sniff:
        HTTP:
            ports:
                - "80"
        TLS:
            ports:
                - "443"
hosts:
    example.com: 1.2.3.4
ntp:
    enable: true
    server: time.apple.com
experimental:
    quic-go-disable-gso: true
`)

	cfg, err := e.LoadAndParse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	got := string(out)

	// 逐个断言关键字段在往返后仍然存在
	mustKeep := []string{
		"mode:", "log-level:", "allow-lan:", "bind-address:", "find-process-mode:",
		"global-client-fingerprint:", "tcp-concurrent:", "unified-delay:",
		"interface-name:", "routing-mark:", "disable-keep-alive:",
		"keep-alive-idle:", "keep-alive-interval:",
		"authentication:", "skip-auth-prefixes:", "lan-allowed-ips:", "lan-disallowed-ips:",
		"port:", "socks-port:", "mixed-port:", "redir-port:", "tproxy-port:",
		"external-controller:", "external-controller-tls:", "external-ui:",
		"external-ui-name:", "external-doh-server:", "secret:",
		"geodata-mode:", "geodata-loader:", "geo-auto-update:", "geo-update-interval:",
		"geox-url:", "profile:", "store-selected:", "store-fake-ip:",
		// DNS
		"enhanced-mode:", "fake-ip-range:", "nameserver:", "fallback:",
		"direct-nameserver:", "use-hosts:", "use-system-hosts:", "prefer-h3:",
		"cache-algorithm:", "nameserver-policy:",
		// TUN（mtu / strict-route 等依赖 inline 兜底）
		"stack:", "auto-route:", "auto-detect-interface:", "dns-hijack:",
		"mtu:", "endpoint-independent-nat:", "strict-route:",
		// Sniffer（sniff 嵌套结构）
		"force-dns-mapping:", "parse-pure-ip:", "override-destination:", "sniff:",
		// 顶层兜底承载的官方参数
		"hosts:", "ntp:", "experimental:", "quic-go-disable-gso:",
	}
	for _, k := range mustKeep {
		if !strings.Contains(got, k) {
			t.Errorf("往返后字段 %q 丢失 —— 用户配了但不会生效", k)
		}
	}

	// 值也不能串位
	for _, kv := range []string{"stack: gvisor", "enhanced-mode: fake-ip", "geodata-loader: standard"} {
		if !strings.Contains(got, kv) {
			t.Errorf("往返后 %q 的值发生变化或丢失", kv)
		}
	}
}

// 策略组与规则集合的官方参数必须经往返保留。
// include-all / filter 丢失会让该组既无 proxies 也无 use，
// format: mrs 丢失会让内核用 YAML 解析器去读二进制规则集 —— 两者都导致加载失败。
func TestProxyGroupAndProviderFieldFidelity(t *testing.T) {
	e := NewMergeEngine()
	raw := []byte(`proxy-groups:
  - name: Auto
    type: url-test
    include-all: true
    filter: "香港|HK"
    exclude-filter: "过期"
    exclude-type: "ss"
    lazy: true
    disable-udp: true
    tolerance: 100
    timeout: 5000
    max-failed-times: 5
    expected-status: "204"
    hidden: false
    icon: "http://example.com/i.png"
rule-providers:
  myset:
    type: http
    behavior: domain
    url: http://example.com/y.mrs
    path: ./y.mrs
    format: mrs
    proxy: DIRECT
    size-limit: 1024
`)
	cfg, err := e.LoadAndParse(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, k := range []string{
		"include-all", "filter", "exclude-filter", "exclude-type", "lazy",
		"disable-udp", "tolerance", "timeout", "max-failed-times",
		"expected-status", "icon",
	} {
		if !strings.Contains(got, k) {
			t.Errorf("策略组字段 %q 往返后丢失", k)
		}
	}
	for _, k := range []string{"format", "size-limit"} {
		if !strings.Contains(got, k) {
			t.Errorf("rule-provider 字段 %q 往返后丢失", k)
		}
	}
}

// mihomo 里 tun.auto-route / auto-detect-interface 与
// sniffer.force-dns-mapping / parse-pure-ip 默认都是 true。
// 用户没配这些项时，生成的配置绝不能写出 false —— 那会静默覆盖内核默认值，
// 表现为 TUN 起来了却不接管路由、嗅探开了却不生效，且不报任何错。
func TestDefaultTrueFieldsNotEmittedAsFalse(t *testing.T) {
	e := NewMergeEngine()

	cfg, err := e.LoadAndParse([]byte("tun:\n  enable: true\nsniffer:\n  enable: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, bad := range []string{
		"auto-route: false",
		"auto-detect-interface: false",
		"force-dns-mapping: false",
		"parse-pure-ip: false",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("用户未配置该项，不应写出 %q（会覆盖内核的 true 默认值）\n产物:\n%s", bad, got)
		}
	}
}

// 用户显式关闭时必须如实写出 false，否则关不掉
func TestExplicitFalseIsPreserved(t *testing.T) {
	e := NewMergeEngine()
	cfg, err := e.LoadAndParse([]byte(
		"ipv6: false\ngeodata-mode: false\n" +
			"tun:\n  enable: true\n  auto-route: false\nsniffer:\n  enable: true\n  parse-pure-ip: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.GenerateYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, "ipv6: false") {
		t.Errorf("顶层 ipv6: false 必须保留（mihomo 默认 true，丢掉等于关不掉），实际产物:\n%s", got)
	}
	if !strings.Contains(got, "geodata-mode: false") {
		t.Errorf("geodata-mode: false 必须保留，实际产物:\n%s", got)
	}
	if !strings.Contains(got, "auto-route: false") {
		t.Errorf("用户显式设为 false 时必须保留，实际产物:\n%s", got)
	}
	if !strings.Contains(got, "parse-pure-ip: false") {
		t.Errorf("用户显式设为 false 时必须保留，实际产物:\n%s", got)
	}
}

// 显式开启也要能正确写出
func TestExplicitTrueIsPreserved(t *testing.T) {
	e := NewMergeEngine()
	cfg, err := e.LoadAndParse([]byte("tun:\n  enable: true\n  auto-route: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := e.GenerateYAML(cfg)
	if !strings.Contains(string(out), "auto-route: true") {
		t.Errorf("显式 true 应保留，实际:\n%s", string(out))
	}
}

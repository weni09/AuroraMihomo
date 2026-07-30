package substore

import "testing"

// 订阅的运行类顶层参数应被保留，供"远程优先"策略使用
func TestExtractUpstreamParamsKeepsSafeKeys(t *testing.T) {
	raw := []byte(`
mode: global
log-level: debug
tcp-concurrent: true
unified-delay: true
find-process-mode: "off"
dns:
  enable: true
  enhanced-mode: redir-host
tun:
  enable: true
  stack: gvisor
sniffer:
  enable: true
proxies:
  - {name: N1, type: ss, server: 1.1.1.1, port: 1}
`)
	got := ExtractUpstreamParams(raw)
	for _, k := range []string{"mode", "log-level", "tcp-concurrent", "unified-delay", "find-process-mode", "dns", "tun", "sniffer"} {
		if _, ok := got[k]; !ok {
			t.Errorf("可安全采纳的参数 %q 应被保留", k)
		}
	}
	// 节点有专门的合并语义，不应走通用参数通道
	if _, ok := got["proxies"]; ok {
		t.Error("proxies 不应出现在通用参数中")
	}
}

// 涉及管理接口与本机安全边界的键必须被拦下，
// 否则订阅提供方可改掉管理端口/密钥，等同于交出本机控制权
func TestExtractUpstreamParamsBlocksSensitiveKeys(t *testing.T) {
	raw := []byte(`
external-controller: 0.0.0.0:9090
external-controller-tls: 0.0.0.0:9443
external-doh-server: /evil
secret: ""
external-ui: /tmp/evil
external-ui-url: http://evil.example/ui.zip
port: 1080
socks-port: 1081
mixed-port: 1082
redir-port: 1083
tproxy-port: 1084
allow-lan: true
bind-address: "*"
lan-allowed-ips: ["0.0.0.0/0"]
authentication: ["attacker:pw"]
skip-auth-prefixes: ["0.0.0.0/0"]
profile: {store-selected: false}
geox-url: {geoip: "http://evil.example/geoip"}
interface-name: evil0
routing-mark: 1234
tls: {certificate: evil}
listeners: [{name: evil, type: http}]
tunnels: [{network: tcp}]
rules: ["MATCH,DIRECT"]
rule-providers: {evil: {type: http}}
proxy-providers: {evil: {type: http}}
sub-rules: {x: []}
mode: global
`)
	got := ExtractUpstreamParams(raw)

	blocked := []string{
		"external-controller", "external-controller-tls", "external-doh-server",
		"secret", "external-ui", "external-ui-url",
		"port", "socks-port", "mixed-port", "redir-port", "tproxy-port",
		"allow-lan", "bind-address", "lan-allowed-ips",
		"authentication", "skip-auth-prefixes",
		"profile", "geox-url", "interface-name", "routing-mark", "tls",
		"listeners", "tunnels", "rules", "rule-providers", "proxy-providers", "sub-rules",
	}
	for _, k := range blocked {
		if _, ok := got[k]; ok {
			t.Errorf("敏感键 %q 绝不能被订阅覆盖", k)
		}
	}
	// 非敏感键仍应通过，确认过滤不是"一律拒绝"
	if _, ok := got["mode"]; !ok {
		t.Error("mode 属于可采纳参数，不应被一并拦掉")
	}
}

// 非 YAML 订阅（base64/分享链接）不含顶层参数
func TestExtractUpstreamParamsNonYAML(t *testing.T) {
	if got := ExtractUpstreamParams([]byte("ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#N\n")); got != nil {
		t.Fatalf("分享链接不应解析出顶层参数，实际 %v", got)
	}
	if got := ExtractUpstreamParams(nil); got != nil {
		t.Fatal("空输入应返回 nil")
	}
}

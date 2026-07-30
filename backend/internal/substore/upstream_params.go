package substore

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// 订阅（远程配置）中禁止被采纳的顶层参数。
//
// 这些键决定管理面板自身的可达性与本机安全边界：一旦允许机场下发的内容
// 覆盖它们，订阅方就能改掉管理端口、清空 API 密钥、开放局域网监听甚至
// 注入代理认证凭据，等同于把本机控制权交给订阅提供方。
// 因此即便用户显式选择"远程优先"，这些键也一律忽略，只保留本地设置。
var forbiddenUpstreamKeys = map[string]bool{
	// 管理接口与凭据
	"external-controller":              true,
	"external-controller-tls":          true,
	"external-controller-unix":         true,
	"external-controller-pipe":         true,
	"external-controller-cors":         true,
	"external-controller-routing-mark": true,
	"external-doh-server":              true,
	"secret":                           true,
	"external-ui":                      true,
	"external-ui-name":                 true,
	"external-ui-url":                  true,
	"tls":                              true,

	// 监听端口与来源准入
	"port":               true,
	"socks-port":         true,
	"mixed-port":         true,
	"redir-port":         true,
	"tproxy-port":        true,
	"allow-lan":          true,
	"bind-address":       true,
	"lan-allowed-ips":    true,
	"lan-disallowed-ips": true,
	"authentication":     true,
	"skip-auth-prefixes": true,

	// 会改变本机文件/进程行为的项
	"profile":        true,
	"geox-url":       true,
	"interface-name": true,
	"routing-mark":   true,

	// 节点/规则等有专门合并语义，不走通用参数通道
	"proxies":         true,
	"proxy-groups":    true,
	"proxy-providers": true,
	"rules":           true,
	"sub-rules":       true,
	"rule-providers":  true,
	"listeners":       true,
	"tunnels":         true,
}

// ExtractUpstreamParams 从订阅原文里提取可以安全采纳的顶层参数。
//
// Sub-Store 管道只关心节点列表，会把订阅的顶层配置整段丢弃；但用户可能希望
// 让订阅下发的 mode / dns / tun / sniffer 等运行参数生效（"远程优先"策略）。
// 这里单独把它们解析出来，并剔除 forbiddenUpstreamKeys 中的敏感键。
//
// 非 YAML 格式（base64 分享链接等）不含顶层参数，返回 nil。
func ExtractUpstreamParams(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var top map[string]interface{}
	if err := yaml.Unmarshal(raw, &top); err != nil || len(top) == 0 {
		return nil
	}

	out := map[string]interface{}{}
	for k, v := range top {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" || forbiddenUpstreamKeys[key] {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

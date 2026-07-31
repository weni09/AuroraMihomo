package substore

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"
)

func RenderTemplate(name string, custom string, nodes []Node) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mihomo-yaml", "clash-yaml":
		y, err := NodesToMihomoYAML(nodes)
		return "mihomo-yaml", y, err
	case "base64-links":
		links := NodesToShareLinks(nodes)
		return "base64-links", base64.StdEncoding.EncodeToString([]byte(links)), nil
	case "share-links":
		return "share-links", NodesToShareLinks(nodes), nil
	case "surge":
		return "surge", NodesToSurge(nodes), nil
	case "surgemac", "surge-mac":
		return "surgemac", NodesToSurgeMac(nodes), nil
	case "loon":
		return "loon", NodesToLoon(nodes), nil
	case "quantumultx", "qx":
		return "quantumultx", NodesToQuantumultX(nodes), nil
	case "sing-box", "singbox":
		out, err := NodesToSingBox(nodes)
		return "sing-box", out, err
	case "v2ray", "v2ray-json":
		out, err := NodesToV2RayJSON(nodes)
		return "v2ray", out, err
	case "json", "plain-json":
		out, err := NodesToPlainJSON(nodes)
		return "json", out, err
	case "stash":
		out, err := NodesToStash(nodes)
		return "stash", out, err
	case "surfboard":
		return "surfboard", NodesToSurfboard(nodes), nil
	case "shadowrocket":
		return "shadowrocket", NodesToShadowrocket(nodes), nil
	case "egern":
		out, err := NodesToEgern(nodes)
		return "egern", out, err
	case "custom":
		out, err := execGoTemplate(custom, nodes)
		return "custom", out, err
	case "noop":
		// 只需要 Engine 跑完管道/去重/改写拿到 nodes，不需要任何渲染结果——
		// 供 RenderFile 的 mihomo 覆写路径使用，避免浪费一次真实渲染。
		return "noop", "", nil
	default:
		// treat name as custom content key fallback
		y, err := NodesToMihomoYAML(nodes)
		return "mihomo-yaml", y, err
	}
}

// goTemplateFuncs 是 Go 模板可用的辅助函数。
//
// 存在的必要性：节点的协议参数存在 Node.Extra 里，其中 ws-opts / reality-opts /
// grpc-opts 等是嵌套结构。模板里若直接 {{ $v }} 输出这类值，Go 会按自己的
// map 打印格式写成 `map[public-key:X short-id:Y]`——这不是合法 YAML，
// 产出的配置会被内核拒绝，且错误发生在用户看不见的渲染阶段。
// 因此必须提供把节点/值序列化成合法 YAML 的能力。
var goTemplateFuncs = template.FuncMap{
	// toYaml 把任意值序列化为 YAML 片段（不带缩进），供 indent 再对齐。
	"toYaml": func(v interface{}) (string, error) {
		s, err := marshalYAML(v)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(s, "\n"), nil
	},
	// indent 给多行文本每行前置 n 个空格，用于把 toYaml 的产物嵌进列表项。
	"indent": func(n int, s string) string {
		pad := strings.Repeat(" ", n)
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if l == "" {
				continue
			}
			lines[i] = pad + l
		}
		return strings.Join(lines, "\n")
	},
	// proxyYaml 把单个节点渲染成 mihomo proxies 列表项所需的完整字段映射，
	// 复用 buildBaseMihomoConfig 的同一套字段规则（含 port<=0 与 udp 的处理），
	// 保证 Go 模板手写的节点与 YAML 覆写模式产出的节点结构一致。
	"proxyYaml": func(n Node) (string, error) {
		s, err := marshalYAML(nodeToProxyMap(n))
		if err != nil {
			return "", err
		}
		return strings.TrimRight(s, "\n"), nil
	},
	// proxiesYaml 一次性渲染整个 proxies 列表（含 "- " 列表项前缀）。
	"proxiesYaml": func(nodes []Node) (string, error) {
		items := make([]map[string]interface{}, 0, len(nodes))
		for _, n := range nodes {
			items = append(items, nodeToProxyMap(n))
		}
		s, err := marshalYAML(items)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(s, "\n"), nil
	},
	// names 取出全部节点名，便于填充策略组的 proxies 成员。
	"names": func(nodes []Node) []string {
		out := make([]string, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, n.Name)
		}
		return out
	},
	// list 把若干参数收成一个切片，供 range 遍历。
	//
	// text/template 没有构造列表的内置函数（内置的 slice 是"对已有切片取子段"，
	// slice "a" "b" 只会静默得到空值）。缺了它，模板想复用一组策略组成员
	// 就只能把整段 YAML 拼成字符串再手写花括号——那样产出的是流式
	// `{name: X, type: select}`，而官方 Sub-Store 产物是块状展开，
	// 两边无法对照。有了 list 就能配合 range 输出块状结构。
	"list": func(items ...interface{}) []interface{} {
		return items
	},
	// fields 按空白切分字符串，配合 list 表达"多列数据"。
	// 例如 range list "a1 AI.list" "a2 CN.list"，再用 fields 拆出名称与文件名，
	// 避免为每个 provider 重复写一遍相同的字段块。
	"fields": strings.Fields,
	// quote 给字符串加双引号并转义，节点名含空格/emoji/特殊字符时必需。
	"quote": func(s string) string {
		out, err := marshalYAML(s)
		if err != nil {
			return `"` + s + `"`
		}
		return strings.TrimRight(out, "\n")
	},
}

// execGoTemplate 把 custom 当 Go text/template 源码解析执行，
// 可用 {{ range .Nodes }} 遍历节点，并可用 goTemplateFuncs 里的辅助函数
// 把节点安全地序列化成合法 YAML。
func execGoTemplate(custom string, nodes []Node) (string, error) {
	if strings.TrimSpace(custom) == "" {
		return "", fmt.Errorf("custom template content is empty")
	}
	tpl, err := template.New("custom").Funcs(goTemplateFuncs).Parse(custom)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, map[string]interface{}{"Nodes": nodes}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// nodeToProxyMap 把单个节点转成 mihomo proxies 列表项的字段映射。
// 由 buildBaseMihomoConfig 与 Go 模板的 proxyYaml/proxiesYaml 共用，
// 避免两条路径对同一节点产出不同字段。
func nodeToProxyMap(n Node) map[string]interface{} {
	item := map[string]interface{}{
		"name":   n.Name,
		"type":   n.Type,
		"server": n.Server,
	}
	// 端口为 0 表示该协议用别的方式指定端口（如 mieru 的 port-range）。
	// 这两者互斥，写出 port: 0 会让内核拒绝加载。
	if n.Port > 0 {
		item["port"] = n.Port
	}
	if n.UDP {
		item["udp"] = true
	}
	for k, v := range n.Extra {
		item[k] = v
	}
	// reality 节点缺 client-fingerprint 时补默认值（见 fingerprint.go）。
	// 必须在拷贝 Extra 之后：上面是整体搬运，缺的字段也会一并缺失。
	applyClientFingerprint(n, item)
	return item
}

// buildBaseMihomoConfig 把节点列表转成一份自动生成的基础 mihomo 配置
// （proxies/proxy-groups/rules），供 NodesToMihomoYAML 直接输出，
// 也供 mihomo 覆写（YAML/JS 模板语言）作为增量修改的底座。
func buildBaseMihomoConfig(nodes []Node) map[string]interface{} {
	proxies := make([]map[string]interface{}, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		proxies = append(proxies, nodeToProxyMap(n))
		names = append(names, n.Name)
	}
	cfg := map[string]interface{}{
		"proxies": proxies,
	}
	// 节点为空时（管道过滤过严、订阅到期等）不能生成成员为空的策略组：
	// mihomo 对 proxies 与 use 同时为空的组会报
	// "'use' or 'proxies' missing" 并拒绝加载整份配置。
	// 此时只输出空 proxies，把"没有可用节点"这个事实如实传下去。
	if len(names) > 0 {
		cfg["proxy-groups"] = []map[string]interface{}{
			{
				"name":    "Proxy",
				"type":    "select",
				"proxies": names,
			},
		}
		cfg["rules"] = []string{"MATCH,Proxy"}
	}
	return cfg
}

func NodesToMihomoYAML(nodes []Node) (string, error) {
	return marshalYAML(buildBaseMihomoConfig(nodes))
}

// NodesToShareLinks 见 sharelink_export.go：
// 各协议的分享链接还原逻辑较多，单独成文件维护。

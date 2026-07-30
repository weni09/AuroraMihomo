package substore

import "gopkg.in/yaml.v3"

// 本文件负责"保序"的 YAML 处理。
//
// 为什么需要它：YAML 覆写此前走 map[string]interface{} 做合并，
// 而 Go 的 map 无序，yaml.Marshal 只能按键名字母序输出。结果是模板里
// `proxies → global-ua → … → rule-providers` 的结构化布局被打散成
// `allow-lan → geo-auto-update → … → unified-delay`，锚点宿主键
// （pr / pr1 / rule-anchor）也被冲到中间，看起来像"没被处理的残留"。
// 官方 Sub-Store 的产物保持模板原序，两边无法对照。
//
// yaml.Node 保留了文档的节点树与顺序，用它做合并即可保序。
// 代价是需要自己处理锚点展开与 flow/block 风格——正好这两件也是要对齐的：
// 官方产物里锚点已展开、且一律块状。

// expandYAMLNode 递归处理节点树，做三件事：
//   - 别名（*x）替换为其指向的真实内容
//   - 合并键（<<: / !!merge <<:）展开成实际字段
//   - 清除锚点定义（&x）并强制块状风格（去掉 {} 与 [] 的流式写法）
//
// 返回新节点，不修改入参（入参可能被多个别名共享，原地改会互相污染）。
func expandYAMLNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	// 别名直接替换成目标内容；目标自身可能还含别名，故继续递归
	if n.Kind == yaml.AliasNode {
		return expandYAMLNode(n.Alias)
	}

	out := *n
	// 锚点定义没有保留价值：引用处都已展开成实际内容
	out.Anchor = ""
	// Style 置零即块状。官方产物里 rule-providers / proxy-groups
	// 都是块状展开，而模板里常写成 {type: http, ...} 流式。
	out.Style = 0

	switch n.Kind {
	case yaml.MappingNode:
		out.Content = expandMappingContent(n.Content)
	case yaml.SequenceNode, yaml.DocumentNode:
		content := make([]*yaml.Node, 0, len(n.Content))
		for _, child := range n.Content {
			content = append(content, expandYAMLNode(child))
		}
		out.Content = content
	}
	return &out
}

// expandMappingContent 处理映射的 key/value 序列，把合并键展开。
//
// 字段顺序对齐官方：自身显式写出的字段保持原位，从锚点合并进来的字段
// 追加在后面。这样 `- {name: X, <<: *pr}` 会渲染成
//
//   - name: X
//     type: select      # 来自锚点
//
// 而不是把 type 顶到 name 前面。
//
// 同名字段以自身为准——这是 YAML 合并键的既有语义：<< 提供的是默认值。
func expandMappingContent(content []*yaml.Node) []*yaml.Node {
	own := make([]*yaml.Node, 0, len(content))
	merged := make([]*yaml.Node, 0)

	for i := 0; i+1 < len(content); i += 2 {
		key, val := content[i], content[i+1]
		if isMergeKey(key) {
			// 合并源可以是单个映射，也可以是映射列表（<<: [*a, *b]）
			for _, m := range mergeSources(val) {
				merged = append(merged, m.Content...)
			}
			continue
		}
		own = append(own, expandYAMLNode(key), expandYAMLNode(val))
	}

	hasOwn := func(name string) bool {
		for i := 0; i < len(own); i += 2 {
			if own[i].Value == name {
				return true
			}
		}
		return false
	}
	// 合并进来的字段去重：既排除被自身覆盖的，也排除多个锚点间的重复
	seen := make(map[string]bool, len(merged)/2)
	result := own
	for i := 0; i+1 < len(merged); i += 2 {
		name := merged[i].Value
		if hasOwn(name) || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, expandYAMLNode(merged[i]), expandYAMLNode(merged[i+1]))
	}
	return result
}

// mihomoTopLevelKeys 是 mihomo 认识的顶层配置键。
//
// 只用于把"锚点脚手架键"与真实配置项区分开（见 anchorScaffoldKeys），
// 不做校验：mihomo 的键会随版本增加，这里漏收一个的后果只是该键不被
// 当作脚手架（保持现状），而误删真实配置的代价要大得多。
var mihomoTopLevelKeys = map[string]bool{
	"port": true, "socks-port": true, "mixed-port": true, "redir-port": true,
	"tproxy-port": true, "authentication": true, "skip-auth-prefixes": true,
	"allow-lan": true, "bind-address": true, "lan-allowed-ips": true,
	"lan-disallowed-ips": true, "mode": true, "log-level": true, "ipv6": true,
	"external-controller": true, "external-controller-tls": true,
	"external-ui": true, "external-ui-name": true, "external-ui-url": true,
	"secret": true, "tcp-concurrent": true, "unified-delay": true,
	"interface-name": true, "routing-mark": true, "global-client-fingerprint": true,
	"global-ua": true, "keep-alive-interval": true, "keep-alive-idle": true,
	"disable-keep-alive": true, "find-process-mode": true, "profile": true,
	"tun": true, "dns": true, "hosts": true, "geodata-mode": true,
	"geodata-loader": true, "geo-auto-update": true, "geo-update-interval": true,
	"geox-url": true, "geosite-matcher": true, "sniffer": true,
	"tls": true, "experimental": true, "ntp": true,
	"proxies": true, "proxy-groups": true, "proxy-providers": true,
	"rules": true, "sub-rules": true, "rule-providers": true,
	"listeners": true, "etag-support": true,
}

// anchorScaffoldKeys 找出"只为定义锚点而存在"的顶层键。
//
// mihomo 配置里复用策略组/规则集模板只能靠 YAML 锚点，而锚点必须挂在某个
// 键上才能书写，于是模板作者会造一个容器键专门放它们：
//
//	pr: &pr {type: fallback, proxies: [...]}      # 给 proxy-groups 用
//	rule-anchor:                                  # 给 rule-providers 用
//	  class: &class {type: http, behavior: classical, ...}
//
// 引用处展开后这些键就没有意义了，而 mihomo 不认识它们——留在输出里既是
// 无效配置，也让用户以为"锚点没被处理掉"。
//
// 判据是结构性的，不认名字（用户可以起任何名字）：
//   - 键名不在 mihomoTopLevelKeys 里，且
//   - 值本身带锚点，或值是映射且其每一个子项都带锚点
//
// 必须在 expandYAMLNode 之前调用——后者会清掉 Anchor 字段。
func anchorScaffoldKeys(doc *yaml.Node) map[string]bool {
	root := doc
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}

	out := make(map[string]bool)
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if mihomoTopLevelKeys[key.Value] {
			continue
		}
		if isAnchorOnly(val) {
			out[key.Value] = true
		}
	}
	return out
}

// isAnchorOnly 判断节点是否纯粹用于承载锚点定义。
func isAnchorOnly(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	// 形如 `pr: &pr {...}`：值自身就是锚点
	if n.Anchor != "" {
		return true
	}
	// 形如 `rule-anchor: {ip: &ip {...}, class: &class {...}}`：
	// 容器本身无锚点，但每个子项都是锚点定义。
	// 要求全部子项都带锚点，避免把混写了真实配置的键整个丢掉。
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i+1].Anchor == "" {
			return false
		}
	}
	return true
}

// dropKeys 从映射节点里移除指定键，返回新节点。
func dropKeys(n *yaml.Node, drop map[string]bool) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode || len(drop) == 0 {
		return n
	}
	out := *n
	content := make([]*yaml.Node, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		if drop[n.Content[i].Value] {
			continue
		}
		content = append(content, n.Content[i], n.Content[i+1])
	}
	out.Content = content
	return &out
}

// isMergeKey 识别合并键。
// yaml.v3 对隐式 `<<:` 与显式 `!!merge <<:` 的标签处理不同，两种都要认：
// 前者 Tag 可能是 !!str 而 Value 为 "<<"，后者 Tag 为 !!merge。
func isMergeKey(key *yaml.Node) bool {
	return key.Tag == "!!merge" || key.Value == "<<"
}

// mergeSources 取出合并键右侧的所有映射节点。
// 支持 `<<: *one` 与 `<<: [*a, *b]` 两种写法。
func mergeSources(val *yaml.Node) []*yaml.Node {
	resolved := expandYAMLNode(val)
	if resolved == nil {
		return nil
	}
	switch resolved.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{resolved}
	case yaml.SequenceNode:
		out := make([]*yaml.Node, 0, len(resolved.Content))
		for _, item := range resolved.Content {
			if item.Kind == yaml.MappingNode {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}

// mergeNodeInto 把 override 深度合并进 base，返回新节点。
//
// 合并语义与此前的 deepMergeInto 保持一致（"+key" 前插、"key+" 追加、
// "key!" 强制覆盖、两侧同为映射则递归）。
//
// 键顺序以 override（模板）为骨架，这是与官方 Sub-Store 对齐的关键：
// 官方产物里 proxies 在最前，其后完全按模板的书写顺序排列。
// 因此先输出 base 独有的键（实际就是系统生成的 proxies / proxy-groups /
// rules），再按模板顺序输出模板写到的键——模板通常自己定义了
// proxy-groups 与 rules，它们在合并中被模板值取代，于是只剩 proxies
// 留在最前，正好就是官方的形态。
func mergeNodeInto(base, override *yaml.Node) *yaml.Node {
	if base == nil || base.Kind != yaml.MappingNode {
		return override
	}
	if override == nil || override.Kind != yaml.MappingNode {
		return base
	}

	// override 里出现过的真实键名（已剥离 + / ! 修饰符）
	overridden := make(map[string]bool, len(override.Content)/2)
	for i := 0; i < len(override.Content); i += 2 {
		key, _ := parseMergeKey(override.Content[i].Value)
		overridden[key] = true
	}

	out := *base
	out.Style = 0
	content := make([]*yaml.Node, 0, len(base.Content)+len(override.Content))
	// 1) base 独有的键，保持 base 内部顺序
	for i := 0; i+1 < len(base.Content); i += 2 {
		if !overridden[base.Content[i].Value] {
			content = append(content, base.Content[i], base.Content[i+1])
		}
	}
	out.Content = content

	// 2) 模板里写到的键，按模板顺序追加（同名项与 base 合并）
	for i := 0; i+1 < len(override.Content); i += 2 {
		rawKey, val := override.Content[i], override.Content[i+1]
		key, mode := parseMergeKey(rawKey.Value)

		existing := nodeMapGet(base, key)
		var merged *yaml.Node
		switch mode {
		case mergeModePrepend:
			merged = concatSeqNodes(val, existing)
		case mergeModeAppend:
			merged = concatSeqNodes(existing, val)
		case mergeModeForce:
			merged = val
		default:
			if existing != nil && existing.Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
				merged = mergeNodeInto(existing, val)
			} else {
				// 标量、列表或类型不匹配：整体覆盖
				merged = val
			}
		}
		nodeMapSet(&out, key, merged)
	}
	return &out
}

// concatSeqNodes 拼接两个列表节点，用于 "+key" 与 "key+" 的前插/追加。
// 任一侧为标量时按单元素列表处理，与 map 版本的 toSlice 行为一致。
func concatSeqNodes(first, second *yaml.Node) *yaml.Node {
	items := make([]*yaml.Node, 0)
	items = append(items, seqItems(first)...)
	items = append(items, seqItems(second)...)
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
}

func seqItems(n *yaml.Node) []*yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.SequenceNode {
		return n.Content
	}
	return []*yaml.Node{n}
}

// nodeMapGet 按键名取映射节点的值，找不到返回 nil。
func nodeMapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// nodeMapSet 设置映射节点的键值：已存在则原地替换（保持原顺序），
// 不存在则追加到末尾。
func nodeMapSet(m *yaml.Node, key string, val *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// toNode 把任意 Go 值转成 yaml.Node，便于与节点树混合操作。
func toNode(v interface{}) (*yaml.Node, error) {
	var n yaml.Node
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, err
	}
	// Unmarshal 出来的是 DocumentNode，取其唯一子节点才是真正的值
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return n.Content[0], nil
	}
	return &n, nil
}

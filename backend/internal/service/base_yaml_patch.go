package service

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// base.yaml 的定点改写工具。
//
// 为什么不能用 domain.Config 做 Unmarshal → 改字段 → Marshal：
// base.yaml 是用户在「配置中心」手写并长期维护的文件，里面有他自己的注释与
// 键顺序。整份结构体往返会：
//   - 丢掉所有注释与空行、重排键顺序；
//   - 把结构体里非 omitempty 的零值实体化成显式配置（例如凭空写出
//     dns.ipv6: false、proxies[].port: 0），而这些用户从未设置过，
//     mihomo 对某些字段会因此改变行为甚至拒绝加载。
//
// 因此这里改用 yaml.Node 直接在文档树上做最小改动：只替换/新增/删除目标键
// 对应的那一个节点，其余节点（含注释）原样保留再序列化回去。
//
// 仍会变化的部分：yaml.v3 重新序列化时缩进固定为 4 空格，且行尾注释的列位置
// 可能对不齐。这是 yaml.v3 的既有行为，不影响语义，也远小于整份结构体往返的
// 破坏面。

// patchBaseYAML 在 base.yaml 文本上按 path 定点写入 value。
//
// path 用点号表示嵌套，例如 "tun.enable"、"tproxy-port"。
// 中间层不存在时会按需创建映射节点。
// value 为 nil 表示删除该键（并逐层清理因此变空的父映射）。
//
// 返回改写后的完整 YAML 文本。原文为空时会生成一份仅含目标键的最小文档。
func patchBaseYAML(src string, path string, value interface{}) (string, error) {
	keys := strings.Split(path, ".")
	if len(keys) == 0 || path == "" {
		return "", fmt.Errorf("path 不能为空")
	}

	var doc yaml.Node
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		// 空配置：造一个空映射作为根，后续按 path 逐层建出来
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	} else {
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			return "", fmt.Errorf("解析 YAML 失败: %w", err)
		}
		if len(doc.Content) == 0 {
			doc.Kind = yaml.DocumentNode
			doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", fmt.Errorf("base 配置的根节点必须是映射，实际 kind=%d", root.Kind)
	}

	if value == nil {
		deleteNodePath(root, keys)
	} else if err := setNodePath(root, keys, value); err != nil {
		return "", err
	}

	out, err := marshalDoc(&doc)
	if err != nil {
		return "", err
	}
	return out, nil
}

// patchBaseYAMLMulti 一次写入多个键，语义与 patchBaseYAML 相同。
//
// 单独提供批量版本是因为开关切换总是成对改动（开 TUN 同时要清 tproxy-port），
// 逐次调用会把文本反复解析/序列化，中间态还可能是"两个模式同时开着"的非法组合。
func patchBaseYAMLMulti(src string, patches map[string]interface{}) (string, error) {
	// 固定顺序遍历，保证同一批改动产出稳定结果，便于测试与 diff 比对
	keys := make([]string, 0, len(patches))
	for k := range patches {
		keys = append(keys, k)
	}
	// 简单排序即可：键数量是个位数
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	cur := src
	for _, k := range keys {
		next, err := patchBaseYAML(cur, k, patches[k])
		if err != nil {
			return "", err
		}
		cur = next
	}
	return cur, nil
}

// findMapEntry 在映射节点里按键名找到「值节点」及其在 Content 中的下标。
// 返回的下标是键节点的位置（值节点为 idx+1）。未找到时返回 -1。
func findMapEntry(m *yaml.Node, key string) (int, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i, m.Content[i+1]
		}
	}
	return -1, nil
}

// setNodePath 沿 keys 向下定位并写入标量值，中间层缺失时创建映射。
func setNodePath(root *yaml.Node, keys []string, value interface{}) error {
	cur := root
	for i := 0; i < len(keys)-1; i++ {
		k := keys[i]
		idx, val := findMapEntry(cur, k)
		if idx < 0 {
			// 中间层不存在：补一个空映射。这里必须新建键值对，
			// 否则后续写入无处落脚
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content, keyNode, valNode)
			cur = valNode
			continue
		}
		if val.Kind != yaml.MappingNode {
			// 用户把该键写成了标量/数组，继续往里写会破坏他的数据，
			// 直接报错让上层把原因透出去
			return fmt.Errorf("配置项 %s 不是映射，无法写入子键 %s",
				strings.Join(keys[:i+1], "."), keys[i+1])
		}
		cur = val
	}

	last := keys[len(keys)-1]
	newVal, err := scalarNode(value)
	if err != nil {
		return err
	}

	idx, existing := findMapEntry(cur, last)
	if idx < 0 {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: last}
		cur.Content = append(cur.Content, keyNode, newVal)
		return nil
	}

	// 就地替换值节点，但保留原有的注释：用户可能在这一行上写了说明，
	// 换个值不该把说明也换掉
	newVal.HeadComment = existing.HeadComment
	newVal.LineComment = existing.LineComment
	newVal.FootComment = existing.FootComment
	cur.Content[idx+1] = newVal
	return nil
}

// deleteNodePath 删除 keys 指向的键，并逐层清理因此变空的父映射。
//
// 清理空父级是必要的：只删 tun.enable 会留下一个空的 `tun: {}`，
// mihomo 对空段的处理不一致，而用户看到的是一个既没内容也删不掉的残留段。
func deleteNodePath(root *yaml.Node, keys []string) {
	// 记录每一层的映射节点，便于自底向上清理
	chain := []*yaml.Node{root}
	cur := root
	for i := 0; i < len(keys)-1; i++ {
		idx, val := findMapEntry(cur, keys[i])
		if idx < 0 || val.Kind != yaml.MappingNode {
			return // 路径不存在，无需删除
		}
		cur = val
		chain = append(chain, cur)
	}

	last := keys[len(keys)-1]
	if idx, _ := findMapEntry(cur, last); idx >= 0 {
		cur.Content = append(cur.Content[:idx], cur.Content[idx+2:]...)
	}

	for i := len(chain) - 1; i > 0; i-- {
		if len(chain[i].Content) != 0 {
			break
		}
		parent := chain[i-1]
		if idx, _ := findMapEntry(parent, keys[i-1]); idx >= 0 {
			parent.Content = append(parent.Content[:idx], parent.Content[idx+2:]...)
		}
	}
}

// scalarNode 把 Go 值转成标量节点。
//
// 只支持开关所需的 bool / int / string：范围收窄是有意的，
// 传进来一个 map 或 slice 说明调用方走错了路径，早报错比写出一份
// 结构诡异的配置好。
func scalarNode(value interface{}) (*yaml.Node, error) {
	n := &yaml.Node{Kind: yaml.ScalarNode}
	switch v := value.(type) {
	case bool:
		n.Tag = "!!bool"
		if v {
			n.Value = "true"
		} else {
			n.Value = "false"
		}
	case int:
		n.Tag = "!!int"
		n.Value = fmt.Sprintf("%d", v)
	case string:
		n.Tag = "!!str"
		n.Value = v
	default:
		return nil, fmt.Errorf("不支持写入的值类型: %T", value)
	}
	return n, nil
}

// marshalDoc 把改动后的文档树序列化回文本，缩进与 mihomo 配置惯例一致。
func marshalDoc(doc *yaml.Node) (string, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("序列化 YAML 失败: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("序列化 YAML 失败: %w", err)
	}
	return sb.String(), nil
}

// readBaseSwitchState 从 base.yaml 文本里读出透明代理相关的开关状态。
//
// 用 yaml.Node 只读取需要的两个键，不做整份结构体解码：base.yaml 里可能有
// 本程序未建模的字段，整份解码在这里既无必要，也会因严格性差异带来解析失败。
func readBaseSwitchState(src string) (tunEnabled bool, tunStack string, tproxyPort int, err error) {
	if strings.TrimSpace(src) == "" {
		return false, "", 0, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return false, "", 0, fmt.Errorf("解析 YAML 失败: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, "", 0, nil
	}
	root := doc.Content[0]

	if _, tunNode := findMapEntry(root, "tun"); tunNode != nil && tunNode.Kind == yaml.MappingNode {
		if _, en := findMapEntry(tunNode, "enable"); en != nil {
			tunEnabled = en.Value == "true"
		}
		if _, st := findMapEntry(tunNode, "stack"); st != nil {
			tunStack = st.Value
		}
	}
	if _, p := findMapEntry(root, "tproxy-port"); p != nil {
		if _, ferr := fmt.Sscanf(p.Value, "%d", &tproxyPort); ferr != nil {
			tproxyPort = 0
		}
	}
	return tunEnabled, tunStack, tproxyPort, nil
}

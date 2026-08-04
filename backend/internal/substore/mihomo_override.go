package substore

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"

	"auroramihomo/backend/internal/model"

	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

// normalizeYAMLFlowColons 修复 yaml.v3 无法解析「flow 映射键后紧跟 [ 或 {」的问题。
//
// 现象：`{type: select, proxies:[A,B]}` 这类紧凑写法 yaml.v3 会报
// "did not find expected ',' or '}'"（报错行号还常比实际行少 1），而 mihomo /
// Clash 生态的真实配置里 `proxies:[` 无空格写法非常普遍——模板作者为省行宽
// 把策略组压成一行。YAML 规范在 flow 上下文里允许 `key:[`（冒号后紧跟 flow
// 指示符即视为键值分隔符），块上下文则必须有空格，所以修复只在 flow 集合
// （{ / [ 深度 > 0）内补一个空格；块上下文与引号内的 `a:[b]` 一律不动，
// 避免把普通标量误改成键值对。
func normalizeYAMLFlowColons(s string) string {
	if !strings.Contains(s, ":[") && !strings.Contains(s, ":{") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s) + 8)
	depth := 0
	var quote byte
	comment := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if comment {
			out.WriteByte(c)
			if c == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			out.WriteByte(c)
			if quote == '"' && c == '\\' && i+1 < len(s) {
				i++
				out.WriteByte(s[i])
			} else if c == quote {
				// 单引号内 '' 是转义的单引号，不能视为字符串结束
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					out.WriteByte(s[i])
					continue
				}
				quote = 0
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			out.WriteByte(c)
		case c == '#' && (i == 0 || isYAMLCommentSep(s[i-1])):
			comment = true
			out.WriteByte(c)
		case c == '{' || c == '[':
			depth++
			out.WriteByte(c)
		case c == '}' || c == ']':
			if depth > 0 {
				depth--
			}
			out.WriteByte(c)
		case c == ':' && depth > 0 && i+1 < len(s) && (s[i+1] == '[' || s[i+1] == '{'):
			out.WriteString(": ")
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// isYAMLCommentSep 判断上一个字节是否为让 `#` 开始注释的分隔：只有前有
// 空白或位于行首时 `#` 才是注释，紧贴其它字符则是标量的一部分。
func isYAMLCommentSep(prev byte) bool {
	switch prev {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// RenderMihomoOverride 是 SubFile.ConfigType=mihomo 时的统一入口：
// 先由 buildBaseMihomoConfig 生成一份自动生成的基础配置（proxies/proxy-groups/rules），
// 再按 lang 决定正文如何在此基础上做增量修改——对齐官方 Sub-Store 的"覆写"概念：
// 模板不是从零手写整份配置，而是对已经含有节点数据的基础配置打补丁。
//
// gotemplate 是个例外：它不基于 buildBaseMihomoConfig，而是把正文整体当
// Go text/template 解析执行，这是本项目最早实现、已有存量数据依赖的写法，
// 保持其独立行为不受本次改造影响。
func RenderMihomoOverride(lang string, content string, nodes []Node) (string, error) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", model.TemplateLangGo:
		return renderGoTemplateOverride(content, nodes)
	case model.TemplateLangYAML:
		return renderYAMLOverride(content, nodes)
	case model.TemplateLangJS:
		return renderScriptOverride(content, nodes)
	default:
		return renderGoTemplateOverride(content, nodes)
	}
}

// renderGoTemplateOverride 执行 Go 模板，再把产出规范化成块状 YAML。
//
// 为什么需要规范化：Go 模板是纯文本替换，模板里写 `a1: {type: http, ...}`
// 就会原样输出流式花括号，而官方 Sub-Store 产物一律是块状展开。
// 模板作者常用花括号把可复用的字段压成一行（text/template 没有构造列表的
// 内置函数，这几乎是唯一的复用手段），因此这个差异非常普遍，
// 光靠"请改模板写法"解决不了存量数据。
//
// 规范化只做形态转换（展开锚点/合并键、去流式花括号、统一 2 空格缩进），
// 不改变任何键值。产出无法解析成 YAML 映射时原样返回：
// 这条路径也承载着历史上"模板自己写全整份配置"的用法，
// 不能因为规范化失败就让原本可用的配置报错。
func renderGoTemplateOverride(content string, nodes []Node) (string, error) {
	out, err := execGoTemplate(content, nodes)
	if err != nil {
		return "", err
	}
	return normalizeYAMLText(out), nil
}

// normalizeYAMLText 把 YAML 文本重新排版成块状、2 空格缩进，并展开锚点。
// 解析失败或结构不是映射时返回原文，绝不因排版需求破坏内容。
func normalizeYAMLText(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	// 先补 flow 中 `key:[` 的空格（yaml.v3 解析不了，见该函数注释），
	// 解析与语义对比都基于补过的文本，避免这里转成块状后又被语义检查拦回
	normalized := normalizeYAMLFlowColons(s)
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(normalized), &doc); err != nil {
		return s
	}
	expanded := expandYAMLNode(&doc)
	if expanded == nil {
		return s
	}
	node := expanded
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) != 1 {
			return s
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return s
	}
	out, err := marshalYAMLNode(node)
	if err != nil {
		return s
	}
	// 规范化前后语义必须一致，否则宁可保留原文
	if !sameYAMLSemantics(normalized, out) {
		return s
	}
	return out
}

// sameYAMLSemantics 判断两份 YAML 文本解析后是否等价。
func sameYAMLSemantics(a, b string) bool {
	var va, vb interface{}
	if err := yaml.Unmarshal([]byte(a), &va); err != nil {
		return false
	}
	if err := yaml.Unmarshal([]byte(b), &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// ValidateTemplateLang 在保存前对模板正文按 lang 做纯语法校验，
// 不做真正渲染（不需要节点数据），让格式错误在保存时就暴露，
// 而不是等到预览或分享直链才发现。content 为空视为合法
// （尚未来得及填写正文，交由已有的"本地内容不能为空"等来源校验处理）。
func ValidateTemplateLang(lang, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", model.TemplateLangGo:
		// 必须与 execGoTemplate 注册同一套 Funcs：Go 模板在 Parse 阶段就会
		// 校验函数名是否已定义，这里若不注册，用了 proxiesYaml 等辅助函数的
		// 模板会在保存时被判为"function not defined"，而实际渲染完全正常。
		if _, err := template.New("custom").Funcs(goTemplateFuncs).Parse(content); err != nil {
			// 措辞以小写起首是 staticcheck ST1005 的要求（错误串会被包裹进
			// 更长的句子）。这里把 "Go" 挪到句中而不是关掉该检查。
			return fmt.Errorf("模板语法错误（Go 模板）: %w", err)
		}
	case model.TemplateLangYAML:
		var v map[string]interface{}
		// flow 中 `key:[` 的紧凑写法 yaml.v3 解析不了，先补空格再校验
		if err := yaml.Unmarshal([]byte(normalizeYAMLFlowColons(content)), &v); err != nil {
			return fmt.Errorf("YAML 覆写内容解析失败: %w", err)
		}
	case model.TemplateLangJS:
		if _, err := goja.Compile("override", content, false); err != nil {
			return fmt.Errorf("JS 脚本语法错误: %w", err)
		}
	}
	return nil
}

// renderYAMLOverride 把正文当 YAML 覆写片段，与基础配置深度合并后输出。
//
// 走 yaml.Node 而非 map[string]interface{}：map 无序，序列化只能按键名
// 字母排序，会把模板里 proxies→…→rule-providers 的结构化布局彻底打散，
// 与官方 Sub-Store 产物无法对照（详见 yamlnode.go 顶部说明）。
// 节点树保留了模板的书写顺序，同时便于在此展开锚点与合并键。
func renderYAMLOverride(content string, nodes []Node) (string, error) {
	base := buildBaseMihomoConfig(nodes)
	content = strings.TrimSpace(content)
	if content == "" {
		return marshalYAML(base)
	}

	var doc yaml.Node
	// flow 中 `key:[` 的紧凑写法 yaml.v3 解析不了，先补空格（见 normalizeYAMLFlowColons）
	if err := yaml.Unmarshal([]byte(normalizeYAMLFlowColons(content)), &doc); err != nil {
		return "", fmt.Errorf("YAML 覆写内容解析失败: %w", err)
	}
	// 必须在 expandYAMLNode 之前采集：后者会清掉 Anchor 字段，
	// 之后就无法分辨哪些顶层键原本只是锚点容器
	scaffold := anchorScaffoldKeys(&doc)
	// 先展开别名/合并键并统一为块状，之后的合并只需处理普通映射
	overrideNode := expandYAMLNode(&doc)
	if overrideNode.Kind == yaml.DocumentNode {
		if len(overrideNode.Content) == 0 {
			return marshalYAML(base)
		}
		overrideNode = overrideNode.Content[0]
	}
	if overrideNode.Kind != yaml.MappingNode {
		return "", fmt.Errorf("YAML 覆写内容必须是一个映射（键值对），实际是 %s", nodeKindName(overrideNode.Kind))
	}
	// 锚点已在各引用处展开，这些容器键（pr / pr1 / rule-anchor 之类）
	// 留下来就是 mihomo 不认识的无效顶层键，也让用户以为锚点没被处理
	overrideNode = dropKeys(overrideNode, scaffold)

	baseNode, err := toNode(base)
	if err != nil {
		return "", err
	}
	merged := mergeNodeInto(baseNode, overrideNode)
	return marshalYAMLNode(merged)
}

func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "列表"
	case yaml.ScalarNode:
		return "标量"
	case yaml.AliasNode:
		return "别名"
	default:
		return "未知结构"
	}
}

// renderScriptOverride 把正文当 function main(config){...return config}，
// 用 goja 执行后拿返回值作为最终配置输出。
func renderScriptOverride(content string, nodes []Node) (string, error) {
	base := buildBaseMihomoConfig(nodes)
	content = strings.TrimSpace(content)
	if content == "" {
		return marshalYAML(base)
	}
	result, err := applyConfigScript(base, content)
	if err != nil {
		return "", err
	}
	return marshalYAML(result)
}

// deepMergeInto 把 override 深度合并进 base（原地修改 base）。
//
// 对齐官方 Sub-Store YAML 覆写的合并语义：
//   - key 本身：标量直接覆盖；两侧都是 map 则递归合并；
//     两侧都是 slice 时默认整体替换（override 的值覆盖 base 的值）
//   - "+key"：把 override 该 key 的 slice 值前插到 base 同名 key 的 slice 前面
//   - "key+"：把 override 该 key 的 slice 值追加到 base 同名 key 的 slice 后面
//   - "key!"：无论类型如何，强制用 override 的值整体覆盖 base 的值（不递归合并）
func deepMergeInto(base, override map[string]interface{}) {
	for rawKey, val := range override {
		key, mode := parseMergeKey(rawKey)
		switch mode {
		case mergeModePrepend:
			base[key] = prependSlice(base[key], val)
		case mergeModeAppend:
			base[key] = appendSlice(base[key], val)
		case mergeModeForce:
			base[key] = val
		default:
			existing, ok := base[key]
			if !ok {
				base[key] = val
				continue
			}
			existingMap, existingIsMap := asStringMap(existing)
			overrideMap, overrideIsMap := asStringMap(val)
			if existingIsMap && overrideIsMap {
				deepMergeInto(existingMap, overrideMap)
				base[key] = existingMap
				continue
			}
			// 标量、slice 或类型不匹配：整体覆盖
			base[key] = val
		}
	}
}

type mergeMode int

const (
	mergeModeReplace mergeMode = iota
	mergeModePrepend
	mergeModeAppend
	mergeModeForce
)

// parseMergeKey 剥离 "+key"/"key+"/"key!" 修饰符，返回真实 key 与合并模式。
func parseMergeKey(rawKey string) (string, mergeMode) {
	switch {
	case strings.HasPrefix(rawKey, "+"):
		return strings.TrimPrefix(rawKey, "+"), mergeModePrepend
	case strings.HasSuffix(rawKey, "+"):
		return strings.TrimSuffix(rawKey, "+"), mergeModeAppend
	case strings.HasSuffix(rawKey, "!"):
		return strings.TrimSuffix(rawKey, "!"), mergeModeForce
	default:
		return rawKey, mergeModeReplace
	}
}

// asStringMap 尝试把 v 转成 map[string]interface{}。
// yaml.v3 解出的嵌套 map 已经是 map[string]interface{}，这里主要兼容
// 万一出现 map[interface{}]interface{} 的历史/边界情况。
func asStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprintf("%v", k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

// toSlice 把任意切片值统一转成 []interface{}。
//
// base 侧的切片来自 Go 代码字面量，可能是具体类型（如 []string），
// override 侧来自 yaml.Unmarshal，总是 []interface{}；
// 只用类型断言只能认出后者，前者会被误判成标量、整体包成单元素切片，
// 导致 rules+: 追加出 [[MATCH,Proxy], DOMAIN,...] 这种错误的嵌套结构。
// 用反射按 Kind 判断才能同时兼容两侧。
func toSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return []interface{}{v}
	}
	out := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func prependSlice(existing, add interface{}) []interface{} {
	return append(toSlice(add), toSlice(existing)...)
}

func appendSlice(existing, add interface{}) []interface{} {
	return append(toSlice(existing), toSlice(add)...)
}

// scriptOverrideTimeout 限制单次覆写脚本的最长执行时间，
// 与 operator.go 的 scriptTimeout 用途一致（节点脚本算子 vs 整配置覆写脚本），
// 两者互相独立，分开定义避免改一处误动另一处的语义。
const scriptOverrideTimeout = 5 * time.Second

// applyConfigScript 用 goja 执行 function main(config){...return config}，
// config 是完整的 mihomo 配置对象，返回值覆盖原配置。
//
// 沙箱执行模式与 operator.go 的 applyScript 一致（超时中断、Interrupt）。
// 注入方式与 operator.go 不同：这里把 config 先编码成 JSON 字符串，
// 在脚本内 JSON.parse 还原——直接 vm.Set 一个含嵌套 slice 的 Go map，
// goja 对嵌套数组的包装不保证 unshift/push 等原地修改能反映到导出结果，
// 经 JSON 序列化后进出脚本的对象是纯 JS 原生对象，读写行为可靠。
func applyConfigScript(config map[string]interface{}, script string) (map[string]interface{}, error) {
	vm := goja.New()

	timer := time.AfterFunc(scriptOverrideTimeout, func() {
		vm.Interrupt("脚本执行超时")
	})
	defer func() {
		timer.Stop()
		vm.ClearInterrupt()
	}()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("序列化 config 失败: %w", err)
	}
	if err := vm.Set("__configJSON__", string(configJSON)); err != nil {
		return nil, fmt.Errorf("注入 config 变量失败: %w", err)
	}

	fullScript := fmt.Sprintf(`
		var config = JSON.parse(__configJSON__);
		%s

		var __result__;
		if (typeof main === 'function') {
			__result__ = main(config);
		} else {
			__result__ = config; // 未定义 main 函数时原样返回，不视为错误
		}
	`, script)

	v, err := vm.RunString(fullScript)
	if err != nil {
		var iErr *goja.InterruptedError
		if errors.As(err, &iErr) {
			return nil, fmt.Errorf("脚本执行超过 %s，已中断", scriptOverrideTimeout)
		}
		return nil, fmt.Errorf("js exception: %w", err)
	}

	var result map[string]interface{}
	if err := vm.ExportTo(v, &result); err != nil {
		return nil, fmt.Errorf("invalid js return type: expected an object (config)")
	}
	return result, nil
}

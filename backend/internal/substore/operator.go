package substore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// OperatorType defines the kind of node manipulator
type OperatorType string

const (
	OpRename       OperatorType = "rename"
	OpFilter       OperatorType = "filter"
	OpFlag         OperatorType = "flag"
	OpSetProperty  OperatorType = "set_property"
	OpScript       OperatorType = "script"
	OpSort         OperatorType = "sort"
	OpRegexSort    OperatorType = "regex_sort"
	OpRegexDelete  OperatorType = "regex_delete"
	OpResolve      OperatorType = "resolve_domain"
	OpUseless      OperatorType = "useless"
	OpRegion       OperatorType = "region"
	OpQuickSetting OperatorType = "quick_setting"
)

// PipelineOperator represents a single step in a Sub-Store pipeline.
type PipelineOperator struct {
	Type    OperatorType           `json:"type"`
	Enabled bool                   `json:"enabled"`
	Payload map[string]interface{} `json:"payload"` // Config data for the operator
}

// ApplyPipeline runs the nodes through a sequence of operators.
//
// 保留这个无 ctx 的签名：调用点众多（含大量测试），且除 resolve_domain
// 之外的算子都是纯内存计算、不需要取消。内部转调带 ctx 的版本。
func ApplyPipeline(nodes []Node, ops []PipelineOperator) ([]Node, error) {
	return ApplyPipelineCtx(context.Background(), nodes, ops)
}

// ApplyPipelineCtx 与 ApplyPipeline 相同，但把 ctx 透传给需要它的算子。
//
// 目前只有 resolve_domain 会发起网络请求（DNS）。它原先用
// context.Background() 自建超时，取消链在此断掉——外层合并被取消后，
// 这一步仍会把每个域名逐个解析完。节点多时累计耗时可观，
// 正好落在关停的等待窗口之外，于是数据库关了它还在跑。
func ApplyPipelineCtx(ctx context.Context, nodes []Node, ops []PipelineOperator) ([]Node, error) {
	var err error
	for _, op := range ops {
		if !op.Enabled {
			continue
		}
		// 算子之间检查取消：单个算子通常很快，但一条管道可能有几十个算子，
		// 其中 resolve_domain 会逐节点发网络请求
		if cerr := ctx.Err(); cerr != nil {
			return nodes, fmt.Errorf("管道已取消: %w", cerr)
		}
		switch op.Type {
		case OpRename:
			nodes, err = applyRename(nodes, op.Payload)
		case OpFilter:
			nodes, err = applyFilter(nodes, op.Payload)
		case OpFlag:
			nodes, err = applyFlag(nodes)
		case OpSetProperty:
			nodes, err = applySetProperty(nodes, op.Payload)
		case OpScript:
			nodes, err = applyScript(nodes, op.Payload)
		case OpSort:
			nodes, err = applySort(nodes, op.Payload)
		case OpRegexSort:
			nodes, err = applyRegexSort(nodes, op.Payload)
		case OpRegexDelete:
			nodes, err = applyRegexDelete(nodes, op.Payload)
		case OpResolve:
			nodes, err = applyResolveDomain(ctx, nodes, op.Payload)
		case OpUseless:
			nodes, err = applyUselessFilter(nodes)
		case OpRegion:
			nodes, err = applyRegionFilter(nodes, op.Payload)
		case OpQuickSetting:
			nodes, err = applyQuickSetting(nodes, op.Payload)
		}
		if err != nil {
			return nodes, fmt.Errorf("operator %s failed: %w", op.Type, err)
		}
	}
	return nodes, nil
}

func applyRename(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	pattern, _ := payload["pattern"].(string)
	replace, _ := payload["replace"].(string)
	if pattern == "" {
		return nodes, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nodes, nil // Skip invalid regex silently per Sub-Store behavior
	}
	for i := range nodes {
		nodes[i].Name = re.ReplaceAllString(nodes[i].Name, replace)
	}
	return nodes, nil
}

// applyFlag 依据节点名推断地区并加上国旗。
// 词典与 region 过滤器共用 regionKeywords，避免两份地区词表漂移；
// 匹配顺序固定（regionOrder），保证同一份订阅多次执行结果一致。
func applyFlag(nodes []Node) ([]Node, error) {
	for i := range nodes {
		name := strings.TrimSpace(nodes[i].Name)
		lowerName := strings.ToLower(name)
		for _, code := range regionOrder {
			flag := regionFlags[code]
			if flag == "" || strings.Contains(name, flag) {
				continue
			}
			if !matchRegion(lowerName, code) {
				continue
			}
			nodes[i].Name = flag + " " + name
			break
		}
	}
	return nodes, nil
}

func applySetProperty(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	for i := range nodes {
		for k, v := range payload {
			// Sub-Store SetProperty allows overriding 'type', 'udp', or extra fields.
			switch k {
			case "type":
				if s, ok := v.(string); ok {
					nodes[i].Type = s
				}
			case "udp":
				if b, ok := v.(bool); ok {
					nodes[i].UDP = b
				} else if s, ok := v.(string); ok && s == "true" {
					nodes[i].UDP = true
				}
			case "port":
				if f, ok := v.(float64); ok {
					nodes[i].Port = int(f)
				}
			default:
				if nodes[i].Extra == nil {
					nodes[i].Extra = make(map[string]interface{})
				}
				nodes[i].Extra[k] = v
			}
		}
	}
	return nodes, nil
}

// scriptTimeout 限制单次用户脚本的最长执行时间
const scriptTimeout = 5 * time.Second

func applyScript(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	scriptCode, _ := payload["script"].(string)
	if strings.TrimSpace(scriptCode) == "" {
		return nodes, nil
	}

	vm := goja.New()

	// 用户脚本可能死循环。公开的分享端点会触发该管道，
	// 无超时保护时反复请求即可打满所有 CPU 核心。
	// 已知限制：当前依赖的 goja 版本未提供内存上限 API（只有 Interrupt），
	// 因此超时中断挡不住 `new Array(1e9)` 这类瞬间耗尽内存的脚本。
	// 缓解措施是脚本算子只对已登录用户开放；若后续 goja 提供
	// SetMemoryLimit，应在此补上内存约束。
	timer := time.AfterFunc(scriptTimeout, func() {
		vm.Interrupt("脚本执行超时")
	})
	defer func() {
		timer.Stop()
		vm.ClearInterrupt()
	}()

	// Convert Go Node slice to generic map slice for Goja manipulation
	jsNodes := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]interface{}{
			"name":   n.Name,
			"type":   n.Type,
			"server": n.Server,
			"port":   n.Port,
			"udp":    n.UDP,
			"source": n.Source,
		}
		for k, v := range n.Extra {
			m[k] = v // Merge extra fields directly into root (like raw YAML)
		}
		jsNodes = append(jsNodes, m)
	}

	// 注入失败意味着脚本拿不到节点数据，继续执行只会得到误导性的空结果
	if err := vm.Set("proxies", jsNodes); err != nil {
		return nil, fmt.Errorf("注入 proxies 变量失败: %w", err)
	}

	// In Sub-Store, the script defines an `operator(proxies, targetPlatform)` function.
	// We inject standard wrapper to call it.
	fullScript := fmt.Sprintf(`
		%s
		
		var __result__;
		if (typeof operator === 'function') {
			__result__ = operator(proxies, 'mihomo');
		} else {
			__result__ = proxies; // Fallback if no function wrapper
		}
	`, scriptCode)

	v, err := vm.RunString(fullScript)
	if err != nil {
		var iErr *goja.InterruptedError
		if errors.As(err, &iErr) {
			return nil, fmt.Errorf("脚本执行超过 %s，已中断", scriptTimeout)
		}
		return nil, fmt.Errorf("js exception: %w", err)
	}

	// Extract back into Node slice
	var resultNodes []map[string]interface{}
	if err := vm.ExportTo(v, &resultNodes); err != nil {
		return nil, fmt.Errorf("invalid js return type: expected array of proxies")
	}

	out := make([]Node, 0, len(resultNodes))
	for _, m := range resultNodes {
		n := Node{
			Extra: make(map[string]interface{}),
		}
		for k, val := range m {
			switch k {
			case "name":
				n.Name, _ = val.(string)
			case "type":
				n.Type, _ = val.(string)
			case "server":
				n.Server, _ = val.(string)
			case "port":
				if f, ok := val.(float64); ok {
					n.Port = int(f)
				} else if i, ok := val.(int64); ok {
					n.Port = int(i)
				}
			case "udp":
				n.UDP, _ = val.(bool)
			case "source":
				n.Source, _ = val.(string)
			default:
				n.Extra[k] = val
			}
		}
		if n.Name != "" && n.Server != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

func applyFilter(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	action, _ := payload["action"].(string)   // "keep" or "drop"
	pattern, _ := payload["pattern"].(string) // regex
	if pattern == "" {
		return nodes, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nodes, nil // Skip silently if regex is bad
	}

	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		match := re.MatchString(n.Name)
		if action == "drop" && match {
			continue // Drop matched
		}
		if action == "keep" && !match {
			continue // Drop unmatched
		}
		out = append(out, n)
	}
	return out, nil
}

// applySort 按节点名排序（Sub-Store: Sort）
// payload.order: asc（默认）| desc
func applySort(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	order, _ := payload["order"].(string)
	desc := strings.EqualFold(order, "desc")

	out := make([]Node, len(nodes))
	copy(out, nodes)
	sort.SliceStable(out, func(i, j int) bool {
		a := strings.ToLower(out[i].Name)
		b := strings.ToLower(out[j].Name)
		if desc {
			return a > b
		}
		return a < b
	})
	return out, nil
}

// applyRegexSort 按关键词顺序排序（Sub-Store: Regex Sort）
// payload.patterns: []string，越靠前优先级越高；未命中的按普通排序追加到末尾
func applyRegexSort(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	raw, _ := payload["patterns"].([]interface{})
	if len(raw) == 0 {
		return applySort(nodes, map[string]interface{}{})
	}

	regexps := make([]*regexp.Regexp, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		re, err := regexp.Compile(s)
		if err != nil {
			continue
		}
		regexps = append(regexps, re)
	}
	if len(regexps) == 0 {
		return applySort(nodes, map[string]interface{}{})
	}

	// rank：命中第 i 个规则则得 i，全都不命中排到最后
	rank := func(n Node) int {
		for i, re := range regexps {
			if re.MatchString(n.Name) {
				return i
			}
		}
		return len(regexps)
	}

	out := make([]Node, len(nodes))
	copy(out, nodes)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		// 同组内回退为普通排序
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// applyRegexDelete 删除节点名中匹配的片段（Sub-Store: Regex Delete）
// 注意：这是「删除名称中的字符」，不是删除节点本身
func applyRegexDelete(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	pattern, _ := payload["pattern"].(string)
	if strings.TrimSpace(pattern) == "" {
		return nodes, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nodes, nil
	}
	for i := range nodes {
		nodes[i].Name = strings.TrimSpace(re.ReplaceAllString(nodes[i].Name, ""))
	}
	return nodes, nil
}

// applyResolveDomain 把节点域名解析为 IP（Sub-Store: Resolve Domain）
// 原始域名保留在 Extra["_origin_server"]，便于回溯
func applyResolveDomain(parent context.Context, nodes []Node, payload map[string]interface{}) ([]Node, error) {
	preferIPv6, _ := payload["ipv6"].(bool)

	timeout := 3 * time.Second
	if v, ok := payload["timeout"].(float64); ok && v > 0 {
		timeout = time.Duration(v) * time.Second
	}

	resolver := &net.Resolver{}
	for i := range nodes {
		host := nodes[i].Server
		if host == "" || net.ParseIP(host) != nil {
			continue // 已是 IP，跳过
		}

		// 派生自 parent 而非 context.Background()：外层取消（进程关停、
		// 请求超时）必须能中断这个逐节点的解析循环
		ctx, cancel := context.WithTimeout(parent, timeout)
		addrs, err := resolver.LookupIPAddr(ctx, host)
		cancel()
		if parent.Err() != nil {
			// 外层已取消：保留剩余节点的原始域名并停止解析。
			// 不返回错误——解析失败本就"保留域名、不阻断管道"，
			// 取消同理，让上层的取消检查去决定是否终止整条管道。
			break
		}
		if err != nil || len(addrs) == 0 {
			continue // 解析失败保留原域名，不阻断整条管道
		}

		var picked string
		for _, a := range addrs {
			isV4 := a.IP.To4() != nil
			if preferIPv6 && !isV4 {
				picked = a.IP.String()
				break
			}
			if !preferIPv6 && isV4 {
				picked = a.IP.String()
				break
			}
		}
		if picked == "" {
			picked = addrs[0].IP.String()
		}

		if nodes[i].Extra == nil {
			nodes[i].Extra = map[string]interface{}{}
		}
		nodes[i].Extra["_origin_server"] = host
		nodes[i].Server = picked
	}
	return nodes, nil
}

// uselessKeywords 常见的无效/信息类节点关键词（Sub-Store: Useless Proxies）
var uselessKeywords = []string{
	"过期", "剩余", "到期", "有效期", "流量", "重置", "官网", "订阅", "群组", "网址",
	"客服", "更新", "距离", "禁止", "免翻", "回国", "邀请", "购买", "续费", "试用",
	"expire", "traffic", "reset", "website", "telegram", "channel", "official",
	"remaining", "renew", "subscribe",
}

// applyUselessFilter 剔除机场塞进订阅的信息类「假节点」
func applyUselessFilter(nodes []Node) ([]Node, error) {
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if !isUselessNode(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// isUselessNode 判定节点是否为机场塞进订阅的信息类「假节点」，
// 供 applyUselessFilter 与 quick_setting 的「过滤非法节点」共用同一套判定，
// 避免两处词典/条件各自维护后逐渐漂移。
func isUselessNode(n Node) bool {
	lower := strings.ToLower(n.Name)
	for _, kw := range uselessKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	// 无有效服务器地址的也视为无效节点
	return n.Server == "" || n.Port == 0
}

// quickSettingReuseTypes 「连接复用」仅对这些协议生效（对齐 Sub-Store）
var quickSettingReuseTypes = map[string]bool{"snell": true, "anytls": true, "trusttunnel": true}

// quickSettingECNTypes 「ECN」仅对这些协议生效
var quickSettingECNTypes = map[string]bool{"tuic": true, "hysteria2": true}

// quickSettingFingerprints 是「常用配置」允许设置的 uTLS 指纹取值，
// 取自 mihomo component/tls/utls.go 的 fingerprints 表中非废弃的那些。
//
// 刻意不含 "none"：内核把 none 视为不启用 uTLS，对 reality 节点等同于
// 选了个会让它连不上的值；而且非空值会绕过 resolveClientFingerprint 的兜底。
// 也不含 chrome120/safari16 等历史指纹与 deprecated 项，避免用户误选。
var quickSettingFingerprints = map[string]bool{
	"chrome": true, "firefox": true, "safari": true, "ios": true,
	"android": true, "edge": true, "random": true,
}

// quickSettingIPVersionMap 把面板枚举翻译成 mihomo 实际接受的 ip-version 取值
var quickSettingIPVersionMap = map[string]string{
	"dual":      "dual",
	"v4-only":   "ipv4",
	"v6-only":   "ipv6",
	"prefer-v4": "ipv4-prefer",
	"prefer-v6": "ipv6-prefer",
}

// quickSettingTriState 解析三态字符串：ENABLED/DISABLED 生效，其余（含 DEFAULT、空值、缺省）视为「不改」
func quickSettingTriState(v interface{}) (value bool, changed bool) {
	s, _ := v.(string)
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ENABLED":
		return true, true
	case "DISABLED":
		return false, true
	default:
		return false, false
	}
}

// applyQuickSetting 对齐 Sub-Store 节点操作面板的「常用配置」快捷开关组。
// 九个字段合并为同一个算子，与官方的 Quick Setting Operator 数据结构一致，
// 不适用的协议直接跳过对应字段，不报错、不影响其它字段。
func applyQuickSetting(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	// 过滤非法节点：先于逐节点字段调整执行，命中的节点整条剔除
	if enabled, changed := quickSettingTriState(payload["useless"]); changed && enabled {
		out := make([]Node, 0, len(nodes))
		for _, n := range nodes {
			if isUselessNode(n) || n.Port <= 0 || n.Port > 65535 {
				continue
			}
			out = append(out, n)
		}
		nodes = out
	}

	udpVal, udpChanged := quickSettingTriState(payload["udp"])
	scertVal, scertChanged := quickSettingTriState(payload["scert"])
	tfoVal, tfoChanged := quickSettingTriState(payload["tfo"])
	aeadVal, aeadChanged := quickSettingTriState(payload["aead"])
	reuseVal, reuseChanged := quickSettingTriState(payload["reuse"])
	ecnVal, ecnChanged := quickSettingTriState(payload["ecn"])

	blockQuic, _ := payload["block_quic"].(string)
	blockQuicChanged := blockQuic == "auto" || blockQuic == "on" || blockQuic == "off"

	ipVersion, ipVersionChanged := quickSettingIPVersionMap[fmt.Sprint(payload["ip_version"])]

	clientFP, _ := payload["client_fingerprint"].(string)
	clientFP = strings.TrimSpace(clientFP)
	// 只接受内核认识的取值：拼错的指纹（mihomo 仅警告 "wrong clientFingerprint"
	// 后当作未设置）会让 reality 节点连不上，且因为字段非空还会绕过
	// resolveClientFingerprint 的兜底，比不填更糟
	clientFPChanged := quickSettingFingerprints[clientFP]

	for i := range nodes {
		typ := strings.ToLower(nodes[i].Type)
		if nodes[i].Extra == nil {
			nodes[i].Extra = map[string]interface{}{}
		}

		if udpChanged {
			nodes[i].UDP = udpVal
			nodes[i].Extra["udp"] = udpVal
		}
		if scertChanged {
			nodes[i].Extra["skip-cert-verify"] = scertVal
		}
		if tfoChanged {
			nodes[i].Extra["tfo"] = tfoVal
			nodes[i].Extra["fast-open"] = tfoVal
		}
		if aeadChanged && typ == "vmess" {
			// Sub-Store 的 mihomo producer 在 aead=true 时把 alterId 强制置 0
			// （AEAD 模式下 alterId 恒为 0），aead=false 时不回填 alterId，
			// 保持原值不动；两种情况都不需要在最终配置里保留 aead 这个中间字段。
			if aeadVal {
				nodes[i].Extra["alterId"] = 0
			}
		}
		if reuseChanged && quickSettingReuseTypes[typ] {
			nodes[i].Extra["reuse"] = reuseVal
		}
		if blockQuicChanged {
			nodes[i].Extra["block-quic"] = blockQuic
		}
		if ecnChanged && quickSettingECNTypes[typ] {
			nodes[i].Extra["ecn"] = ecnVal
		}
		if ipVersionChanged {
			nodes[i].Extra["ip-version"] = ipVersion
		}
		if clientFPChanged && clientFingerprintTypes[typ] {
			nodes[i].Extra["client-fingerprint"] = clientFP
		}
	}
	return nodes, nil
}

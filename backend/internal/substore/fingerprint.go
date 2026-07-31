package substore

// uTLS 客户端指纹（client-fingerprint）的补全规则。
//
// 为什么必须补：mihomo 的 reality 握手强依赖 uTLS，而 uTLS 又只能由
// client-fingerprint 指定，内核侧没有任何默认值——
// component/tls/utls.go 的 GetFingerprint 对空串直接返回"未启用"，
// 随后 transport/vmess/tls.go 就地报
// "REALITY is based on uTLS, please set a client-fingerprint"，
// 整个节点不可用。sing-box 同样报 "uTLS is required by reality client"。
//
// 而机场下发的 reality 节点里这个字段经常是缺的（分享链接的 fp 参数可选），
// 于是"订阅能拉到、节点也在列表里、一连就失败"，用户无从判断原因。
// 因此对 reality 节点在输出阶段补一个默认指纹，与官方 Sub-Store 的
// mihomo producer 同一策略（proxy['reality-opts'] && !proxy['client-fingerprint']
// 时补 chrome）。
//
// 刻意不用 global-client-fingerprint 兜底：该配置在 mihomo v1.19.29 已被移除，
// 内核只打一行 error 日志、不生效，写了等于没写。

// defaultClientFingerprint 是 reality 节点缺指纹时补上的值。
// 取 chrome 而非 random：random 每个进程随机挑一个，同一份配置在不同
// 机器/重启后行为不一致，排查连接问题时无法复现；chrome 也是官方
// Sub-Store 的选择，与各类现成模板的产物一致。
const defaultClientFingerprint = "chrome"

// clientFingerprintTypes 是 mihomo 真正接受 client-fingerprint 的协议
// （核对 v1.19.29 的 adapter/outbound/*.go 中 proxy:"client-fingerprint" 标签）。
// vless/vmess/trojan 走 reality 或普通 TLS，ss/snell/anytls 则是
// shadow-tls / restls 系的包装需要它。
//
// 用途是给用户显式设置指纹时做协议门控：写到不认识这个键的协议上
// 虽不会被内核拒绝（其结构解码器忽略多余键），但会让产出的配置里
// 多出一批无意义字段，干扰用户比对。
var clientFingerprintTypes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true,
	"ss": true, "shadowsocks": true, "snell": true, "anytls": true,
}

// realityEnabled 判断节点是否真的启用了 reality。
//
// 判据是 public-key 非空，而不是"reality-opts 这个键存在"：
// mihomo 的 RealityOptions.Parse()（adapter/outbound/reality.go）在
// PublicKey 为空时返回 nil config，即 reality 并未生效，
// 此时补指纹只会凭空多出一个字段。上游偶尔会下发
// reality-opts: {} 这种空壳，必须区分开。
func realityEnabled(n Node) bool {
	ro, ok := n.Extra["reality-opts"].(map[string]interface{})
	if !ok {
		return false
	}
	return mapString(ro, "public-key") != ""
}

// resolveClientFingerprint 返回节点最终应使用的 client-fingerprint。
//
// 三种情况：
//   - 已有值（来自订阅原文或用户在管道里显式设置）：原样返回，不覆盖用户意图，
//     即便是 "none" 也照旧——那是用户明确表达"不要 uTLS"
//   - reality 已启用但值为空：返回默认指纹，否则节点必然连不上
//   - 其余：返回空串，表示不需要这个字段
func resolveClientFingerprint(n Node) string {
	if fp := extraString(n, "client-fingerprint"); fp != "" {
		return fp
	}
	if realityEnabled(n) {
		return defaultClientFingerprint
	}
	return ""
}

// applyClientFingerprint 把补全后的指纹写进已构建好的输出字段映射。
//
// 供各"整体拷贝 Extra"的导出器（mihomo proxies、Stash）在拷贝完成后调用：
// 那些导出器把 Extra 原样搬进结果，缺失的字段自然也一并缺失，
// 因此补全必须发生在拷贝之后。
func applyClientFingerprint(n Node, item map[string]interface{}) {
	if fp := resolveClientFingerprint(n); fp != "" {
		item["client-fingerprint"] = fp
	}
}

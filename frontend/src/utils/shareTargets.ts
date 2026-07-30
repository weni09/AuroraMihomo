/**
 * 外部分享链接的输出格式选项。
 *
 * 单条订阅与组合订阅共用这一份：两者在后端走的是同一条 RenderByToken 路径，
 * 支持的 target 完全相同，之前两个页面各自硬编码一份列表，导致订阅页少了
 * surgemac / v2ray / json / surfboard / egern 五种格式——同样的能力在一个
 * 页面能选、另一个页面选不到。
 *
 * 取值为空串表示不追加 target 参数，由后端按默认的 mihomo-yaml 输出。
 * 其余取值会被后端 ResolveTarget 归一化（如 qx -> quantumultx、
 * plain -> share-links），故此处沿用简短别名即可。
 */
export interface ShareTargetOption {
  value: string
  label: string
}

export const shareTargetOptions: ShareTargetOption[] = [
  { value: '', label: '默认（Mihomo YAML）' },
  { value: 'clash', label: 'Clash / Mihomo' },
  { value: 'base64', label: 'Base64 订阅' },
  { value: 'plain', label: '明文分享链接' },
  { value: 'surge', label: 'Surge' },
  { value: 'surgemac', label: 'Surge Mac' },
  { value: 'loon', label: 'Loon' },
  { value: 'qx', label: 'QuantumultX' },
  { value: 'singbox', label: 'sing-box' },
  { value: 'v2ray', label: 'V2Ray' },
  { value: 'json', label: '纯 JSON' },
  { value: 'stash', label: 'Stash' },
  { value: 'surfboard', label: 'Surfboard' },
  { value: 'shadowrocket', label: 'Shadowrocket' },
  { value: 'egern', label: 'Egern' },
]

/**
 * 拼出外部分享链接。
 *
 * token 为空表示分享已撤销，此时返回空串，由调用方据此隐藏整块链接区域——
 * 否则会拼出以 /share/ 结尾的地址，看着能复制，实际访问必定 404。
 */
export const buildShareUrl = (origin: string, token: string, target?: string): string => {
  if (!token) return ''
  const base = `${origin}/api/v1/share/${token}`
  return target ? `${base}?target=${target}` : base
}

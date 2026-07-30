// 处理管道算子在两层之间的转换：
//   UI 模型   payload 为对象，region/regex_sort 用多行文本便于编辑
//   后端契约  payload 为 JSON 字符串，region/regex_sort 需要字符串数组
const toLines = (text: string | undefined): string[] =>
  (text || '').split('\n').map(s => s.trim()).filter(Boolean)

/** UI 模型 → 后端契约 */
export function toBackendOperators(ops: any[]): any[] {
  return (ops || []).map(op => {
    let payload: any = {}
    if (op.type === 'set_property' && op.payload?.key) {
      let val: any = op.payload.value
      if (op.payload.valType === 'boolean') val = val === 'true'
      if (op.payload.valType === 'number') val = Number(val)
      payload[op.payload.key] = val
    } else if (op.type === 'set_property') {
      payload = {}
    } else if (op.type === 'region') {
      payload = { action: op.payload?.action || 'keep', regions: toLines(op.payload?.regionsText) }
    } else if (op.type === 'regex_sort') {
      payload = { patterns: toLines(op.payload?.patternsText) }
    } else {
      payload = op.payload || {}
    }
    return { type: op.type, enabled: op.enabled !== false, payload: JSON.stringify(payload) }
  })
}

/** 后端契约 → UI 模型 */
export function toUiOperators(ops: any[]): any[] {
  return (ops || []).map(op => {
    let payload: any = {}
    try {
      payload = op.payload ? JSON.parse(op.payload) : {}
    } catch {
      payload = {}
    }
    if (op.type === 'region') {
      payload = { action: payload.action || 'keep', regionsText: (payload.regions || []).join('\n') }
    } else if (op.type === 'regex_sort') {
      payload = { patternsText: (payload.patterns || []).join('\n') }
    } else if (op.type === 'set_property') {
      const key = Object.keys(payload)[0] || ''
      const raw = key ? payload[key] : ''
      payload = {
        key,
        value: String(raw ?? ''),
        valType: typeof raw === 'boolean' ? 'boolean' : typeof raw === 'number' ? 'number' : 'string',
      }
    }
    return { type: op.type, enabled: op.enabled !== false, payload }
  })
}

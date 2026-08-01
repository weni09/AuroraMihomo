import { loadYaml, dumpYaml, stripYamlComments } from './yaml'
import { baseConfigSchema } from '../schemas/baseConfig'

/**
 * 按「键」与「业务标识」对齐的配置语义对比。
 *
 * 为什么不能只靠逐行文本对比：本地配置是用户手写的几十行，最终配置是合并
 * 产物的上千行，两侧规模悬殊是**合法的**（最终配置真的多出上千条规则）。
 * 逐行算法在这种输入上只会产出一个横跨上千行的巨块，等于说「整份都是新增」，
 * 而真正要回答的问题——「我设的键还在不在」「哪个键被合并改写了」——一条都
 * 看不到。规范化时按字典序排键还会让「仅左侧有」与「仅右侧有」的键在同一
 * 行号相遇，把互不相关的键摆在一起对比，制造出虚假配对。
 *
 * 所以这里换一个粒度：先按顶层键对齐，集合类键再按业务标识（节点名、规则
 * matcher）对齐。对齐规则与后端 engine.BuildDiff（backend/internal/engine/merge.go）
 * 保持一致，否则同一个「改了什么」在前后端会有两套说法。
 */

/** 单个条目/键的变化方向 */
export type DiffStatus = 'added' | 'removed' | 'changed' | 'same'

/** 集合类键（proxies/rules 等）内部的一条明细 */
export interface EntryDiff {
  /** 业务标识：节点名、策略组名、provider 名，或规则的 matcher */
  name: string
  status: DiffStatus
  /** 左右两侧的紧凑呈现，changed 时两者都有值 */
  left: string
  right: string
}

/** 一个顶层键的对比结果 */
export interface KeyDiff {
  /** 配置里的原始键名，如 `proxy-groups`。用户是拿它去对 YAML 的 */
  key: string
  /** 中文标签，取自 baseConfigSchema；无对应项时等于 key */
  label: string
  status: DiffStatus
  /** 紧凑摘要，如 `76 项` / `true` / `8 键` */
  leftBrief: string
  rightBrief: string
  /** 该键子树的 YAML 原文，供下钻做逐行对比——限定到单个键后两侧规模才可比 */
  leftText: string
  rightText: string
  /**
   * 明细条目。
   * 集合类键按业务标识对齐后逐条列出；其余键展开成叶子路径逐个比值。
   * status 为 same 时为空数组：没有可说的差异，列出来只是噪声。
   */
  entries: EntryDiff[]
}

export interface ConfigDiff {
  keys: KeyDiff[]
  /** 各状态的键数量，供界面直接显示汇总 */
  counts: Record<DiffStatus, number>
}

/**
 * 顶层键 → 中文标签。
 *
 * 复用配置中心的表单元信息，避免同一个键在两个页面有两种叫法。
 * 两处来源：
 *   - FormSection.id 恰好等于顶层键的分组（dns/tun/sniffer/proxy-groups/rules）
 *   - 不含点号的 FormField.key，即标量顶层键（mode/allow-lan/port…）
 *
 * 含点号的 field key（如 `geox-url.geoip`）是子路径，顶层标签由下面的
 * EXTRA_LABELS 或键名兜底，不能直接拿子字段的 title 当顶层键的名字。
 */
const buildLabelMap = (): Map<string, string> => {
  const map = new Map<string, string>()
  for (const section of baseConfigSchema) {
    // 分组 id 与顶层键同名时（dns/tun/sniffer/rules…）用分组标题
    map.set(section.id, section.title)
    for (const field of section.fields) {
      if (!field.key.includes('.')) map.set(field.key, field.title)
    }
  }
  // 表单里刻意没有专属入口、但配置里一定会出现的键（见 schemas/baseConfig.ts
  // 中 advancedExcludedKeys 的注释：它们走「高级参数」兜底框）
  for (const [key, label] of Object.entries(EXTRA_LABELS)) map.set(key, label)
  return map
}

const EXTRA_LABELS: Record<string, string> = {
  proxies: '节点 (Proxies)',
  'proxy-groups': '策略组 (Proxy Groups)',
  'rule-providers': '规则集 (Rule Providers)',
  'proxy-providers': '节点集合 (Proxy Providers)',
  rules: '路由规则 (Rules)',
  // hosts 不在此列：它已有专属表单字段（key 为 hosts，无点号），
  // 标签由 buildLabelMap 从 baseConfigSchema 取到，在这里重复定义会盖掉它
  listeners: '入站监听 (Listeners)',
  'sub-rules': '子规则 (Sub Rules)',
  experimental: '实验性参数',
  tunnels: '隧道 (Tunnels)',
  ntp: 'NTP 校时',
  tls: 'TLS',
  'global-ua': '全局 User-Agent',
}

const labelMap = buildLabelMap()

/**
 * 集合类键的对齐方式。
 *
 * 这些键的元素有业务标识，按标识对齐才能区分「节点被改了」与「节点被换掉了」。
 * 其余键没有这种标识，按路径逐个比值即可。
 */
type CollectionKind = 'named-list' | 'keyed-map' | 'rules'

const COLLECTION_KINDS: Record<string, CollectionKind> = {
  proxies: 'named-list',
  'proxy-groups': 'named-list',
  'proxy-providers': 'keyed-map',
  'rule-providers': 'keyed-map',
  rules: 'rules',
}

/**
 * 对比两份配置文本。
 *
 * 任一侧无法解析时返回 null，而不是退回某种「尽力而为」的结果：
 * 语义对比建立在解析产物上，解析不了就没有可对齐的结构，
 * 硬凑出来的结论会误导人。调用方据此提示用户并落回逐行文本对比。
 *
 * 空文本按空配置处理（而非解析失败）：尚未生成过最终配置是正常状态，
 * 此时「本地的键全都还没生效」本身就是有用的结论。
 */
export const buildConfigDiff = (leftText: string, rightText: string): ConfigDiff | null => {
  const left = parseConfig(leftText)
  const right = parseConfig(rightText)
  if (left === null || right === null) return null

  const allKeys = new Set([...Object.keys(left), ...Object.keys(right)])
  const out: KeyDiff[] = [...allKeys].map((key) =>
    diffKey(key, left[key], right[key], key in left, key in right),
  )

  // 先算出各键的状态再排序：状态是由明细条目反推的（见 diffKey），
  // 排序时若另算一遍会与显示出来的状态不一致。
  out.sort((a, b) =>
    STATUS_ORDER[a.status] !== STATUS_ORDER[b.status]
      ? STATUS_ORDER[a.status] - STATUS_ORDER[b.status]
      : a.key.localeCompare(b.key),
  )

  const counts: Record<DiffStatus, number> = { added: 0, removed: 0, changed: 0, same: 0 }
  for (const k of out) counts[k.status]++

  return { keys: out, counts }
}

/**
 * 解析为顶层键映射。空文档视为空配置，解析失败返回 null。
 *
 * 非映射的文档（比如顶层是数组或纯标量）同样按 null 处理：mihomo 配置必须是
 * 映射，走到这里说明这份文本不是配置文件，按键对齐无从下手。
 */
const parseConfig = (text: string): Record<string, unknown> | null => {
  if (!text.trim()) return {}
  // 纯注释文档必须在 loadYaml 之前挡掉：js-yaml 对没有实际内容的文档**抛异常**
  // 而不是返回 undefined，落到下面的 catch 会被当成「这份配置写坏了」，
  // 于是整个语义对比被判为不可用。
  if (stripYamlComments(text) === '') return {}
  try {
    const parsed = loadYaml(text)
    if (parsed === null || parsed === undefined) return {}
    if (typeof parsed !== 'object' || Array.isArray(parsed)) return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

/**
 * 键的排列顺序：有差异的排在前面，原样保留的垫在后面。
 *
 * 不用字典序：一份配置里绝大多数键是没变的，字典序会把两三条真问题
 * 均匀撒在几十行「原样保留」之间，用户还得自己找。
 */
const STATUS_ORDER: Record<DiffStatus, number> = { changed: 0, removed: 1, added: 2, same: 3 }

const diffKey = (
  key: string,
  leftValue: unknown,
  rightValue: unknown,
  inLeft: boolean,
  inRight: boolean,
): KeyDiff => {
  const base = {
    key,
    label: labelMap.get(key) ?? key,
    leftBrief: inLeft ? brief(leftValue) : '',
    rightBrief: inRight ? brief(rightValue) : '',
    leftText: inLeft ? subtreeText(leftValue) : '',
    rightText: inRight ? subtreeText(rightValue) : '',
  }

  if (inLeft && !inRight) return { ...base, status: 'removed', entries: [] }
  if (!inLeft && inRight) return { ...base, status: 'added', entries: [] }

  // 完全相同时直接短路，省去对上千条规则再走一遍对齐
  if (deepEqual(leftValue, rightValue)) return { ...base, status: 'same', entries: [] }

  /*
   * 否则由明细条目反推状态，而不是就此判定为 changed。
   *
   * 因为对齐规则本身会消掉一部分差异：节点名的大小写、规则分段的多余空白
   * 都不构成「变了」（对齐后端 normalizeKey / normalizeRule）。若直接判 changed，
   * 这类配置会显示成「已改写」却一条明细都列不出来——用户点开看到空列表，
   * 只会以为页面坏了。让状态与明细同源，两者就不会互相打脸。
   */
  const entries = diffEntries(key, leftValue, rightValue)
  if (!entries.length) return { ...base, status: 'same', entries: [] }
  return { ...base, status: 'changed', entries }
}

/** 按键的性质选择对齐策略 */
const diffEntries = (key: string, left: unknown, right: unknown): EntryDiff[] => {
  const kind = COLLECTION_KINDS[key]
  if (kind === 'rules' && Array.isArray(left) && Array.isArray(right)) {
    return diffRules(left as unknown[], right as unknown[])
  }
  if (kind === 'named-list' && Array.isArray(left) && Array.isArray(right)) {
    return diffNamedList(left as unknown[], right as unknown[])
  }
  if (kind === 'keyed-map' && isPlainRecord(left) && isPlainRecord(right)) {
    return diffKeyedMap(left, right)
  }
  return diffByPath(left, right)
}

/**
 * 按 name 对齐的列表（proxies / proxy-groups）。
 *
 * 与后端 diffProxies / diffProxyGroups 同构：以 name 建索引，
 * 缺失即新增、存在但值不同即改写、剩余的旧项即删除。
 * 名称比较走 normalizeKey，使 "HK01" 与 " hk01 " 视为同一节点
 * （对齐后端 engine.normalizeKey，见 internal/engine/normalize.go）。
 *
 * 无 name 的元素退回按位置比：这类元素没有身份，位置是唯一线索。
 */
const diffNamedList = (left: unknown[], right: unknown[]): EntryDiff[] => {
  const out: EntryDiff[] = []
  const leftByName = new Map<string, unknown>()
  const leftOrder: string[] = []
  const anonymousLeft: unknown[] = []

  for (const item of left) {
    const name = nameOf(item)
    if (name === null) {
      anonymousLeft.push(item)
      continue
    }
    const k = normalizeKey(name)
    if (!leftByName.has(k)) leftOrder.push(k)
    leftByName.set(k, item)
  }

  const seen = new Set<string>()
  const anonymousRight: unknown[] = []
  for (const item of right) {
    const name = nameOf(item)
    if (name === null) {
      anonymousRight.push(item)
      continue
    }
    const k = normalizeKey(name)
    seen.add(k)
    // 摘要不取名字：条目自己的 name 已经显示了，再重复一遍是噪声。
    // 这里描述"内容长什么样"才是补充信息。
    if (!leftByName.has(k)) {
      out.push({ name, status: 'added', left: '', right: briefBody(item) })
    } else if (!sameElement(leftByName.get(k), item)) {
      // 改写的条目直接点名变了哪些字段：`7 项参数 → 7 项参数` 什么也没说明
      const prev = leftByName.get(k)
      out.push({
        name,
        status: 'changed',
        left: fieldSummary(prev, item),
        right: fieldSummary(item, prev),
      })
    }
  }

  for (const k of leftOrder) {
    if (!seen.has(k)) {
      const item = leftByName.get(k)
      out.push({ name: nameOf(item) ?? k, status: 'removed', left: briefBody(item), right: '' })
    }
  }

  out.push(...diffPositional(anonymousLeft, anonymousRight))
  return out
}

/** 按 map 键对齐（rule-providers / proxy-providers），与后端 diffRuleProviders 同构 */
const diffKeyedMap = (
  left: Record<string, unknown>,
  right: Record<string, unknown>,
): EntryDiff[] => {
  const out: EntryDiff[] = []
  for (const [name, value] of Object.entries(right)) {
    if (!(name in left)) {
      out.push({ name, status: 'added', left: '', right: briefBody(value) })
    } else if (!deepEqual(left[name], value)) {
      // 与 diffNamedList 同理：点名变了哪些字段，而不是笼统给规模
      out.push({
        name,
        status: 'changed',
        left: fieldSummary(left[name], value),
        right: fieldSummary(value, left[name]),
      })
    }
  }
  for (const [name, value] of Object.entries(left)) {
    if (!(name in right)) out.push({ name, status: 'removed', left: briefBody(value), right: '' })
  }
  return out
}

/**
 * 规则按 matcher 对齐，而非整行字符串。
 *
 * 直接比整行会把 `DOMAIN,x,DIRECT` → `DOMAIN,x,PROXY` 报成「删一条 + 加一条」
 * 两条互不相关的记录，而它实际是一条规则改了目标策略。
 * 桶消费法（同 matcher 的旧规则排成队列，命中即消费）保证重复 matcher
 * 也不会串位——这是后端 diffRules 的做法，此处保持一致。
 */
const diffRules = (left: unknown[], right: unknown[]): EntryDiff[] => {
  const out: EntryDiff[] = []
  const buckets = new Map<string, string[]>()
  const order: string[] = []

  for (const raw of left) {
    const rule = normalizeRule(String(raw))
    const key = normalizeKey(matcherOf(rule))
    if (!buckets.has(key)) {
      buckets.set(key, [])
      order.push(key)
    }
    buckets.get(key)!.push(rule)
  }

  for (const raw of right) {
    const rule = normalizeRule(String(raw))
    const key = normalizeKey(matcherOf(rule))
    const bucket = buckets.get(key)

    if (bucket && bucket.length) {
      // 完全相同的行视为未变化，消费掉以免被后续误判为新增/删除
      const exact = bucket.indexOf(rule)
      if (exact >= 0) {
        bucket.splice(exact, 1)
        continue
      }
      // 同 matcher 但目标不同：一条规则改了策略
      const old = bucket.shift()!
      out.push({ name: matcherOf(rule), status: 'changed', left: old, right: rule })
      continue
    }

    out.push({ name: matcherOf(rule), status: 'added', left: '', right: rule })
  }

  // 未被消费的旧规则即被删除的
  for (const key of order) {
    for (const rule of buckets.get(key) ?? []) {
      out.push({ name: matcherOf(rule), status: 'removed', left: rule, right: '' })
    }
  }
  return out
}

/**
 * 其余键：递归展开成叶子路径逐个比值。
 *
 * 这样 dns 段里究竟是哪个字段变了能落到 `nameserver[0]` 这种精度，
 * 而不是笼统地说「dns 被改写」——后者用户还得自己去两份文件里翻。
 */
const diffByPath = (left: unknown, right: unknown): EntryDiff[] => {
  const out: EntryDiff[] = []
  walkPaths('', left, right, out)
  return out
}

/**
 * 深度上限。
 *
 * 配置里的 experimental / listeners 等兜底段结构不可控，理论上可以很深；
 * 展开到十几层的路径（`a.b.c.d.e…`）在界面上已经没法读了，不如到此为止
 * 报成整块改写，让用户点进去看该键的全文对比。
 */
const MAX_PATH_DEPTH = 6

const walkPaths = (path: string, left: unknown, right: unknown, out: EntryDiff[]): void => {
  if (deepEqual(left, right)) return

  const depth = path ? path.split(/[.[]/).length : 0
  const bothRecords = isPlainRecord(left) && isPlainRecord(right)
  const bothArrays = Array.isArray(left) && Array.isArray(right)

  if (depth >= MAX_PATH_DEPTH || (!bothRecords && !bothArrays)) {
    out.push({
      name: path || '(整个键)',
      status: statusFromPresence(left, right),
      left: left === undefined ? '' : brief(left),
      right: right === undefined ? '' : brief(right),
    })
    return
  }

  if (bothRecords) {
    for (const key of new Set([...Object.keys(left), ...Object.keys(right)])) {
      const child = path ? `${path}.${key}` : key
      walkPaths(child, pick(left, key), pick(right, key), out)
    }
    return
  }

  // 两侧都是数组：按下标比。
  // 数组刻意不排序也不按内容配对——proxies/rules 之外的数组（nameserver、
  // fallback…）顺序本身有语义，重排会把「顺序变了」这个真实差异掩盖掉。
  const len = Math.max((left as unknown[]).length, (right as unknown[]).length)
  for (let i = 0; i < len; i++) {
    walkPaths(`${path}[${i}]`, (left as unknown[])[i], (right as unknown[])[i], out)
  }
}

/** 按下标对齐的位置比对，用于无 name 的匿名元素 */
const diffPositional = (left: unknown[], right: unknown[]): EntryDiff[] => {
  const out: EntryDiff[] = []
  const len = Math.max(left.length, right.length)
  for (let i = 0; i < len; i++) {
    if (deepEqual(left[i], right[i])) continue
    out.push({
      name: `[${i}]`,
      status: statusFromPresence(left[i], right[i]),
      left: left[i] === undefined ? '' : brief(left[i]),
      right: right[i] === undefined ? '' : brief(right[i]),
    })
  }
  return out
}

/**
 * 由「两侧是否存在」判定方向。
 *
 * 用 undefined 表示缺失有个已知边界：配置里显式写 `key:`（值为 null）与
 * 不写这个键，在这里都会落到 removed/added。两者对 mihomo 的效果也基本
 * 一致（都不提供有效值），差异不值得为此引入一层包装。
 */
const statusFromPresence = (left: unknown, right: unknown): DiffStatus => {
  if (left === undefined) return 'added'
  if (right === undefined) return 'removed'
  return 'changed'
}

const pick = (obj: Record<string, unknown>, key: string): unknown =>
  key in obj ? obj[key] : undefined

/** 取元素的业务标识；没有 name 字段时返回 null，交给位置比对 */
const nameOf = (item: unknown): string | null => {
  if (!isPlainRecord(item)) return null
  const name = item.name
  return typeof name === 'string' && name.trim() !== '' ? name.trim() : null
}

/**
 * 已按 name 对齐的两个元素是否算「没变」。
 *
 * 比较前把 name 归一到 NormalizeName 的形态（NFC + 去首尾空白 + 折叠内部
 * 空白），与后端 NormalizeConfig 在合并前的清洗一致——纯空白差异是订阅源
 * 的书写噪声，不该报成节点被改。
 *
 * 但**大小写保留**，与后端一致：NormalizeConfig 只做 NormalizeName，大小写
 * 折叠仅用于同一性判断（normalizeKey）。名字是策略组 proxies 列表和面板里
 * 引用节点的字面量，`HK-01` 改成 `hk-01` 是用户看得见的真实变化。
 */
const sameElement = (a: unknown, b: unknown): boolean => {
  if (!isPlainRecord(a) || !isPlainRecord(b)) return deepEqual(a, b)
  return deepEqual(withNormalizedName(a), withNormalizedName(b))
}

const withNormalizedName = (item: Record<string, unknown>): Record<string, unknown> => {
  if (typeof item.name !== 'string') return item
  return { ...item, name: normalizeName(item.name) }
}

/** 规则的 matcher：最后一个逗号之前的部分（与后端 parseRuleParts 一致） */
const matcherOf = (rule: string): string => {
  const idx = rule.lastIndexOf(',')
  return idx < 0 ? rule : rule.slice(0, idx)
}

/** 清洗规则行的分段空白，对齐后端 normalizeRule */
const normalizeRule = (rule: string): string =>
  rule
    .normalize('NFC')
    .trim()
    .split(',')
    .map((p) => p.trim())
    .join(',')

/** 名称的显示形态：NFC + 去首尾空白 + 折叠内部空白，对齐后端 NormalizeName */
const normalizeName = (s: string): string =>
  s
    .normalize('NFC')
    .trim()
    .replace(/[\s\u00a0]+/g, ' ')

/** 同一性比较键：在 normalizeName 之上再折叠大小写，对齐后端 normalizeKey */
const normalizeKey = (s: string): string => normalizeName(s).toLowerCase()

/**
 * 紧凑摘要：集合给规模，标量给值。
 *
 * 长字符串截断，避免一条 base64 节点链接把表格撑到横向滚动。
 */
const BRIEF_MAX = 60

const brief = (value: unknown): string => {
  if (value === null) return 'null'
  if (value === undefined) return ''
  if (Array.isArray(value)) return `${value.length} 项`
  if (typeof value === 'object') {
    if (isPlainRecord(value)) {
      const name = nameOf(value)
      // 节点/策略组这类有名字的对象，显示名字比显示「7 键」有用
      return name ? name : `${Object.keys(value).length} 键`
    }
    return String(value)
  }
  const text = String(value)
  // 空字符串必须有可见形态：否则 `path: ''` 渲染成 `path: ` 后面空着，
  // 与「没有这个键」看起来一模一样，而两者对内核的含义并不相同。
  if (text === '') return "''"
  return text.length > BRIEF_MAX ? `${text.slice(0, BRIEF_MAX)}…` : text
}

/**
 * 已按 name 对齐的条目用的摘要：跳过名字，只描述内容。
 *
 * 条目行本身已经显示了 name，brief 再返回一遍名字会渲染成
 * 「新增 HK-01 HK-01」。这里给出 `7 键` 这类结构规模，才是补充信息。
 */
const briefBody = (value: unknown): string => {
  if (isPlainRecord(value) && nameOf(value) !== null) {
    // 名字不计入键数：它已经单独显示了
    const rest = Object.keys(value).filter((k) => k !== 'name').length
    return `${rest} 项参数`
  }
  return brief(value)
}

/**
 * 同名条目被改写时，列出这一侧与对侧不同的字段及其值。
 *
 * 比「7 项参数 → 7 项参数」有用得多：直接说清是 server 变了还是 port 变了。
 * 字段数多时截断——要逐条核对应该用该键的逐行对比。
 */
const FIELD_SUMMARY_MAX = 4

const fieldSummary = (self: unknown, other: unknown): string => {
  if (!isPlainRecord(self) || !isPlainRecord(other)) return brief(self)

  const changed = [...new Set([...Object.keys(self), ...Object.keys(other)])]
    .filter((k) => !deepEqual(self[k], other[k]))
    .sort()
  if (!changed.length) return briefBody(self)

  const shown = changed
    .slice(0, FIELD_SUMMARY_MAX)
    .map((k) => (k in self ? `${k}: ${brief(self[k])}` : `${k}: (无)`))
    .join(', ')
  const rest = changed.length - FIELD_SUMMARY_MAX
  return rest > 0 ? `${shown} 等 ${changed.length} 项` : shown
}

/**
 * 该键子树的 YAML 原文，供下钻逐行对比。
 *
 * 标量直接给字面量：为一个 `mode: rule` 生成 YAML 文档没有意义。
 * dump 失败时退回 JSON：这里是展示用途，宁可形态不理想也不能让整页抛错。
 */
const subtreeText = (value: unknown): string => {
  if (value === null || value === undefined) return ''
  if (typeof value !== 'object') return String(value)
  try {
    return dumpYaml(value)
  } catch {
    return JSON.stringify(value, null, 2)
  }
}

/**
 * 结构化深比较。
 *
 * 不用 JSON.stringify 比对：那样键顺序会影响结果（`{a,b}` 与 `{b,a}` 被判为
 * 不同），而 YAML 里键序不改变语义，会造出一堆假差异。后端用 json.Marshal
 * 能这么比是因为 Go 结构体字段顺序固定，map 的 Marshal 又会自动排键；
 * JS 对象保留插入顺序，没这个保证。
 */
const deepEqual = (a: unknown, b: unknown): boolean => {
  if (a === b) return true
  if (a === null || b === null || a === undefined || b === undefined) return false
  if (typeof a !== typeof b) return false
  if (typeof a !== 'object') return false

  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false
    return a.every((item, i) => deepEqual(item, b[i]))
  }

  // Date/Map/Set 等非纯对象按值的字符串形态比：它们的自有键是空的，
  // 逐键比较会把两个不同的 Date 判为相等
  if (!isPlainRecord(a) || !isPlainRecord(b)) return String(a) === String(b)

  const ka = Object.keys(a)
  const kb = Object.keys(b)
  if (ka.length !== kb.length) return false
  return ka.every((k) => k in b && deepEqual(a[k], b[k]))
}

/** 是否是可按键遍历的纯对象（排除 Date/Map/Set，它们的自有键为空） */
const isPlainRecord = (value: unknown): value is Record<string, unknown> => {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false
  const proto = Object.getPrototypeOf(value)
  return proto === null || proto === Object.prototype
}

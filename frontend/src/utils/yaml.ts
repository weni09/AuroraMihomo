import * as yaml from 'js-yaml'

/**
 * 与后端 gopkg.in/yaml.v3 行为对齐的 schema：YAML 1.2 core + 合并键。
 *
 * 需要合并键：mihomo / Clash 配置广泛用锚点复用策略组与规则集模板，
 * 既有 `<<: *anchor` 的隐式写法，也有官方 Sub-Store 导出配置里常见的
 * `!!merge <<: *anchor` 显式标签写法。js-yaml 的裸 core schema 对显式
 * !!merge 会抛 "unknown scalar tag"，而后端能解析——两侧宽严不一致会让
 * 一份后端能正常渲染的模板在前端保存前校验时被误判为非法而拦下。
 *
 * 但**不能**为此整体切到 YAML 1.1：1.1 把 `off`/`on`/`yes`/`no` 当布尔，
 * 而 yaml.v3 遵循 1.2、把它们当字符串。这个差异会实际损坏配置——
 * 本项目的 `find-process-mode` 合法取值就含 `off`（见 schemas/baseConfig.ts），
 * 走 1.1 时它被读成布尔 false，回写就变成 `find-process-mode: false`，
 * 内核不认识这个值。同理 1.1 的 60 进制会把 `1:30` 读成 90。
 *
 * 因此只补合并键这一项，其余严格保持 1.2 语义，与后端一致。
 */
const MIHOMO_SCHEMA = yaml.CORE_SCHEMA.withTags(yaml.mergeTag)

/**
 * 全项目统一的 YAML 解析入口。
 */
export const loadYaml = (text: string): unknown =>
  yaml.load(text, { schema: MIHOMO_SCHEMA })

/**
 * 序列化回 YAML。
 *
 * noRefs 是必须的。原始 `<<: *anchor` 的合并确实在 load 阶段就展开了，
 * 但展开产物里多处仍指向**同一个对象/数组实例**（合并键复用的就是同一份
 * 值），js-yaml 的 dump 默认会为这种重复引用自动生成新锚点，输出
 * `proxies: &ref_0` / `proxies: *ref_0`。
 *
 * 结果是「格式化」把 `&pr`/`*pr` 换成了 `&ref_0`/`*ref_0`，锚点数从
 * 64 降到 26 而非归零——用户点了格式化却发现锚点还在。
 * noRefs 让每处都写成完整值，代价是文件变长，但这正是格式化的目的：
 * 展开复用、便于逐条核对。
 *
 * schema 必须与 loadYaml 用同一个，否则 load→dump 往返会变形：
 * dump 的默认 schema 是 YAML 1.1，会把字符串 "off" 直接写成裸 `off`，
 * 再被 loadYaml（1.2）读回来仍是字符串——看似自洽，但换个 1.1 的消费者
 * 就成了布尔。统一 schema 让"写出去的形态"和"读回来的语义"始终对应。
 */
export const dumpYaml = (value: unknown): string =>
  yaml.dump(value, { schema: MIHOMO_SCHEMA, lineWidth: -1, noRefs: true })

/**
 * 为「文本对比」规范化 YAML。
 *
 * 对比的两侧性质不同：本地配置是用户手写的原文（带注释、自定义键序），
 * 最终配置是后端 `yaml.Marshal` 的产物（注释丢失、键按结构体顺序重排）。
 * 直接比原始文本，差异列表会被"注释消失了""键换位置了"这类无意义的行淹没，
 * 真正的内容变化反而找不到。
 *
 * 这里把两侧都过一遍相同的解析 + 重序列化，并对映射键排序，使得：
 *   - 注释在解析阶段自然丢弃（两侧都没有，不构成差异）
 *   - 键序统一为字典序（两侧一致，不构成差异）
 *   - 锚点被展开（dumpYaml 的 noRefs），两侧写法差异不构成差异
 *
 * 剩下的差异就只有真正的内容变化。
 *
 * 解析失败时原样返回：宁可让用户看到带噪声的原始对比，
 * 也不能因为一份配置暂时写坏了就整个页面报错、什么都看不到。
 * 但这样单独用有陷阱——见 normalizeYamlPairForDiff。
 */
export const normalizeYamlForDiff = (text: string): string =>
  tryNormalizeYaml(text) ?? text

/**
 * 规范化对比的两侧。
 *
 * 必须成对处理，不能各自独立调用 normalizeYamlForDiff：那样只要有一侧
 * 解析失败，就会拿"规范化后的文本"去比"原始文本"，于是注释、键序、锚点
 * 这些本该被消掉的噪声全部回到差异里——整份配置显示为逐行全改，
 * 比不规范化更难看懂，而用户看到的只是"对照又乱了"。
 *
 * 因此规范化是全有或全无：任一侧失败就两侧都用原文，
 * 至少两边形态一致，差异仍然可读。返回 normalized 告知调用方实际用了哪种，
 * 以便在界面上说明「因为有一侧无法解析，本次按原文对比」。
 */
export const normalizeYamlPairForDiff = (
  left: string,
  right: string,
): { left: string; right: string; normalized: boolean } => {
  const nl = tryNormalizeYaml(left)
  const nr = tryNormalizeYaml(right)
  if (nl === null || nr === null) return { left, right, normalized: false }
  return { left: nl, right: nr, normalized: true }
}

/**
 * 尝试规范化，失败返回 null（而非静默退回原文），
 * 让调用方能区分"规范化成功"与"这份配置解析不了"。
 */
const tryNormalizeYaml = (text: string): string | null => {
  if (!text.trim()) return ''
  // 只有注释的文档没有可比内容，规范化为空串。
  //
  // 必须在 loadYaml 之前判断：js-yaml 对空文档**抛异常**
  // （"expected a document, but the input is empty"）而不是返回 undefined，
  // 落到下面的 catch 就会把整段注释当成"解析失败"，
  // 于是对比里出现一堆纯注释的差异行。
  if (stripYamlComments(text) === '') return ''

  try {
    const parsed = loadYaml(text)
    // 显式 `null` / `~` 文档同样没有可比内容
    if (parsed === undefined || parsed === null) return ''
    return dumpYaml(sortKeysDeep(parsed))
  } catch {
    return null
  }
}

/**
 * 去掉整行注释与空白，用于判断一份 YAML 是否只有注释。
 *
 * 只处理整行注释（行首 # ）：行内 `key: value # 说明` 里的 # 不能删，
 * 那会连带删掉合法的值。判断"是否只有注释"用不到那么精细。
 */
const stripYamlComments = (text: string): string =>
  text
    .split(/\r?\n/)
    .filter((line) => {
      const trimmed = line.trim()
      return trimmed !== '' && !trimmed.startsWith('#')
    })
    .join('\n')
    .trim()

/**
 * 递归按键名排序，让两侧的映射键顺序一致。
 *
 * 数组刻意不排序：proxies / rules 的顺序是有语义的（规则匹配讲先后，
 * 策略组内节点顺序影响展示与部分策略），排序会把"顺序变了"这个真实差异
 * 掩盖掉，也会让 diff 结果与实际生效行为脱节。
 *
 * 只重建「纯对象」。Date / Map / Set 这类有内部状态的对象一旦被
 * `Object.keys` 重建就只剩一个空壳：`Object.keys(new Date())` 是空数组，
 * 于是 `expire: 2024-01-02` 在对比里显示成 `expire: {}`——两侧都成了 {}，
 * 值改了也看不出来，等于静默丢失一处差异。原样返回交给 dump 处理。
 */
const sortKeysDeep = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(sortKeysDeep)
  if (value === null || typeof value !== 'object') return value
  if (!isPlainObject(value)) return value

  const src = value as Record<string, unknown>
  const out: Record<string, unknown> = {}
  for (const key of Object.keys(src).sort()) {
    out[key] = sortKeysDeep(src[key])
  }
  return out
}

/** 是否是可安全按键重建的纯对象（字面量 / Object.create(null) / JSON 产物） */
const isPlainObject = (value: object): boolean => {
  const proto = Object.getPrototypeOf(value)
  return proto === null || proto === Object.prototype
}

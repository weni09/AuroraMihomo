import { describe, expect, it } from 'vitest'
import { buildConfigDiff, type EntryDiff, type KeyDiff } from './configDiff'

/** 取指定顶层键的对比结果，断言存在 */
const keyOf = (diff: ReturnType<typeof buildConfigDiff>, key: string): KeyDiff => {
  const found = diff?.keys.find((k) => k.key === key)
  expect(found, `未找到顶层键 ${key}`).toBeDefined()
  return found!
}

/** 取排在第 n 位的键，断言存在——用于验证排序 */
const nthKey = (diff: ReturnType<typeof buildConfigDiff>, n: number): KeyDiff => {
  const found = diff?.keys[n]
  expect(found, `第 ${n} 位没有键`).toBeDefined()
  return found!
}

/** 取明细里第 n 条，断言存在 */
const nthEntry = (entries: KeyDiff['entries'], n: number): EntryDiff => {
  const found = entries[n]
  expect(found, `第 ${n} 条明细不存在`).toBeDefined()
  return found!
}

/**
 * 这一组是本次修复的核心动机。
 *
 * 逐行文本对比在「本地 41 行 vs 最终 1224 行」这种真实输入上只会产出一个
 * 横跨上千行的巨块，等于说「整份都是新增」。用户真正要问的是
 * 「我设的键还在不在、有没有被合并改掉」，必须按键回答。
 */
describe('本地键在最终配置里的去向', () => {
  const local = ['mode: rule', 'port: 7890', 'log-level: debug'].join('\n')

  it('值一致的键报原样保留', () => {
    const diff = buildConfigDiff(local, ['mode: rule', 'port: 7890', 'log-level: debug'].join('\n'))
    expect(keyOf(diff, 'mode').status).toBe('same')
    expect(diff!.counts.same).toBe(3)
  })

  it('值被合并改掉的键报改写，并给出两侧的值', () => {
    const diff = buildConfigDiff(local, ['mode: global', 'port: 7890', 'log-level: debug'].join('\n'))
    const mode = keyOf(diff, 'mode')
    expect(mode.status).toBe('changed')
    expect(mode.leftBrief).toBe('rule')
    expect(mode.rightBrief).toBe('global')
  })

  it('本地设了但最终没有的键报删除——这是配置被丢弃的信号', () => {
    const diff = buildConfigDiff(local, ['mode: rule', 'port: 7890'].join('\n'))
    const dropped = keyOf(diff, 'log-level')
    expect(dropped.status).toBe('removed')
    expect(dropped.rightBrief).toBe('')
  })

  it('合并补入的键报新增', () => {
    const diff = buildConfigDiff(local, [local, 'global-ua: clash'].join('\n'))
    expect(keyOf(diff, 'global-ua').status).toBe('added')
  })

  /**
   * 一份配置里绝大多数键是没变的。若按字典序排，两三条真问题会被均匀撒在
   * 几十行「原样保留」之间，用户还得自己找——那和现在的巨块没本质区别。
   */
  it('有差异的键排在原样保留的键之前', () => {
    const diff = buildConfigDiff(
      ['aaa: 1', 'bbb: 2', 'zzz: 3'].join('\n'),
      ['aaa: 1', 'bbb: 2', 'zzz: 999'].join('\n'),
    )
    expect(nthKey(diff, 0).key).toBe('zzz')
    expect(nthKey(diff, 0).status).toBe('changed')
  })
})

/**
 * proxies 从 1 项变 76 项是真实数据里的情况。
 * 只报「proxies 被改写」等于什么都没说，得说清是「原有的那个还在，另外多了 75 个」
 * 还是「原有的被换掉了」——后者意味着用户的本地节点丢了。
 */
describe('节点列表按 name 对齐', () => {
  const one = ['proxies:', '  - name: HK-01', '    type: ss', '    server: 1.1.1.1'].join('\n')

  it('保留原有节点并追加新节点时，原节点不出现在明细里，新节点报新增', () => {
    const two = [one, '  - name: JP-01', '    type: ss', '    server: 2.2.2.2'].join('\n')
    const entries = keyOf(buildConfigDiff(one, two), 'proxies').entries
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ name: 'JP-01', status: 'added' })
  })

  it('同名节点参数变化报改写，而不是先删后增', () => {
    const changed = ['proxies:', '  - name: HK-01', '    type: ss', '    server: 9.9.9.9'].join('\n')
    const entries = keyOf(buildConfigDiff(one, changed), 'proxies').entries
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ name: 'HK-01', status: 'changed' })
  })

  /**
   * 改写的条目必须点名变了哪些字段。
   * 只给「7 项参数 → 7 项参数」等于什么都没说，用户还得自己去翻两份文件。
   */
  it('改写的条目点名变化的字段与值', () => {
    const changed = ['proxies:', '  - name: HK-01', '    type: ss', '    server: 9.9.9.9'].join('\n')
    const entry = nthEntry(keyOf(buildConfigDiff(one, changed), 'proxies').entries, 0)
    expect(entry.left).toContain('server: 1.1.1.1')
    expect(entry.right).toContain('server: 9.9.9.9')
    // 没变的字段不列出来，否则真正变的那个又被埋进去了
    expect(entry.left).not.toContain('type')
  })

  /**
   * 条目行本身已经显示 name，摘要里再重复一遍会渲染成「新增 HK-01 HK-01」。
   * 这是真实数据里发现的显示缺陷。
   */
  it('新增条目的摘要不重复名字', () => {
    const two = [one, '  - name: JP-01', '    type: ss', '    server: 2.2.2.2'].join('\n')
    const entry = nthEntry(keyOf(buildConfigDiff(one, two), 'proxies').entries, 0)
    expect(entry.name).toBe('JP-01')
    expect(entry.right).not.toContain('JP-01')
  })

  it('本地节点在最终配置里消失时报删除', () => {
    const other = ['proxies:', '  - name: JP-01', '    type: ss', '    server: 2.2.2.2'].join('\n')
    const entries = keyOf(buildConfigDiff(one, other), 'proxies').entries
    expect(entries.map((e) => [e.name, e.status])).toEqual([
      ['JP-01', 'added'],
      ['HK-01', 'removed'],
    ])
  })

  /**
   * 与后端 NormalizeConfig 对齐（backend/internal/engine/normalize.go）：
   * 合并前会把名称做 NFC + 去首尾空白 + 折叠内部空白，所以纯空白差异是
   * 订阅源的书写噪声，不该报成节点被改。
   */
  it('名称的首尾与内部多余空白不算换了节点', () => {
    const spaced = [
      'proxies:',
      "  - name: '  HK-01 '",
      '    type: ss',
      '    server: 1.1.1.1',
    ].join('\n')
    expect(keyOf(buildConfigDiff(one, spaced), 'proxies').status).toBe('same')
  })

  /**
   * 大小写则相反，必须报出来：后端 NormalizeConfig 只做 NormalizeName、
   * 保留大小写，折叠仅用于同一性判断。名字是策略组 proxies 列表与面板里
   * 引用节点的字面量，改了大小写是用户看得见的真实变化。
   */
  it('名称大小写变化算真实改写，但仍对齐为同一节点', () => {
    const lower = ['proxies:', '  - name: hk-01', '    type: ss', '    server: 1.1.1.1'].join('\n')
    const entries = keyOf(buildConfigDiff(one, lower), 'proxies').entries
    // 一条 changed，而不是「删一个 + 加一个」
    expect(entries).toHaveLength(1)
    expect(nthEntry(entries, 0).status).toBe('changed')
  })
})

/**
 * 规则对齐方式直接决定可读性：整行比对会把改目标策略报成两条无关记录。
 * 这里的用例与后端 diff_test.go 的同名场景对应。
 */
describe('规则按 matcher 对齐', () => {
  it('同 matcher 换目标策略报改写', () => {
    const before = ['rules:', '  - DOMAIN,a.com,DIRECT'].join('\n')
    const after = ['rules:', '  - DOMAIN,a.com,PROXY'].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'rules').entries
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      name: 'DOMAIN,a.com',
      status: 'changed',
      left: 'DOMAIN,a.com,DIRECT',
      right: 'DOMAIN,a.com,PROXY',
    })
  })

  /**
   * 重复 matcher 是合法配置（同一域名先后走不同策略组的写法）。
   * 桶消费法必须保证消费不串位，否则会凭空报出改写。
   */
  it('重复 matcher 时逐条消费，不串位', () => {
    const before = ['rules:', '  - DOMAIN,a.com,DIRECT', '  - DOMAIN,a.com,PROXY'].join('\n')
    const after = ['rules:', '  - DOMAIN,a.com,PROXY', '  - DOMAIN,a.com,DIRECT'].join('\n')
    // 两条规则原封不动，只是顺序换了；按 matcher 桶消费后无差异条目
    expect(keyOf(buildConfigDiff(before, after), 'rules').entries).toHaveLength(0)
  })

  it('新 matcher 报新增，消失的 matcher 报删除', () => {
    const before = ['rules:', '  - DOMAIN,a.com,DIRECT'].join('\n')
    const after = ['rules:', '  - DOMAIN,b.com,DIRECT'].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'rules').entries
    expect(entries.map((e) => [e.name, e.status])).toEqual([
      ['DOMAIN,b.com', 'added'],
      ['DOMAIN,a.com', 'removed'],
    ])
  })

  it('分段空白不构成差异（对齐后端 normalizeRule）', () => {
    const before = ['rules:', '  - DOMAIN,a.com,DIRECT'].join('\n')
    const after = ['rules:', '  - DOMAIN , a.com , DIRECT'].join('\n')
    expect(keyOf(buildConfigDiff(before, after), 'rules').status).toBe('same')
  })
})

describe('规则集按 map 键对齐', () => {
  it('新增/改写/删除三态都能落到具体 provider 名', () => {
    const before = [
      'rule-providers:',
      '  cn:',
      '    type: http',
      '    url: http://a',
      '  ads:',
      '    type: http',
      '    url: http://b',
    ].join('\n')
    const after = [
      'rule-providers:',
      '  cn:',
      '    type: http',
      '    url: http://changed',
      '  new:',
      '    type: http',
      '    url: http://c',
    ].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'rule-providers').entries
    expect(entries.map((e) => [e.name, e.status]).sort()).toEqual([
      ['ads', 'removed'],
      ['cn', 'changed'],
      ['new', 'added'],
    ])
  })

  /**
   * 真实数据里合并引擎会给每个 provider 补一个 `path: ''`，而远程层没有这个键。
   * 这类差异过去被埋在逐行巨块里，现在必须能点名到字段。
   */
  it('改写的 provider 点名变化的字段', () => {
    const before = ['rule-providers:', '  cn:', '    type: http', "    path: ''"].join('\n')
    const after = ['rule-providers:', '  cn:', '    type: http'].join('\n')
    const entry = nthEntry(keyOf(buildConfigDiff(before, after), 'rule-providers').entries, 0)
    // 空字符串要有可见形态，否则与「没有这个键」在界面上无法区分
    expect(entry.left).toBe("path: ''")
    expect(entry.right).toBe('path: (无)')
  })
})

/**
 * dns 这类嵌套段若只报「被改写」，用户还得自己去两份文件里翻是哪个字段。
 * 展开到叶子路径才能一眼看到是 nameserver 变了还是 enable 变了。
 */
describe('嵌套键展开成叶子路径', () => {
  it('定位到具体子字段而非笼统报整段改写', () => {
    const before = ['dns:', '  enable: true', '  ipv6: false'].join('\n')
    const after = ['dns:', '  enable: true', '  ipv6: true'].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'dns').entries
    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({ name: 'ipv6', status: 'changed', left: 'false', right: 'true' })
  })

  it('数组元素定位到下标', () => {
    const before = ['dns:', '  nameserver:', '    - 1.1.1.1', '    - 8.8.8.8'].join('\n')
    const after = ['dns:', '  nameserver:', '    - 1.1.1.1', '    - 9.9.9.9'].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'dns').entries
    expect(nthEntry(entries, 0).name).toBe('nameserver[1]')
  })

  /**
   * 数组顺序不做归一：nameserver / fallback 的先后决定解析优先级，
   * 排序会把「顺序变了」这个真实差异掩盖掉。
   */
  it('数组顺序变化算真实差异', () => {
    const before = ['dns:', '  nameserver:', '    - 1.1.1.1', '    - 8.8.8.8'].join('\n')
    const after = ['dns:', '  nameserver:', '    - 8.8.8.8', '    - 1.1.1.1'].join('\n')
    expect(keyOf(buildConfigDiff(before, after), 'dns').status).toBe('changed')
  })

  it('子字段缺失与新增分别报 removed / added', () => {
    const before = ['dns:', '  enable: true', '  ipv6: false'].join('\n')
    const after = ['dns:', '  enable: true', '  listen: 0.0.0.0:53'].join('\n')
    const entries = keyOf(buildConfigDiff(before, after), 'dns').entries
    expect(entries.map((e) => [e.name, e.status]).sort()).toEqual([
      ['ipv6', 'removed'],
      ['listen', 'added'],
    ])
  })
})

/**
 * 映射键顺序不改变 YAML 语义。用 JSON.stringify 比对会把 {a,b} 与 {b,a}
 * 判为不同，凭空造出一堆假差异——那正是现在这个页面的毛病之一。
 */
describe('键顺序不构成差异', () => {
  it('顶层键换序判为完全一致', () => {
    const diff = buildConfigDiff(
      ['mode: rule', 'port: 7890'].join('\n'),
      ['port: 7890', 'mode: rule'].join('\n'),
    )
    expect(diff!.counts.changed).toBe(0)
    expect(diff!.counts.same).toBe(2)
  })

  it('嵌套映射换序同样判为一致', () => {
    const diff = buildConfigDiff(
      ['dns:', '  enable: true', '  ipv6: false'].join('\n'),
      ['dns:', '  ipv6: false', '  enable: true'].join('\n'),
    )
    expect(keyOf(diff, 'dns').status).toBe('same')
  })
})

describe('标签与摘要', () => {
  it('标签取自配置中心的表单元信息，保持两个页面叫法一致', () => {
    const diff = buildConfigDiff('mode: rule', 'mode: global')
    // baseConfigSchema 里 mode 字段的 title
    expect(keyOf(diff, 'mode').label).toContain('模式')
  })

  it('未建模的键回落到键名本身，而不是显示空白', () => {
    const diff = buildConfigDiff('my-custom-key: 1', 'my-custom-key: 2')
    expect(keyOf(diff, 'my-custom-key').label).toBe('my-custom-key')
  })

  it('集合类键的摘要给规模而非全文', () => {
    const before = ['rules:', '  - DOMAIN,a.com,DIRECT'].join('\n')
    const after = ['rules:', '  - DOMAIN,a.com,DIRECT', '  - DOMAIN,b.com,DIRECT'].join('\n')
    const rules = keyOf(buildConfigDiff(before, after), 'rules')
    expect(rules.leftBrief).toBe('1 项')
    expect(rules.rightBrief).toBe('2 项')
  })

  it('提供该键子树的 YAML 供下钻逐行对比', () => {
    const before = ['dns:', '  enable: true'].join('\n')
    const after = ['dns:', '  enable: false'].join('\n')
    const dns = keyOf(buildConfigDiff(before, after), 'dns')
    expect(dns.leftText).toContain('enable: true')
    expect(dns.rightText).toContain('enable: false')
  })
})

describe('无法对比的输入', () => {
  /**
   * 语义对比建立在解析产物上。解析不了就没有可对齐的结构，
   * 硬凑出来的结论会误导人——返回 null 让调用方如实提示并落回逐行文本。
   */
  it('一侧 YAML 写坏时返回 null，而不是抛错或给出臆测结果', () => {
    expect(buildConfigDiff('mode: rule', 'bad: [unclosed')).toBeNull()
    expect(buildConfigDiff('bad: [unclosed', 'mode: rule')).toBeNull()
  })

  it('顶层不是映射的文档返回 null——那不是一份 mihomo 配置', () => {
    expect(buildConfigDiff('- a\n- b', 'mode: rule')).toBeNull()
  })

  /**
   * 空内容不是解析失败：最终配置尚未生成过是正常状态，
   * 此时「本地的键全都还没生效」本身就是有用的结论。
   */
  it('空文本按空配置处理，本地键全部报删除', () => {
    const diff = buildConfigDiff('mode: rule\nport: 7890', '')
    expect(diff).not.toBeNull()
    expect(diff!.counts.removed).toBe(2)
  })

  it('只有注释的文档同样按空配置处理', () => {
    const diff = buildConfigDiff('mode: rule', '# 什么都没有\n')
    expect(diff).not.toBeNull()
    expect(keyOf(diff, 'mode').status).toBe('removed')
  })
})

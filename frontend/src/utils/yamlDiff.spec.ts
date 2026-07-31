import { describe, expect, it } from 'vitest'
import { loadYaml, normalizeYamlForDiff, normalizeYamlPairForDiff } from './yaml'

/**
 * 配置差异页面的规范化。
 *
 * 本地配置是用户手写原文（带注释、自定义键序），最终配置是后端
 * yaml.Marshal 的产物（注释丢失、键序重排）。裸文本对比会被这些无意义的
 * 差异淹没，真正的内容变化反而找不到——规范化就是为了消掉这类噪声。
 */
describe('normalizeYamlForDiff', () => {
  it('注释与键序不同但内容相同时，规范化结果一致', () => {
    const handWritten = [
      '# 我的代理配置',
      'mode: rule',
      'port: 7890',
      '# 下面这项必须放最后',
      'mixed-port: 7891',
    ].join('\n')
    const generated = ['mixed-port: 7891', 'mode: rule', 'port: 7890'].join('\n')

    expect(normalizeYamlForDiff(handWritten)).toBe(normalizeYamlForDiff(generated))
  })

  it('内容真的不同时仍然产生差异', () => {
    const a = ['port: 7890', 'mode: rule'].join('\n')
    const b = ['port: 7899', 'mode: rule'].join('\n')
    expect(normalizeYamlForDiff(a)).not.toBe(normalizeYamlForDiff(b))
  })

  it('嵌套映射的键也被排序', () => {
    const a = ['dns:', '  enable: true', '  listen: 0.0.0.0:53'].join('\n')
    const b = ['dns:', '  listen: 0.0.0.0:53', '  enable: true'].join('\n')
    expect(normalizeYamlForDiff(a)).toBe(normalizeYamlForDiff(b))
  })

  // 数组顺序有语义：规则匹配讲先后，策略组内节点顺序影响行为。
  // 排序数组会把「顺序变了」这个真实差异掩盖掉。
  it('数组顺序不被排序——那是有语义的真实差异', () => {
    const a = ['rules:', '  - MATCH,DIRECT', '  - DOMAIN,a.com,REJECT'].join('\n')
    const b = ['rules:', '  - DOMAIN,a.com,REJECT', '  - MATCH,DIRECT'].join('\n')
    expect(normalizeYamlForDiff(a)).not.toBe(normalizeYamlForDiff(b))
  })

  it('锚点被展开，两侧写法差异不构成差异', () => {
    const withAnchor = [
      'base: &b {type: select, proxies: [A]}',
      'groups:',
      '  g1: *b',
    ].join('\n')
    const expanded = [
      'base: {type: select, proxies: [A]}',
      'groups:',
      '  g1: {type: select, proxies: [A]}',
    ].join('\n')
    expect(normalizeYamlForDiff(withAnchor)).toBe(normalizeYamlForDiff(expanded))
  })

  it('空文本与只有注释的文本都归一为空串', () => {
    expect(normalizeYamlForDiff('')).toBe('')
    expect(normalizeYamlForDiff('   \n  ')).toBe('')
    expect(normalizeYamlForDiff('# 只有注释\n# 没有内容')).toBe('')
  })

  // 一份配置暂时写坏了，不该让整个对比页面报错、什么都看不到
  it('解析失败时原样返回，而不是抛错', () => {
    const broken = 'port: 7890\n  bad indent: [unclosed'
    expect(() => normalizeYamlForDiff(broken)).not.toThrow()
    expect(normalizeYamlForDiff(broken)).toBe(broken)
  })

  it('规范化后的输出本身是合法 YAML，可再次解析', () => {
    const src = ['# c', 'b: 2', 'a:', '  - 1', '  - 2'].join('\n')
    const out = normalizeYamlForDiff(src)
    expect(() => loadYaml(out)).not.toThrow()
    expect(loadYaml(out)).toEqual({ a: [1, 2], b: 2 })
  })

  it('规范化是幂等的——再跑一次结果不变', () => {
    const src = ['# c', 'b: 2', 'a: 1'].join('\n')
    const once = normalizeYamlForDiff(src)
    expect(normalizeYamlForDiff(once)).toBe(once)
  })

  // YAML 1.1 会把 off/on/yes/no 读成布尔，而后端 yaml.v3 遵循 1.2 当字符串。
  // 本项目的 find-process-mode 合法取值就含 off，一旦被读成布尔，
  // 对照里会显示成 `find-process-mode: false`——与两侧原文都不符。
  it('off/on/yes/no 保持字符串，与后端 yaml.v3 一致', () => {
    const out = normalizeYamlForDiff('find-process-mode: off\nmode: rule\n')
    expect(out).toContain('find-process-mode: off')
    expect(out).not.toContain('false')
    expect(loadYaml('a: off\nb: on\nc: yes\nd: no')).toEqual({
      a: 'off',
      b: 'on',
      c: 'yes',
      d: 'no',
    })
  })

  // YAML 1.1 的 60 进制会把 1:30 读成 90，把 12:34:56 读成 45296。
  // 配置里 `listen: 0.0.0.0:53` 之类的值一旦被这样解释就完全变形。
  it('冒号分隔的值不被当成 60 进制数字', () => {
    expect(loadYaml('t: 1:30')).toEqual({ t: '1:30' })
    expect(loadYaml('t: 12:34:56')).toEqual({ t: '12:34:56' })
  })

  // 曾经 sortKeysDeep 用 Object.keys 重建所有对象，而 Object.keys(new Date())
  // 是空数组，于是日期值在对照里全变成 {}：两侧都是 {}，改了也看不出来。
  it('日期值不被重建成空对象', () => {
    const out = normalizeYamlForDiff('expire: 2024-01-02\n')
    expect(out).not.toContain('{}')
    // 不同的日期必须仍然构成差异
    expect(out).not.toBe(normalizeYamlForDiff('expire: 2025-06-07\n'))
  })

  it('规范化后的键序与内容仍可完整往返', () => {
    const src = 'find-process-mode: off\nport: 7890\nexpire: 2024-01-02\n'
    const out = normalizeYamlForDiff(src)
    expect(loadYaml(out)).toEqual(loadYaml(src))
  })

  // 真实场景：本地只写了少量覆盖项，最终配置包含远程订阅的大量节点。
  // 规范化后差异应集中在新增的键上，而不是散落在每一行。
  it('本地配置的项在最终配置中保持一致时不产生差异行', () => {
    const local = ['# 本地覆盖', 'mode: rule', 'port: 7890'].join('\n')
    const final = ['mode: rule', 'port: 7890', 'proxies:', '  - {name: A, type: ss}'].join('\n')

    const nl = normalizeYamlForDiff(local).split('\n').filter(Boolean)
    const nf = normalizeYamlForDiff(final).split('\n').filter(Boolean)
    // local 的每一行都应原样出现在 final 里
    for (const line of nl) {
      expect(nf).toContain(line)
    }
  })
})

/**
 * 成对规范化。
 *
 * 各自独立调用 normalizeYamlForDiff 有个陷阱：一侧解析失败时它退回原文，
 * 于是变成「规范化文本 vs 原始文本」的对比——注释、键序、锚点这些本该被
 * 消掉的噪声全部回到差异里，整份配置显示为逐行全改，比不规范化更难看懂。
 */
describe('normalizeYamlPairForDiff', () => {
  const local = ['# 我的配置', 'mode: rule', 'port: 7890'].join('\n')
  const broken = 'mode: rule\nport: 7890\nbad: [unclosed'

  it('两侧都能解析时正常规范化', () => {
    const r = normalizeYamlPairForDiff(local, 'port: 7890\nmode: rule')
    expect(r.normalized).toBe(true)
    // 内容相同、只差注释与键序，规范化后应完全一致
    expect(r.left).toBe(r.right)
  })

  it('任一侧解析失败时两侧都退回原文，形态保持一致', () => {
    const r = normalizeYamlPairForDiff(local, broken)
    expect(r.normalized).toBe(false)
    expect(r.left).toBe(local)
    expect(r.right).toBe(broken)

    // 左右互换同样成立
    const swapped = normalizeYamlPairForDiff(broken, local)
    expect(swapped.normalized).toBe(false)
    expect(swapped.left).toBe(broken)
    expect(swapped.right).toBe(local)
  })

  // 这条是本次修复的核心：退回原文后，注释行不该凭空成为差异
  it('退回原文时不会把注释行变成差异', () => {
    const r = normalizeYamlPairForDiff(local, broken)
    expect(r.left).toContain('# 我的配置')
    // 若只有一侧被规范化，左侧注释会消失而右侧保留，形成假差异
    const onlyLeftNormalized = normalizeYamlForDiff(local)
    expect(onlyLeftNormalized).not.toContain('# 我的配置')
    expect(r.left).not.toBe(onlyLeftNormalized)
  })

  it('空内容侧不影响另一侧的规范化', () => {
    const r = normalizeYamlPairForDiff(local, '')
    expect(r.normalized).toBe(true)
    expect(r.right).toBe('')
    expect(r.left).not.toContain('# 我的配置')
  })
})

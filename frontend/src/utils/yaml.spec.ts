import { describe, expect, it } from 'vitest'
import { dumpYaml, loadYaml } from './yaml'

/**
 * YAML 工具的锚点处理。
 *
 * mihomo 配置广泛用 `<<: *anchor` 复用策略组模板，「格式化」的目的就是把
 * 这些复用展开成可逐条核对的完整内容。曾出现的问题是：格式化后锚点仍在，
 * 只是从 `&pr` 变成了 js-yaml 自动生成的 `&ref_0`——因为合并展开后多处
 * 仍指向同一个对象实例，dump 默认会为重复引用重新造锚点。
 */

/** 统计文本里的锚点定义与引用（&name / *name） */
const countAnchors = (s: string) => (s.match(/(^|\s)[&*][A-Za-z0-9_]+/gm) || []).length

describe('dumpYaml 的锚点展开', () => {
  it('合并键复用的内容被完全展开，输出不含任何锚点', () => {
    const src = `pr: &pr {type: fallback, proxies: [A, B]}
pr1: &pr1 {type: select, proxies: [DIRECT, REJECT]}
proxy-groups:
  - {name: AI, <<: *pr}
  - {name: Media, <<: *pr}
  - {name: Check, <<: *pr1}
`
    expect(countAnchors(src)).toBeGreaterThan(0)

    const out = dumpYaml(loadYaml(src))

    expect(countAnchors(out), `格式化后仍有锚点：\n${out}`).toBe(0)
    // 尤其不能出现 js-yaml 自动生成的 ref 锚点
    expect(out).not.toMatch(/[&*]ref_\d/)
  })

  it('展开后每处都是完整值，而非指向别处的引用', () => {
    const src = `base: &base {type: fallback, proxies: [A, B]}
groups:
  - {name: G1, <<: *base}
  - {name: G2, <<: *base}
`
    const out = dumpYaml(loadYaml(src))
    const back = loadYaml(out) as { groups: Array<{ name: string; type: string; proxies: string[] }> }

    // 两组都应各自带全字段
    for (const g of back.groups) {
      expect(g.type).toBe('fallback')
      expect(g.proxies).toEqual(['A', 'B'])
    }
    // 每处都写出完整列表：三处 proxies 各自带 A、B 两行，
    // 而不是一处定义 + 两处 *ref 引用。
    // 断言列表项出现次数而非 proxies 键次数——后者在有 ref 时同样是 3。
    expect(out.match(/^\s+- A$/gm)?.length, `A 应出现 3 次（base + G1 + G2）：\n${out}`).toBe(3)
    expect(out.match(/^\s+- B$/gm)?.length).toBe(3)
  })

  it('展开不改变语义：重新解析后与原对象一致', () => {
    const src = `pr: &pr {type: fallback, proxies: [A, B]}
proxy-groups:
  - {name: AI, <<: *pr}
  - {name: X, <<: *pr, type: select}
`
    const obj = loadYaml(src)
    const reparsed = loadYaml(dumpYaml(obj))

    expect(reparsed).toEqual(obj)
    // 局部覆盖仍要生效（X 自己的 type 覆盖锚点里的）
    const g = (reparsed as { 'proxy-groups': Array<Record<string, unknown>> })['proxy-groups']
    expect(g[1]?.type).toBe('select')
  })
})

describe('loadYaml 的 schema 宽容度', () => {
  it('接受显式 !!merge 标签（Sub-Store 导出配置的常见写法）', () => {
    // js-yaml 默认的 YAML 1.2 core schema 会对此抛
    // "unknown scalar tag"，而后端的 yaml.v3 能解析。
    // 两侧宽严不一致会导致后端能用的模板在前端存不进去。
    const src = `pr: &pr {type: fallback, proxies: [A]}
groups:
  - {name: G, !!merge <<: *pr}
`
    const obj = loadYaml(src) as { groups: Array<{ name: string; type: string }> }
    expect(obj.groups[0]?.type).toBe('fallback')
  })

  it('非法 YAML 仍应抛错，不能被 schema 放宽掩盖', () => {
    expect(() => loadYaml('a:\n  - b\n c: broken indent')).toThrow()
  })
})

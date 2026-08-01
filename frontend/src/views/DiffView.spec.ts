import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'

// api 必须在导入 store / 组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

// CodeMirror 的 MergeView 在 happy-dom 下无法真正布局，且本测试关心的是
// "交给它的两侧文本是什么"，不是渲染结果。替身把 props 暴露成可断言的文本。
vi.mock('../components/DiffEditor.vue', () => ({
  default: {
    name: 'DiffEditor',
    props: ['original', 'modified', 'language', 'height'],
    template: '<div class="diff-stub" :data-left="original" :data-right="modified" />',
  },
}))
vi.mock('../components/CodeEditor.vue', () => ({
  default: { name: 'CodeEditor', props: ['modelValue'], template: '<div class="code-stub" />' },
}))

import api from '../api'
import DiffView from './DiffView.vue'

const mockedApi = vi.mocked(api, true)

/** 三份配置按 /config/base、/config/final、/config/remote 顺序返回 */
const mockConfigs = (local: string, final: string, remote = '') => {
  mockedApi.get.mockImplementation((url: string) => {
    const content = url === '/config/base' ? local : url === '/config/final' ? final : remote
    return Promise.resolve({ data: { content } })
  })
}

const mountView = async () => {
  const w = mount(DiffView)
  // onMounted 里的三个并发请求 + 后续渲染
  await new Promise((r) => setTimeout(r, 0))
  await w.vm.$nextTick()
  return w
}

/** 按标签文字点击一个 tab（对比对象或看法都用同一套 role="tab" 按钮） */
const clickTab = async (w: Awaited<ReturnType<typeof mountView>>, text: string) => {
  const tab = w.findAll('[role="tab"]').find((t) => t.text().includes(text))
  expect(tab, `未找到标签「${text}」`).toBeDefined()
  await tab!.trigger('click')
  await w.vm.$nextTick()
}

/** 切到「本地 → 最终」的逐行文本对比 */
const openTextDiff = async (w: Awaited<ReturnType<typeof mountView>>) => {
  await clickTab(w, '本地 → 最终')
  await clickTab(w, '逐行文本')
}

describe('DiffView 默认给出语义对比', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockedApi.get.mockReset()
  })

  /**
   * 本次修复的核心。
   *
   * 逐行文本对比在真实数据（本地 41 行 vs 最终 1224 行）上只产出一个横跨
   * 上千行的巨块，等于说「整份都是新增」，用户真正要问的「我设的项还在不在、
   * 被合并改成了什么」一条都看不到。所以默认必须是按键对齐的语义对比。
   */
  it('进页面即落在「本地 → 最终」的语义对比，而不是三栏全文', async () => {
    mockConfigs('mode: rule\nport: 7890\n', 'mode: global\nport: 7890\n')
    const w = await mountView()

    expect(w.text()).toContain('按顶层键对比')
    // 逐行视图未被渲染
    expect(w.find('.diff-stub').exists()).toBe(false)
  })

  it('本地设的项被合并改掉时点名该键与两侧的值', async () => {
    mockConfigs('mode: rule\nport: 7890\n', 'mode: global\nport: 7890\n')
    const w = await mountView()

    expect(w.text()).toContain('已改写')
    expect(w.text()).toContain('mode')
    // 原样保留的键收进一行，不与真问题混在一起
    expect(w.text()).toContain('1 个原样保留的配置项')
  })

  /**
   * 「被丢弃」是这个页面最该突出的信号：用户设了某项，但它没进最终配置。
   * 逐行视图里这条会被埋在上千行新增里。
   */
  it('本地设了但最终没有的项报「被丢弃」', async () => {
    mockConfigs('mode: rule\nlog-level: debug\n', 'mode: rule\n')
    const w = await mountView()

    expect(w.text()).toContain('被丢弃')
    expect(w.text()).toContain('log-level')
  })

  it('没有差异时明确说全部原样保留，而不是留一片空白', async () => {
    mockConfigs('mode: rule\nport: 7890\n', 'port: 7890\nmode: rule\n')
    const w = await mountView()

    // 键序不同但内容一致——语义对比不该把它算成差异
    expect(w.text()).toContain('2 个配置项全部原样保留')
  })

  it('明细可展开，列出对齐后的具体条目', async () => {
    const local = ['proxies:', '  - name: HK-01', '    type: ss'].join('\n')
    const final = [local, '  - name: JP-01', '    type: ss'].join('\n')
    mockConfigs(local, final)
    const w = await mountView()

    // 明细按钮显示条数
    const detailBtn = w.findAll('button').find((b) => b.text().includes('1 条'))
    expect(detailBtn, '未找到明细展开按钮').toBeDefined()
    await detailBtn!.trigger('click')
    await w.vm.$nextTick()

    // 展开后能看到具体是哪个节点新增的
    expect(w.text()).toContain('JP-01')
  })

  /**
   * 语义对比建立在解析产物上，解析不了就没有可对齐的结构。
   * 此时如实说明原因并落到逐行文本，而不是显示一个空表格。
   */
  it('一侧 YAML 写坏时说明按键对比无从下手，并自动切到逐行文本', async () => {
    mockConfigs('mode: rule\n', 'mode: rule\nbad: [unclosed')
    const w = await mountView()

    expect(w.text()).toContain('按键对比无从下手')
    expect(w.find('.diff-stub').exists()).toBe(true)

    // 语义对比按钮此时不可点——点了也没有结果可显示
    const semanticTab = w.findAll('[role="tab"]').find((t) => t.text().includes('语义对比'))
    expect(semanticTab!.attributes('disabled')).toBeDefined()
  })

  it('对比一侧无内容时提示缺内容，而不是渲染空对比', async () => {
    mockConfigs('', 'mode: rule\n')
    const w = await mountView()

    expect(w.text()).toContain('无法对比')
    expect(w.find('.diff-stub').exists()).toBe(false)
    expect(w.text()).not.toContain('按顶层键对比')
  })
})

describe('DiffView 的逐行文本对照', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockedApi.get.mockReset()
  })

  it('两侧都能解析时，注释与键序差异被消掉', async () => {
    mockConfigs('# 我的配置\nmode: rule\nport: 7890\n', 'port: 7890\nmode: rule\n')
    const w = await mountView()
    await openTextDiff(w)

    // 内容一致，应给出明确提示而不是渲染一个空的差异视图
    expect(w.text()).toContain('两者内容一致')
    expect(w.find('.diff-stub').exists()).toBe(false)
  })

  /**
   * 一侧解析失败时，两侧必须都用原文。
   * 否则是「规范化文本 vs 原始文本」的对比，注释与键序噪声全部回到差异里，
   * 整份配置显示为逐行全改。
   */
  it('一侧 YAML 写坏时两侧都退回原文，并如实提示', async () => {
    const local = '# 我的配置\nmode: rule\nport: 7890\n'
    const broken = 'mode: rule\nport: 7890\nbad: [unclosed'
    mockConfigs(local, broken)
    const w = await mountView()
    // 解析失败时已自动落到逐行文本，无需再点看法切换
    await clickTab(w, '本地 → 最终')

    expect(w.text()).toContain('无法解析')

    const stub = w.find('.diff-stub')
    expect(stub.exists()).toBe(true)
    // 左侧保留注释——说明没有被单方面规范化
    expect(stub.attributes('data-left')).toBe(local)
    expect(stub.attributes('data-right')).toBe(broken)
  })

  it('关掉规范化开关时按原文对比，且不显示解析失败提示', async () => {
    const local = '# 我的配置\nmode: rule\n'
    mockConfigs(local, 'mode: rule\n')
    const w = await mountView()
    await openTextDiff(w)

    // 两侧内容相同，规范化下判定为一致
    expect(w.text()).toContain('两者内容一致')

    const checkbox = w.find('input[type="checkbox"]')
    await checkbox.setValue(false)
    await w.vm.$nextTick()

    // 原文对比下注释构成真实差异
    const stub = w.find('.diff-stub')
    expect(stub.exists()).toBe(true)
    expect(stub.attributes('data-left')).toBe(local)
    expect(w.text()).not.toContain('无法解析')
  })

  /** 规范化开关只影响逐行对比；语义对比按键对齐，注释与键序天然不构成差异 */
  it('规范化开关只在逐行视图下出现', async () => {
    mockConfigs('mode: rule\n', 'mode: global\n')
    const w = await mountView()

    expect(w.find('input[type="checkbox"]').exists()).toBe(false)
    await clickTab(w, '逐行文本')
    expect(w.find('input[type="checkbox"]').exists()).toBe(true)
  })
})

describe('DiffView 的三栏对照', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockedApi.get.mockReset()
  })

  it('切到三栏对照时展示三份全文，并隐藏看法切换', async () => {
    mockConfigs('mode: rule\n', 'mode: global\n', 'mode: direct\n')
    const w = await mountView()
    await clickTab(w, '三栏对照')

    expect(w.findAll('.code-stub')).toHaveLength(3)
    // 看法切换只对成对比较有意义
    expect(w.findAll('[role="tab"]').some((t) => t.text().includes('语义对比'))).toBe(false)
  })

  it('远程订阅为空时给出针对性说明，而不是空编辑器', async () => {
    mockConfigs('mode: rule\n', 'mode: global\n', '')
    const w = await mountView()
    await clickTab(w, '三栏对照')

    expect(w.text()).toContain('未配置远程来源')
    expect(w.findAll('.code-stub')).toHaveLength(2)
  })
})

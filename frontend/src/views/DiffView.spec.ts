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
    const content =
      url === '/config/base' ? local : url === '/config/final' ? final : remote
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

/** 切到「本地 → 最终」对比页 */
const openPair = async (w: Awaited<ReturnType<typeof mountView>>) => {
  const tab = w.findAll('[role="tab"]').find((t) => t.text().includes('本地'))
  await tab!.trigger('click')
  await w.vm.$nextTick()
}

describe('DiffView 的文本对照', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockedApi.get.mockReset()
  })

  it('两侧都能解析时，注释与键序差异被消掉', async () => {
    mockConfigs('# 我的配置\nmode: rule\nport: 7890\n', 'port: 7890\nmode: rule\n')
    const w = await mountView()
    await openPair(w)

    // 内容一致，应给出明确提示而不是渲染一个空的差异视图
    expect(w.text()).toContain('两者内容一致')
    expect(w.find('.diff-stub').exists()).toBe(false)
  })

  /**
   * 本次修复的核心：一侧解析失败时，两侧必须都用原文。
   * 否则是「规范化文本 vs 原始文本」的对比，注释与键序噪声全部回到差异里，
   * 整份配置显示为逐行全改。
   */
  it('一侧 YAML 写坏时两侧都退回原文，并如实提示', async () => {
    const local = '# 我的配置\nmode: rule\nport: 7890\n'
    const broken = 'mode: rule\nport: 7890\nbad: [unclosed'
    mockConfigs(local, broken)
    const w = await mountView()
    await openPair(w)

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
    await openPair(w)

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

  it('对比一侧无内容时提示缺内容，而不是渲染空差异', async () => {
    mockConfigs('', 'mode: rule\n')
    const w = await mountView()
    await openPair(w)

    expect(w.text()).toContain('无法对比')
    expect(w.find('.diff-stub').exists()).toBe(false)
  })
})

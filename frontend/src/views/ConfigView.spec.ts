import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'

// api 必须在导入 store / 组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import ConfigView from './ConfigView.vue'
import { useConfigStore } from '../stores/config'

const mockedApi = vi.mocked(api, true)

/** 一份包含「非受管高级参数」的基础配置：advanced-raw 负责的正是这些键 */
const BASE_YAML = [
  'mode: rule',
  'mixed-port: 7890',
  'listeners:',
  '  - name: in-1',
  '    type: socks',
  'experimental:',
  '  sniff-tls-sni: true',
  'hosts:',
  "  'router.local': 192.168.1.1",
  'proxy-groups:',
  '  - name: PROXY',
  '    type: select',
].join('\n')

/**
 * CodeEditor 打桩：真实实现会构造 CodeMirror EditorView，
 * 在 happy-dom 下既慢又依赖大量浏览器 API，而这里要验证的是
 * ConfigView 对编辑器输入的处理逻辑，与编辑器内部实现无关。
 */
const stubs = {
  CodeEditor: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<div class="code-editor-stub"></div>',
  },
}

/** 按 URL 分流 get mock：/config/unmerged 返回推导结果，其余返回 base 内容 */
function mockConfigGet(unmerged: boolean) {
  mockedApi.get.mockImplementation((url: string) => {
    if (String(url).includes('/config/unmerged')) {
      return Promise.resolve({ data: { unmerged } })
    }
    return Promise.resolve({ data: { content: BASE_YAML } })
  })
}

async function mountLoaded() {
  mockConfigGet(false)
  const wrapper = mount(ConfigView, { global: { stubs } })
  const store = useConfigStore()
  await store.fetchBase()
  return { wrapper, store }
}

/** 直接调用组件暴露的输入处理函数，绕开 CodeMirror 的事件链 */
type ConfigVm = {
  onCodeInput: (key: string, type: string, value: string) => void
  clearRawField: (key: string) => void
  reloadBase: () => Promise<void>
  onHostsMap: (key: string, value: Record<string, unknown> | undefined) => void
}
const vmOf = (wrapper: ReturnType<typeof mount>) => wrapper.vm as unknown as ConfigVm

describe('ConfigView 源码字段的破坏性写入防护', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  /**
   * 回归：清空 advanced-raw 编辑器不得删除 model 里的非受管键。
   *
   * 修复前 value.trim() 为空时会解析成 {}，随后「先删后写」把 listeners /
   * experimental 等键一次删光。用户全选后重新输入必然经过「空」这个中间态，
   * 而此后只要解析持续失败，这些键就再也回不来。
   */
  it('advanced-raw 传入空文本时不得清空非受管键', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    expect(store.model.listeners).toBeDefined()

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', '')
    vi.runAllTimers()

    expect(store.model.listeners).toBeDefined()
    expect(store.model.experimental).toBeDefined()
  })

  it('advanced-raw 传入纯空白同样不得清空', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', '   \n  \n')
    vi.runAllTimers()

    expect(store.model.listeners).toBeDefined()
    expect(store.model.experimental).toBeDefined()
  })

  it('advanced-raw 有内容时仍按「全量替换」语义生效', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()

    // 新内容只含 experimental，listeners 应被移除（这是该字段的既有语义）
    vmOf(wrapper).onCodeInput('advanced-raw', 'code', 'experimental:\n  sniff-tls-sni: false\n')
    vi.runAllTimers()

    expect(store.model.experimental).toEqual({ 'sniff-tls-sni': false })
    expect(store.model.listeners).toBeUndefined()
    // 受管键不受影响
    expect(store.model.mode).toBe('rule')
  })

  it('advanced-raw 语法错误时不写回，保留原值', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', 'listeners: [unclosed')
    vi.runAllTimers()

    expect(store.model.listeners).toEqual([{ name: 'in-1', type: 'socks' }])
  })

  /** 同一个 bug 的另一半：策略组字段空文本会把 proxy-groups 置空 */
  it('proxy-groups-raw 空文本时不得清空策略组', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    expect(store.model['proxy-groups']).toHaveLength(1)

    vmOf(wrapper).onCodeInput('proxy-groups-raw', 'code', '')
    vi.runAllTimers()

    expect(store.model['proxy-groups']).toHaveLength(1)
  })

  it('清空需走显式入口 clearRawField', async () => {
    const { wrapper, store } = await mountLoaded()
    // happy-dom 未实现 window.confirm，只能整体替换而非 spyOn
    const confirmStub = vi.fn(() => true)
    vi.stubGlobal('confirm', confirmStub)

    vmOf(wrapper).clearRawField('advanced-raw')

    expect(confirmStub).toHaveBeenCalled()
    expect(store.model.listeners).toBeUndefined()
    expect(store.model.experimental).toBeUndefined()
    // 受管键不该被这个入口牵连
    expect(store.model.mode).toBe('rule')
  })

  it('clearRawField 取消确认时不做任何改动', async () => {
    const { wrapper, store } = await mountLoaded()
    vi.stubGlobal('confirm', vi.fn(() => false))

    vmOf(wrapper).clearRawField('advanced-raw')

    expect(store.model.listeners).toBeDefined()
  })

  /**
   * hosts 自「域名解析」分组有了专属表单后就是受管键，
   * 必须与 advanced-raw 彻底脱钩：否则高级参数的「先删非受管键再写入」
   * 会把 hosts 表单刚配好的映射删掉，而用户在两个页面之间切换时看不出原因。
   */
  it('advanced-raw 全量替换不得动 hosts（已是受管键）', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    expect(store.model.hosts).toEqual({ 'router.local': '192.168.1.1' })

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', 'experimental:\n  sniff-tls-sni: false\n')
    vi.runAllTimers()

    expect(store.model.hosts).toEqual({ 'router.local': '192.168.1.1' })
  })

  it('清空高级参数不得清掉 hosts', async () => {
    const { wrapper, store } = await mountLoaded()
    vi.stubGlobal('confirm', vi.fn(() => true))

    vmOf(wrapper).clearRawField('advanced-raw')

    expect(store.model.hosts).toEqual({ 'router.local': '192.168.1.1' })
    // 真正的非受管键仍按既有语义被清掉
    expect(store.model.listeners).toBeUndefined()
  })
})

describe('ConfigView 的 hosts 表单', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  /** 光是把 onHostsMap 接上不够：字段还得真的出现在「域名解析」分组里 */
  it('切到「域名解析」分组时渲染出 hosts 行编辑器与已有映射', async () => {
    const { wrapper } = await mountLoaded()
    const dnsNav = wrapper.findAll('button').find((b) => b.text() === '域名解析 (DNS)')
    expect(dnsNav).toBeDefined()
    await dnsNav!.trigger('click')

    expect(wrapper.text()).toContain('自定义 hosts 映射')
    const domainInput = wrapper.findAll('input').find((i) => i.attributes('aria-label')?.includes('的域名'))
    expect(domainInput).toBeDefined()
    expect((domainInput!.element as HTMLInputElement).value).toBe('router.local')
  })

  it('写入映射后落到 model 的顶层 hosts', async () => {
    const { wrapper, store } = await mountLoaded()
    vmOf(wrapper).onHostsMap('hosts', { 'a.local': '10.0.0.1' })

    expect(store.model.hosts).toEqual({ 'a.local': '10.0.0.1' })
  })

  /** 空映射要删键而不是留下 `hosts: {}`——后者是「显式配置了空映射」，语义不同 */
  it('映射被清空时删除 hosts 键，不写出空映射', async () => {
    const { wrapper, store } = await mountLoaded()

    vmOf(wrapper).onHostsMap('hosts', undefined)
    expect('hosts' in store.model).toBe(false)

    vmOf(wrapper).onHostsMap('hosts', {})
    expect('hosts' in store.model).toBe(false)
  })
})

describe('ConfigView 编辑器输入的防抖与落盘', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('连续输入只在防抖结束后提交一次，且取最后一次的值', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    const vm = vmOf(wrapper)

    vm.onCodeInput('advanced-raw', 'code', 'experimental:\n  a: 1\n')
    vm.onCodeInput('advanced-raw', 'code', 'experimental:\n  a: 2\n')
    vm.onCodeInput('advanced-raw', 'code', 'experimental:\n  a: 3\n')
    // 防抖窗口内尚未提交
    expect(store.model.experimental).toEqual({ 'sniff-tls-sni': true })

    vi.runAllTimers()
    expect(store.model.experimental).toEqual({ a: 3 })
  })

  /**
   * 回归：防抖引入的窗口不得吞掉最后一次修改。
   * 用户改完立刻点保存（250ms 内）时，saveBase 前必须先 flush 挂起的输入，
   * 否则写回的是上一次的内容，而界面上看不出差别。
   */
  it('保存前会 flush 挂起的输入，PUT 的内容包含最后一次修改', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    mockedApi.put.mockResolvedValue({ data: { message: '保存成功' } })

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', 'experimental:\n  flushed: true\n')
    // 不推进定时器，直接点「保存基础配置」
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === '保存基础配置')
    expect(saveBtn).toBeDefined()
    await saveBtn!.trigger('click')

    expect(mockedApi.put).toHaveBeenCalledTimes(1)
    const body = mockedApi.put.mock.calls[0]![1] as { content: string }
    expect(body.content).toContain('flushed: true')
    expect(store.model.experimental).toEqual({ flushed: true })
  })

  /** 放弃修改必须连挂起的提交一起丢弃，否则它会在 fetchBase 之后把旧值写回 */
  it('放弃修改会丢弃挂起的输入，不在重新加载后写回', async () => {
    vi.useFakeTimers()
    const { wrapper, store } = await mountLoaded()
    vi.stubGlobal('confirm', vi.fn(() => true))

    vmOf(wrapper).onCodeInput('advanced-raw', 'code', 'experimental:\n  discarded: true\n')
    await vmOf(wrapper).reloadBase()
    vi.runAllTimers()

    // 应是服务端返回的原值，而不是被放弃的那次编辑
    expect(store.model.experimental).toEqual({ 'sniff-tls-sni': true })
  })
})

describe('ConfigView 未应用提示', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  /**
   * 仅点击「保存基础配置」后，界面应显示「未应用生效」的黄色提示条，
   * 点击提示条上的「应用并生效」后触发 saveAndMerge 并消除提示。
   */
  it('仅保存基础配置时出现未应用提示，应用并生效后提示消失', async () => {
    const { wrapper, store } = await mountLoaded()
    mockedApi.put.mockResolvedValue({ data: { message: '保存成功' } })
    mockedApi.post.mockResolvedValue({ data: { message: '已应用并生效' } })

    expect(wrapper.text()).not.toContain('本地基础配置已保存，但尚未应用生效')

    // 手动调用 store.saveBase 模拟成功保存基础配置
    await store.saveBase()
    await wrapper.vm.$nextTick()

    expect(store.unmergedChanges).toBe(true)
    expect(wrapper.text()).toContain('本地基础配置已保存，但尚未应用生效')

    // 调用 store.saveAndMerge 模拟应用合并
    await store.saveAndMerge()
    await wrapper.vm.$nextTick()

    expect(mockedApi.post).toHaveBeenCalledWith('/config/merge')
    expect(store.unmergedChanges).toBe(false)
    expect(wrapper.text()).not.toContain('本地基础配置已保存，但尚未应用生效')
  })

  /**
   * 按钮路径：提示条上的「应用并生效」必须真的把合并动作发出去。
   * 与上一个用例的区别在于这里走模板的 @click 而不是直接调 store，
   * 防止哪天按钮绑错 handler 而状态流转测试仍然通过。
   * 本用例没有挂起的编辑器输入，flushCodeInputs 是同步空操作，
   * 因此点击按钮不会踩到防抖窗口。
   */
  it('点击提示条上的「应用并生效」按钮会触发合并并消除提示', async () => {
    const { wrapper, store } = await mountLoaded()
    mockedApi.put.mockResolvedValue({ data: { message: '保存成功' } })
    mockedApi.post.mockResolvedValue({ data: { message: '已应用并生效' } })

    // 先保存基础配置，让提示条渲染出来
    await store.saveBase()
    await wrapper.vm.$nextTick()

    const applyBtn = wrapper.findAll('button').find((b) => b.text() === '应用并生效')
    expect(applyBtn).toBeDefined()
    await applyBtn!.trigger('click')

    // 点击后 handler 是异步链（PUT 保存 → POST 合并 → 刷新透明代理状态），
    // 无法直接 await，轮询等待断言成立
    await vi.waitFor(() => {
      expect(mockedApi.post).toHaveBeenCalledWith('/config/merge')
    })
    await wrapper.vm.$nextTick()

    expect(store.unmergedChanges).toBe(false)
    expect(wrapper.text()).not.toContain('本地基础配置已保存，但尚未应用生效')
  })

  /**
   * 回归：提示必须跨刷新存活，但结论来自后端推导而不是本地存储。
   * 刷新后 fetchBase 会重新请求 /config/unmerged，后端按合并指纹
   * 判断 base 仍未合并 → 新页面恢复提示；应用合并之后再刷新，
   * 后端推导已对齐 → 提示消失。换浏览器/清缓存/换机器同理。
   */
  it('保存后刷新页面提示仍在（后端推导），应用并生效后再刷新提示消失', async () => {
    mockedApi.put.mockResolvedValue({ data: { message: '保存成功' } })
    mockedApi.post.mockResolvedValue({ data: { message: '已应用并生效' } })

    // 第一次加载 + 保存基础配置
    const { store } = await mountLoaded()
    await store.saveBase()
    expect(store.unmergedChanges).toBe(true)

    // 模拟刷新：后端按指纹推导仍为未合并 → 新页面恢复提示
    mockConfigGet(true)
    setActivePinia(createPinia())
    const wrapper2 = mount(ConfigView, { global: { stubs } })
    const store2 = useConfigStore()
    await store2.fetchBase()
    await vi.waitFor(() => {
      expect(store2.unmergedChanges).toBe(true)
    })
    await wrapper2.vm.$nextTick()

    expect(wrapper2.text()).toContain('本地基础配置已保存，但尚未应用生效')

    // 应用并生效（后端指纹随之更新）
    await store2.saveAndMerge()

    // 再刷新：后端推导已对齐 → 无提示
    mockConfigGet(false)
    setActivePinia(createPinia())
    const wrapper3 = mount(ConfigView, { global: { stubs } })
    const store3 = useConfigStore()
    await store3.fetchBase()
    await vi.waitFor(() => {
      expect(store3.unmergedChanges).toBe(false)
    })
    await wrapper3.vm.$nextTick()

    expect(wrapper3.text()).not.toContain('本地基础配置已保存，但尚未应用生效')
  })
})

describe('ConfigView 加载失败时的界面防护', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('加载失败时隐藏配置表单并禁用两个保存按钮', async () => {
    mockedApi.get.mockRejectedValue({ message: '网络错误' })
    const wrapper = mount(ConfigView, { global: { stubs } })
    const store = useConfigStore()
    await store.fetchBase()
    await wrapper.vm.$nextTick()

    // 表单整块不渲染，避免用户照着空表单填一遍再保存
    expect(wrapper.find('.code-editor-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('基础配置加载失败')

    for (const label of ['保存基础配置', '保存并应用']) {
      const btn = wrapper.findAll('button').find((b) => b.text() === label)
      expect(btn, label).toBeDefined()
      expect(btn!.attributes('disabled'), label).toBeDefined()
    }
  })

  it('加载成功后表单渲染且保存按钮可用', async () => {
    const { wrapper } = await mountLoaded()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).not.toContain('基础配置加载失败')
    const btn = wrapper.findAll('button').find((b) => b.text() === '保存基础配置')
    expect(btn!.attributes('disabled')).toBeUndefined()
  })
})

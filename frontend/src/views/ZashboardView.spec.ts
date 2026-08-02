import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

// api 必须在导入组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import ZashboardView from './ZashboardView.vue'
import { clearPageChrome, usePageChrome } from '../composables/usePageChrome'

const mockedApi = vi.mocked(api, true)

/**
 * 小屏不再叠页面大标题条：对接信息与「新标签页」注入 App 顶栏；
 * 本页仅在 lg+ 保留无标题工具条。用 page chrome 状态 + class 契约断言。
 */
describe('ZashboardView 移动端 header / page chrome', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearPageChrome()
  })

  it('成功加载时写入 page chrome，且页面级 header 仅桌面显示、无大标题', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '127.0.0.1',
        port: '9090',
        url: 'http://127.0.0.1:9090/ui/?hostname=127.0.0.1&port=9090&secret=s',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const { subtitle, action } = usePageChrome()
    expect(subtitle.value).toBe('已对接内核 127.0.0.1:9090')
    expect(action.value?.label).toBe('新标签页')
    expect(action.value?.disabled).toBe(false)

    const header = wrapper.get('[data-testid="zashboard-page-header"]')
    expect(header.classes()).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))
    // 页面级不再放 h1，标题由 App 顶栏承担
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.text()).toContain('在新标签页打开')
    expect(wrapper.text()).not.toContain('重新加载')
    expect(wrapper.find('iframe').exists()).toBe(true)

    wrapper.unmount()
    expect(subtitle.value).toBe('')
    expect(action.value).toBeNull()
  })

  it('入口不可用时错误说明仍完整展示，且清空对接副标题', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        available: false,
        message: '内核未启用外部控制接口',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    expect(wrapper.text()).toContain('面板暂时无法打开')
    expect(wrapper.text()).toContain('内核未启用外部控制接口')
    expect(wrapper.find('iframe').exists()).toBe(false)

    const { subtitle, action } = usePageChrome()
    expect(subtitle.value).toBe('')
    // 外开不可用，但仍挂「新标签页」入口（disabled），避免顶栏布局跳动
    expect(action.value?.label).toBe('新标签页')
    expect(action.value?.disabled).toBe(true)

    wrapper.unmount()
  })
})

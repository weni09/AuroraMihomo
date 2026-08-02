import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

// api 必须在导入组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import ZashboardView from './ZashboardView.vue'

const mockedApi = vi.mocked(api, true)

/**
 * 页面级 header 在小屏与 App 顶栏重复占高，挤压 iframe。
 * 契约：header 容器必须带 hidden lg:flex，由 Tailwind 在 <lg 隐藏、≥lg 显示。
 * 只断言 class 字符串，不依赖真实视口宽度（happy-dom 不会跑媒体查询布局）。
 */
describe('ZashboardView 移动端 header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('成功加载时页面级 header 带 hidden lg:flex，桌面仍保留操作入口文案', async () => {
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

    const header = wrapper.get('[data-testid="zashboard-page-header"]')
    const cls = header.classes()
    expect(cls).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))

    expect(wrapper.text()).toContain('重新加载')
    expect(wrapper.text()).toContain('在新标签页打开')
    expect(wrapper.find('iframe').exists()).toBe(true)

    wrapper.unmount()
  })

  it('入口不可用时错误说明仍完整展示（不因隐藏 header 而吞掉）', async () => {
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

    wrapper.unmount()
  })
})

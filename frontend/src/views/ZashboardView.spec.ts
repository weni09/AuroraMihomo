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
 * 页面级 header 在小屏仍要保留标题与操作（尤其「新标签页」），
 * 但须压成单行矮条，避免与 App 顶栏叠两层大块挤掉 iframe。
 * 契约用 class 断言（happy-dom 不跑媒体查询布局）。
 */
describe('ZashboardView 移动端 header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('成功加载时 header 始终可见且为单行紧凑布局，并保留操作文案', async () => {
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
    // 始终展示（不再 hidden），小屏强制 nowrap 单行 + 更矮 py
    expect(cls).toEqual(expect.arrayContaining(['flex', 'flex-nowrap', 'py-1.5']))
    expect(cls).not.toContain('hidden')

    expect(wrapper.get('h1').text()).toBe('Zashboard')
    expect(wrapper.text()).toContain('重新加载')
    // 模板里桌面全文与小屏短文案都在 DOM（用 sm: 显隐），断言两者之一即可
    expect(wrapper.text()).toMatch(/新标签页|在新标签页打开/)
    expect(wrapper.find('iframe').exists()).toBe(true)

    wrapper.unmount()
  })

  it('入口不可用时错误说明仍完整展示', async () => {
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
    // 错误态下操作仍在，方便重试
    expect(wrapper.text()).toContain('重新加载')

    wrapper.unmount()
  })
})

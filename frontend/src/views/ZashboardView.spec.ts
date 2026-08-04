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
 * 移动端：页面 header 隐藏，对接信息与「新标签页」注入 App 顶栏。
 * 桌面（lg+）：完整页面 header（标题 + 两按钮）保持原样。
 */
describe('ZashboardView 移动端 / 桌面 header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearPageChrome()
  })

  it('成功加载时写入 page chrome，且桌面 header 仍含标题与两按钮', async () => {
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
    // 小屏隐藏、仅 lg 显示完整条
    expect(header.classes()).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))
    expect(wrapper.get('h1').text()).toBe('Zashboard')
    expect(wrapper.text()).toContain('重新加载')
    expect(wrapper.text()).toContain('在新标签页打开')
    expect(wrapper.find('iframe').exists()).toBe(true)

    wrapper.unmount()
    expect(subtitle.value).toBe('')
    expect(action.value).toBeNull()
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

    const { subtitle, action } = usePageChrome()
    expect(subtitle.value).toBe('')
    expect(action.value?.label).toBe('新标签页')
    expect(action.value?.disabled).toBe(true)

    wrapper.unmount()
  })

  it('首次配置写入 zashboard 合法后端（type=clash + protocol=http）', async () => {
    // 回归：曾误写 type:'http' 且漏写 protocol，zashboard 拼请求地址得
    // undefined:// 导致 /rules 等全部请求失败（手机端首次配置必踩）。
    window.localStorage.clear()
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '192.168.1.129',
        port: '9090',
        url: '/ui/?hostname=192.168.1.129&port=9090&secret=abc',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const list = JSON.parse(window.localStorage.getItem('setup/api-list')!)
    expect(list).toHaveLength(1)
    expect(list[0]).toMatchObject({
      type: 'clash',
      protocol: 'http',
      host: '192.168.1.129',
      port: '9090',
      password: 'abc',
    })

    wrapper.unmount()
  })

  it('已存的坏记录（type=http 缺 protocol）会被收敛为 clash/http', async () => {
    window.localStorage.setItem(
      'setup/api-list',
      JSON.stringify([{ uuid: 'u1', type: 'http', host: '1.2.3.4', port: '9090', password: '', label: 'x' }]),
    )
    window.localStorage.setItem('setup/active-uuid', 'u1')
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '192.168.1.129',
        port: '9090',
        url: '/ui/?hostname=192.168.1.129&port=9090&secret=abc',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const list = JSON.parse(window.localStorage.getItem('setup/api-list')!)
    expect(list[0]).toMatchObject({
      type: 'clash',
      protocol: 'http',
      host: '192.168.1.129',
      port: '9090',
      password: 'abc',
    })

    wrapper.unmount()
  })
})

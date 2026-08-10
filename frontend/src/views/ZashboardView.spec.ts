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

function stubLocation(partial: Partial<Location> & { protocol: string; hostname: string; port?: string; origin?: string }) {
  const value = {
    protocol: partial.protocol,
    hostname: partial.hostname,
    port: partial.port ?? '',
    origin:
      partial.origin ||
      `${partial.protocol}//${partial.hostname}${partial.port ? `:${partial.port}` : ''}`,
    href: partial.href || `${partial.protocol}//${partial.hostname}/`,
    host: partial.host || (partial.port ? `${partial.hostname}:${partial.port}` : partial.hostname),
  }
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value,
  })
  return value
}

/**
 * 移动端：页面 header 隐藏，对接信息与「新标签页」注入 App 顶栏。
 * 桌面（lg+）：完整页面 header（标题 + 两按钮）保持原样。
 */
describe('ZashboardView 移动端 / 桌面 header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearPageChrome()
    window.localStorage.clear()
    // 默认内网 http 页面；各用例可覆盖
    stubLocation({ protocol: 'http:', hostname: '127.0.0.1', port: '8899' })
    mockedApi.post.mockResolvedValue({ data: { ok: true } })
  })

  it('成功加载时写入 page chrome，且桌面 header 仍含标题与两按钮', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '127.0.0.1',
        port: '8899',
        url: '/ui/?hostname=127.0.0.1&port=8899&secondaryPath=%2Fmihomo-api&secret=s',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    // 挂 iframe 前必须对齐 aurora_session，反代只认 cookie
    expect(mockedApi.post).toHaveBeenCalledWith('/auth/session', null, { skipErrorToast: true })

    const { subtitle, action } = usePageChrome()
    // UI 展示以浏览器 location 为准（同源反代端口）
    expect(subtitle.value).toBe('已对接内核 127.0.0.1:8899')
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

  it('首次配置写入 zashboard 合法后端（clash + 页面协议 + 同源反代路径）', async () => {
    // 回归：曾误写 type:'http' 且漏写 protocol，zashboard 拼请求地址得
    // undefined:// 导致 /rules 等全部请求失败（手机端首次配置必踩）。
    // 同源反代后 protocol/host/port 跟随浏览器 location，
    // secondaryPath 指向 /mihomo-api。
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        // 故意给后端错误 port，前端必须以 location 为准
        host: 'wrong.example',
        port: '80',
        url: '/ui/?hostname=wrong.example&port=80&secondaryPath=%2Fmihomo-api&secret=abc',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const list = JSON.parse(window.localStorage.getItem('setup/api-list')!)
    expect(list).toHaveLength(1)
    expect(list[0]).toMatchObject({
      type: 'clash',
      protocol: 'http',
      secondaryPath: '/mihomo-api',
      host: '127.0.0.1',
      port: '8899',
      password: 'abc',
    })

    wrapper.unmount()
  })

  it('已存的坏记录（type=http 缺 protocol/路径）会被收敛为 clash/页面协议/同源反代', async () => {
    window.localStorage.setItem(
      'setup/api-list',
      JSON.stringify([{ uuid: 'u1', type: 'http', host: '1.2.3.4', port: '9090', password: '', label: 'x' }]),
    )
    window.localStorage.setItem('setup/active-uuid', 'u1')
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '1.2.3.4',
        port: '9090',
        url: '/ui/?hostname=1.2.3.4&port=9090&secondaryPath=%2Fmihomo-api&secret=abc',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const list = JSON.parse(window.localStorage.getItem('setup/api-list')!)
    expect(list[0]).toMatchObject({
      type: 'clash',
      protocol: 'http',
      secondaryPath: '/mihomo-api',
      host: '127.0.0.1',
      port: '8899',
      password: 'abc',
    })

    wrapper.unmount()
  })

  it('https 页面写入 protocol=https 与隐含 443，即使后端误报 port=80', async () => {
    // 公网 nginx + 证书场景：页面协议为 https，面板记录必须跟随；
    // 后端 TrustedProxies 未配时可能误报 port=80，前端用 location 兜底。
    stubLocation({ protocol: 'https:', hostname: 'aurora.615246.xyz', port: '' })
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: 'aurora.615246.xyz',
        port: '80',
        url: '/ui/?hostname=aurora.615246.xyz&port=80&secondaryPath=%2Fmihomo-api&secret=abc',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    const list = JSON.parse(window.localStorage.getItem('setup/api-list')!)
    expect(list[0]).toMatchObject({
      type: 'clash',
      protocol: 'https',
      secondaryPath: '/mihomo-api',
      host: 'aurora.615246.xyz',
      port: '443',
    })
    expect(usePageChrome().subtitle.value).toBe('已对接内核 aurora.615246.xyz:443')

    wrapper.unmount()
  })
})

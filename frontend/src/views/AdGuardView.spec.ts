import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'

// api 必须在导入 store / 组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import AdGuardView from './AdGuardView.vue'
import { clearPageChrome, usePageChrome } from '../composables/usePageChrome'

const mockedApi = vi.mocked(api, true)

function statusPayload(partial: Record<string, unknown> = {}) {
  return {
    // 既有用例覆盖「已启用组件」路径；默认 true 避免未设字段时落到关组件引导
    componentEnabled: true,
    installed: false,
    running: false,
    pid: 0,
    version: '',
    workDir: '',
    webAddr: '127.0.0.1:3000',
    dnsPort: 1053,
    wiring: 'off',
    wiringLabel: '未对接',
    entryPath: '/adguard/',
    ...partial,
  }
}

describe('AdGuardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    clearPageChrome()
  })

  it('未安装时展示下载安装 CTA，不挂 iframe', async () => {
    mockedApi.get.mockResolvedValue({ data: statusPayload({ installed: false }) })

    const wrapper = mount(AdGuardView)
    await flushPromises()

    expect(wrapper.find('[data-testid="adguard-install-cta"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('下载并安装 AdGuard Home')
    expect(wrapper.text()).toContain('GPL-3.0')
    expect(wrapper.find('[data-testid="adguard-iframe"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="adguard-start-prompt"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('组件未启用时提示前往系统设置，不展示安装/iframe', async () => {
    mockedApi.get.mockResolvedValue({
      data: statusPayload({ componentEnabled: false, installed: true, running: true }),
    })

    const wrapper = mount(AdGuardView)
    await flushPromises()

    expect(wrapper.find('[data-testid="adguard-component-disabled"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('请在系统设置中启用 AdGuard Home 组件')
    expect(wrapper.find('a[href="/settings#components"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="adguard-install-cta"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="adguard-iframe"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('已安装未运行时提示启动，仍不挂 iframe', async () => {
    mockedApi.get.mockResolvedValue({
      data: statusPayload({ installed: true, running: false, version: 'v0.107.50' }),
    })

    const wrapper = mount(AdGuardView)
    await flushPromises()

    expect(wrapper.find('[data-testid="adguard-start-prompt"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('启动 AdGuard Home')
    expect(wrapper.find('[data-testid="adguard-iframe"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="adguard-install-cta"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('运行中挂 iframe，并写入 page chrome', async () => {
    mockedApi.get.mockResolvedValue({
      data: statusPayload({
        installed: true,
        running: true,
        version: 'v0.107.50',
        wiringLabel: '已对接',
        wiring: 'on',
        entryPath: '/adguard/',
      }),
    })

    const wrapper = mount(AdGuardView)
    await flushPromises()

    const iframe = wrapper.get('[data-testid="adguard-iframe"]')
    expect(iframe.attributes('src')).toBe('/adguard/')
    expect(iframe.attributes('title')).toBe('AdGuard Home')

    const { subtitle, action } = usePageChrome()
    expect(subtitle.value).toBe('已对接')
    expect(action.value?.label).toBe('新标签页')
    expect(action.value?.disabled).toBe(false)

    const header = wrapper.get('[data-testid="adguard-page-header"]')
    expect(header.classes()).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))

    wrapper.unmount()
    expect(subtitle.value).toBe('')
    expect(action.value).toBeNull()
  })
})

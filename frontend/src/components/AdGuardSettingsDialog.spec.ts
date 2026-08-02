import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import AdGuardSettingsDialog from './AdGuardSettingsDialog.vue'

const mockedApi = vi.mocked(api, true)
const body = () => new DOMWrapper(document.body)

function statusPayload(partial: Record<string, unknown> = {}) {
  return {
    componentEnabled: true,
    installed: true,
    running: true,
    pid: 4242,
    version: 'v0.107.50',
    workDir: '/data/adguardhome',
    webAddr: '127.0.0.1:3000',
    dnsPort: 1053,
    dnsMode: 0,
    wiring: 'off',
    wiringLabel: '未对接',
    entryPath: '/adguard/',
    cdnProviders: [],
    autoUpdate: false,
    ...partial,
  }
}

describe('AdGuardSettingsDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mockedApi.get.mockImplementation(async (url: string) => {
      if (url === '/adguard/status') {
        return { data: statusPayload() }
      }
      if (url === '/settings/update') {
        return {
          data: {
            useMihomoProxy: true,
            mihomoProxyUrl: 'http://127.0.0.1:7890',
            cdnProviders: [],
          },
        }
      }
      if (url === '/update/check') {
        return { data: { success: true, message: '检查完成' } }
      }
      return { data: {} }
    })
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.post.mockResolvedValue({ data: { success: true, message: 'ok' } })
  })

  afterEach(() => {
    document.body.removeAttribute('style')
    document.body.innerHTML = ''
  })

  it('打开时渲染运行/端口/版本/DNS 分区与出网说明', async () => {
    // Dialog 内容 teleport 到 body，须 attachTo 才能在 document 中找到
    const wrapper = mount(AdGuardSettingsDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()
    await nextTick()

    expect(body().find('[data-testid="adguard-settings-dialog-body"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-runtime"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-webport"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-version"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-dnsmode"]').exists()).toBe(true)
    expect(body().text()).toContain('运行中')
    expect(body().text()).toContain('v0.107.50')
    expect(body().text()).toContain('未托管')
    expect(body().find('[data-testid="adguard-egress-note"]').text()).toContain(
      '下载出网遵循系统设置',
    )

    wrapper.unmount()
  })
})

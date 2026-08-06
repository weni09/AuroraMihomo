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
    entryPath: '/adguard-ui/',
    cdnProviders: [],
    autoUpdate: false,
    autoUpdateCron: '0 0 4 * * *',
    username: 'admin',
    managedBy: 'process',
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
      if (url === '/adguard/check-update') {
        return { data: { success: true, message: 'AdGuard Home 已是最新' } }
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

  it('打开时渲染账号/运行/端口/版本与自动更新分区', async () => {
    const wrapper = mount(AdGuardSettingsDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()
    await nextTick()

    expect(body().find('[data-testid="adguard-settings-dialog-body"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-account"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-runtime"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-webport"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-dnsport"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-settings-version"]').exists()).toBe(true)
    expect(body().find('[data-testid="adguard-auto-update"]').exists()).toBe(true)
    expect(body().text()).toContain('运行中')
    expect(body().text()).toContain('v0.107.50')
    expect(body().text()).toContain('启用 AdGuard Home 自动更新')
    expect(body().text()).not.toContain('与 Aurora 管理员密码保持同步')
    expect(body().find('[data-testid="adguard-egress-note"]').text()).toContain(
      '下载出网遵循系统设置',
    )

    wrapper.unmount()
  })

  it('服务模式展示系统服务看护文案，自启开关切换走 /adguard/boot', async () => {
    mockedApi.get.mockImplementation(async (url: string) => {
      if (url === '/adguard/status') {
        return { data: statusPayload({ managedBy: 'systemd', desiredRunning: true }) }
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
      return { data: {} }
    })
    const wrapper = mount(AdGuardSettingsDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()
    await nextTick()

    // 服务模式下展示系统服务看护说明与「停止不清自启」语义提示
    expect(body().find('[data-testid="agh-managed-by"]').text()).toContain('systemd 服务看护')
    expect(body().text()).toContain('由系统服务随开机自启')
    expect(body().text()).toContain('手动「停止」只临时停止')

    // 点击自启开关 → PUT /adguard/boot（服务模式驱动 systemctl enable/disable）
    const sw = body().find('[data-testid="agh-boot-switch"] [role="switch"]')
    expect(sw.exists()).toBe(true)
    await sw.trigger('click')
    await flushPromises()
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/boot', { enabled: false })

    wrapper.unmount()
  })

  it('Web 监听区展示监听地址输入框，保存时带 host', async () => {
    const wrapper = mount(AdGuardSettingsDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()
    await nextTick()

    const hostInput = body().find('[data-testid="agh-web-host"]')
    expect(hostInput.exists()).toBe(true)
    // 默认从 status.webAddr 解析出 host
    expect((hostInput.element as HTMLInputElement).value).toBe('127.0.0.1')

    // 改 host 为 0.0.0.0 后保存 → PUT /adguard/web-port 带 host
    await hostInput.setValue('0.0.0.0')
    await body().find('[data-testid="adguard-settings-webport"] button').trigger('click')
    await flushPromises()
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/web-port', { port: 3000, host: '0.0.0.0' })

    wrapper.unmount()
  })

  it('exec 模式不显示系统服务文案，自启提示为面板重启拉起', async () => {
    const wrapper = mount(AdGuardSettingsDialog, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()
    await nextTick()

    expect(body().find('[data-testid="agh-managed-by"]').exists()).toBe(false)
    expect(body().text()).toContain('面板重启时自动拉起')

    wrapper.unmount()
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import { useSettingsStore } from './settings'
import { useNotifyStore } from './notify'

const mockedApi = vi.mocked(api, true)

describe('useSettingsStore self-update & backup', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('backupDatabase 调用 POST /system/backup', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '数据库备份完成' } })
    const store = useSettingsStore()
    await store.backupDatabase()
    expect(mockedApi.post).toHaveBeenCalledWith('/system/backup')
    // 完成即复位标志，不残留占用态
    expect(store.backingUp).toBe(false)
  })

  it('checkSelfUpdate 拉取并保存主程序版本信息', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        configured: true,
        currentVersion: 'v0.3.4',
        latestVersion: 'v0.4.0',
        updateAvailable: true,
        message: '发现新版本 v0.4.0',
      },
    })
    const store = useSettingsStore()
    await store.checkSelfUpdate()
    expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/check')
    expect(store.selfUpdateInfo?.updateAvailable).toBe(true)
    expect(store.selfUpdateInfo?.latestVersion).toBe('v0.4.0')
    expect(store.checkingSelfUpdate).toBe(false)
  })

  it('未配置仓库时保留 configured=false 供前端提示', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        configured: false,
        currentVersion: 'v0.3.4',
        message: '主程序自升级未配置',
      },
    })
    const store = useSettingsStore()
    await store.checkSelfUpdate()
    expect(store.selfUpdateInfo?.configured).toBe(false)
  })

  it('updateSelf 异步触发 POST 并开始轮询状态，完成后停止', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '升级已开始' } })
    // 首拉：进行中
    mockedApi.get
      .mockResolvedValueOnce({ data: { running: true, phase: 'downloading', percent: 50, message: '下载中' } })
    const store = useSettingsStore()
    await store.updateSelf()
    expect(mockedApi.post).toHaveBeenCalledWith('/system/self-update')
    await vi.waitFor(() => {
      expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/status')
    })
    expect(store.selfUpdateStatus?.phase).toBe('downloading')
    expect(store.selfUpdateStatus?.percent).toBe(50)
    // 第二次轮询返回完成态 → 停止轮询、复位标志
    mockedApi.get.mockResolvedValueOnce({ data: { running: false, phase: 'idle', percent: 0, message: '' } })
    await store.pollSelfUpdate()
    expect(store.updatingSelf).toBe(false)
    expect(store.selfUpdatePollTimer).toBeNull()
  })

  it('升级完成（服务重启后恢复 idle）自动刷新数据', async () => {
    localStorage.setItem('aurora_token', 'test-token')
    const store = useSettingsStore()
    store.updatingSelf = true
    store.selfUpdateStatus = { running: true, phase: 'restarting', percent: 0, message: '即将重启', targetVersion: 'v0.12.0' }
    // /status 返回 idle（服务已重启、内存态清空）；随后 fetch、宿主状态、check 被调用
    mockedApi.get.mockImplementation((url: string) => {
      if (url === '/system/self-update/status') {
        return Promise.resolve({ data: { running: false, phase: 'idle', percent: 0, message: '' } })
      }
      if (url === '/settings/update') {
        return Promise.resolve({
          data: {
            autoUpdateEnabled: true,
            autoUpdateCron: '0 0 4 * * *',
            cdnProviders: [],
            useMihomoProxy: true,
            selfRepo: 'weni09/AuroraMihomo',
            mihomoPath: '',
            zashboardDir: '',
            mihomoPresent: false,
            zashboardPresent: false,
            defaultCDN: [],
            logRetentionDays: 7,
            logCleanupCron: '0 30 3 * * *',
            logCleanupEnabled: true,
            monitorEnabled: true,
            monitorIntervalSec: 3,
          },
        })
      }
      if (url === '/system/status') {
        return Promise.resolve({
          data: { status: 'stopped', version: '', pid: 0, appVersion: 'v0.12.0' },
        })
      }
      if (url === '/system/self-update/check') {
        return Promise.resolve({
          data: {
            configured: true,
            currentVersion: 'v0.12.0',
            latestVersion: 'v0.12.0',
            updateAvailable: false,
            message: '当前已是最新版本 (v0.12.0)',
          },
        })
      }
      return Promise.resolve({ data: {} })
    })
    await store.pollSelfUpdate()
    // 升级完成：状态清空、标志复位、数据已刷新
    expect(store.updatingSelf).toBe(false)
    expect(store.selfUpdateStatus).toBeNull()
    // 完成状态已记录：升级区块显示「已升级到 v0.12.0」
    expect(store.selfUpdateDone?.version).toBe('v0.12.0')
    await vi.waitFor(() => {
      expect(store.settings?.selfRepo).toBe('weni09/AuroraMihomo')
      expect(store.selfUpdateInfo?.updateAvailable).toBe(false)
    })
    expect(mockedApi.get).toHaveBeenCalledWith('/settings/update')
    expect(mockedApi.get).toHaveBeenCalledWith('/system/status')
    expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/check')
    const { useMihomoStore } = await import('./mihomo')
    expect(useMihomoStore().appVersion).toBe('v0.12.0')
  })

  it('refreshSelfUpdateInfo 静默拉取不弹 toast', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        configured: true,
        currentVersion: 'v0.11.15',
        latestVersion: 'v0.11.16',
        updateAvailable: true,
        message: '发现新版本 v0.11.16',
      },
    })
    const store = useSettingsStore()
    const notifySpy = vi.spyOn(useNotifyStore(), 'success')
    await store.refreshSelfUpdateInfo(true)
    expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/check')
    expect(store.selfUpdateInfo?.latestVersion).toBe('v0.11.16')
    // silent=true 不应弹「发现新版本」toast
    expect(notifySpy).not.toHaveBeenCalled()
  })

  it('新升级开始时清除旧的完成状态', async () => {
    const store = useSettingsStore()
    store.selfUpdateDone = { version: 'v0.11.15', at: '2026-08-13' }
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '升级已开始' } })
    mockedApi.get.mockResolvedValueOnce({ data: { running: true, phase: 'downloading', percent: 10, message: '下载中' } })
    await store.updateSelf()
    expect(store.selfUpdateDone).toBeNull()
  })

  it('轮询到 failed 状态时展示错误并停止', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '升级已开始' } })
    mockedApi.get
      .mockResolvedValueOnce({ data: { running: true, phase: 'downloading', percent: 10, message: '下载中' } })
    const store = useSettingsStore()
    await store.updateSelf()
    await vi.waitFor(() => {
      expect(mockedApi.get).toHaveBeenCalledWith('/system/self-update/status')
    })
    mockedApi.get.mockResolvedValueOnce({
      data: { running: false, phase: 'failed', error: { code: 'download_failed', message: '所有下载源均失败' } },
    })
    await store.pollSelfUpdate()
    expect(store.selfUpdateStatus?.phase).toBe('failed')
    expect(store.selfUpdateStatus?.error?.code).toBe('download_failed')
    expect(store.updatingSelf).toBe(false)
    expect(store.selfUpdatePollTimer).toBeNull()
  })

  it('下载中单次轮询失败不停止，连续 5 次才停', async () => {
    const store = useSettingsStore()
    store.updatingSelf = true
    store.selfUpdateStatus = { running: true, phase: 'downloading', percent: 10, message: '下载中' }
    mockedApi.get.mockRejectedValue(new Error('transient'))
    await store.pollSelfUpdate()
    expect(store.updatingSelf).toBe(true)
    expect(store.selfUpdatePollFails).toBe(1)
    for (let i = 0; i < 4; i++) {
      await store.pollSelfUpdate()
    }
    expect(store.updatingSelf).toBe(false)
    expect(store.selfUpdatePollFails).toBe(5)
  })

  it('restarting 阶段轮询失败不停止，等服务重启恢复', async () => {
    const store = useSettingsStore()
    store.updatingSelf = true
    store.selfUpdateStatus = { running: true, phase: 'restarting', percent: 0, message: '即将重启' }
    // 断连不计数、不停轮询
    mockedApi.get.mockRejectedValue(new Error('connection reset'))
    await store.pollSelfUpdate()
    expect(store.updatingSelf).toBe(true)
    expect(store.selfUpdatePollFails).toBe(0)
  })

  it('重复触发被防抖：并发点第二次 updateSelf 不发请求', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true } })
    const store = useSettingsStore()
    store.updatingSelf = true // 模拟已有一次升级在跑
    await store.updateSelf()
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('save 携带 selfRepo 到 PUT /settings/update', async () => {
    mockedApi.put.mockResolvedValue({
      data: {
        autoUpdateEnabled: true,
        autoUpdateCron: '0 0 4 * * *',
        cdnProviders: [],
        useMihomoProxy: true,
        selfRepo: 'myuser/AuroraMihomo',
        mihomoPath: '',
        zashboardDir: '',
        mihomoPresent: false,
        zashboardPresent: false,
        defaultCDN: [],
        logRetentionDays: 7,
        logCleanupCron: '0 30 3 * * *',
        logCleanupEnabled: true,
        monitorEnabled: true,
        monitorIntervalSec: 3,
      },
    })
    const store = useSettingsStore()
    await store.save({ selfRepo: 'myuser/AuroraMihomo' })
    expect(mockedApi.put).toHaveBeenCalledWith(
      '/settings/update',
      expect.objectContaining({ selfRepo: 'myuser/AuroraMihomo' }),
    )
    expect(store.settings?.selfRepo).toBe('myuser/AuroraMihomo')
  })

  it('save 携带 rawCdnProviders 到 PUT /settings/update', async () => {
    mockedApi.put.mockResolvedValue({
      data: {
        autoUpdateEnabled: true,
        autoUpdateCron: '0 0 4 * * *',
        cdnProviders: [],
        rawCdnProviders: ['ghproxy.com', 'github'],
        useMihomoProxy: true,
        selfRepo: 'weni09/AuroraMihomo',
        mihomoPath: '',
        zashboardDir: '',
        mihomoPresent: false,
        zashboardPresent: false,
        defaultCDN: [],
        defaultRawCDN: ['github', 'ghproxy.com'],
        logRetentionDays: 7,
        logCleanupCron: '0 30 3 * * *',
        logCleanupEnabled: true,
        monitorEnabled: true,
        monitorIntervalSec: 3,
      },
    })
    const store = useSettingsStore()
    await store.save({ rawCdnProviders: ['ghproxy.com', 'github'] })
    expect(mockedApi.put).toHaveBeenCalledWith(
      '/settings/update',
      expect.objectContaining({ rawCdnProviders: ['ghproxy.com', 'github'] }),
    )
    expect(store.settings?.rawCdnProviders).toEqual(['ghproxy.com', 'github'])
    expect(store.settings?.defaultRawCDN).toEqual(['github', 'ghproxy.com'])
  })
})

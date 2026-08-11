import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import { useSettingsStore } from './settings'

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

  it('updateSelf 调用 POST /system/self-update 并复位标志', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '新版本已下载，即将重启生效' } })
    const store = useSettingsStore()
    await store.updateSelf()
    expect(mockedApi.post).toHaveBeenCalledWith('/system/self-update')
    expect(store.updatingSelf).toBe(false)
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
})

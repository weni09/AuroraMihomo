import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import { useMihomoStore } from './mihomo'

const mockedApi = vi.mocked(api, true)

describe('useMihomoStore 内核守护（期望运行）', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    // fetchStatus 有未登录 guard（无 token 直接 return），
    // 测试需模拟已登录态
    localStorage.setItem('aurora_token', 'test-token')
  })

  afterEach(() => {
    localStorage.removeItem('aurora_token')
  })

  it('fetchStatus 映射 desiredRunning', async () => {
    mockedApi.get.mockResolvedValue({
      data: { status: 'running', version: 'v1.2.3', pid: 42, desiredRunning: false },
    })
    const store = useMihomoStore()
    await store.fetchStatus()
    expect(store.desiredRunning).toBe(false)
  })

  it('applyStatus 只认布尔 desiredRunning，缺省保持默认 true', () => {
    const store = useMihomoStore()
    expect(store.desiredRunning).toBe(true)
    store.applyStatus({ status: 'stopped' })
    expect(store.desiredRunning).toBe(true)
    store.applyStatus({ desiredRunning: false })
    expect(store.desiredRunning).toBe(false)
  })

  it('setBoot(true) 调用 PUT /mihomo/boot 并更新状态', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: '内核守护已开启' } })
    const store = useMihomoStore()
    store.desiredRunning = false
    await store.setBoot(true)
    expect(mockedApi.put).toHaveBeenCalledWith('/mihomo/boot', { enabled: true })
    expect(store.desiredRunning).toBe(true)
  })

  it('setBoot(false) 失败时不覆盖本地状态', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: false, message: '失败' } })
    const store = useMihomoStore()
    store.desiredRunning = true
    await store.setBoot(false)
    expect(mockedApi.put).toHaveBeenCalledWith('/mihomo/boot', { enabled: false })
    // success:false → 不本地覆盖
    expect(store.desiredRunning).toBe(true)
  })

  it('start/stop 走 runAction 并在成功后刷新状态（含 desiredRunning）', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '内核已停止' } })
    mockedApi.get.mockResolvedValue({
      data: { status: 'stopped', version: 'v1.2.3', pid: 0, desiredRunning: false },
    })
    const store = useMihomoStore()
    await store.stop()
    expect(mockedApi.post).toHaveBeenCalledWith('/mihomo/stop')
    expect(store.desiredRunning).toBe(false)
  })
})

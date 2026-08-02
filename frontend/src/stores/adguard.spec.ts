import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import { useAdGuardStore } from './adguard'

const mockedApi = vi.mocked(api, true)

describe('useAdGuardStore component controls', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('emptyStatus 默认 componentEnabled=false', () => {
    const store = useAdGuardStore()
    expect(store.status.componentEnabled).toBe(false)
  })

  it('fetchStatus 映射 componentEnabled', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        installed: false,
        running: false,
        entryPath: '/adguard/',
      },
    })
    const store = useAdGuardStore()
    await store.fetchStatus()
    expect(store.status.componentEnabled).toBe(true)
    expect(mockedApi.get).toHaveBeenCalledWith('/adguard/status')
  })

  it('setComponent 调用 PUT /adguard/component', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: { componentEnabled: true, installed: false, running: false, entryPath: '/adguard/' },
    })
    const store = useAdGuardStore()
    await store.setComponent(true)
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/component', { enabled: true })
    expect(store.status.componentEnabled).toBe(true)
  })

  it('uninstall 调用 POST /adguard/uninstall', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '已卸载' } })
    mockedApi.get.mockResolvedValue({
      data: { componentEnabled: false, installed: false, running: false, entryPath: '/adguard/' },
    })
    const store = useAdGuardStore()
    await store.uninstall(true)
    expect(mockedApi.post).toHaveBeenCalledWith('/adguard/uninstall', { confirm: true })
  })
})

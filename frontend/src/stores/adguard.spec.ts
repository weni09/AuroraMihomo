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
        entryPath: '/adguard-ui/',
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
      data: { componentEnabled: true, installed: false, running: false, entryPath: '/adguard-ui/' },
    })
    const store = useAdGuardStore()
    await store.setComponent(true)
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/component', { enabled: true })
    expect(store.status.componentEnabled).toBe(true)
  })

  it('uninstall 调用 POST /adguard/uninstall', async () => {
    mockedApi.post.mockResolvedValue({ data: { success: true, message: '已卸载' } })
    mockedApi.get.mockResolvedValue({
      data: { componentEnabled: false, installed: false, running: false, entryPath: '/adguard-ui/' },
    })
    const store = useAdGuardStore()
    await store.uninstall(true)
    expect(mockedApi.post).toHaveBeenCalledWith('/adguard/uninstall', { confirm: true })
  })

  it('fetchStatus 映射 dnsMode / cdnProviders', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        installed: true,
        running: false,
        dnsMode: 2,
        cdnProviders: ['https://cdn.example/'],
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.fetchStatus()
    expect(store.status.dnsMode).toBe(2)
    expect(store.status.cdnProviders).toEqual(['https://cdn.example/'])
  })

  it('setWebPort 调用 PUT /adguard/web-port', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        installed: true,
        running: false,
        webAddr: '127.0.0.1:4123',
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.setWebPort(4123)
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/web-port', { port: 4123 })
    expect(store.status.webAddr).toBe('127.0.0.1:4123')
  })

  it('setDnsMode 调用 PUT /adguard/dns-mode', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        installed: true,
        running: true,
        dnsMode: 1,
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.setDnsMode(1)
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/dns-mode', { mode: 1 })
    expect(store.status.dnsMode).toBe(1)
  })

  it('setCdnProviders 调用 PUT /adguard/cdn', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        installed: true,
        running: false,
        cdnProviders: ['https://a.example/'],
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.setCdnProviders(['https://a.example/'])
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/cdn', { providers: ['https://a.example/'] })
  })

  it('setCredentials 调用 PUT /adguard/credentials', async () => {
    mockedApi.put.mockResolvedValue({ data: { success: true, message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        username: 'admin',
        passwordSync: true,
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.setCredentials({
      username: 'admin',
      password: 'new-secret',
      syncWithAurora: true,
    })
    expect(mockedApi.put).toHaveBeenCalledWith('/adguard/credentials', {
      username: 'admin',
      password: 'new-secret',
      syncWithAurora: true,
    })
    expect(store.status.passwordSync).toBe(true)
  })

  it('fetchStatus 映射 username / passwordSync', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        componentEnabled: true,
        username: 'agh',
        passwordSync: true,
        entryPath: '/adguard-ui/',
      },
    })
    const store = useAdGuardStore()
    await store.fetchStatus()
    expect(store.status.username).toBe('agh')
    expect(store.status.passwordSync).toBe(true)
  })
})

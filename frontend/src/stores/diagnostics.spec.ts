import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import { useDiagnosticsStore } from './diagnostics'

const mockedApi = vi.mocked(api, true)

describe('useDiagnosticsStore 网络诊断', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('run 发 POST /diagnostics/run 保存 requestId、置 running=true', async () => {
    mockedApi.post.mockResolvedValue({ data: { requestId: 'req-123' } })
    const store = useDiagnosticsStore()
    const targets = [
      { type: 'ping' as const, target: '1.1.1.1' },
      { type: 'tcp' as const, target: 'example.com', port: 443 },
    ]
    await store.run(targets, '/home/aurora')

    expect(mockedApi.post).toHaveBeenCalledWith('/diagnostics/run', {
      targets,
      path: '/home/aurora',
    })
    expect(store.requestId).toBe('req-123')
    expect(store.running).toBe(true)
  })

  it('run 失败时置 error 并复位 running，异常继续上抛', async () => {
    mockedApi.post.mockRejectedValue(new Error('network down'))
    const store = useDiagnosticsStore()

    await expect(store.run([], '/tmp')).rejects.toThrow('network down')
    expect(store.error).toBe('诊断启动失败')
    expect(store.running).toBe(false)
  })

  it('handleProgress 只收匹配 requestId 的事件，忽略其它', () => {
    const store = useDiagnosticsStore()
    store.requestId = 'req-123'

    store.handleProgress('diagnostic.progress', {
      requestId: 'req-123',
      target: '1.1.1.1',
      type: 'ping',
      path: '/home/aurora',
      status: 'success',
      latencyMs: 12,
    })
    expect(store.results).toHaveLength(1)

    // 其它请求的事件不混入
    store.handleProgress('diagnostic.progress', {
      requestId: 'req-999',
      target: '1.1.1.1',
      type: 'ping',
      path: '/home/aurora',
      status: 'fail',
      error: 'timeout',
    })
    // 非诊断类型的事件同样忽略
    store.handleProgress('other.event', {
      requestId: 'req-123',
      target: '1.1.1.1',
      type: 'ping',
      path: '/home/aurora',
      status: 'success',
    })
    expect(store.results).toHaveLength(1)
  })

  it('fetchResult done=true 时回填 results、running=false', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        done: true,
        results: [
          {
            target: '1.1.1.1',
            type: 'ping',
            path: '/home/aurora',
            status: 'success',
            latencyMs: 12,
          },
        ],
      },
    })
    const store = useDiagnosticsStore()
    store.running = true

    const done = await store.fetchResult('req-123')
    expect(done).toBe(true)
    expect(mockedApi.get).toHaveBeenCalledWith('/diagnostics/result/req-123')
    expect(store.results).toHaveLength(1)
    expect(store.results[0]?.latencyMs).toBe(12)
    expect(store.running).toBe(false)
  })

  it('fetchResult done=false 时不回填、保持 running，供继续轮询', async () => {
    mockedApi.get.mockResolvedValue({ data: { done: false } })
    const store = useDiagnosticsStore()
    store.running = true
    store.results = [
      {
        target: '1.1.1.1',
        type: 'ping',
        path: '/home/aurora',
        status: 'success',
      },
    ]

    const done = await store.fetchResult('req-123')
    expect(done).toBe(false)
    expect(store.running).toBe(true)
    expect(store.results).toHaveLength(1)
  })

  it('reset 清空运行状态与结果', () => {
    const store = useDiagnosticsStore()
    store.running = true
    store.requestId = 'req-123'
    store.error = '诊断启动失败'
    store.results = [
      {
        target: '1.1.1.1',
        type: 'ping',
        path: '/home/aurora',
        status: 'success',
      },
    ]

    store.reset()
    expect(store.running).toBe(false)
    expect(store.requestId).toBe('')
    expect(store.results).toEqual([])
    expect(store.error).toBe('')
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// api 必须在导入组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), delete: vi.fn() },
}))

// 真实 WebSocket 在 happy-dom 下连不上，且本测试关心的是"收到事件后怎么处理"。
// 替身把 onEvent 回调暴露出来，让测试能直接投递内核日志事件。
let emitRealtime: ((type: string, data: unknown, raw: unknown) => void) | undefined
vi.mock('../composables/useRealtime', () => ({
  useRealtime: (handler: (type: string, data: unknown, raw: unknown) => void) => {
    emitRealtime = handler
    return { status: { value: 'live' } }
  },
}))

import api from '../api'
import LogsView from './LogsView.vue'

const mockedApi = vi.mocked(api, true)

type KernelLine = { time: string; stream: string; level?: string; message: string }

/** 记录每次内核日志请求的 URL，用于断言级别筛选是否真的传给了后端 */
const kernelRequests: string[] = []

const mockLogs = (kernelByLevel: (level: string) => KernelLine[]) => {
  mockedApi.get.mockImplementation((url: string) => {
    if (url.startsWith('/mihomo/logs')) {
      kernelRequests.push(url)
      const level = new URL(url, 'http://x').searchParams.get('level') || ''
      return Promise.resolve({ data: kernelByLevel(level) })
    }
    return Promise.resolve({ data: { logs: [], total: 0 } })
  })
}

const mountView = async () => {
  const w = mount(LogsView)
  await new Promise((r) => setTimeout(r, 0))
  await w.vm.$nextTick()
  return w
}

/** 用真实的 USER-AGENT 告警作为噪音样本，这是本功能的动机场景 */
const warnLine = (n: number): KernelLine => ({
  time: `2026-08-01T03:31:${String(n).padStart(2, '0')}Z`,
  stream: 'stdout',
  level: 'warning',
  message: `parse classical rule [USER-AGENT,app${n}*] error: unsupported rule type: USER-AGENT`,
})

const errLine: KernelLine = {
  time: '2026-08-01T03:30:00Z',
  stream: 'stderr',
  level: 'error',
  message: 'Parse config error',
}

/** 后端自己写的 system 行没有级别 */
const systemLine: KernelLine = {
  time: '2026-08-01T03:29:00Z',
  stream: 'system',
  message: 'mihomo started pid=123',
}

describe('LogsView 内核日志级别', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    kernelRequests.length = 0
    emitRealtime = undefined
  })

  it('首次加载不带 level 参数，级别标签按级别渲染', async () => {
    mockLogs(() => [errLine, warnLine(1), systemLine])
    const w = await mountView()

    expect(kernelRequests).toHaveLength(1)
    expect(kernelRequests[0]).not.toContain('level=')

    const text = w.text()
    expect(text).toContain('[错误]')
    expect(text).toContain('[告警]')
    // 无级别的 system 行不该被硬填一个级别标签
    expect(text).toContain('mihomo started pid=123')
    expect(text).not.toContain('[日志]')
  })

  it('选择级别后重新向后端拉取，而不是只筛本地列表', async () => {
    // 关键回归：本地列表全是 warning，若只在前端筛 error 会得到空列表。
    // 后端在更大的缓冲上先筛后截，能把被挤掉的 error 捞回来。
    mockLogs((level) => (level === 'error' ? [errLine] : Array.from({ length: 20 }, (_, i) => warnLine(i))))
    const w = await mountView()

    expect(w.text()).not.toContain('Parse config error')

    const select = w.findAllComponents({ name: 'Select' })[0]
    await select!.setValue('error')
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()

    expect(kernelRequests.some((u) => u.includes('level=error'))).toBe(true)
    expect(w.text()).toContain('Parse config error')
    expect(w.text()).not.toContain('unsupported rule type')
  })

  it('筛选生效期间，实时推送的不匹配级别不再涌入', async () => {
    mockLogs((level) => (level === 'error' ? [errLine] : [warnLine(1)]))
    const w = await mountView()

    const select = w.findAllComponents({ name: 'Select' })[0]
    await select!.setValue('error')
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()

    // 筛着 error 时来了一条 warning：不该显示
    emitRealtime?.('log.message', warnLine(2), { at: '2026-08-01T03:32:00Z' })
    await w.vm.$nextTick()
    expect(w.text()).not.toContain('unsupported rule type')

    // 无级别的行始终保留：筛 error 却看不到崩溃栈是反直觉的
    emitRealtime?.('log.message', systemLine, { at: '2026-08-01T03:32:01Z' })
    await w.vm.$nextTick()
    expect(w.text()).toContain('mihomo started pid=123')
  })

  it('拉取失败时保留现有列表，不清空成"看起来筛不出内容"', async () => {
    mockLogs(() => [warnLine(1)])
    const w = await mountView()
    expect(w.text()).toContain('unsupported rule type')

    mockedApi.get.mockRejectedValueOnce(new Error('boom'))
    const select = w.findAllComponents({ name: 'Select' })[0]
    await select!.setValue('error')
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()

    expect(w.text()).toContain('unsupported rule type')
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'

// api 必须在导入组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}))

// useRealtime 内部会 new WebSocket，happy-dom 下连不上，且本测试不关心
// 真实连接。替身把 onEvent 回调暴露出来，让测试能直接投递进度事件，
// status 固定为已连接（页面不展示「轮询兜底」提示）。
let emitRealtime: ((type: string, data: unknown, raw: unknown) => void) | undefined
vi.mock('../composables/useRealtime', () => ({
  useRealtime: (handler: (type: string, data: unknown, raw: unknown) => void) => {
    emitRealtime = handler
    return { status: { value: 'live' } }
  },
}))

import api from '../api'
import DiagnosticsView from './DiagnosticsView.vue'
import { useDiagnosticsStore, type ProbeResult } from '../stores/diagnostics'

const mockedApi = vi.mocked(api, true)

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useDiagnosticsStore()
  const wrapper = mount(DiagnosticsView)
  return { wrapper, store }
}

describe('DiagnosticsView 网络诊断页', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    emitRealtime = undefined
    mockedApi.post.mockResolvedValue({ data: { requestId: 'req-1' } })
    mockedApi.get.mockResolvedValue({ data: { done: false } })
  })

  it('渲染页头、预设一键全测按钮与手动输入区', () => {
    const { wrapper } = mountView()
    const text = wrapper.text()
    expect(text).toContain('网络诊断')
    expect(text).toContain('从面板宿主机视角排查出网问题')
    // 预设按钮与手动「诊断」按钮都在
    const buttons = wrapper.findAll('button').map((b) => b.text())
    expect(buttons).toContain('开始诊断')
    expect(buttons).toContain('诊断')
    // 手动输入区：目标 + 类型 + 端口（tcp 时显示）
    expect(wrapper.find('input#diag-target').exists()).toBe(true)
    expect(wrapper.find('input#diag-port').exists()).toBe(true)
    // 默认未运行、无结果：结果区不出现
    expect(text).not.toContain('诊断结果')
    wrapper.unmount()
  })

  it('点击「开始诊断」以预设目标调用 store.run，进入进行中状态', async () => {
    const { wrapper } = mountView()
    const presetBtn = wrapper.findAll('button').find((b) => b.text() === '开始诊断')
    expect(presetBtn).toBeDefined()

    await presetBtn!.trigger('click')
    await flushPromises()

    expect(mockedApi.post).toHaveBeenCalledTimes(1)
    const [, payload] = mockedApi.post.mock.calls[0] as unknown as [
      string,
      { path: string; targets: Array<{ type: string; target: string; port?: number }> },
    ]
    expect(payload.path).toBe('both')
    const targets = payload.targets
    // 预设目标覆盖 GitHub API / raw / 公共 DNS（含 Do53 端口）
    expect(targets).toContainEqual({ type: 'dns', target: 'api.github.com' })
    expect(targets).toContainEqual({ type: 'tcp', target: 'api.github.com', port: 443 })
    expect(targets).toContainEqual({ type: 'http', target: 'https://api.github.com/' })
    expect(targets).toContainEqual({ type: 'tcp', target: 'raw.githubusercontent.com', port: 443 })
    expect(targets).toContainEqual({ type: 'http', target: 'https://raw.githubusercontent.com/' })
    expect(targets).toContainEqual({ type: 'dns', target: '1.1.1.1' })
    expect(targets).toContainEqual({ type: 'tcp', target: '1.1.1.1', port: 53 })
    expect(targets).toContainEqual({ type: 'dns', target: '8.8.8.8' })
    expect(targets).toContainEqual({ type: 'tcp', target: '8.8.8.8', port: 53 })
    expect(targets).toContainEqual({ type: 'dns', target: '223.5.5.5' })
    expect(targets).toContainEqual({ type: 'tcp', target: '223.5.5.5', port: 53 })

    // running=true：结果区出现「进行中…」与清空按钮
    expect(wrapper.text()).toContain('进行中…')
    expect(wrapper.text()).toContain('清空')
    wrapper.unmount()
  })

  it('渲染诊断结果：Badge 状态、路径、延迟与错误', async () => {
    const results: ProbeResult[] = [
      { target: 'api.github.com', type: 'dns', path: 'direct', status: 'success', latencyMs: 12 },
      { target: 'api.github.com', type: 'dns', path: 'proxy', status: 'fail', error: 'connect timeout' },
      { target: '8.8.8.8', type: 'tcp', path: 'direct', status: 'timeout', latencyMs: 5000 },
    ]
    const { wrapper, store } = mountView()
    // pinia 状态是响应式的：挂载后预置 results 即可触发重渲染
    store.results = results
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('诊断结果')
    // 状态徽标与文本
    expect(text).toContain('success')
    expect(text).toContain('fail')
    expect(text).toContain('timeout')
    // 路径
    expect(text).toContain('direct')
    expect(text).toContain('proxy')
    // 目标与类型
    expect(text).toContain('api.github.com')
    expect(text).toContain('8.8.8.8')
    // 延迟
    expect(text).toContain('12ms')
    expect(text).toContain('5000ms')
    // 错误信息
    expect(text).toContain('connect timeout')
    // 未运行时不显示「进行中…」
    expect(text).not.toContain('进行中…')

    // Badge 变体映射：success → ok（success 配色）、fail → err（destructive 配色）
    const successBadge = wrapper.findAll('span').find((s) => s.text() === 'success')
    expect(successBadge?.classes().join(' ')).toContain('bg-success')
    const failBadge = wrapper.findAll('span').find((s) => s.text() === 'fail')
    expect(failBadge?.classes().join(' ')).toContain('bg-destructive')
    const timeoutBadge = wrapper.findAll('span').find((s) => s.text() === 'timeout')
    expect(timeoutBadge?.classes().join(' ')).toContain('bg-elevated')

    wrapper.unmount()
  })

  it('无结果且未运行时隐藏结果区', () => {
    const { wrapper } = mountView()
    const text = wrapper.text()
    expect(text).not.toContain('诊断结果')
    expect(text).not.toContain('清空')
    expect(text).not.toContain('进行中…')
    wrapper.unmount()
  })

  it('WS 进度事件按 requestId 写入 store 并渲染', async () => {
    const { wrapper, store } = mountView()
    // 模拟一次已启动的诊断（store.run 已置 requestId/running）
    store.requestId = 'req-1'
    store.running = true

    expect(emitRealtime).toBeDefined()
    emitRealtime!(
      'diagnostic.progress',
      {
        requestId: 'req-1',
        target: 'example.com',
        type: 'http',
        path: 'direct',
        status: 'success',
        latencyMs: 8,
      },
      {},
    )

    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('example.com')
    expect(text).toContain('direct')
    expect(text).toContain('8ms')
    wrapper.unmount()
  })
})

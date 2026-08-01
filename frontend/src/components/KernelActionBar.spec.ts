import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, DOMWrapper, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import KernelActionBar from './KernelActionBar.vue'
import { useMihomoStore } from '../stores/mihomo'

// api 在 store 模块求值时就会持有引用；更新内核路径也依赖它
vi.mock('../api', () => ({
  default: {
    post: vi.fn(),
    get: vi.fn(),
  },
}))

/**
 * KernelActionBar：启停/重启/重载等操作条。
 *
 * 停止与重启会中断代理连接，必须先走 ModalDialog 确认，确认前不得调用 store。
 * Dialog 内容经 DialogPortal teleport 到 document.body，断言从 body 取。
 */

const body = () => new DOMWrapper(document.body)

function mountBar(props: Record<string, unknown> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useMihomoStore()
  const stopSpy = vi.spyOn(store, 'stop').mockResolvedValue(undefined as never)
  const restartSpy = vi.spyOn(store, 'restart').mockResolvedValue(undefined as never)
  const wrapper = mount(KernelActionBar, {
    props,
    attachTo: document.body,
    global: { plugins: [pinia] },
  })
  return { wrapper, store, stopSpy, restartSpy }
}

afterEach(() => {
  // reka-ui Dialog 会在 body 上留 style / portal 节点，避免污染后续用例
  document.body.removeAttribute('style')
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('KernelActionBar 危险操作确认', () => {
  it('点击停止先弹出确认，确认前不调用 stop', async () => {
    const { wrapper, stopSpy } = mountBar()

    await wrapper.get('[data-testid="kernel-stop"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(stopSpy).not.toHaveBeenCalled()
    expect(body().find('[role="dialog"]').exists()).toBe(true)

    await body().get('[data-testid="kernel-confirm-ok"]').trigger('click')
    await flushPromises()

    expect(stopSpy).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('取消确认不调用 stop', async () => {
    const { wrapper, stopSpy } = mountBar()

    await wrapper.get('[data-testid="kernel-stop"]').trigger('click')
    await wrapper.vm.$nextTick()
    expect(body().find('[role="dialog"]').exists()).toBe(true)

    await body().get('[data-testid="kernel-confirm-cancel"]').trigger('click')
    await flushPromises()

    expect(stopSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

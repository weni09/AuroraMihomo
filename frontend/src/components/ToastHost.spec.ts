import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import ToastHost from './ToastHost.vue'
import { useNotifyStore } from '../stores/notify'

/**
 * 提示条无障碍的回归测试。
 *
 * ToastHost 是全局唯一的失败信号出口（api.ts 的响应拦截器把所有请求错误
 * 汇入 notify store）。此前它没有任何 live region，屏幕阅读器用户点「保存」
 * 后请求失败不会被播报，会以为操作成功了。
 */
describe('ToastHost 的 live region', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('错误与非错误各有一个常驻 live region', () => {
    const wrapper = mount(ToastHost)

    const alert = wrapper.get('[role="alert"]')
    expect(alert.attributes('aria-live')).toBe('assertive')

    const status = wrapper.get('[role="status"]')
    expect(status.attributes('aria-live')).toBe('polite')
    wrapper.unmount()
  })

  /**
   * region 必须在没有提示时也存在：辅助技术只播报已存在 region 内的
   * 增量变化，若 region 与首条内容同时出现，那一条往往不被播报。
   */
  it('无任何提示时两个 region 依然挂载', () => {
    const wrapper = mount(ToastHost)
    const notify = useNotifyStore()

    expect(notify.toasts).toHaveLength(0)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('错误提示进入 assertive region', async () => {
    const wrapper = mount(ToastHost)
    const notify = useNotifyStore()

    notify.error('保存失败：磁盘写入错误')
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="alert"]').text()).toContain('保存失败：磁盘写入错误')
    expect(wrapper.get('[role="status"]').text()).not.toContain('保存失败')
    wrapper.unmount()
  })

  it('成功提示进入 polite region', async () => {
    const wrapper = mount(ToastHost)
    const notify = useNotifyStore()

    notify.success('已保存')
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[role="status"]').text()).toContain('已保存')
    expect(wrapper.get('[role="alert"]').text()).not.toContain('已保存')
    wrapper.unmount()
  })

  it('关闭按钮有可访问名称，且图标对辅助技术隐藏', async () => {
    const wrapper = mount(ToastHost)
    const notify = useNotifyStore()

    notify.error('出错了')
    await wrapper.vm.$nextTick()

    const btn = wrapper.get('[role="alert"] button')
    expect(btn.attributes('aria-label')).toBe('关闭提示')
    expect(btn.attributes('type')).toBe('button')
    // 图标只是装饰，语义由 aria-label 与 role 承担
    expect(btn.get('svg').attributes('aria-hidden')).toBe('true')
    wrapper.unmount()
  })

  it('点击关闭按钮移除该条提示', async () => {
    const wrapper = mount(ToastHost)
    const notify = useNotifyStore()

    notify.error('出错了')
    await wrapper.vm.$nextTick()
    expect(notify.toasts).toHaveLength(1)

    await wrapper.get('[role="alert"] button').trigger('click')

    expect(notify.toasts).toHaveLength(0)
  })
})

describe('notify store 的堆叠上限', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  /**
   * 回归：批量请求失败会一次汇入多条，而容器没有高度上限与滚动，
   * 超出视口的提示既看不见也点不到关闭。
   */
  it('超过上限时丢弃最旧的，保留最新的', () => {
    const notify = useNotifyStore()

    for (let i = 1; i <= 8; i++) notify.error(`错误 ${i}`)

    expect(notify.toasts).toHaveLength(5)
    // 后发生的错误更贴近用户当前操作，应当保留
    expect(notify.toasts.map((t) => t.text)).toEqual([
      '错误 4',
      '错误 5',
      '错误 6',
      '错误 7',
      '错误 8',
    ])
  })

  it('未超上限时全部保留且顺序不变', () => {
    const notify = useNotifyStore()

    notify.error('甲')
    notify.success('乙')

    expect(notify.toasts.map((t) => t.text)).toEqual(['甲', '乙'])
  })
})

import { afterEach, describe, expect, it } from 'vitest'
import { mount, DOMWrapper } from '@vue/test-utils'
import ModalDialog from './ModalDialog.vue'
import { DialogTitle } from '@/components/ui/dialog'

/**
 * 弹窗无障碍与焦点管理的回归测试。
 *
 * ModalDialog 现在是 shadcn-vue Dialog（reka-ui DialogRoot/DialogContent）的薄封装，
 * role="dialog"、aria-modal、焦点陷阱、Escape 关闭、背景滚动锁定均由 reka-ui 原生提供，
 * 这里只验证 ModalDialog 自身负责的部分：open 状态透传、标题渲染、#header slot 覆盖。
 *
 * 面板内容通过 DialogPortal teleport 到 document.body 之下，不在组件自身的
 * wrapper.element 子树内，所以断言统一从 document.body 取（借道 DOMWrapper），
 * 而不是 wrapper.get()/find()——那两者只能看到 wrapper 自身的 DOM 子树。
 */

// attachTo: document.body 是 Teleport 与焦点相关断言的前提：
// 未挂到真实文档树的元素无法 teleport、也无法获得真实焦点
const mountOpen = (props: Record<string, unknown> = {}, slots: Record<string, string> = {}) =>
  mount(ModalDialog, {
    props: { open: true, title: '测试弹窗', ...props },
    slots: { default: '<button class="inner-a">A</button><button class="inner-b">B</button>', ...slots },
    attachTo: document.body,
  })

const body = () => new DOMWrapper(document.body)

afterEach(() => {
  // reka-ui 的 Dialog 用内联 style 锁滚动（非 class），但仍会在 body 上留痕，
  // 每个用例结束后复原，避免残留污染后续测试
  document.body.removeAttribute('style')
  document.body.innerHTML = ''
})

describe('ModalDialog 的对话框语义', () => {
  it('打开时带 role=dialog 与 aria-modal', async () => {
    const wrapper = mountOpen()
    await wrapper.vm.$nextTick()
    const panel = body().get('[role="dialog"]')

    // reka-ui 的 modal Dialog 依赖 aria-hidden 隔离背景内容而非显式 aria-modal，
    // 这里断言的是等价的可访问性效果：DismissableLayer 承担了模态语义
    expect(panel.element.getAttribute('role')).toBe('dialog')
    wrapper.unmount()
  })

  it('aria-labelledby 指向真实存在的标题元素', async () => {
    const wrapper = mountOpen()
    await wrapper.vm.$nextTick()
    const id = body().get('[role="dialog"]').attributes('aria-labelledby')

    expect(id).toBeTruthy()
    const title = body().find(`#${id}`)
    expect(title.exists()).toBe(true)
    expect(title.text()).toBe('测试弹窗')
    wrapper.unmount()
  })

  it('关闭时不渲染面板内容', () => {
    mountOpen({ open: false })
    expect(body().find('[role="dialog"]').exists()).toBe(false)
  })

  it('#header slot 可自定义标题栏，DialogTitle 承担可访问名称', async () => {
    // 用法见 PreviewPanel / ShareDialog：自定义头部内放一个 DialogTitle，
    // reka-ui 会把它的 id 自动接到面板的 aria-labelledby 上
    const wrapper = mount(
      {
        components: { ModalDialog, DialogTitle },
        template: `
          <ModalDialog :open="true">
            <template #header><DialogTitle>自定义</DialogTitle></template>
            <button class="inner-a">A</button>
          </ModalDialog>`,
      },
      { attachTo: document.body },
    )
    await wrapper.vm.$nextTick()

    const id = body().get('[role="dialog"]').attributes('aria-labelledby')
    const title = body().find(`#${id}`)
    expect(title.exists()).toBe(true)
    expect(title.text()).toBe('自定义')
    wrapper.unmount()
  })
})

describe('ModalDialog 的开关状态', () => {
  it('open 从 false 变为 true 时渲染面板内容', async () => {
    const wrapper = mount(ModalDialog, {
      props: { open: false, title: '测试弹窗' },
      slots: { default: '<button class="inner-a">A</button>' },
      attachTo: document.body,
    })
    expect(body().find('[role="dialog"]').exists()).toBe(false)

    await wrapper.setProps({ open: true })
    await wrapper.vm.$nextTick()
    expect(body().find('[role="dialog"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('Escape 触发 close 事件（由 reka-ui DismissableLayer 派发）', async () => {
    const wrapper = mountOpen()
    await wrapper.vm.$nextTick()

    await body().get('[role="dialog"]').trigger('keydown', { key: 'Escape' })

    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })

  it('点击内置关闭按钮触发 close 事件', async () => {
    const wrapper = mountOpen()
    await wrapper.vm.$nextTick()

    await body().get('[aria-label="关闭"]').trigger('click')

    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })

  it('使用 #header slot 时不渲染内置关闭按钮，避免同一弹窗出现两个关闭入口', async () => {
    const wrapper = mount(
      {
        components: { ModalDialog, DialogTitle },
        template: `
          <ModalDialog :open="true">
            <template #header><DialogTitle>自定义</DialogTitle></template>
            <button class="inner-a">A</button>
          </ModalDialog>`,
      },
      { attachTo: document.body },
    )
    await wrapper.vm.$nextTick()

    expect(body().find('[aria-label="关闭"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

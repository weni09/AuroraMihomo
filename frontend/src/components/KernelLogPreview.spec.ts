import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import KernelLogPreview from './KernelLogPreview.vue'

/**
 * 共享内核日志预览：终端观感列表，供 Dashboard / Mihomo 复用。
 * 只验证空态文案与行字段渲染，不测高度 class 透传细节。
 */
describe('KernelLogPreview', () => {
  it('空列表显示空态文案（暂无/等待）', () => {
    const wrapper = mount(KernelLogPreview, {
      props: { lines: [] },
    })

    expect(wrapper.text()).toMatch(/暂无|等待/)
    wrapper.unmount()
  })

  it('有日志时渲染 time、stream、message', () => {
    const wrapper = mount(KernelLogPreview, {
      props: {
        lines: [
          {
            time: '12:34:56',
            stream: 'stdout',
            message: 'kernel started',
          },
        ],
      },
    })

    const text = wrapper.text()
    expect(text).toContain('12:34:56')
    expect(text).toContain('stdout')
    expect(text).toContain('kernel started')
    wrapper.unmount()
  })
})

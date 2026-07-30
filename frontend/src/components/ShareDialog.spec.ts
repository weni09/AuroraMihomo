import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, DOMWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

// api 必须在导入组件之前 mock：store 模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
    post: vi.fn(),
  },
}))

import api from '../api'
import ShareDialog from './ShareDialog.vue'

const mockedApi = api as unknown as {
  get: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

/**
 * 分享设置弹窗的开合行为。
 *
 * 订阅 / 组合 / 文件三个模块共用这一个组件，所以这里的行为一处坏掉就是三处坏掉。
 * 保存成功后必须自动关闭：此前只 emit('changed') 而漏了 emit('close')，
 * 用户点完保存看不出有没有生效，只能自己去点关闭。
 */
const emptyList = { data: { items: [] } }

const mountDialog = () =>
  mount(ShareDialog, {
    props: {
      open: true,
      kind: 'subscription' as const,
      id: 1,
      name: '测试订阅',
      shareToken: 'tok-1',
    },
    // ModalDialog 内部依赖 teleport 与真实文档树
    attachTo: document.body,
  })

// ModalDialog 的面板内容经 DialogPortal teleport 到 document.body 之下，
// 不在 wrapper.element 的子树内，findAll('button') 找不到它们，
// 断言统一从 document.body 取（借道 DOMWrapper）
const body = () => new DOMWrapper(document.body)

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  mockedApi.get.mockResolvedValue(emptyList)
  document.body.className = ''
  document.body.innerHTML = ''
})

describe('ShareDialog 保存后的开合', () => {
  it('保存成功后触发 close，弹窗随之关闭', async () => {
    mockedApi.put.mockResolvedValueOnce(emptyList)
    const wrapper = mountDialog()
    await wrapper.vm.$nextTick()

    const saveBtn = body().findAll('button').find((b) => b.text().includes('保存设置'))
    expect(saveBtn).toBeDefined()
    await saveBtn!.trigger('click')
    // store.update 内部有 await，需把微任务队列跑干
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    expect(mockedApi.put).toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeTruthy()
    // changed 仍需发出，供调用方刷新列表
    expect(wrapper.emitted('changed')).toBeTruthy()
    wrapper.unmount()
  })

  it('保存失败时不关闭，让用户留在表单里改', async () => {
    mockedApi.put.mockRejectedValueOnce({ response: { data: { message: '有效期格式不对' } } })
    const wrapper = mountDialog()
    await wrapper.vm.$nextTick()

    const saveBtn = body().findAll('button').find((b) => b.text().includes('保存设置'))
    await saveBtn!.trigger('click')
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeFalsy()
    expect(wrapper.emitted('changed')).toBeFalsy()
    wrapper.unmount()
  })

  it('重置与撤销不关闭弹窗——用户要接着看新链接或重新生成', async () => {
    // confirm 会拦住这两个操作，测试环境里直接放行。
    // happy-dom 未实现 window.confirm，spyOn 拿不到函数，只能直接赋值
    const originalConfirm = window.confirm
    window.confirm = () => true
    mockedApi.post.mockResolvedValue(emptyList)

    const wrapper = mountDialog()
    await wrapper.vm.$nextTick()

    const resetBtn = body().findAll('button').find((b) => b.text().includes('重置链接'))
    expect(resetBtn).toBeDefined()
    await resetBtn!.trigger('click')
    await new Promise((r) => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    expect(mockedApi.post).toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeFalsy()

    window.confirm = originalConfirm
    wrapper.unmount()
  })
})

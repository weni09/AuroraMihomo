import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// api 必须在导入 store 之前 mock：store 模块在求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
    post: vi.fn(),
  },
}))

import api from '../api'
import { useConfigStore } from './config'
import { useNotifyStore } from './notify'

const mockedApi = vi.mocked(api, true)

/**
 * 回归测试：基础配置加载失败后不得写回空配置。
 *
 * 修复前的路径是：fetchBase 失败 → model 保持初值 {} → saveBase 把
 * dumpYaml({}) 也就是 "{}" PUT 上去。后端两道守卫都拦不住
 * （"{}" trim 后非空，且是合法 YAML），真实配置被整份覆盖。
 */
describe('config store 的写回守卫', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('加载成功后 baseLoaded 为真，且解析出 model', async () => {
    mockedApi.get.mockResolvedValueOnce({ data: { content: 'mixed-port: 7890\nmode: rule\n' } })
    const store = useConfigStore()

    await store.fetchBase()

    expect(store.baseLoaded).toBe(true)
    expect(store.baseLoadError).toBe('')
    expect(store.model['mixed-port']).toBe(7890)
  })

  it('加载失败时 baseLoaded 为假并记录原因', async () => {
    mockedApi.get.mockRejectedValueOnce(new Error('网络错误'))
    const store = useConfigStore()

    await store.fetchBase()

    expect(store.baseLoaded).toBe(false)
    expect(store.baseLoadError).toBe('网络错误')
  })

  it('加载失败后调用 saveBase 必须拒绝，且不发出任何 PUT', async () => {
    mockedApi.get.mockRejectedValueOnce(new Error('网络错误'))
    const store = useConfigStore()
    await store.fetchBase()
    // 拒绝原因经 notify 呈现（会弹 toast），而不是只写进 store.message——
    // 后者用户看不到。断言用户可见的那条路径。
    const notify = useNotifyStore()
    const errSpy = vi.spyOn(notify, 'error')

    const ok = await store.saveBase()

    expect(ok).toBe(false)
    expect(mockedApi.put).not.toHaveBeenCalled()
    expect(errSpy).toHaveBeenCalledWith(expect.stringContaining('尚未加载成功'))
  })

  it('从未加载过就 saveBase（model 仍是初值 {}）同样必须拒绝', async () => {
    const store = useConfigStore()

    const ok = await store.saveBase()

    expect(ok).toBe(false)
    expect(mockedApi.put).not.toHaveBeenCalled()
  })

  it('saveAndMerge 依赖 saveBase 的返回值，未加载时不得触发合并', async () => {
    mockedApi.get.mockRejectedValueOnce(new Error('网络错误'))
    const store = useConfigStore()
    await store.fetchBase()

    await store.saveAndMerge()

    expect(mockedApi.put).not.toHaveBeenCalled()
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('加载成功后 saveBase 正常写回，内容为序列化后的 model', async () => {
    mockedApi.get.mockResolvedValueOnce({ data: { content: 'mode: rule\n' } })
    mockedApi.put.mockResolvedValueOnce({ data: { message: '保存成功' } })
    const store = useConfigStore()
    await store.fetchBase()

    const ok = await store.saveBase()

    expect(ok).toBe(true)
    expect(mockedApi.put).toHaveBeenCalledTimes(1)
    const [url, body] = mockedApi.put.mock.calls[0]!
    expect(url).toBe('/config/base')
    expect((body as { content: string }).content).toContain('mode: rule')
    // 关键：写回的绝不能是空对象的序列化结果
    expect((body as { content: string }).content.trim()).not.toBe('{}')
  })
})

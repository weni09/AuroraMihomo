import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'

// api 必须在导入 store / 组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import SettingsView from './SettingsView.vue'

const mockedApi = vi.mocked(api, true)

/**
 * IntersectionObserver 在 happy-dom 下不存在，而分组导航的高亮依赖它。
 * 这里只需要它可被构造与调用，观察行为本身由 observe 的调用记录验证。
 */
class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []
  observed: Element[] = []
  disconnected = false
  constructor(public cb: IntersectionObserverCallback) {
    FakeIntersectionObserver.instances.push(this)
  }
  observe(el: Element) {
    this.observed.push(el)
  }
  disconnect() {
    this.disconnected = true
  }
  unobserve() {}
  takeRecords() {
    return []
  }
}

/**
 * 本页 onMounted 会打三个请求，统一给出足够骨架，避免各 await 抛错。
 * env 必须带 modes 与 warnings 两个数组：store 的 getter 直接对它们
 * 调 .some()，缺一个就是 undefined 报错（表现为不相关的 unhandled rejection）。
 */
function stubApi() {
  mockedApi.get.mockImplementation((url: string) => {
    // 规则接口独立于 status 返回骨架：字段缺了规则块会渲染「未加载」分支
    if (url.includes('/transparent/rules')) {
      return Promise.resolve({
        data: {
          customRules: '',
          exemptPorts: '853, 443',
          iptablesBackend: 'nf_tables',
          builtinNFTRules: 'table inet aurora_tproxy { }',
          policyRoutes: ['ip rule add fwmark 1 table 100'],
          activeRules: '',
        },
      })
    }
    if (url.includes('transparent')) {
      return Promise.resolve({
        data: {
          enabled: false,
          mode: 'off',
          pendingConfirm: false,
          env: { modes: [], warnings: [], iptablesBackend: 'nf_tables' },
        },
      })
    }
    return Promise.resolve({ data: {} })
  })
}

/**
 * 分组导航的锚点契约。
 *
 * 这类退化没法靠类型检查发现：导航项写错 id、或某个 section 忘了加 id，
 * 页面看着完全正常，只有点了导航才发现跳不动。而这两处一个在 script、
 * 一个在 template，改动时很容易只动一边。
 */
describe('SettingsView 分组导航', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    FakeIntersectionObserver.instances = []
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
    // onMounted 里用 requestAnimationFrame 等 DOM 稳定，happy-dom 下直接同步执行
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    stubApi()
  })

  it('每个导航项都指向一个真实存在的 section 锚点', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    // 直接取导航定义里的 id 逐个核对，而不是点击后看高亮状态：
    // jumpToSection 在目标不存在时会提前返回、activeSection 停在旧值，
    // 于是「锚点指向了不存在的 section」这种退化反而看不出来（实测漏过）。
    const vm = wrapper.vm as unknown as {
      sections: ReadonlyArray<{ id: string; title: string }>
    }
    expect(vm.sections.length).toBeGreaterThan(0)

    for (const sec of vm.sections) {
      expect(
        wrapper.find(`section#${sec.id}`).exists(),
        `导航项「${sec.title}」指向的 section#${sec.id} 不存在`,
      ).toBe(true)
    }

    wrapper.unmount()
  })

  it('section 数量与导航项一一对应，不多不少', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    // 新增 section 却忘了加导航项（或反之）都会被这条挡下
    const navCount = wrapper.findAll('#settings-nav button').length
    expect(wrapper.findAll('section[id]').length).toBe(navCount)

    wrapper.unmount()
  })

  it('所有 section 都被观察器纳入，才能正确高亮', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    const navCount = wrapper.findAll('#settings-nav button').length
    const observer = FakeIntersectionObserver.instances[0]
    expect(observer, 'onMounted 应创建 IntersectionObserver').toBeDefined()
    expect(observer!.observed.length).toBe(navCount)

    wrapper.unmount()
  })

  it('卸载时断开观察器，避免离开页面后仍回调', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()
    const observer = FakeIntersectionObserver.instances[0]!
    wrapper.unmount()
    expect(observer.disconnected).toBe(true)
  })

  /**
   * 回归防护：透明代理那一节必须始终挂载，不能做成标签式切换。
   *
   * 启用透明代理后有 90 秒确认窗口，未确认自动回滚。若切走分组会把
   * 待确认横幅与确认按钮一起卸载，而规则配错、主机即将失联时那是唯一
   * 的补救入口。所以七个 section 必须同时在 DOM 里。
   */
  it('切换导航不卸载其它分组（透明代理的确认入口必须常在）', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    const sectionCount = wrapper.findAll('section[id]').length
    expect(sectionCount).toBeGreaterThan(1)

    // 点最后一项导航，之前的 section 不应消失
    const buttons = wrapper.findAll('#settings-nav button')
    await buttons[buttons.length - 1]!.trigger('click')

    expect(wrapper.findAll('section[id]').length).toBe(sectionCount)
    expect(wrapper.find('#transparent').exists()).toBe(true)

    wrapper.unmount()
  })
})

/**
 * 底部悬浮操作条。
 *
 * 悬浮条脱离文档流，正文要靠 main 自己的底部内边距让位。两者分处
 * 不同元素，谁都能被单独改掉：删了内边距，滚到最底时「保存合并策略」
 * 就被压在条下面点不到；而这在类型检查和快照里都看不出来。
 */
describe('SettingsView 底部悬浮操作条', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    stubApi()
  })

  it('「保存设置」在悬浮条内，且只有一个', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    const bar = wrapper.find('main > div.fixed')
    expect(bar.exists(), '应存在底部悬浮条').toBe(true)

    const saveButtons = wrapper
      .findAll('button')
      .filter((b) => /^保存设置|^保存中/.test(b.text()))
    expect(saveButtons.length, '保存设置按钮应唯一，不能残留原来那个').toBe(1)
    expect(bar.element.contains(saveButtons[0]!.element), '保存设置应在悬浮条内').toBe(true)

    wrapper.unmount()
  })

  it('main 留出底部内边距，否则悬浮条会压住末节内容', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    // 断言 class 而非计算样式：happy-dom 不跑 tailwind，量不到真实像素。
    // 具体间隙由浏览器核对（正文尾部留 137px，悬浮条占 74px）。
    expect(wrapper.find('main').classes()).toContain('pb-28')

    wrapper.unmount()
  })

  it('悬浮条不拦截正文点击，只有条本身可点', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    // 外层铺满整行，必须 pointer-events-none，否则底部一条横带吃掉所有点击；
    // 内层要重新打开，不然按钮自己也点不动。这两个 class 是成对的。
    const bar = wrapper.find('main > div.fixed')
    expect(bar.classes()).toContain('pointer-events-none')
    expect(bar.find('div').classes()).toContain('pointer-events-auto')

    wrapper.unmount()
  })

  it('分组自己的保存按钮留在原处，不并入悬浮条', async () => {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    await wrapper.vm.$nextTick()

    // 合并策略、改密码各打独立接口，挪进悬浮条会让「这条按钮存哪些改动」失去边界
    const bar = wrapper.find('main > div.fixed')
    for (const label of ['保存合并策略', '修改密码']) {
      const btn = wrapper.findAll('button').find((b) => b.text().includes(label))
      expect(btn, `应存在「${label}」按钮`).toBeDefined()
      expect(
        bar.element.contains(btn!.element),
        `「${label}」不应被并入悬浮条`,
      ).toBe(false)
    }

    wrapper.unmount()
  })
})

describe('SettingsView 自定义防火墙规则', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    FakeIntersectionObserver.instances = []
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 0
    })
    stubApi()
  })

  async function mountLoaded() {
    const wrapper = mount(SettingsView, { attachTo: document.body })
    // 徽章文案由接口数据驱动，等它出现说明 onMounted 的请求链已走完
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('iptables 后端：nf_tables')
    })
    return wrapper
  }

  it('渲染规则编辑器、免代理端口输入、iptables 后端徽章与内置规则查看入口', async () => {
    const wrapper = await mountLoaded()

    expect(wrapper.text()).toContain('iptables 后端：nf_tables（与 nftables 互通）')
    expect(wrapper.find('textarea').exists()).toBe(true)
    expect(wrapper.text()).toContain('免代理端口')
    // 免代理端口由接口数据回填
    expect(wrapper.find('input#transparent-exempt-ports').element as HTMLInputElement).toHaveProperty('value', '853, 443')
    expect(wrapper.text()).toContain('查看面板内置防火墙规则')
    // 内置规则文本按当前配置参数生成，应已渲染
    expect(wrapper.text()).toContain('table inet aurora_tproxy { }')

    wrapper.unmount()
  })

  it('点保存规则提交 PUT（含免代理端口）并提示已保存', async () => {
    mockedApi.put.mockResolvedValue({ data: { message: 'ok' } })
    const wrapper = await mountLoaded()

    await wrapper.find('textarea').setValue('-t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN')
    await wrapper.find('input#transparent-exempt-ports').setValue('853, 443')
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === '保存规则')
    expect(saveBtn).toBeDefined()
    await saveBtn!.trigger('click')

    await vi.waitFor(() => {
      expect(mockedApi.put).toHaveBeenCalledWith(
        '/transparent/rules',
        {
          customRules: '-t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN',
          exemptPorts: '853, 443',
        },
        // transparent store 对这类后台保存统一跳过错误 toast（拦截器不再弹第二遍）
        { skipErrorToast: true },
      )
    })

    wrapper.unmount()
  })
})

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
import { useNotifyStore } from './notify'
import { useTransparentStore, type TransparentEnvReport } from './transparent'

const mockedApi = vi.mocked(api, true)

/** 造一份环境报告，只覆盖用例关心的字段 */
function env(patch: Partial<TransparentEnvReport> = {}): TransparentEnvReport {
  return {
    os: 'linux',
    arch: 'amd64',
    kernel: '6.8.0',
    distro: 'ubuntu',
    packageManager: 'apt-get',
    root: true,
    capNetAdmin: true,
    capNetRaw: true,
    capNetAdminBounding: true,
    inContainer: false,
    hostNetwork: true,
    tunDevice: '/dev/net/tun',
    iptablesBackend: 'nf_tables',
    modes: [],
    warnings: [],
    ...patch,
  }
}

/**
 * status 接口走原生 fetch（与 axios 实例解耦），单测用这个 stub 响应体。
 * 每个用例 beforeEach 会 unstub，避免泄漏到其它用例。
 */
function stubStatusFetch(data: Record<string, unknown>, init?: { ok?: boolean; status?: number }) {
  const ok = init?.ok !== false
  const fetchMock = vi.fn().mockResolvedValue({
    ok,
    status: init?.status ?? (ok ? 200 : 500),
    headers: { get: () => 'application/json' },
    json: async () => data,
    text: async () => JSON.stringify(data),
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('自动准备环境入口的显示条件', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('有模式因缺工具不可用时，允许装包', () => {
    const s = useTransparentStore()
    s.status.env = env({
      modes: [
        { mode: 'tun', available: true, reason: '', missing: [], installHint: '' },
        {
          mode: 'tproxy',
          available: false,
          reason: '缺少必要工具',
          missing: ['nft 或 iptables'],
          installHint: 'apt-get install -y nftables',
        },
      ],
    })

    expect(s.canInstallPackages).toBe(true)
    expect(s.canProvision).toBe(true)
  })

  /**
   * 关键用例：缺 /dev/net/tun 是装包解决不了的（要改 compose 映射设备）。
   * 若这里给出装包按钮，用户点完只会得到"已满足，跳过"，白跑一趟还更困惑。
   */
  it('只缺 TUN 设备时不给装包入口', () => {
    const s = useTransparentStore()
    s.status.env = env({
      tunDevice: '',
      modes: [
        {
          mode: 'tun',
          available: false,
          reason: '容器内没有 TUN 设备',
          missing: ['/dev/net/tun'],
          installHint: '', // 装包解决不了，后端不给安装命令
        },
        { mode: 'tproxy', available: true, reason: '', missing: [], installHint: '' },
      ],
    })

    expect(s.canInstallPackages).toBe(false)
    expect(s.canProvision).toBe(false)
  })

  it('全部可用且无告警时不显示入口', () => {
    const s = useTransparentStore()
    s.status.env = env({
      modes: [
        { mode: 'tun', available: true, reason: '', missing: [], installHint: '' },
        { mode: 'tproxy', available: true, reason: '', missing: [], installHint: '' },
      ],
    })

    expect(s.canProvision).toBe(false)
  })

  it('有 sysctl 告警时允许调整系统参数', () => {
    const s = useTransparentStore()
    s.status.env = env({
      warnings: ['net.ipv4.ip_forward 为 0，作为局域网网关时需要开启：sysctl -w ...'],
    })

    expect(s.canApplySysctl).toBe(true)
    expect(s.canProvision).toBe(true)
  })

  it('rp_filter 告警同样算可调整', () => {
    const s = useTransparentStore()
    s.status.env = env({
      warnings: ['net.ipv4.conf.all.rp_filter 为 1（严格反向路径校验），会导致 TProxy 丢包'],
    })

    expect(s.canApplySysctl).toBe(true)
  })

  it('与 sysctl 无关的告警不该触发入口', () => {
    const s = useTransparentStore()
    s.status.env = env({
      warnings: ['容器使用 host 网络，sysctl 需在宿主机上设置'],
    })

    expect(s.canApplySysctl).toBe(false)
  })
})

describe('开关禁用与环境就绪', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('首次拉取完成前即使 modes 为空也不该显示成「环境不具备」语义上的可判定态', () => {
    const s = useTransparentStore()
    // 初始：未就绪、loading 可能为 false（进页前）
    expect(s.envReady).toBe(false)
    expect(s.anyAvailable).toBe(false)
    expect(s.switchDisabled).toBe(true)
  })

  it('成功 applyStatus 后 envReady，有可用模式则开关可点', () => {
    const s = useTransparentStore()
    s.applyStatus({
      enabled: true,
      mode: 'tun',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env({
        modes: [
          { mode: 'tun', available: true, reason: '可用', missing: [], installHint: '' },
          { mode: 'tproxy', available: true, reason: '可用', missing: [], installHint: '' },
        ],
      }),
    })
    s.envReady = true
    s.loading = false
    expect(s.anyAvailable).toBe(true)
    expect(s.switchDisabled).toBe(false)
  })

  it('fetch 写入 status 后标记 envReady', async () => {
    const s = useTransparentStore()
    const payload = {
      enabled: false,
      mode: 'off',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env({
        modes: [{ mode: 'tun', available: true, reason: '', missing: [], installHint: '' }],
      }),
    }
    const fetchMock = stubStatusFetch(payload)
    await s.fetch()
    expect(s.envReady).toBe(true)
    expect(s.anyAvailable).toBe(true)
    expect(s.switchDisabled).toBe(false)
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/transparent/status',
      expect.objectContaining({ method: 'GET', cache: 'no-store' }),
    )
  })
})

describe('provision 动作', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  /**
   * 装完包后工具才出现在 PATH 上，后端会返回重新探测的 env。
   * 写回 status.env 才能让界面立刻反映"现在可用了"，
   * 否则会停在"提示成功但模式仍显示不可用"的割裂状态。
   */
  it('成功后把重新探测的 env 写回状态', async () => {
    const s = useTransparentStore()
    s.status.env = env({
      modes: [
        {
          mode: 'tproxy',
          available: false,
          reason: '缺少必要工具',
          missing: ['nft 或 iptables'],
          installHint: 'apt-get install -y nftables',
        },
      ],
    })

    mockedApi.post.mockResolvedValueOnce({
      data: {
        success: true,
        message: '完成 2 项，跳过 0 项',
        steps: [{ name: '安装软件包', command: 'apt-get install', success: true, detail: '', skipped: false }],
        notPersistent: false,
        manualCommands: ['apt-get install -y nftables'],
        env: env({
          modes: [{ mode: 'tproxy', available: true, reason: '可用', missing: [], installHint: '' }],
        }),
      },
    })

    const ok = await s.provision({ installPackages: true, applySysctl: false })

    expect(ok).toBe(true)
    expect(s.status.env.modes[0]!.available).toBe(true)
    expect(s.provisioning).toBe(false)
  })

  // 部分成功要按失败处理：剩下那半仍需用户处理，报成功会让人以为不用管了
  it('部分失败时返回 false 并保留步骤明细', async () => {
    const s = useTransparentStore()
    mockedApi.post.mockResolvedValueOnce({
      data: {
        success: false,
        message: '完成 1 项，跳过 0 项，失败 1 项',
        steps: [
          { name: '安装软件包', command: 'apt-get install', success: true, detail: '', skipped: false },
          {
            name: '使 sysctl 生效',
            command: 'sysctl --system',
            success: false,
            detail: 'sysctl: cannot stat ...',
            skipped: false,
          },
        ],
        notPersistent: false,
        manualCommands: [],
        env: env(),
      },
    })

    const ok = await s.provision({ installPackages: true, applySysctl: true })

    expect(ok).toBe(false)
    // 失败项的原始输出要留着，用户得对着它排查
    expect(s.provisionResult?.steps[1]!.detail).toContain('cannot stat')
  })

  it('请求失败时不留下半截结果，且解除 loading', async () => {
    const s = useTransparentStore()
    mockedApi.post.mockRejectedValueOnce(new Error('403'))

    const ok = await s.provision({ installPackages: true, applySysctl: true })

    expect(ok).toBe(false)
    expect(s.provisionResult).toBeNull()
    expect(s.provisioning).toBe(false)
  })

  it('选项原样透传给后端', async () => {
    const s = useTransparentStore()
    mockedApi.post.mockResolvedValueOnce({
      data: {
        success: true,
        message: 'ok',
        steps: [],
        notPersistent: false,
        manualCommands: [],
        env: env(),
      },
    })

    await s.provision({ installPackages: true, applySysctl: false })

    expect(mockedApi.post).toHaveBeenCalledWith(
      '/transparent/provision',
      {
        installPackages: true,
        applySysctl: false,
      },
      { skipErrorToast: true },
    )
  })
})

/**
 * 「配置了端口但未接管」这个中间状态必须原样透传到界面。
 *
 * 它是后端唯一能表达"基础配置里有 tproxy-port，但防火墙规则不是面板下发的"
 * 的渠道。前端把它丢掉，用户就会面对一个关着的开关和一份写着 tproxy-port 的
 * 配置，无从判断是自己漏了一步还是面板出了错。
 */
describe('端口已配置但未接管的状态', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('透传 portConfiguredOnly 与端口号，且不误报为已启用', async () => {
    const s = useTransparentStore()
    stubStatusFetch({
      enabled: false,
      mode: 'off',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7894,
      tunStack: 'mixed',
      portConfiguredOnly: true,
      rulesOutOfSync: false,
      env: env(),
    })

    await s.fetch()

    expect(s.status.portConfiguredOnly).toBe(true)
    // 提示文案里要显示具体端口，用户才能对上自己填的那个值
    expect(s.status.tproxyPort).toBe(7894)
    // 关键：这不是"已启用"。流量并没有被接管。
    expect(s.status.enabled).toBe(false)
  })

  it('面板确实接管后不再提示', async () => {
    const s = useTransparentStore()
    stubStatusFetch({
      enabled: true,
      mode: 'tproxy',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env(),
    })

    await s.fetch()

    expect(s.status.portConfiguredOnly).toBe(false)
    expect(s.status.enabled).toBe(true)
  })
})

/**
 * 已启用状态下切换模式必须真的提交给后端。
 *
 * 回归的是一个前端 bug：模式下拉框只改本地 ref，没有任何提交路径（唯一的提交点
 * 在开关的 toggle 上）；同时有个 watch 监听整个 status 且 deep，会把本地 ref
 * 拉回 status.mode。于是用户在 TUN 已启用时选 TProxy，选项立刻弹回 TUN——
 * 表现为"切不了、还会变回去"，而开关已经开着也没法靠关掉再开来切（会断网）。
 *
 * 这里从 store 侧钉住协议：切换是一次 enabled: true + 新模式的提交。
 */
describe('已启用状态下切换模式', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('切到 TProxy 会带着新模式提交，且状态随响应更新', async () => {
    const s = useTransparentStore()
    // 当前是 TUN 已启用
    s.status = {
      enabled: true,
      mode: 'tun',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env(),
    }
    mockedApi.put.mockResolvedValueOnce({
      data: {
        enabled: true,
        mode: 'tproxy',
        pendingConfirm: true,
        secondsLeft: 90,
        tproxyPort: 7893,
        tunStack: 'mixed',
        portConfiguredOnly: false,
        env: env(),
      },
    })

    const ok = await s.update({ enabled: true, mode: 'tproxy', tunStack: 'mixed' })

    expect(ok).toBe(true)
    // 关键：请求确实发出去了，且带的是新模式而不是旧模式
    expect(mockedApi.put).toHaveBeenCalledWith(
      '/transparent',
      {
        enabled: true,
        mode: 'tproxy',
        tunStack: 'mixed',
      },
      expect.objectContaining({ skipErrorToast: true, timeout: expect.any(Number) }),
    )
    expect(s.status.mode).toBe('tproxy')
    // TProxy 会进入确认窗口，倒计时要起来
    expect(s.status.pendingConfirm).toBe(true)
    s.stopCountdown()
  })

  it('切换失败时状态保持在原模式，供界面回退选项', async () => {
    const s = useTransparentStore()
    s.status = {
      enabled: true,
      mode: 'tun',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env(),
    }
    mockedApi.put.mockRejectedValueOnce(new Error('无法启用 tproxy 模式: 缺少 nft'))

    const ok = await s.update({ enabled: true, mode: 'tproxy', tunStack: 'mixed' })

    expect(ok).toBe(false)
    // 后端拒绝了，实际仍是 TUN——界面据此把下拉框拨回去
    expect(s.status.mode).toBe('tun')
    expect(s.status.enabled).toBe(true)
  })
})

/**
 * 开关改完必须让「配置中心」重读 base.yaml。
 *
 * 两处编辑的是同一份文件（tun.enable / tproxy-port），而配置中心把它缓存在
 * 自己的 model 里、保存时整份写回。缓存不刷新的话，用户切到 TProxy 后去配置
 * 中心会看到 TUN 开关仍开着，在那页点保存就会把过期配置写回去、覆盖掉切换。
 */
describe('切换模式后同步配置中心的缓存', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  const okStatus = (mode: 'tun' | 'tproxy') => ({
    data: {
      enabled: true,
      mode,
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      env: env(),
    },
  })

  it('配置中心已加载过时，切换后会重新拉取 base 配置', async () => {
    const { useConfigStore } = await import('./config')
    const cfg = useConfigStore()
    // 模拟用户去过配置中心：缓存里是 TUN 开着的旧状态
    cfg.baseLoaded = true
    cfg.model = { tun: { enable: true } }

    mockedApi.put.mockResolvedValueOnce(okStatus('tproxy'))
    // fetchBase 读到的新内容：TUN 已被显式关掉
    mockedApi.get.mockResolvedValueOnce({
      data: { content: 'tun:\n  enable: false\ntproxy-port: 7893\n' },
    })

    const s = useTransparentStore()
    await s.update({ enabled: true, mode: 'tproxy', tunStack: 'mixed' })

    expect(mockedApi.get).toHaveBeenCalledWith('/config/base', { skipErrorToast: true })
    // 配置中心的缓存已跟上，TUN 开关不会再显示为开着
    expect(cfg.model.tun?.enable).toBe(false)
  })

  it('配置中心没加载过时不额外请求（那一页 onMounted 自会拉取）', async () => {
    const { useConfigStore } = await import('./config')
    const cfg = useConfigStore()
    cfg.baseLoaded = false

    mockedApi.put.mockResolvedValueOnce(okStatus('tproxy'))

    const s = useTransparentStore()
    await s.update({ enabled: true, mode: 'tproxy', tunStack: 'mixed' })

    expect(mockedApi.get).not.toHaveBeenCalledWith('/config/base', { skipErrorToast: true })
  })

  it('刷新配置中心失败不影响本次切换的结果', async () => {
    const { useConfigStore } = await import('./config')
    const cfg = useConfigStore()
    cfg.baseLoaded = true

    mockedApi.put.mockResolvedValueOnce(okStatus('tproxy'))
    mockedApi.get.mockRejectedValueOnce(new Error('500'))

    const s = useTransparentStore()
    const ok = await s.update({ enabled: true, mode: 'tproxy', tunStack: 'mixed' })

    // 切换本身成功了，不能因为"另一个页面的缓存没刷上"就报失败
    expect(ok).toBe(true)
    expect(s.status.mode).toBe('tproxy')
  })
})

/**
 * 规则与配置脱节的状态必须透传，界面据此显示红色告警。
 *
 * 规则里烧进了 tproxy-port / DNS 端口 / 内核 API 端口，用户在配置中心改完这些，
 * 合并末尾会自动重新下发规则。重下发失败时内核听在新端口、规则还往旧端口投，
 * 流量进黑洞，而用户刚看到的是「配置已生效」——不提示等于让他毫无线索。
 */
describe('规则与配置不一致的提示', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('透传 rulesOutOfSync', async () => {
    const s = useTransparentStore()
    stubStatusFetch({
      enabled: true,
      mode: 'tproxy',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: true,
      env: env(),
    })

    await s.fetch()

    expect(s.status.rulesOutOfSync).toBe(true)
    // 仍然是"已启用"：规则在跑，只是与配置对不上，不能报成未启用
    expect(s.status.enabled).toBe(true)
  })

  it('规则同步正常时不提示', async () => {
    const s = useTransparentStore()
    stubStatusFetch({
      enabled: true,
      mode: 'tproxy',
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: env(),
    })

    await s.fetch()

    expect(s.status.rulesOutOfSync).toBe(false)
  })
})

describe('自定义防火墙规则', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.unstubAllGlobals()
  })

  it('fetchRules 拉取规则与内置规则展示数据', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        customRules: '-t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN\n',
        exemptPorts: '853, 443',
        iptablesBackend: 'nf_tables',
        builtinNFTRules: 'table inet aurora_tproxy { }',
        policyRoutes: ['ip rule add fwmark 1 table 100'],
        activeRules: 'table inet aurora_tproxy { }',
      },
    })
    const s = useTransparentStore()
    await s.fetchRules()

    expect(s.rules?.customRules).toContain('-t nat -A')
    expect(s.rules?.exemptPorts).toBe('853, 443')
    expect(s.rules?.iptablesBackend).toBe('nf_tables')
    expect(s.rules?.policyRoutes).toEqual(['ip rule add fwmark 1 table 100'])
    expect(mockedApi.get).toHaveBeenCalledWith('/transparent/rules', { skipErrorToast: true })
  })

  it('fetchRules 失败时静默落空展示，不弹错误', async () => {
    mockedApi.get.mockRejectedValue(new Error('network'))
    const notify = useNotifyStore()
    const errSpy = vi.spyOn(notify, 'error')
    const s = useTransparentStore()
    await s.fetchRules()

    expect(s.rules).toEqual({
      customRules: '',
      exemptPorts: '',
      iptablesBackend: '',
      builtinNFTRules: '',
      policyRoutes: [],
      activeRules: '',
    })
    expect(errSpy).not.toHaveBeenCalled()
  })

  it('saveRules 提交规则原文与免代理端口并在成功后刷新展示数据', async () => {
    mockedApi.put.mockResolvedValue({ data: { message: 'ok' } })
    mockedApi.get.mockResolvedValue({
      data: {
        customRules: 'iptables -A INPUT -j ACCEPT\n',
        exemptPorts: '853, 443',
        iptablesBackend: 'legacy',
        builtinNFTRules: '',
        policyRoutes: [],
        activeRules: '',
      },
    })
    const s = useTransparentStore()
    s.status.enabled = true
    s.status.mode = 'tproxy'

    const ok = await s.saveRules('iptables -A INPUT -j ACCEPT\n', '853, 443')

    expect(ok).toBe(true)
    expect(mockedApi.put).toHaveBeenCalledWith('/transparent/rules', {
      customRules: 'iptables -A INPUT -j ACCEPT\n',
      exemptPorts: '853, 443',
    }, { skipErrorToast: true })
    // 保存成功后重新拉取，内置规则文本可能随参数变化
    expect(mockedApi.get).toHaveBeenCalledWith('/transparent/rules', { skipErrorToast: true })
  })

  it('saveRules 失败时返回 false 且不清空已有展示数据', async () => {
    mockedApi.put.mockRejectedValue({ response: { data: { message: '自定义规则第 1 行必须以 iptables 开头' } } })
    mockedApi.get.mockResolvedValue({
      data: {
        customRules: 'old',
        exemptPorts: '',
        iptablesBackend: '',
        builtinNFTRules: '',
        policyRoutes: [],
        activeRules: '',
      },
    })
    const s = useTransparentStore()
    await s.fetchRules()
    const before = s.rules

    const ok = await s.saveRules('非法行', '853')

    expect(ok).toBe(false)
    expect(s.rules).toBe(before)
  })
})

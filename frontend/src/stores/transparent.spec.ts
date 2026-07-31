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
    modes: [],
    warnings: [],
    ...patch,
  }
}

describe('自动准备环境入口的显示条件', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
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

describe('provision 动作', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
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

    expect(mockedApi.post).toHaveBeenCalledWith('/transparent/provision', {
      installPackages: true,
      applySysctl: false,
    })
  })
})

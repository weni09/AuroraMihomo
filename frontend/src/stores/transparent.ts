import { defineStore } from 'pinia'
import { useNotifyStore } from './notify'
import api from '../api'
import { apiErrorMessage } from '../utils/apiError'

/** 透明代理模式。off 表示关闭 */
export type TransparentMode = 'off' | 'tun' | 'tproxy'

/** 单个模式的可用性结论 */
export interface TransparentModeStatus {
  mode: TransparentMode
  available: boolean
  /** 不可用的原因，面向用户，直接展示 */
  reason: string
  /** 缺失的依赖（命令名或内核特性） */
  missing: string[]
  /** 补齐依赖的具体命令，可复制执行 */
  installHint: string
}

/** 环境检测报告 */
export interface TransparentEnvReport {
  os: string
  arch: string
  kernel: string
  distro: string
  packageManager: string
  root: boolean
  capNetAdmin: boolean
  capNetRaw: boolean
  /**
   * 为 true 而 capNetAdmin 为 false，说明容器 cap_add 给了权限但当前进程
   * 拿不到（非 root 且二进制无 file capability）。这两种情况修复方式不同，
   * 界面上要区分提示。
   */
  capNetAdminBounding: boolean
  inContainer: boolean
  hostNetwork: boolean
  /** /dev/net/tun 的实际路径，不存在时为空 */
  tunDevice: string
  modes: TransparentModeStatus[]
  /** 不阻止启用但需要用户知道的问题 */
  warnings: string[]
}

/** 环境准备的单步结果 */
export interface TransparentProvisionStep {
  /** 步骤名，面向用户 */
  name: string
  /** 实际执行的命令，用于审计"面板到底动了什么" */
  command: string
  success: boolean
  /** 成功时是简要说明；失败时是命令原始输出，不要截断后再展示 */
  detail: string
  /** 无需执行（已满足条件） */
  skipped: boolean
}

/** 环境准备结果 */
export interface TransparentProvisionResult {
  success: boolean
  message: string
  steps: TransparentProvisionStep[]
  /** 改动会随重启或容器重建丢失，需提示用户 */
  notPersistent: boolean
  /** 等价手动命令。成功与否都会返回：失败时是兜底，成功时便于记进部署脚本 */
  manualCommands: string[]
  /** 执行后重新探测的环境报告 */
  env: TransparentEnvReport
}

export interface TransparentStatus {
  enabled: boolean
  mode: TransparentMode
  /** 刚启用、等待确认网络正常；超时未确认会自动回滚 */
  pendingConfirm: boolean
  /** 距自动回滚的剩余秒数 */
  secondsLeft: number
  tproxyPort: number
  tunStack: string
  env: TransparentEnvReport
}

const emptyEnv = (): TransparentEnvReport => ({
  os: '',
  arch: '',
  kernel: '',
  distro: '',
  packageManager: '',
  root: false,
  capNetAdmin: false,
  capNetRaw: false,
  capNetAdminBounding: false,
  inContainer: false,
  hostNetwork: false,
  tunDevice: '',
  modes: [],
  warnings: [],
})

/**
 * 透明代理开关。
 *
 * 两处与其它 store 不同的地方：
 *   - 状态里带环境检测结论，界面据此决定开关是否可操作。环境不支持时
 *     后端会拒绝写入，前端提前禁用只是为了不让用户白点一次。
 *   - 启用 TProxy 后进入待确认状态，需要在时限内确认网络正常，否则后端自动回滚。
 *     这里用本地倒计时驱动显示，真正的回滚由后端计时并持久化，
 *     前端刷新或关掉页面都不影响。TUN 不进入该状态（mihomo 自管规则、
 *     退出即清理），是否待确认一律以后端返回的 pendingConfirm 为准。
 */
export const useTransparentStore = defineStore('transparent', {
  state: () => ({
    status: {
      enabled: false,
      mode: 'off' as TransparentMode,
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      env: emptyEnv(),
    } as TransparentStatus,
    loading: false,
    saving: false,
    /** 正在执行环境准备。装包可能几十秒，按钮需要禁用并给出反馈 */
    provisioning: false,
    /**
     * 最近一次环境准备的结果。保留在 store 里而不是执行完就丢：
     * 失败时用户需要对着命令原始输出排查，切走再回来不该丢失。
     */
    provisionResult: null as TransparentProvisionResult | null,
    /** 本地倒计时定时器 id */
    timer: 0 as unknown as ReturnType<typeof setInterval> | 0,
  }),
  getters: {
    /** 有任一模式可用；都不可用时开关禁用 */
    anyAvailable(state): boolean {
      return state.status.env.modes.some((m) => m.available)
    },
    /** 可选模式列表，供界面渲染单选 */
    availableModes(state): TransparentModeStatus[] {
      return state.status.env.modes.filter((m) => m.available)
    },
    /** 取指定模式的结论，用于展示不可用原因 */
    modeStatus(state) {
      return (mode: TransparentMode): TransparentModeStatus | undefined =>
        state.status.env.modes.find((m) => m.mode === mode)
    },
    /**
     * 是否有缺失的软件包可以自动装。
     *
     * 判据是"有模式因为缺工具而不可用"，而不是简单看 modes 里有没有
     * 不可用项：缺 /dev/net/tun 那种装包也解决不了（要改 compose 映射设备），
     * 给按钮反而误导。
     */
    canInstallPackages(state): boolean {
      return state.status.env.modes.some((m) => !m.available && m.installHint !== '')
    },
    /**
     * sysctl 是否有可调项。
     *
     * 后端的告警文案里带 sysctl 键名，据此判断——比在前端复刻一遍
     * "哪些值算不合规"的规则可靠，那种复刻一定会跟后端漂移。
     */
    canApplySysctl(state): boolean {
      return state.status.env.warnings.some((w) => w.includes('ip_forward') || w.includes('rp_filter'))
    },
    /** 只要有一类可做，就显示自动准备入口 */
    canProvision(): boolean {
      return this.canInstallPackages || this.canApplySysctl
    },
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<TransparentStatus>('/transparent/status')
        this.apply(res.data)
      } catch (e) {
        useNotifyStore().error(apiErrorMessage(e, '加载透明代理状态失败'))
      } finally {
        this.loading = false
      }
    },

    async update(payload: {
      enabled: boolean
      mode: TransparentMode
      tproxyPort?: number
      tunStack?: string
    }) {
      this.saving = true
      try {
        const res = await api.put<TransparentStatus>('/transparent', payload)
        this.apply(res.data)
        if (payload.enabled) {
          // 只有进入确认窗口时才提"请确认"：TUN 不开窗口，
          // 对它说这句话会让用户去找一个不存在的确认按钮。
          // 以后端返回的 pendingConfirm 为准，而不是按模式猜。
          useNotifyStore().success(
            this.status.pendingConfirm
              ? '透明代理已启用，请确认网络正常'
              : '透明代理已启用',
          )
        } else {
          useNotifyStore().success('透明代理已关闭')
        }
        return true
      } catch (e) {
        // 后端会把"缺什么、怎么补"放在错误里，原样展示才有可操作性
        useNotifyStore().error(apiErrorMessage(e, '设置透明代理失败'))
        return false
      } finally {
        this.saving = false
      }
    },

    async confirm() {
      try {
        const res = await api.post<TransparentStatus>('/transparent/confirm')
        this.apply(res.data)
        useNotifyStore().success('已确认，自动回滚取消')
        return true
      } catch (e) {
        useNotifyStore().error(apiErrorMessage(e, '确认失败'))
        return false
      }
    },

    /**
     * 尝试自动补齐环境（装依赖 / 调 sysctl）。
     *
     * 不改开关状态、不动防火墙，只把环境从"不可用"推向"可用"，
     * 所以不涉及那套 90 秒确认窗口。
     *
     * 后端返回的 env 是执行后重新探测的结果，这里直接写回 status.env：
     * 用户点完按钮应当立刻看到模式是否变为可用，而不用自己再刷一次。
     */
    async provision(opts: { installPackages: boolean; applySysctl: boolean }) {
      this.provisioning = true
      // 先清掉上次结果，避免新一轮执行期间界面上还挂着旧的成功/失败信息
      this.provisionResult = null
      try {
        const res = await api.post<TransparentProvisionResult>('/transparent/provision', opts)
        this.provisionResult = res.data
        if (res.data?.env) {
          this.status.env = res.data.env
        }
        const notify = useNotifyStore()
        if (res.data?.success) {
          notify.success(res.data.message || '环境准备完成')
        } else {
          // 部分失败也走 error：成功一半仍需要用户处理剩下那半，
          // 用 success 提示会让人以为不用管了
          notify.error(res.data?.message || '环境准备未完全成功，详情见下方步骤')
        }
        return res.data?.success === true
      } catch (e) {
        // 后端把"为什么不能做"（非 root、非 Linux 等）放在错误里，原样展示
        useNotifyStore().error(apiErrorMessage(e, '环境准备失败'))
        return false
      } finally {
        this.provisioning = false
      }
    },

    /** 写入状态并按需启动/停止倒计时 */
    apply(s: TransparentStatus | undefined) {
      if (!s) return
      this.status = { ...s, env: s.env || emptyEnv() }
      if (this.status.pendingConfirm && this.status.secondsLeft > 0) {
        this.startCountdown()
      } else {
        this.stopCountdown()
      }
    },

    startCountdown() {
      this.stopCountdown()
      this.timer = setInterval(() => {
        if (this.status.secondsLeft > 0) {
          this.status.secondsLeft -= 1
        }
        if (this.status.secondsLeft <= 0) {
          this.stopCountdown()
          // 倒计时归零说明后端应已回滚，拉一次真实状态而不是自行推断：
          // 回滚是否成功、当前是什么模式，只有后端知道
          void this.fetch()
        }
      }, 1000)
    },

    stopCountdown() {
      if (this.timer) {
        clearInterval(this.timer as ReturnType<typeof setInterval>)
        this.timer = 0
      }
    },
  },
})

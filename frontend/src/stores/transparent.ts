import { defineStore } from 'pinia'
import { useNotifyStore } from './notify'
import api from '../api'

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
 *   - 启用后进入待确认状态，需要在时限内确认网络正常，否则后端自动回滚。
 *     这里用本地倒计时驱动显示，真正的回滚由后端计时并持久化，
 *     前端刷新或关掉页面都不影响。
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
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<TransparentStatus>('/transparent/status')
        this.apply(res.data)
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '加载透明代理状态失败')
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
          useNotifyStore().success('透明代理已启用，请确认网络正常')
        } else {
          useNotifyStore().success('透明代理已关闭')
        }
        return true
      } catch (e: any) {
        // 后端会把"缺什么、怎么补"放在 message 里，原样展示才有可操作性
        useNotifyStore().error(e?.response?.data?.message || '设置透明代理失败')
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
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '确认失败')
        return false
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

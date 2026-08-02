import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

/** AdGuard Home 运行与对接状态（与后端 AdGuardStatusResp 对齐） */
export interface AdGuardStatus {
  installed: boolean
  running: boolean
  pid: number
  version: string
  workDir: string
  webAddr: string
  dnsPort: number
  /** "off" | "on" */
  wiring: string
  wiringLabel: string
  lastError?: string
  /** 同源反代入口，iframe 用 */
  entryPath: string
}

/** 一键对接向导勾选项 */
export interface WiringOptions {
  redirectTProxy: boolean
  resolveConflict: boolean
  patchUpstream: boolean
  weakenTunHijack: boolean
}

/** GET /adguard/wiring 预检结果 */
export interface WiringPreview {
  wiring: string
  actions: string[]
  warnings?: string[]
  aghDnsPort: number
}

interface Result {
  success?: boolean
  message?: string
}

const emptyStatus = (): AdGuardStatus => ({
  installed: false,
  running: false,
  pid: 0,
  version: '',
  workDir: '',
  webAddr: '',
  dnsPort: 0,
  wiring: 'off',
  wiringLabel: '未对接',
  entryPath: '/adguard/',
})

export const useAdGuardStore = defineStore('adguard', {
  state: () => ({
    status: emptyStatus(),
    isLoading: false,
    /** 安装/更新/启停/对接等耗时操作进行中 */
    actionLoading: false,
    /** 最近一次预检结果；与 action wiringPreview 区分命名，避免 Pinia 状态/动作同名冲突 */
    preview: null as WiringPreview | null,
  }),
  getters: {
    installed: (s) => s.status.installed,
    running: (s) => s.status.running,
    wiringOn: (s) => s.status.wiring === 'on',
  },
  actions: {
    async fetchStatus() {
      this.isLoading = true
      try {
        const res = await api.get<AdGuardStatus>('/adguard/status')
        this.status = {
          ...emptyStatus(),
          ...res.data,
          entryPath: res.data?.entryPath || '/adguard/',
        }
      } catch (error) {
        console.error('Failed to fetch AdGuard status', error)
      } finally {
        this.isLoading = false
      }
    },

    /**
     * 控制类 POST 共用收尾：后端失败常为 200 + success:false（Result 约定），
     * 不看 success 会把失败当成功提示出去。
     */
    async runAction(path: string, fallbackText: string, body?: unknown) {
      this.actionLoading = true
      try {
        const res = body === undefined ? await api.post<Result>(path) : await api.post<Result>(path, body)
        const text = res.data?.message || fallbackText
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        // 拦截器已 toast 网络/HTTP 错误；这里只保证 loading 收尾与状态刷新
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    async install() {
      await this.runAction('/adguard/install', 'AdGuard Home 已安装')
    },
    async start() {
      await this.runAction('/adguard/start', 'AdGuard Home 已启动')
    },
    async stop() {
      await this.runAction('/adguard/stop', 'AdGuard Home 已停止')
    },
    async restart() {
      await this.runAction('/adguard/restart', 'AdGuard Home 已重启')
    },
    /** 与设置页共用更新入口 */
    async update() {
      await this.runAction('/update/adguard', 'AdGuard Home 已更新')
    },

    async wiringPreview() {
      try {
        const res = await api.get<WiringPreview>('/adguard/wiring')
        this.preview = res.data
        return res.data
      } catch (error) {
        console.error(error)
        this.preview = null
        return null
      }
    },

    async wiringApply(opts: WiringOptions) {
      await this.runAction('/adguard/wiring/apply', 'DNS 对接已应用', opts)
      await this.wiringPreview()
    },

    async wiringRollback() {
      await this.runAction('/adguard/wiring/rollback', 'DNS 对接已回滚')
      await this.wiringPreview()
    },
  },
})

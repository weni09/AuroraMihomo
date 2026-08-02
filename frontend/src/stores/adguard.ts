import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

/** AdGuard Home 运行与对接状态（与后端 AdGuardStatusResp 对齐） */
export interface AdGuardStatus {
  /** 组件开关：关则隐藏侧栏并停进程，文件保留 */
  componentEnabled: boolean
  installed: boolean
  running: boolean
  pid: number
  version: string
  workDir: string
  webAddr: string
  dnsPort: number
  /** DNS 服务模式：0 未托管 / 1 绑定 53 / 2 重定向 */
  dnsMode: number
  /** "off" | "on" */
  wiring: string
  wiringLabel: string
  lastError?: string
  /** 同源反代入口，iframe 用 */
  entryPath: string
  /** AdGuard 专用升级镜像（空则用全局 CDN） */
  cdnProviders: string[]
  /** 是否参加系统自动更新 */
  autoUpdate: boolean
  /** AGH 管理员用户名 */
  username: string
  /** 是否与 Aurora 管理员密码同步 */
  passwordSync: boolean
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
  componentEnabled: false,
  installed: false,
  running: false,
  pid: 0,
  version: '',
  workDir: '',
  webAddr: '',
  dnsPort: 0,
  dnsMode: 0,
  wiring: 'off',
  wiringLabel: '未对接',
  entryPath: '/adguard/',
  cdnProviders: [],
  autoUpdate: false,
  username: 'admin',
  passwordSync: false,
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
    componentEnabled: (s) => s.status.componentEnabled,
    installed: (s) => s.status.installed,
    running: (s) => s.status.running,
    wiringOn: (s) => s.status.wiring === 'on',
  },
  actions: {
    async fetchStatus() {
      this.isLoading = true
      try {
        const res = await api.get<AdGuardStatus>('/adguard/status')
        const dnsMode = Number(res.data?.dnsMode)
        this.status = {
          ...emptyStatus(),
          ...res.data,
          // 显式归一：缺省/非法值按关闭处理，避免侧栏误显示
          componentEnabled: res.data?.componentEnabled === true,
          entryPath: res.data?.entryPath || '/adguard/',
          dnsMode: Number.isFinite(dnsMode) && dnsMode >= 0 && dnsMode <= 2 ? dnsMode : 0,
          cdnProviders: Array.isArray(res.data?.cdnProviders) ? res.data.cdnProviders : [],
          autoUpdate: res.data?.autoUpdate === true,
          username: res.data?.username?.trim() || 'admin',
          passwordSync: res.data?.passwordSync === true,
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

    /** 组件级开关：PUT /adguard/component；关时后端停进程，前端靠 status 隐藏侧栏 */
    async setComponent(enabled: boolean) {
      this.actionLoading = true
      try {
        const res = await api.put<Result>('/adguard/component', { enabled })
        const text =
          res.data?.message || (enabled ? 'AdGuard Home 组件已启用' : 'AdGuard Home 组件已关闭')
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    /** 彻底卸载：POST /adguard/uninstall，须 confirm=true */
    async uninstall(confirm: boolean) {
      await this.runAction('/adguard/uninstall', 'AdGuard Home 已彻底卸载', { confirm })
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

    /** 检查更新（复用全局 /update/check，含 AdGuard 版本描述） */
    async checkUpdate() {
      this.actionLoading = true
      try {
        const res = await api.get<{ message?: string; success?: boolean }>('/update/check')
        const text = res.data?.message || '检查完成'
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
      } catch (error) {
        console.error(error)
      } finally {
        this.actionLoading = false
      }
    },

    /** Web 管理端口：PUT /adguard/web-port；运行中后端会重启 */
    async setWebPort(port: number) {
      this.actionLoading = true
      try {
        const res = await api.put<Result>('/adguard/web-port', { port })
        const text = res.data?.message || `Web 端口已设置为 ${port}`
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    /** DNS 高位监听端口（重定向 53 的目标）；禁止 53 */
    async setDnsPort(port: number) {
      this.actionLoading = true
      try {
        const res = await api.put<Result>('/adguard/dns-port', { port })
        const text = res.data?.message || `DNS 端口已设置为 ${port}`
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    /** DNS 服务模式 0/1/2 */
    async setDnsMode(mode: number) {
      this.actionLoading = true
      try {
        const res = await api.put<Result>('/adguard/dns-mode', { mode })
        const text = res.data?.message || 'DNS 服务模式已更新'
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    /** AdGuard 专用升级镜像列表 */
    async setCdnProviders(providers: string[]) {
      this.actionLoading = true
      try {
        const res = await api.put<Result>('/adguard/cdn', { providers })
        const text = res.data?.message || '升级链接已保存'
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
    },

    /**
     * 设置 AGH 管理员账号。password 必填；syncWithAurora 可选写入同步开关。
     * 不在前端/库中存明文密码。
     */
    async setCredentials(payload: {
      username?: string
      password: string
      syncWithAurora?: boolean
    }) {
      this.actionLoading = true
      try {
        const body: {
          username?: string
          password: string
          syncWithAurora?: boolean
        } = { password: payload.password }
        const u = payload.username?.trim()
        if (u) body.username = u
        if (payload.syncWithAurora !== undefined) {
          body.syncWithAurora = payload.syncWithAurora
        }
        const res = await api.put<Result>('/adguard/credentials', body)
        const text = res.data?.message || 'AdGuard 账号已更新'
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetchStatus()
      } catch (error) {
        console.error(error)
        await this.fetchStatus()
      } finally {
        this.actionLoading = false
      }
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

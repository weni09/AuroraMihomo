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
  /** 是否启用 AdGuard 独立自动更新 */
  autoUpdate: boolean
  /** 自动更新 cron（5/6 段） */
  autoUpdateCron: string
  /** AGH 管理员用户名 */
  username: string
  /** 用户期望运行；面板重启后据此自启 */
  desiredRunning: boolean
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
  entryPath: '/adguard-ui/',
  cdnProviders: [],
  autoUpdate: false,
  autoUpdateCron: '0 0 4 * * *',
  username: 'admin',
  desiredRunning: false,
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
          entryPath: res.data?.entryPath || '/adguard-ui/',
          dnsMode: Number.isFinite(dnsMode) && dnsMode >= 0 && dnsMode <= 2 ? dnsMode : 0,
          cdnProviders: Array.isArray(res.data?.cdnProviders) ? res.data.cdnProviders : [],
          autoUpdate: res.data?.autoUpdate === true,
          autoUpdateCron: (res.data?.autoUpdateCron || '').trim() || '0 0 4 * * *',
          username: res.data?.username?.trim() || 'admin',
          desiredRunning: res.data?.desiredRunning === true,
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

    /** 仅检查 AdGuard Home 版本 */
    async checkUpdate() {
      this.actionLoading = true
      try {
        const res = await api.get<{ message?: string; success?: boolean }>('/adguard/check-update')
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

    /** DNS 监听端口（含入口 53 或高位端口如 5353）；后端校验占用 */
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

    /**
     * 一键入口 DNS 方案（TUN/TProxy 通用）：
     * AGH :53 + 上游/后备仅 mihomo、Bootstrap 国内纯 IP；mihomo dns.enable + listen 0.0.0.0:1053
     */
    async applyEntryDNSPreset() {
      this.actionLoading = true
      try {
        const res = await api.post<Result>('/adguard/dns-entry-preset')
        const text = res.data?.message || '已应用入口 DNS 方案'
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
     * 设置 AGH 管理员账号。password 必填。
     * 不在前端/库中存明文密码。
     */
    async setCredentials(payload: {
      username?: string
      password: string
    }) {
      this.actionLoading = true
      try {
        const body: {
          username?: string
          password: string
        } = { password: payload.password }
        const u = payload.username?.trim()
        if (u) body.username = u
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

    /** 保存 AdGuard 自动更新开关与/或 cron */
    async setAutoUpdate(payload: { enabled?: boolean; cron?: string }) {
      this.actionLoading = true
      try {
        const body: { enabled?: boolean; cron?: string } = {}
        if (payload.enabled !== undefined) body.enabled = payload.enabled
        if (payload.cron !== undefined) body.cron = payload.cron
        const res = await api.put<Result>('/adguard/auto-update', body)
        const text = res.data?.message || '自动更新设置已保存'
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

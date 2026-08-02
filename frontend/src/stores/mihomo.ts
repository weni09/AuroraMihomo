import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

interface Status {
  status: string
  version: string
  pid: number
  appVersion?: string
  /** 宿主当前时间 RFC3339 */
  serverTime?: string
  /** IANA 时区名，如 Asia/Shanghai */
  timezone?: string
}

export const useMihomoStore = defineStore('mihomo', {
  state: () => ({
    status: '未知',
    version: '未知',
    appVersion: '未知',
    pid: 0,
    /** 最近一次状态接口带回的宿主时间（RFC3339），供控制台时钟对齐 */
    serverTime: '' as string,
    timezone: '' as string,
    isLoading: false,
    recentLogs: [] as Array<{ time: string; stream: string; message: string }>,
    wsStatus: 'connecting',
  }),
  actions: {
    applyStatus(payload: Partial<Status>) {
      if (payload.status) this.status = payload.status
      if (payload.version) this.version = payload.version
      if (payload.appVersion) this.appVersion = payload.appVersion
      if (typeof payload.pid === 'number') this.pid = payload.pid
      if (payload.serverTime) this.serverTime = payload.serverTime
      if (payload.timezone) this.timezone = payload.timezone
    },
    pushLog(line: { time?: string; stream?: string; message?: string }) {
      this.recentLogs.push({
        time: line.time || new Date().toISOString(),
        stream: line.stream || '日志',
        message: line.message || '',
      })
      if (this.recentLogs.length > 100) this.recentLogs.splice(0, this.recentLogs.length - 100)
    },
    async fetchStatus() {
      // 未登录时不要打受保护接口：App 壳层与部分页面都会调这里，
      // 登录页上 401 只会污染控制台，没有可展示的状态。
      if (!localStorage.getItem('aurora_token')) {
        return
      }
      this.isLoading = true
      try {
        const response = await api.get<Status>('/system/status')
        this.applyStatus(response.data)
      } catch (error) {
        console.error('Failed to fetch status', error)
        this.status = '错误'
      } finally {
        this.isLoading = false
      }
    },
    /**
     * 内核控制操作共用的收尾：结果以 toast 告知并刷新状态。
     *
     * 这些接口失败时返回的是 200 + success:false（Result 约定），
     * 不看 success 会把失败当成功提示出去。
     */
    async runAction(path: string, fallbackText: string) {
      const res = await api.post(path)
      const text = res.data?.message || fallbackText
      if (res.data?.success === false) {
        useNotifyStore().error(text)
      } else {
        useNotifyStore().success(text)
      }
      await this.fetchStatus()
    },
    async start() {
      await this.runAction('/mihomo/start', '内核已启动')
    },
    async stop() {
      await this.runAction('/mihomo/stop', '内核已停止')
    },
    async restart() {
      await this.runAction('/mihomo/restart', '内核已重启')
    },
    async reload() {
      await this.runAction('/mihomo/reload', '配置已重载')
    },
  },
})

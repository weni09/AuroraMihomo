import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

export interface UpdateSettings {
  autoUpdateEnabled: boolean
  autoUpdateCron: string
  /**
   * GitHub Release 资产（内核 / 面板二进制）的下载源。
   * 仅这一类需要镜像：GitHub REST API 无可用镜像，版本查询直连官方。
   */
  cdnProviders: string[]
  /** 下载与版本查询是否优先经由本地 mihomo 代理 */
  useMihomoProxy: boolean
  /** 当前探测到的代理地址，未就绪时为空串 */
  mihomoProxyUrl: string
  mihomoPath: string
  zashboardDir: string
  mihomoPresent: boolean
  zashboardPresent: boolean
  /** mihomo -v 探测结果，未知时为空串 */
  mihomoVersion: string
  /** 上次更新记录的 release tag，历史安装为空串 */
  zashboardVersion: string
  defaultCDN: string[]
  /** 日志归档保留天数，只影响已轮转的历史文件 */
  logRetentionDays: number
  /** 日志清理任务的调度表达式（6 段，含秒） */
  logCleanupCron: string
  /** 是否启用定时清理；关闭后仅靠大小轮转限制磁盘占用 */
  logCleanupEnabled: boolean
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    // 检查更新与各组件更新是独立动作，各自可能耗时数秒到数十秒，
    // 曾经共用一个 updating 标志：点其中一个，所有按钮都会显示「处理中」，
    // 用户分不清到底是哪个操作在跑。现在拆成独立标志，互不影响。
    checkingUpdate: false,
    updatingMihomo: false,
    updatingZashboard: false,
    updatingAdGuard: false,
    settings: null as UpdateSettings | null,
    loading: false,
  }),
  getters: {
    // 任一进行中都算「有更新相关操作在跑」，用于需要整体禁用的场景
    updating: (s) =>
      s.checkingUpdate || s.updatingMihomo || s.updatingZashboard || s.updatingAdGuard,
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<UpdateSettings>('/settings/update')
        this.settings = res.data
      } catch (e) {
        console.error(e)
        useNotifyStore().error('加载设置失败')
      } finally {
        this.loading = false
      }
    },
    async save(payload: {
      autoUpdateEnabled?: boolean
      autoUpdateCron?: string
      cdnProviders?: string[]
      useMihomoProxy?: boolean
      logRetentionDays?: number
      logCleanupCron?: string
      logCleanupEnabled?: boolean
    }) {
      this.loading = true
      try {
        const res = await api.put<UpdateSettings>('/settings/update', payload)
        this.settings = res.data
        useNotifyStore().success('设置已保存')
      } catch (e: any) {
        console.error(e)
        useNotifyStore().error(e?.response?.data?.message || e?.message || '保存失败')
      } finally {
        this.loading = false
      }
    },
    // 检查更新只读取版本信息，不下载任何内容
    async checkUpdate() {
      if (this.checkingUpdate) return
      this.checkingUpdate = true
      try {
        const res = await api.get('/update/check')
        useNotifyStore().success(res.data?.message || '检查完成')
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || e?.message || '检查更新失败')
      } finally {
        this.checkingUpdate = false
      }
    },
    // 下载动辄几十 MB，updatingMihomo 用于禁用按钮，避免重复点击触发并发下载
    async updateMihomo() {
      if (this.updatingMihomo) return
      this.updatingMihomo = true
      try {
        const res = await api.post('/update/mihomo')
        useNotifyStore().success(res.data?.message || 'Mihomo 已更新')
        await this.fetch()
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || e?.message || '更新失败')
      } finally {
        this.updatingMihomo = false
      }
    },
    async updateZashboard() {
      if (this.updatingZashboard) return
      this.updatingZashboard = true
      try {
        const res = await api.post('/update/zashboard')
        useNotifyStore().success(res.data?.message || 'Zashboard 已更新')
        await this.fetch()
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || e?.message || '更新失败')
      } finally {
        this.updatingZashboard = false
      }
    },
    // AdGuard 为可选组件；更新会停机换二进制，若原先在跑会再拉起
    async updateAdGuard() {
      if (this.updatingAdGuard) return
      this.updatingAdGuard = true
      try {
        const res = await api.post('/update/adguard')
        const data = res.data as { success?: boolean; message?: string } | undefined
        const text = data?.message || 'AdGuard Home 已更新'
        if (data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
        }
        await this.fetch()
      } catch (e: unknown) {
        const err = e as { response?: { data?: { message?: string } }; message?: string }
        useNotifyStore().error(err?.response?.data?.message || err?.message || '更新失败')
      } finally {
        this.updatingAdGuard = false
      }
    },
  },
})

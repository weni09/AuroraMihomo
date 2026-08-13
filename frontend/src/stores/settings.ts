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
  /** 上次成功的全局下载源；空串表示尚未记过。下次优先尝试此源 */
  lastCdnProvider?: string
  /** 下载与版本查询是否优先经由本地 mihomo 代理 */
  useMihomoProxy: boolean
  /** 主程序（AuroraMihomo 自身）仓库，空串 = 停用面板内自升级 */
  selfRepo: string
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
  /** 服务器资源监控总开关（控制台资源卡片） */
  monitorEnabled: boolean
  /** 资源卡片刷新间隔（秒），可选 1/3/5/10/30 */
  monitorIntervalSec: number
}

/** 主程序（AuroraMihomo 自身）自升级检查结果 */
export interface SelfUpdateInfo {
  /** 是否已配置主程序仓库（未配置时无法自升级） */
  configured: boolean
  /** 当前运行版本 */
  currentVersion: string
  /** 远端最新 release tag，查询失败时为空串 */
  latestVersion: string
  /** 是否存在可升级的新版本 */
  updateAvailable: boolean
  /** 变更日志（release notes），截断至 4KB，空串表示拉不到 */
  releaseNotes?: string
  /** 提示信息：配置缺失 / 已最新 / 有新版本 / 查询失败原因 */
  message?: string
}

/** 主程序自升级的运行状态（轮询 /system/self-update/status） */
export interface SelfUpdateStatus {
  running: boolean
  phase: string
  percent: number
  message: string
  targetVersion?: string
  error?: { code: string; message: string }
  startedAt?: string
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    // 检查更新与各组件更新是独立动作，各自可能耗时数秒到数十秒，
    // 曾经共用一个 updating 标志：点其中一个，所有按钮都会显示「处理中」，
    // 用户分不清到底是哪个操作在跑。现在拆成独立标志，互不影响。
    checkingUpdate: false,
    updatingMihomo: false,
    updatingZashboard: false,
    backingUp: false,
    // 主程序自升级的独立状态：检查与升级各自耗时较长（下载可达分钟级），
    // 与组件更新一样拆开标志，避免互相误禁用按钮
    checkingSelfUpdate: false,
    updatingSelf: false,
    selfUpdateInfo: null as SelfUpdateInfo | null,
    // 主程序自升级运行状态（轮询 /system/self-update/status），
    // 含阶段/进度/错误；null 表示从未触发过升级
    selfUpdateStatus: null as SelfUpdateStatus | null,
    /** 主程序升级状态轮询定时器 id */
    selfUpdatePollTimer: null as number | null,
    /** 连续轮询失败次数：瞬时 5xx 不该停；重启断连会连续失败后停 */
    selfUpdatePollFails: 0,
    settings: null as UpdateSettings | null,
    loading: false,
  }),
  getters: {
    // 任一进行中都算「有更新相关操作在跑」，用于需要整体禁用的场景
    // AdGuard 更新只在 AdGuard 页设置弹窗，不占用本 store
    updating: (s) => s.checkingUpdate || s.updatingMihomo || s.updatingZashboard,
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<UpdateSettings>('/settings/update')
        this.settings = res.data
      } catch (e: unknown) {
        // HTTP 错误由 api 拦截器统一 toast
        console.error(e)
      } finally {
        this.loading = false
      }
    },
    async save(payload: {
      autoUpdateEnabled?: boolean
      autoUpdateCron?: string
      cdnProviders?: string[]
      useMihomoProxy?: boolean
      selfRepo?: string
      logRetentionDays?: number
      logCleanupCron?: string
      logCleanupEnabled?: boolean
      monitorEnabled?: boolean
      monitorIntervalSec?: number
    }) {
      this.loading = true
      try {
        const res = await api.put<UpdateSettings>('/settings/update', payload)
        this.settings = res.data
        useNotifyStore().success('设置已保存')
      } catch (e: unknown) {
        console.error(e)
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
      } catch (e: unknown) {
        console.error(e)
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
      } catch (e: unknown) {
        console.error(e)
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
      } catch (e: unknown) {
        console.error(e)
      } finally {
        this.updatingZashboard = false
      }
    },
    // 数据库在线备份（VACUUM INTO），备份文件落在服务端 backups 目录
    async backupDatabase() {
      if (this.backingUp) return
      this.backingUp = true
      try {
        const res = await api.post('/system/backup')
        useNotifyStore().success(res.data?.message || '备份完成')
      } catch (e: unknown) {
        console.error(e)
      } finally {
        this.backingUp = false
      }
    },
    // 检查主程序（AuroraMihomo 自身）是否有新版本；只读版本信息，不下载
    async checkSelfUpdate() {
      if (this.checkingSelfUpdate) return
      this.checkingSelfUpdate = true
      try {
        const res = await api.get<SelfUpdateInfo>('/system/self-update/check')
        this.selfUpdateInfo = res.data
        if (res.data?.configured) {
          useNotifyStore().success(res.data.message || '检查完成')
        }
      } catch (e: unknown) {
        console.error(e)
      } finally {
        this.checkingSelfUpdate = false
      }
    },
    // 一键自升级：异步触发（POST 立即返回），随后轮询状态接口
    // 展示下载/校验/重启阶段；服务重启导致轮询失败即停止。
    async updateSelf() {
      if (this.updatingSelf) return
      this.updatingSelf = true
      this.selfUpdateStatus = null
      this.selfUpdatePollFails = 0
      try {
        const res = await api.post('/system/self-update')
        useNotifyStore().success(res.data?.message || '升级已开始')
        this.startSelfUpdatePolling()
      } catch (e: unknown) {
        console.error(e)
        useNotifyStore().error('无法启动升级，请查看后端日志')
        this.updatingSelf = false
      }
    },
    startSelfUpdatePolling() {
      this.stopSelfUpdatePolling()
      void this.pollSelfUpdate()
      this.selfUpdatePollTimer = window.setInterval(() => {
        void this.pollSelfUpdate()
      }, 1000)
    },
    stopSelfUpdatePolling() {
      if (this.selfUpdatePollTimer != null) {
        clearInterval(this.selfUpdatePollTimer)
        this.selfUpdatePollTimer = null
      }
    },
    async pollSelfUpdate() {
      try {
        const res = await api.get<SelfUpdateStatus>('/system/self-update/status')
        this.selfUpdatePollFails = 0
        this.selfUpdateStatus = res.data
        if (!res.data.running) {
          this.stopSelfUpdatePolling()
          this.updatingSelf = false
          if (res.data.error) {
            useNotifyStore().error(res.data.error.message || '升级失败')
          }
        }
      } catch (e: unknown) {
        // 重启中断连是预期；下载中的瞬时失败不该停轮询。
        // restarting 或连续 5 次失败才停（约 5s）。
        console.error(e)
        this.selfUpdatePollFails += 1
        if (this.selfUpdateStatus?.phase === 'restarting' || this.selfUpdatePollFails >= 5) {
          this.stopSelfUpdatePolling()
          this.updatingSelf = false
        }
      }
    },
  },
})

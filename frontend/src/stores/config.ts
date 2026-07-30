import { defineStore } from 'pinia'
import api from '../api'
import { loadYaml, dumpYaml } from '../utils/yaml'
import { useNotifyStore } from './notify'

/**
 * 远程配置来源类型。
 * none 表示不使用远程配置（默认），url 表示直接填外部分享的订阅链接。
 *
 * 'all'（聚合全部订阅）已从界面移除：多机场聚合会把互相冲突的节点、
 * 策略组与规则一起并进最终配置，结果难以预期。存量数据仍可能是该值，
 * 故类型保留，只是不再提供新选项。
 */
export type RemoteSourceType = 'none' | 'all' | 'subscription' | 'collection' | 'file' | 'url'

/** 远程配置来源的可选项（仅 Sub-Store 内的实体会出现在这里） */
export interface RemoteSourceOption {
  type: 'subscription' | 'collection' | 'file'
  id: number
  name: string
  disabled: boolean
  reason: string
}

export const useConfigStore = defineStore('config', {
  state: () => ({
    raw: '',
    model: {} as Record<string, any>,
    loading: false,
    /**
     * 基础配置是否已成功从服务端读到。
     *
     * 必须有这个标志：model 初值是 {}，与「服务端确实是一份空配置」无法区分。
     * fetchBase 失败时若仍允许保存，dumpYaml({}) 得到的 "{}" 能通过后端的
     * 非空校验与 YAML 解析，会把用户真实配置整份覆盖成空。
     * 因此未加载成功前一律禁止写回。
     */
    baseLoaded: false,
    /** 基础配置加载失败的原因，用于在页面上给出错误态而非空表单 */
    baseLoadError: '',
    /**
     * 远程来源类型。默认 none：不使用远程配置，
     * 最终配置就等于本页填写的本地配置。
     */
    remoteSourceType: 'none' as RemoteSourceType,
    remoteSourceId: 0,
    /** 外部分享过来的订阅链接，仅 remoteSourceType === 'url' 时使用 */
    remoteSourceUrl: '',
    /** 定时拉取的 Cron 表达式，与系统设置的自动更新同一套语法 */
    remoteSourceCron: '0 0 * * * *',
    /** 是否启用定时拉取 */
    remoteSourceCronEnabled: true,
    remoteSourceOptions: [] as RemoteSourceOption[],
    /** 手动拉取的进行状态；结果以 toast 呈现，不再存于 state */
    pulling: false,
  }),
  actions: {
    async fetchRemoteSource() {
      try {
        const res = await api.get('/settings/remote-source')
        this.remoteSourceType = res.data?.sourceType || 'none'
        this.remoteSourceId = res.data?.sourceId || 0
        this.remoteSourceUrl = res.data?.sourceUrl || ''
        this.remoteSourceCron = res.data?.cron || '0 0 * * * *'
        this.remoteSourceCronEnabled = res.data?.cronEnabled !== false
        this.remoteSourceOptions = res.data?.options || []
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '加载远程来源失败')
      }
    },

    /**
     * 手动拉取远程配置并与本地合并。
     * 与定时拉取做同一件事，只是即时触发，不必等下一个周期。
     */
    async pullAndMerge() {
      this.pulling = true
      try {
        const res = await api.post('/config/pull-merge')
        const text = res.data?.message || '已拉取并合并'
        if (res.data?.success === false) useNotifyStore().error(text)
        else useNotifyStore().success(text)
        // 合并可能产生冲突或改动，刷新差异视图的数据源
        return res.data?.success !== false
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '拉取失败')
        return false
      } finally {
        this.pulling = false
      }
    },
    async saveRemoteSource() {
      this.loading = true
      try {
        const needsId = ['subscription', 'collection', 'file'].includes(this.remoteSourceType)
        // 前端先挡一道：选了实体类型却没选具体实体时，提交的 sourceId 为 0，
        // 后端必然回「远程来源配置非法: id=0」。那条报错对用户毫无指导意义，
        // 这里直接说清缺什么。
        if (needsId && !this.remoteSourceId) {
          useNotifyStore().error('请先选择具体的来源，再保存')
          return
        }
        if (this.remoteSourceType === 'url' && !this.remoteSourceUrl.trim()) {
          useNotifyStore().error('请填写外部订阅链接，再保存')
          return
        }
        const res = await api.put('/settings/remote-source', {
          sourceType: this.remoteSourceType,
          // none/all/url 不需要 id，后端也会忽略
          sourceId: needsId ? this.remoteSourceId : 0,
          sourceUrl: this.remoteSourceType === 'url' ? this.remoteSourceUrl.trim() : '',
          cron: this.remoteSourceCron.trim(),
          cronEnabled: this.remoteSourceCronEnabled,
        })
        this.remoteSourceType = res.data?.sourceType || 'none'
        this.remoteSourceId = res.data?.sourceId || 0
        this.remoteSourceUrl = res.data?.sourceUrl || ''
        this.remoteSourceCron = res.data?.cron || '0 0 * * * *'
        this.remoteSourceCronEnabled = res.data?.cronEnabled !== false
        this.remoteSourceOptions = res.data?.options || []
        useNotifyStore().success(
          this.remoteSourceType === 'none'
            ? '已设为不使用远程配置，最终配置将只用本地配置'
            : '远程来源已保存，下次合并生效',
        )
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '保存失败')
      } finally {
        this.loading = false
      }
    },
    async fetchBase() {
      this.loading = true
      this.baseLoadError = ''
      try {
        const res = await api.get<{ content: string }>('/config/base')
        this.raw = res.data.content || ''
        this.model = (loadYaml(this.raw) as any) || {}
        // 只有真正读到内容才放开写回，见 baseLoaded 的说明
        this.baseLoaded = true
      } catch (e: any) {
        this.baseLoaded = false
        // baseLoadError 保留为持久错误态：页面据此显示「配置未加载、
        // 禁止保存」的警示区，防止误覆盖服务端配置。
        // 这类需要持续可见的状态不适合只用一闪而过的 toast 表达。
        this.baseLoadError = e?.response?.data?.message || e?.message || '加载基础配置失败'
        useNotifyStore().error(this.baseLoadError)
      } finally {
        this.loading = false
      }
    },
    // 返回是否保存成功，供调用方决定后续动作
    async saveBase(): Promise<boolean> {
      // 未加载成功时拒绝写回：此时 model 仍是初值 {}，保存等于把服务端
      // 配置整份清空，且后端拦不住（"{}" 既非空字符串又是合法 YAML）。
      // saveAndMerge 依赖本函数的返回值，因此这一道守卫同时覆盖两条写入路径。
      if (!this.baseLoaded) {
        useNotifyStore().error('基础配置尚未加载成功，请先重新加载再保存，以免覆盖服务端配置')
        return false
      }
      this.loading = true
      try {
        const content = dumpYaml(this.model)
        const res = await api.put('/config/base', { content })
        this.raw = content
        useNotifyStore().success(res.data?.message || '保存成功')
        return true
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || e?.message || '保存失败')
        return false
      } finally {
        this.loading = false
      }
    },
    /**
     * 保存本地配置并重新生成最终配置。
     *
     * 刻意不拉取远程：用户改的是自己的 mihomo 配置，
     * 不该因此去打上游机场。远程内容的更新由定时拉取
     * 或 pullAndMerge（立即拉取并合并）负责。
     */
    async saveAndMerge() {
      // 保存失败时不得继续合并，否则会用旧配置重新生成，
      // 且成功提示会覆盖掉真正的错误信息
      if (!(await this.saveBase())) return
      this.loading = true
      try {
        const res = await api.post('/config/merge')
        useNotifyStore().success(res.data?.message || '已应用并生效')
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || e?.message || '合并失败')
      } finally {
        this.loading = false
      }
    },
  },
})

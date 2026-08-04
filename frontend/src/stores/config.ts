import { defineStore } from 'pinia'
import api from '../api'
import { loadYaml, dumpYaml } from '../utils/yaml'
import { useNotifyStore } from './notify'
import { useTransparentStore } from './transparent'

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
    /**
     * 本地基础配置已保存到数据库，但尚未与远程层合并并下发给内核。
     *
     * 当用户只点「保存基础配置」而未点「保存并应用」或「立即拉取并合并」时为 true。
     * 界面据此弹出黄条提示，避免用户以为「保存了就等于配置生效了」。
     *
     * 结论来自后端（/config/unmerged，按合并指纹推导）而非本地推断：
     * 刷新页面、换浏览器、清缓存、换机器都得到同一结论；
     * 后端定时拉取或透明代理开关触发的合并也会自动让提示消失。
     * 保存/合并成功后本地即时置位只是让界面不等待下一次请求。
     */
    unmergedChanges: false,
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
      } catch (e: unknown) {
        // HTTP 错误由 api 拦截器统一 toast，此处只记日志
        console.error(e)
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
        if (res.data?.success === false) {
          useNotifyStore().error(text)
        } else {
          useNotifyStore().success(text)
          this.unmergedChanges = false
        }
        // 合并可能产生冲突或改动，刷新差异视图的数据源
        return res.data?.success !== false
      } catch (e: unknown) {
        console.error(e)
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
      } catch (e: unknown) {
        console.error(e)
      } finally {
        this.loading = false
      }
    },
    async fetchBase() {
      this.loading = true
      this.baseLoadError = ''
      try {
        // 失败时页面要长期展示 baseLoadError，自行 toast 一次即可
        const res = await api.get<{ content: string }>('/config/base', {
          skipErrorToast: true,
        })
        this.raw = res.data.content || ''
        this.model = (loadYaml(this.raw) as any) || {}
        // 只有真正读到内容才放开写回，见 baseLoaded 的说明
        this.baseLoaded = true
        // 未合并状态以服务端推导为准（合并指纹比较，见后端 BaseUnmerged），
        // 刷新页面/换浏览器都一致。不阻塞主流程：提示只是辅助信息，
        // 接口失败时保持原值。
        void this.fetchUnmerged()
      } catch (e: unknown) {
        this.baseLoaded = false
        // baseLoadError 保留为持久错误态：页面据此显示「配置未加载、
        // 禁止保存」的警示区，防止误覆盖服务端配置。
        // 这类需要持续可见的状态不适合只用一闪而过的 toast 表达。
        const { apiErrorMessage } = await import('../utils/apiError')
        this.baseLoadError = apiErrorMessage(e, '加载基础配置失败')
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
        // 保存后必然未合并（后端指纹失配）；本地即时置位，
        // 无需等下一次 fetchUnmerged 往返
        this.unmergedChanges = true
        useNotifyStore().success(res.data?.message || '保存成功')
        return true
      } catch (e: unknown) {
        console.error(e)
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
      let merged = false
      try {
        const res = await api.post('/config/merge')
        this.unmergedChanges = false
        useNotifyStore().success(res.data?.message || '已应用并生效')
        merged = true
      } catch (e: unknown) {
        console.error(e)
      } finally {
        this.loading = false
      }

      // 透明代理开关读的是 base.yaml 里的 tun.enable / tproxy-port，
      // 本页刚改过它，系统设置那边的开关状态必须跟着刷新。
      //
      // 放在 try 之外：这一步失败只是"另一个页面的显示没同步"，
      // 若与合并共用 catch，一次拉取失败就会在"已应用并生效"之后
      // 再弹一条"合并失败"，两条互相矛盾的提示。
      // fetch 自身已有错误处理，这里不再重复兜。
      if (merged) {
        await useTransparentStore().fetch()
      }
    },
    /**
     * 从服务端读取「base 已保存但未合并」状态。
     *
     * 由后端按合并指纹推导（每次合并成功记录 base 内容哈希并比较当前值），
     * 前端不在本地做任何推断——换浏览器、清缓存、换机器结论一致。
     */
    async fetchUnmerged() {
      try {
        const res = await api.get<{ unmerged: boolean }>('/config/unmerged')
        this.unmergedChanges = res.data?.unmerged === true
      } catch {
        // 失败不打扰用户：提示是辅助状态，保持原值即可
      }
    },
  },
})

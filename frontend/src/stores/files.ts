import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

export interface SubFile {
  id: number
  name: string
  content: string
  type: string
  /** 远程正文地址，支持多行、每行一个 */
  syncUrl: string
  /** local=正文取自编辑器；remote=取自 syncUrl */
  sourceMode: string
  /** 空=不合并；localFirst / remoteFirst 决定本地与远程的拼接顺序 */
  mergeSources: string
  /** 空=失败即报错；enabled=跳过并提示；quiet=静默跳过 */
  ignoreFailedRemote: string
  /** 拉取远程正文时使用的 User-Agent */
  userAgent: string
  /** 直链凭据，前端据此拼出 /api/v1/file/{shareToken} */
  shareToken: string
  /** file=原样输出；mihomo=作为模板套用订阅节点渲染成 mihomo 配置 */
  configType: string
  /** mihomo 类型下节点来源：subscription | collection */
  sourceType: string
  sourceId: number
  /** mihomo 类型下模板正文的书写语言：gotemplate（默认）/ yaml（覆写深合并）/ javascript（脚本覆写） */
  templateLang: string
  /** 流量显示链接，供客户端读取 subscription-userinfo */
  trafficUrl: string
  shareName: string
  /** RFC3339 时间串，空串表示永不过期 */
  expiresAt: string
}

export const useFilesStore = defineStore('files', {
  state: () => ({
    files: [] as SubFile[],
    loading: false,
  }),
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<SubFile[]>('/files')
        this.files = res.data || []
      } catch (e: unknown) {
        // 拦截器已 toast
        console.error(e)
      } finally {
        this.loading = false
      }
    },
    async create(payload: any) {
      await api.post('/files', payload)
      await this.fetch()
    },
    async update(id: number, payload: any) {
      await api.put(`/files/${id}`, payload)
      await this.fetch()
    },
    async remove(id: number) {
      await api.delete(`/files/${id}`)
      await this.fetch()
    },
    async sync(id: number) {
      try {
        // 领域 fallback：上游地址问题比通用 HTTP 句更有用
        await api.post(`/files/${id}/sync`, null, { skipErrorToast: true })
        useNotifyStore().success('已从上游同步最新内容')
      } catch (e: unknown) {
        const { apiErrorMessage } = await import('../utils/apiError')
        useNotifyStore().error(apiErrorMessage(e, '同步失败，请检查上游地址'))
        throw e
      } finally {
        await this.fetch()
      }
    }
  }
})

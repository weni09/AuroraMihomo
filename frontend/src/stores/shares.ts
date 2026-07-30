import { defineStore } from 'pinia'
import { useNotifyStore } from './notify'
import api from '../api'

/** 分享的载体类型 */
export type ShareKind = 'subscription' | 'collection' | 'file'

export interface ShareItem {
  kind: ShareKind
  id: number
  /** 所属实体名称（订阅名/组合名/文件名） */
  sourceName: string
  /** 分享的自定义展示名，留空时回落到 sourceName */
  shareName: string
  shareToken: string
  /** 相对路径，前端拼上 origin 得到完整链接；撤销后为空串 */
  url: string
  /** RFC3339 时间串，空串表示永不过期 */
  expiresAt: string
  expired: boolean
  /** 所属实体是否启用，停用后链接同样失效 */
  enabled: boolean
  /** 已撤销（无凭据） */
  revoked: boolean
}

export const useSharesStore = defineStore('shares', {
  state: () => ({
    items: [] as ShareItem[],
    loading: false,
  }),
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<{ items: ShareItem[] }>('/shares')
        this.items = res.data?.items || []
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '加载分享列表失败')
      } finally {
        this.loading = false
      }
    },
    /** 修改分享的展示名与有效期；expiresAt 传空串表示永不过期 */
    async update(kind: ShareKind, id: number, payload: { shareName: string; expiresAt: string }) {
      this.loading = true
      try {
        const res = await api.put<{ items: ShareItem[] }>(`/shares/${kind}/${id}`, payload)
        this.items = res.data?.items || []
        useNotifyStore().success('分享设置已保存')
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '保存失败')
        throw e
      } finally {
        this.loading = false
      }
    },
    /** 重置凭据：旧链接立即失效 */
    async reset(kind: ShareKind, id: number) {
      this.loading = true
      try {
        const res = await api.post<{ items: ShareItem[] }>(`/shares/${kind}/${id}/reset`)
        this.items = res.data?.items || []
        useNotifyStore().success('已生成新的分享链接，旧链接已失效')
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '重置失败')
        throw e
      } finally {
        this.loading = false
      }
    },
    /** 撤销分享：清空凭据但保留实体 */
    async revoke(kind: ShareKind, id: number) {
      this.loading = true
      try {
        const res = await api.post<{ items: ShareItem[] }>(`/shares/${kind}/${id}/revoke`)
        this.items = res.data?.items || []
        useNotifyStore().success('分享已撤销')
      } catch (e: any) {
        useNotifyStore().error(e?.response?.data?.message || '撤销失败')
        throw e
      } finally {
        this.loading = false
      }
    },
  },
})

import { defineStore } from 'pinia'
import api from '../api'

/** 预览结果：处理前后的内容对照 */
export interface PreviewResult {
  format: string
  original: string
  processed: string
  count: number
  warnings: string[]
}

/** 预览请求。字段按 kind 取用，未用到的可省略 */
export interface PreviewPayload {
  kind: 'subscription' | 'collection' | 'file'
  target?: string
  operators?: any[]
  // subscription
  url?: string
  content?: string
  userAgent?: string
  // collection
  subIds?: number[]
  // file
  configType?: string
  sourceMode?: string
  syncUrl?: string
  mergeSources?: string
  ignoreFailedRemote?: string
  sourceType?: string
  sourceId?: number
  templateLang?: string
}

/**
 * 即时预览。
 *
 * 预览的是「当前表单」而非已保存的记录，因此新建时也能用，
 * 改完处理管道不必先落库再看效果。后端保证不写库。
 */
export const usePreviewStore = defineStore('preview', {
  state: () => ({
    result: null as PreviewResult | null,
    loading: false,
    error: '',
  }),
  actions: {
    async run(payload: PreviewPayload) {
      this.loading = true
      this.error = ''
      // 清空旧结果，避免加载期间仍显示上一次的内容而被误读
      this.result = null
      try {
        const res = await api.post<PreviewResult>('/preview', payload)
        this.result = res.data
      } catch (e: any) {
        this.error = e?.response?.data?.message || e?.message || '预览失败'
      } finally {
        this.loading = false
      }
    },
    close() {
      this.result = null
      this.error = ''
    },
  },
})

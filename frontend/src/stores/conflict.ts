import { defineStore } from 'pinia'
import api from '../api'

export interface ConflictItem {
  id: number
  key: string
  type: string
  path: string
  localValue: string
  remoteValue: string
  manualValue: string
  resolution: string
  resolved: boolean
}

export const useConflictStore = defineStore('conflict', {
  state: () => ({
    conflicts: [] as ConflictItem[],
    loading: false,
    message: '',
  }),
  getters: {
    unresolved: (s) => s.conflicts.filter((c) => !c.resolved),
    unresolvedCount: (s) => s.conflicts.filter((c) => !c.resolved).length,
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<ConflictItem[]>('/conflicts')
        this.conflicts = res.data || []
      } catch (e) {
        console.error(e)
        this.message = '加载冲突列表失败'
      } finally {
        this.loading = false
      }
    },
    // resolution: local | remote | merge | manual
    async resolve(id: number, resolution: string, manualValue = '') {
      const res = await api.post(`/conflicts/${id}/resolve`, { resolution, manualValue })
      this.message = res.data?.message || '已处理'
      await this.fetch()
    },
  },
})

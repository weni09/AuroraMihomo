import { defineStore } from 'pinia'
import api from '../api'

export const useConfigLabStore = defineStore('configlab', {
  state: () => ({
    // 配置差异页要展示的三份原始 YAML：本地 / 最终 / 远程（可能为空）
    localContent: '',
    finalContent: '',
    remoteContent: '',
    loadingConfigs: false,
    configsError: '',
    collections: [] as any[],
    message: '',
  }),
  actions: {
    /**
     * 拉取本地配置、最终配置、远程订阅三份原始 YAML，供并排对照展示。
     *
     * 三者独立请求：某一份因未合并过/未配置远程来源而为空字符串，
     * 不应影响其余两份的加载（后端已保证这类情况返回空内容而非报错）。
     */
    async fetchConfigs() {
      this.loadingConfigs = true
      this.configsError = ''
      try {
        const [local, final, remote] = await Promise.all([
          api.get('/config/base'),
          api.get('/config/final'),
          api.get('/config/remote'),
        ])
        this.localContent = local.data?.content || ''
        this.finalContent = final.data?.content || ''
        this.remoteContent = remote.data?.content || ''
      } catch (e: any) {
        this.configsError = e?.response?.data?.message || '加载配置失败'
      } finally {
        this.loadingConfigs = false
      }
    },
    async fetchCollections() {
      const res = await api.get('/collections')
      this.collections = res.data || []
    },
    async createCollection(payload: any) {
      await api.post('/collections', payload)
      await this.fetchCollections()
    },
    async updateCollection(id: number, payload: any) {
      await api.put(`/collections/${id}`, payload)
      await this.fetchCollections()
    },
    async deleteCollection(id: number) {
      await api.delete(`/collections/${id}`)
      await this.fetchCollections()
    },
    /**
     * 构建已保存的组合并返回产物。
     *
     * 界面已改用 /preview：它能预览未保存的表单，还给出处理前后的对照。
     * 此方法保留是因为 /collections/:id/build 接口仍在，
     * 供需要「按 id 直接构建」的场景使用。
     */
    async buildCollection(id: number) {
      const res = await api.post(`/collections/${id}/build`)
      this.message = `构建成功，共 ${res.data?.count || 0} 个节点`
      return res.data
    },
  },
})

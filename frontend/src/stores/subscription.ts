import { defineStore } from 'pinia'
import api from '../api'
import { useNotifyStore } from './notify'

export interface TrafficInfo {
  upload: number
  download: number
  used: number
  total: number
  expire: number
  expireAt: string
}

export interface Subscription {
  id: number
  name: string
  url: string
  content: string
  enabled: boolean
  /**
   * 自动更新间隔（秒）。界面已不再使用：订阅不再各自定时回源，
   * 刷新时机统一由配置中心的定时拉取与手动即时拉取决定。
   * 字段保留仅为兼容接口返回。
   */
  interval: number
  userAgent: string
  nodeCount: number
  status: string
  errorMessage: string
  lastUpdate: string
  operators: any[]
  shareToken: string
  traffic: TrafficInfo | null
}

export const useSubscriptionStore = defineStore('subscription', {
  state: () => ({
    subscriptions: [] as Subscription[],
    isLoading: false,
  }),
  actions: {
    async fetchSubscriptions() {
      this.isLoading = true
      try {
        const response = await api.get<Subscription[]>('/subscriptions')
        this.subscriptions = response.data || []
      } catch (error: unknown) {
        // 拦截器已 toast
        console.error('Failed to fetch subscriptions', error)
      } finally {
        this.isLoading = false
      }
    },
    async updateSubscription(id: number, payload: Record<string, any>) {
      await api.put(`/subscriptions/${id}`, payload)
      await this.fetchSubscriptions()
    },
    async createSubscription(payload: Record<string, any>) {
      await api.post('/subscriptions', payload)
      await this.fetchSubscriptions()
    },
    async deleteSubscription(id: number) {
      await api.delete(`/subscriptions/${id}`)
      await this.fetchSubscriptions()
    },
    // 「刷新缓存」：只回源刷新该订阅自身的节点缓存（供分享/预览使用），
    // 不生成最终配置、不重启内核——这两件事归配置中心管。
    async updateNow(id: number) {
      const res = await api.post(`/subscriptions/${id}/update`)
      // 后端对失败也返回 200 + success:false（见 Result 约定），
      // 只看 HTTP 状态会把失败当成功报出去
      if (res.data?.success === false) {
        useNotifyStore().error(res.data?.message || '刷新失败')
      } else {
        useNotifyStore().success(res.data?.message || '订阅缓存已刷新')
      }
      await this.fetchSubscriptions()
    },
    // 全局「刷新缓存」：逐条回源刷新全部订阅（含已禁用）的节点缓存。
    // 单条失败不中断整体，后端在结果里汇总成败与失败名单；
    // 明细在各订阅「缓存状态」列，这里只 toast 汇总。
    async updateAll() {
      const res = await api.post<RefreshAllResult>('/subscriptions/refresh-all')
      const result = res.data
      if (result.failed > 0) {
        const names = result.failedNames?.length
          ? `（${result.failedNames.join('、')}）`
          : ''
        useNotifyStore().error(
          `已刷新 ${result.success}/${result.total} 条订阅缓存，${result.failed} 条失败${names}，详情见各订阅「缓存状态」列`,
        )
      } else {
        useNotifyStore().success(`已刷新全部 ${result.total} 条订阅缓存`)
      }
      await this.fetchSubscriptions()
    },
    // 拉取远程订阅并重新合并配置由配置中心承担（/config/pull-merge），
    // 单个订阅页不提供这个入口。
    // 探测订阅流量参数：V2Board 类机场只在特定 flag 参数下下发
    // subscription-userinfo 头（如 &flag=clashmeta），探测接口逐一尝试
    // 常见组合，返回「有流量信息且节点完整」的候选供一键应用。
    async probeParams(payload: { url: string; userAgent?: string }) {
      const res = await api.post('/subscriptions/probe', payload)
      return res.data as ProbeResp
    },
  },
})

export interface ProbeCandidate {
  params: string
  url: string
  hasUserInfo: boolean
  usedBytes: number
  totalBytes: number
  nodeCount: number
  placeholder: boolean
  error?: string
}

export interface ProbeResp {
  candidates: ProbeCandidate[]
  bestUrl: string
}

// 全局「刷新缓存」的结果汇总：逐条回源，单条失败不中断整体。
export interface RefreshAllResult {
  total: number
  success: number
  failed: number
  failedNames: string[]
}

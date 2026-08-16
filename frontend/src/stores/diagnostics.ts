import { defineStore } from 'pinia'
import api from '../api'

export interface DiagnosticTarget {
  type: 'ping' | 'dns' | 'tcp' | 'http' | 'traceroute'
  target: string
  port?: number
}

export interface ProbeResult {
  target: string
  type: string
  path: string
  status: 'success' | 'fail' | 'timeout' | 'error'
  latencyMs?: number
  detail?: Record<string, unknown>
  error?: string
}

export const useDiagnosticsStore = defineStore('diagnostics', {
  state: () => ({
    running: false,
    requestId: '' as string,
    results: [] as ProbeResult[],
    error: '' as string,
  }),
  getters: {
    // 按 target|type 分组：同一目标的同一探测类型在列表上并排展示
    groupedResults: (s) => {
      const map = new Map<string, ProbeResult[]>()
      for (const r of s.results) {
        const key = `${r.target}|${r.type}`
        if (!map.has(key)) map.set(key, [])
        map.get(key)!.push(r)
      }
      return map
    },
  },
  actions: {
    async run(targets: DiagnosticTarget[], path: string) {
      this.running = true
      this.error = ''
      this.results = []
      try {
        const res = await api.post<{ requestId: string }>('/diagnostics/run', {
          targets,
          path,
        })
        this.requestId = res.data.requestId
      } catch (e) {
        this.error = '诊断启动失败'
        this.running = false
        // 清空 requestId：否则 handleProgress 会继续收上一轮的进度事件混入本轮
        this.requestId = ''
        throw e
      }
    },
    // WS hub 推送入口：只收集属于当前请求的事件，避免上一轮/其它请求的进度混入
    handleProgress(type: string, data: Record<string, unknown>) {
      if (type !== 'diagnostic.progress' || data.requestId !== this.requestId) return
      this.results.push(data as unknown as ProbeResult)
    },
    // 轮询兜底：进度事件可能丢（WS 断连/重连窗口），结束时用全量结果回填
    async fetchResult(requestId: string) {
      const res = await api.get<{ done: boolean; results?: ProbeResult[] }>(
        `/diagnostics/result/${requestId}`,
      )
      if (res.data.done) {
        this.results = res.data.results || []
        this.running = false
      }
      return res.data.done
    },
    reset() {
      this.running = false
      this.requestId = ''
      this.results = []
      this.error = ''
    },
  },
})

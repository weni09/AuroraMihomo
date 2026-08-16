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

// InvalidTarget 是后端校验拒绝的目标（SSRF/空目标等），不阻塞其余合法目标。
export interface InvalidTarget {
  target: string
  reason: string
}

export const useDiagnosticsStore = defineStore('diagnostics', {
  state: () => ({
    running: false,
    requestId: '' as string,
    results: [] as ProbeResult[],
    // invalidResults 是后端校验拒绝的目标，预置为 error 结果：
    // 与 results 分离保存，轮询回填（fetchResult）时不丢失
    invalidResults: [] as ProbeResult[],
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
    // 预设目标清单来自后端（含代理端口 TCP 探测目标，前端无法自行推导代理地址）
    async fetchPresetTargets(): Promise<DiagnosticTarget[]> {
      const res = await api.get<{ targets: DiagnosticTarget[] }>('/diagnostics/targets')
      return res.data.targets || []
    },
    async run(targets: DiagnosticTarget[], path: string) {
      this.running = true
      this.error = ''
      this.results = []
      this.invalidResults = []
      try {
        const res = await api.post<{ requestId: string; invalid?: InvalidTarget[] }>(
          '/diagnostics/run',
          {
            targets,
            path,
          },
        )
        this.requestId = res.data.requestId || ''
        // 后端校验跳过的非法目标：渲染为已完成的 error 结果
        // （InvalidTarget 不含 type，按本次请求的目标回查）
        const typeByTarget: Record<string, DiagnosticTarget['type']> = {}
        for (const t of targets) {
          if (!(t.target in typeByTarget)) typeByTarget[t.target] = t.type
        }
        for (const inv of res.data.invalid || []) {
          this.invalidResults.push({
            target: inv.target,
            type: typeByTarget[inv.target] ?? 'http',
            path,
            status: 'error',
            error: inv.reason,
          })
        }
        this.results = [...this.invalidResults]
        if (!this.requestId) {
          // 全部目标非法：没有可运行的请求，直接结束
          this.running = false
        }
      } catch (e) {
        this.error = '诊断启动失败'
        this.running = false
        // 清空 requestId：否则 handleProgress 会继续收上一轮的进度事件混入本轮
        this.requestId = ''
        throw e
      }
    },
    // WS hub 推送入口：只收集属于当前请求且仍在运行的事件，
    // 避免上一轮/其它请求的进度混入，也避免 fetchResult 回填后迟到的重复事件
    handleProgress(type: string, data: Record<string, unknown>) {
      if (type !== 'diagnostic.progress' || !this.running || data.requestId !== this.requestId) return
      // 剥离事件包装的 requestId 字段，与 fetchResult 回填的服务端 ProbeResult 形状一致
      const result = { ...data } as unknown as ProbeResult & { requestId?: unknown }
      delete result.requestId
      this.results.push(result)
    },
    // 轮询兜底：进度事件可能丢（WS 断连/重连窗口），结束时用全量结果回填
    async fetchResult(requestId: string) {
      const res = await api.get<{ done: boolean; results?: ProbeResult[] }>(
        `/diagnostics/result/${requestId}`,
      )
      if (res.data.done) {
        // 非法目标预置结果保留在列表头部，与后端全量结果合并
        this.results = [...this.invalidResults, ...(res.data.results || [])]
        this.running = false
      }
      return res.data.done
    },
    reset() {
      this.running = false
      this.requestId = ''
      this.results = []
      this.invalidResults = []
      this.error = ''
    },
  },
})

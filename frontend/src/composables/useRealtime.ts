import { onMounted, onUnmounted, ref } from 'vue'

export type RealtimeHandler = (type: string, data: any, raw: any) => void

function wsURL() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  // 开发模式下 Vite 代理对 WS upgrade 支持不稳定，直连后端
  const host = import.meta.env.DEV ? '127.0.0.1:8899' : location.host
  // WebSocket 无法自定义请求头，令牌只能走 query
  const token = localStorage.getItem('aurora_token') || ''
  return `${proto}://${host}/ws${token ? `?token=${encodeURIComponent(token)}` : ''}`
}

/**
 * 服务端关停时主动发送的关闭码（RFC 6455 的 Service Restart）。
 * 后端在 Hub 关闭时发出，见 backend/api/aurora.go 的 WS 写循环。
 */
const CLOSE_SERVICE_RESTART = 1012

/** 服务端重启期间的重连间隔：比普通退避更长，等它把端口重新监听起来 */
const RESTART_RETRY_DELAY = 3000

export function useRealtime(onEvent?: RealtimeHandler) {
  const status = ref<'connecting' | 'live' | 'closed' | 'error' | 'restarting'>('connecting')
  let ws: WebSocket | null = null
  let timer: number | null = null
  let stopped = false
  let attempt = 0

  const connect = () => {
    if (stopped) return
    if (status.value !== 'restarting') status.value = 'connecting'
    ws = new WebSocket(wsURL())
    ws.onopen = () => {
      status.value = 'live'
      attempt = 0
    }
    ws.onclose = (ev) => {
      // 服务端主动通知重启：与"网络异常"区分开。
      // 不区分的话前端会按 500ms 起的退避立刻重试，而此时服务端正在关停，
      // 每次都失败，用户看到一串报错；这里改用固定较长间隔安静等待。
      if (ev.code === CLOSE_SERVICE_RESTART) {
        status.value = 'restarting'
        scheduleReconnect(RESTART_RETRY_DELAY)
        return
      }
      status.value = 'closed'
      scheduleReconnect()
    }
    ws.onerror = () => {
      status.value = 'error'
    }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data)
        onEvent?.(msg.type, msg.data, msg)
      } catch {
        // ignore malformed
      }
    }
  }

  const scheduleReconnect = (fixedDelay?: number) => {
    if (stopped) return
    if (timer) window.clearTimeout(timer)
    attempt += 1
    // fixedDelay 用于服务端重启：此时退避没有意义，等固定间隔再试即可
    const delay = fixedDelay ?? Math.min(10000, 500 * attempt)
    timer = window.setTimeout(connect, delay)
  }

  onMounted(connect)
  onUnmounted(() => {
    stopped = true
    if (timer) window.clearTimeout(timer)
    ws?.close()
  })

  return { status }
}

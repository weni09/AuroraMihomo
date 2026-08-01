import { useMihomoStore } from '../stores/mihomo'
import { useRealtime } from './useRealtime'

/**
 * 订阅内核状态与日志推送，写入 mihomo store。
 * 控制台与内核管理页共用，避免一边实时一边过期。
 *
 * onExtra：控制台还需处理 config.updated 等非内核事件时传入。
 */
export function useMihomoRealtime(onExtra?: (type: string, data: unknown) => void) {
  const store = useMihomoStore()
  return useRealtime((type, data) => {
    if (type === 'mihomo.status' && data && typeof data === 'object') {
      const d = data as { status?: string; version?: string; pid?: number; appVersion?: string }
      store.applyStatus({
        status: d.status,
        version: d.version,
        pid: d.pid,
        appVersion: d.appVersion,
      })
    }
    if (type === 'log.message' && data && typeof data === 'object') {
      const d = data as { time?: string; stream?: string; message?: string }
      store.pushLog(d)
    }
    onExtra?.(type, data)
  })
}

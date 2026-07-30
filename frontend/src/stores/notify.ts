import { defineStore } from 'pinia'

export type ToastLevel = 'error' | 'success' | 'info'

export interface Toast {
  id: number
  level: ToastLevel
  text: string
}

let seq = 0

/**
 * 同时展示的最大条数。
 *
 * 批量请求失败（如并发拉取多个订阅）会一次汇入多条，容器没有高度上限，
 * 超出视口的那几条既看不见也点不到关闭。丢最旧的而非拒绝最新的：
 * 后发生的错误通常更贴近用户当前的操作。
 */
const MAX_TOASTS = 5

// 全局消息中心：API 请求失败会经拦截器汇入这里，
// 避免各页面各自 try/catch 导致的静默失败。
export const useNotifyStore = defineStore('notify', {
  state: () => ({
    toasts: [] as Toast[],
  }),
  actions: {
    push(level: ToastLevel, text: string, timeout = 6000) {
      const id = ++seq
      this.toasts.push({ id, level, text })
      if (this.toasts.length > MAX_TOASTS) {
        this.toasts.splice(0, this.toasts.length - MAX_TOASTS)
      }
      if (timeout > 0) {
        setTimeout(() => this.dismiss(id), timeout)
      }
    },
    error(text: string) {
      this.push('error', text, 8000)
    },
    success(text: string) {
      this.push('success', text, 3000)
    },
    dismiss(id: number) {
      const i = this.toasts.findIndex(t => t.id === id)
      if (i >= 0) this.toasts.splice(i, 1)
    },
  },
})

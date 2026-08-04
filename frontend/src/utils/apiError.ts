import axios from 'axios'

/**
 * 从捕获到的异常里取出后端给的错误文案。
 *
 * 为什么需要它：后端把「缺什么、怎么补」这类可操作信息放在响应体里
 * （例如透明代理不可用时会带上安装命令），直接丢掉换成一句通用提示，
 * 用户就失去了修复线索。
 *
 * catch 到的值类型是 unknown（项目禁止 any），这里集中做窄化，
 * 各 store 不必各自写一遍类型断言。
 *
 * 后端错误体有两种形态：go-zero 的 httpx.Error 直接写纯文本，
 * 业务层用 {"message": "..."} 或 {"msg": "..."} 包一层，两种都要认。
 * 无响应体时再按超时/离线/HTTP 状态给出可读中文，供拦截器与 store 共用，
 * 避免 api.ts 与本文件各维护一套 extract 逻辑。
 */
export function apiErrorMessage(e: unknown, fallback: string): string {
  if (axios.isAxiosError(e)) {
    const data: unknown = e.response?.data
    if (typeof data === 'string' && data.trim() !== '') {
      return data.trim()
    }
    if (data && typeof data === 'object') {
      const rec = data as Record<string, unknown>
      for (const key of ['message', 'msg'] as const) {
        const msg = rec[key]
        if (typeof msg === 'string' && msg.trim() !== '') {
          return msg.trim()
        }
      }
    }
    if (e.code === 'ECONNABORTED') {
      return '请求超时，请稍后重试'
    }
    if (!e.response) {
      return '无法连接服务端，请检查服务是否运行'
    }
    // 有 HTTP 状态但无可用文案：优先调用方 fallback；fallback 为空时给通用句
    if (fallback.trim() !== '') {
      return fallback
    }
    return `请求失败（HTTP ${e.response.status}）`
  }
  if (e instanceof Error && e.message !== '') {
    return e.message
  }
  return fallback
}

/** 拦截器用：无调用方 fallback 时的默认中文提取 */
export function apiErrorMessageDefault(e: unknown): string {
  return apiErrorMessage(e, '请求失败')
}

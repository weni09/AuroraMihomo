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
 * 业务层用 {"message": "..."} 包一层，两种都要认。
 */
export function apiErrorMessage(e: unknown, fallback: string): string {
  if (axios.isAxiosError(e)) {
    const data: unknown = e.response?.data
    if (typeof data === 'string' && data.trim() !== '') {
      return data.trim()
    }
    if (data && typeof data === 'object') {
      const msg = (data as Record<string, unknown>).message
      if (typeof msg === 'string' && msg.trim() !== '') {
        return msg.trim()
      }
    }
    // 到这里说明服务端没给可用文案。不能往下走 Error 分支——AxiosError
    // 自己也是 Error，它的 message 是 "Request failed with status code 400"
    // 这类英文技术串，展示给用户毫无意义，不如用调用方给的中文兜底。
    return fallback
  }
  if (e instanceof Error && e.message !== '') {
    return e.message
  }
  return fallback
}

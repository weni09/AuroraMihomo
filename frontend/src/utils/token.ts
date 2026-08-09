/**
 * JWT 解析与过期判断。
 *
 * token 只存 localStorage，前端不可能验证签名（密钥在后端），这里只做
 * 「宽松过期检查」：exp 可读且已过期则视为无效。签名错误或 exp 缺失的
 * token 无法判定，按有效放行——真正的鉴权以后端 401 为准（见 router 的
 * 处理：401 会清 token 跳登录，用户不会被错误拦在门外）。
 */

/** 解析 JWT 载荷（不校验签名）。失败或非 object 返回 null */
export function parseJwtPayload(token: string): Record<string, unknown> | null {
  try {
    const part = token.split('.')[1]
    if (!part) return null
    // base64url → UTF-8；补齐 padding 以兼容严格解析
    const b64 = part.replace(/-/g, '+').replace(/_/g, '/')
    const padded = b64.padEnd(Math.ceil(b64.length / 4) * 4, '=')
    const json = decodeURIComponent(
      Array.from(atob(padded), (c) => `%${c.charCodeAt(0).toString(16).padStart(2, '0')}`).join(''),
    )
    const obj: unknown = JSON.parse(json)
    return typeof obj === 'object' && obj !== null ? (obj as Record<string, unknown>) : null
  } catch {
    return null
  }
}

/**
 * token 是否已过期。无法解析、无 exp 或 exp 非数字时返回 false（无法判定，
 * 交给后端鉴权兜底）。留 5 秒余量：路由守卫在令牌恰好到期的边界上不应
 * 放行一个马上失效的 token，导致一进页面就刷出一片 401。
 */
export function isTokenExpired(token: string, leewayMs = 5_000): boolean {
  const payload = parseJwtPayload(token)
  if (!payload) return false
  const exp = payload.exp
  if (typeof exp !== 'number' || !Number.isFinite(exp)) return false
  return exp * 1000 + leewayMs < Date.now()
}

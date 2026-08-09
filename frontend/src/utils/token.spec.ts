import { describe, expect, it } from 'vitest'
import { isTokenExpired, parseJwtPayload } from './token'

/** 生成一个带指定 exp（Unix 秒）的 JWT 载荷部分，签名随便填，前端不校验 */
function makeToken(expSec: number | null): string {
  const b64 = (s: string) =>
    btoa(s)
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '')
  const header = b64(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const payload = b64(
    JSON.stringify(expSec === null ? { uid: 1 } : { uid: 1, exp: expSec }),
  )
  return `${header}.${payload}.fakesignature`
}

describe('parseJwtPayload', () => {
  it('解析合法 token 返回载荷', () => {
    const p = parseJwtPayload(makeToken(1_800_000_000))
    expect(p?.uid).toBe(1)
    expect(p?.exp).toBe(1_800_000_000)
  })

  it('畸形 token 返回 null（不抛错）', () => {
    expect(parseJwtPayload('not-a-jwt')).toBeNull()
    expect(parseJwtPayload('a.b')).toBeNull()
    expect(parseJwtPayload('')).toBeNull()
  })
})

describe('isTokenExpired', () => {
  it('过期 token 判定为过期', () => {
    expect(isTokenExpired(makeToken(1_000_000))).toBe(true)
  })

  it('未过期 token 判定为未过期', () => {
    expect(isTokenExpired(makeToken(4_100_000_000))).toBe(false)
  })

  it('无 exp 的 token 无法判定，按有效处理', () => {
    expect(isTokenExpired(makeToken(null))).toBe(false)
  })
})

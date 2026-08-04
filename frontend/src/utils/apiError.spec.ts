import { describe, expect, it } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import { apiErrorMessage, apiErrorMessageDefault } from './apiError'

// 后端把「缺什么、怎么补」放在错误体里（如透明代理不可用时带安装命令），
// 丢掉它用户就没有修复线索，所以两种响应形态都必须能取到。
function axiosErrorWith(data: unknown, status = 400): AxiosError {
  const err = new AxiosError('Request failed with status code ' + status)
  err.response = {
    data,
    status,
    statusText: 'Bad Request',
    headers: new AxiosHeaders(),
    config: { headers: new AxiosHeaders() },
  }
  return err
}

describe('apiErrorMessage', () => {
  it('取出 go-zero 直接写的纯文本错误体', () => {
    const e = axiosErrorWith('无法启用 tproxy 模式: 缺少 nftables。可执行：apk add nftables')
    expect(apiErrorMessage(e, '兜底')).toContain('apk add nftables')
  })

  it('取出业务层包了一层的 message 字段', () => {
    const e = axiosErrorWith({ message: '当前没有待确认的启用操作' })
    expect(apiErrorMessage(e, '兜底')).toBe('当前没有待确认的启用操作')
  })

  it('取出 msg 字段（部分接口）', () => {
    expect(apiErrorMessage(axiosErrorWith({ msg: '简写错误' }), '兜底')).toBe('简写错误')
  })

  it('响应体为空或无可用文案时回落到兜底文案', () => {
    expect(apiErrorMessage(axiosErrorWith(''), '兜底')).toBe('兜底')
    expect(apiErrorMessage(axiosErrorWith('   '), '兜底')).toBe('兜底')
    expect(apiErrorMessage(axiosErrorWith({}), '兜底')).toBe('兜底')
    expect(apiErrorMessage(axiosErrorWith({ message: '' }), '兜底')).toBe('兜底')
  })

  it('非结构化的响应体不会导致抛错', () => {
    expect(apiErrorMessage(axiosErrorWith(null), '兜底')).toBe('兜底')
    expect(apiErrorMessage(axiosErrorWith(42), '兜底')).toBe('兜底')
    expect(apiErrorMessage(axiosErrorWith({ message: 123 }), '兜底')).toBe('兜底')
  })

  it('超时与离线有固定中文提示', () => {
    const timeout = new AxiosError('timeout', 'ECONNABORTED')
    expect(apiErrorMessage(timeout, '兜底')).toBe('请求超时，请稍后重试')

    const offline = new AxiosError('Network Error')
    expect(apiErrorMessage(offline, '兜底')).toBe('无法连接服务端，请检查服务是否运行')
  })

  it('无文案且无 fallback 时用 HTTP 状态句', () => {
    expect(apiErrorMessage(axiosErrorWith({}, 502), '')).toBe('请求失败（HTTP 502）')
  })

  it('普通 Error 用它自己的 message', () => {
    expect(apiErrorMessage(new Error('网络超时'), '兜底')).toBe('网络超时')
  })

  it('完全未知的抛出值回落到兜底文案', () => {
    expect(apiErrorMessage('字符串异常', '兜底')).toBe('兜底')
    expect(apiErrorMessage(undefined, '兜底')).toBe('兜底')
  })

  it('apiErrorMessageDefault 在无 body 时给出请求失败', () => {
    expect(apiErrorMessageDefault(axiosErrorWith({}))).toBe('请求失败')
  })
})

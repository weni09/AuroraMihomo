import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useNotifyStore } from '../stores/notify'
import { copyTextSync, useCopy } from './useCopy'

/**
 * happy-dom 不实现 execCommand / ClipboardEvent.clipboardData，
 * 测试里按需挂上，避免 vi.spyOn 直接抛 "property is not defined"。
 */
function installExecCommand(impl: (commandId: string) => boolean) {
  Object.defineProperty(document, 'execCommand', {
    configurable: true,
    writable: true,
    value: vi.fn(impl),
  })
  return document.execCommand as unknown as ReturnType<typeof vi.fn>
}

describe('copyTextSync', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('优先走 copy 事件 setData，不依赖临时 textarea 焦点', () => {
    const setData = vi.fn()
    installExecCommand(() => {
      const ce = new Event('copy', { bubbles: true, cancelable: true }) as ClipboardEvent
      Object.defineProperty(ce, 'clipboardData', {
        value: { setData },
      })
      document.dispatchEvent(ce)
      return true
    })

    expect(copyTextSync('http://192.168.1.129:8899/api/v1/share/tok')).toBe(true)
    expect(setData).toHaveBeenCalledWith('text/plain', 'http://192.168.1.129:8899/api/v1/share/tok')
    expect(document.body.querySelector('textarea')).toBeNull()
  })

  it('copy 事件未写入时回退 textarea 选区', () => {
    // execCommand 不派发事件 → copyViaCopyEvent 失败 → textarea 路径
    installExecCommand((cmd) => {
      // 第一次是 copy 事件路径；第二次是 textarea 路径
      return cmd === 'copy'
    })
    // 第一次调用：监听器挂着但我们的 install 不 dispatch，written=false
    // 需要更精细：第一次 false，第二次 true
    let n = 0
    installExecCommand(() => {
      n += 1
      if (n === 1) return false // 事件路径：未 written 且 suggested false
      return true // textarea 路径
    })

    expect(copyTextSync('fallback-text')).toBe(true)
    expect(n).toBe(2)
  })

  it('两条同步路径都失败时返回 false', () => {
    installExecCommand(() => false)
    expect(copyTextSync('x')).toBe(false)
  })

  it('copy 事件无 clipboardData 时不算成功，继续走后续路径', () => {
    let n = 0
    installExecCommand(() => {
      n += 1
      if (n === 1) {
        // 派发无 clipboardData 的 copy：旧逻辑会误标 written=true
        document.dispatchEvent(new Event('copy', { bubbles: true, cancelable: true }))
        return true
      }
      return true // textarea 回退
    })
    expect(copyTextSync('need-fallback')).toBe(true)
    expect(n).toBe(2)
  })
})

describe('useCopy', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('空文本不写剪贴板，报错误 toast', async () => {
    const exec = installExecCommand(() => true)
    const { copy } = useCopy()
    const notify = useNotifyStore()

    await copy('', '订阅链接')

    expect(exec).not.toHaveBeenCalled()
    expect(notify.toasts.some((t) => t.level === 'error' && t.text.includes('为空'))).toBe(true)
    expect(notify.toasts.some((t) => t.level === 'success')).toBe(false)
  })

  it('同步写入成功时 toast 成功，且不调用 Clipboard API', async () => {
    const setData = vi.fn()
    installExecCommand(() => {
      const ce = new Event('copy', { bubbles: true, cancelable: true }) as ClipboardEvent
      Object.defineProperty(ce, 'clipboardData', { value: { setData } })
      document.dispatchEvent(ce)
      return true
    })
    const writeText = vi.fn()
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    const { copy } = useCopy()
    const notify = useNotifyStore()
    await copy('http://192.168.1.129:8899/api/v1/share/abc', '订阅链接')

    expect(writeText).not.toHaveBeenCalled()
    expect(setData).toHaveBeenCalled()
    expect(notify.toasts.some((t) => t.level === 'success' && t.text === '订阅链接已复制')).toBe(true)
  })

  it('同步失败后回退 Clipboard API 成功', async () => {
    installExecCommand(() => false)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    const { copy } = useCopy()
    const notify = useNotifyStore()
    await copy('hello', '链接')

    expect(writeText).toHaveBeenCalledWith('hello')
    expect(notify.toasts.some((t) => t.level === 'success' && t.text === '链接已复制')).toBe(true)
  })

  it('全部失败时 toast 错误，绝不报已复制', async () => {
    installExecCommand(() => false)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: vi.fn().mockRejectedValue(new Error('NotAllowedError')),
      },
    })

    const { copy } = useCopy()
    const notify = useNotifyStore()
    await copy('hello', '订阅链接')

    expect(notify.toasts.some((t) => t.level === 'success')).toBe(false)
    expect(
      notify.toasts.some((t) => t.level === 'error' && t.text.includes('复制失败')),
    ).toBe(true)
  })
})

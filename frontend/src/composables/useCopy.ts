import { useNotifyStore } from '../stores/notify'

/**
 * 复制文本到剪贴板，结果以 toast 告知。
 *
 * 顺序约束（本项目常以 http://内网IP:端口 打开，且复制入口常在
 * DropdownMenu / Dialog 里）：
 *
 * 1. 必须在用户手势的同步调用栈里完成写入。先 await Clipboard API
 *    再 fallback 时，手势已过期，菜单/焦点陷阱也会让后续 execCommand 落空，
 *    却曾误报「已复制」。
 * 2. 优先走 copy 事件 + clipboardData.setData：不创建、不聚焦临时
 *    textarea。reka 的 Dialog/Menu 带 FocusScope，对 body 上的隐藏
 *    输入框一 focus 就会把焦点抢回去，textarea 方案在分享设置弹窗里
 *    会静默写成空剪贴板。
 * 3. Clipboard API 仅作次选（HTTPS / localhost）；再不行才用 textarea。
 * 4. 任何路径都必须以真实写入结果决定 toast，禁止假成功。
 */
export function useCopy() {
  const notify = useNotifyStore()

  const copy = async (text: string, label = '链接') => {
    if (!text) {
      notify.error(`${label}为空，无法复制`)
      return
    }

    if (copyTextSync(text)) {
      notify.success(`${label}已复制`)
      return
    }

    try {
      if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
        notify.success(`${label}已复制`)
        return
      }
    } catch {
      // fall through
    }

    // 剪贴板被策略禁用（部分内网浏览器 / RDP / 权限）时，
    // prompt 里的文本仍可选中复制，避免「报了失败却拿不到链接」。
    try {
      window.prompt(`${label}（请手动全选复制）`, text)
    } catch {
      // 极旧环境可能没有 prompt
    }
    notify.error(`${label}复制失败，请从弹出框中手动复制`)
  }

  return { copy }
}

/** 同步写入；供单测与 copy() 共用。 */
export function copyTextSync(text: string): boolean {
  if (copyViaCopyEvent(text)) return true
  return copyViaTextarea(text)
}

/** @deprecated 兼容旧导出名；请用 copyTextSync */
export function copyViaExecCommand(text: string): boolean {
  return copyTextSync(text)
}

/**
 * 在 document 级监听 copy，用 clipboardData 写入。
 * 不依赖当前选区，也不移动焦点——Dialog/Menu 内最稳。
 */
function copyViaCopyEvent(text: string): boolean {
  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') {
    return false
  }

  let written = false
  const onCopy = (e: Event) => {
    const ce = e as ClipboardEvent
    try {
      // clipboardData 缺失时绝不能标成功：否则 preventDefault 后剪贴板仍空，
      // 又会回到「toast 成功、粘贴空白」。
      if (!ce.clipboardData) return
      ce.clipboardData.setData('text/plain', text)
      // 阻止浏览器再把「当前选区」写进剪贴板，否则会覆盖我们刚 set 的内容
      ce.preventDefault()
      written = true
    } catch {
      written = false
    }
  }

  try {
    document.addEventListener('copy', onCopy)
    // 无选区时也会派发 copy；靠监听器 setData
    document.execCommand('copy')
    document.removeEventListener('copy', onCopy)
    // 以监听器是否真的 setData 为准：部分环境 execCommand 返回 true 却未派发事件
    return written
  } catch {
    document.removeEventListener('copy', onCopy)
    return false
  }
}

/** 最后手段：临时 textarea + 选区。可能与 FocusScope 冲突，故放最后。 */
function copyViaTextarea(text: string): boolean {
  if (typeof document === 'undefined') return false
  try {
    const el = document.createElement('textarea')
    el.value = text
    el.setAttribute('readonly', '')
    el.setAttribute('aria-hidden', 'true')
    // 不调用 focus()：避免触发 reka FocusScope 把焦点抢回对话框
    el.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;padding:0;border:0;opacity:0;pointer-events:none;'
    document.body.appendChild(el)
    el.select()
    el.setSelectionRange(0, text.length)
    const ok = typeof document.execCommand === 'function' && document.execCommand('copy')
    document.body.removeChild(el)
    return !!ok
  } catch {
    return false
  }
}

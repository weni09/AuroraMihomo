import { useNotifyStore } from '../stores/notify'

/**
 * 复制文本到剪贴板，结果以 toast 告知。
 *
 * 订阅 / 组合 / 文件三个页面此前各写了一份相同的复制逻辑（含
 * 非 HTTPS 环境的退化路径）与各自的「✓ 已复制」按钮内变字状态。
 * 收敛到一处，避免此后再各自演化；反馈统一走 toast，
 * 与系统其余操作提示一致。
 */
export function useCopy() {
  const notify = useNotifyStore()

  const copy = async (text: string, label = '链接') => {
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // 非 HTTPS（或不支持 Clipboard API）时退回 execCommand。
      // 该 API 已废弃但仍是局域网 http 访问下唯一可用的方案，
      // 而本项目常以 http://内网IP:端口 部署。
      try {
        const el = document.createElement('textarea')
        el.value = text
        // 移出视口而非 display:none —— 隐藏元素无法被选中
        el.style.position = 'fixed'
        el.style.left = '-9999px'
        document.body.appendChild(el)
        el.select()
        document.execCommand('copy')
        document.body.removeChild(el)
      } catch {
        notify.error(`${label}复制失败，请手动选中复制`)
        return
      }
    }
    notify.success(`${label}已复制`)
  }

  return { copy }
}

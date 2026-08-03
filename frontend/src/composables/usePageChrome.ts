import { ref } from 'vue'

/**
 * 页面向 App 移动端顶栏注入的次要信息与动作。
 *
 * 用模块级 ref 而非 pinia：与 useSidebar / useTheme 一样属于界面壳层，
 * 不是业务领域状态。页面 onMounted 写入、onBeforeUnmount 清空，
 * 避免离开路由后顶栏仍显示上一页的副标题或按钮。
 *
 * 消费者：Zashboard / AdGuard（副标题、刷新、新标签页等）。
 */
export interface PageChromeAction {
  label: string
  disabled?: boolean
  onClick: () => void
}

const subtitle = ref('')
/** 兼容旧单按钮；优先用 actions */
const action = ref<PageChromeAction | null>(null)
const actions = ref<PageChromeAction[]>([])

export function usePageChrome() {
  return { subtitle, action, actions }
}

export function setPageChrome(opts: {
  subtitle?: string
  action?: PageChromeAction | null
  actions?: PageChromeAction[]
}) {
  if (opts.subtitle !== undefined) subtitle.value = opts.subtitle
  if (opts.actions !== undefined) {
    actions.value = opts.actions
    // 同步首项到 action，兼容只读 action 的旧代码/测试
    action.value = opts.actions[0] ?? null
  } else if (opts.action !== undefined) {
    action.value = opts.action
    actions.value = opts.action ? [opts.action] : []
  }
}

export function clearPageChrome() {
  subtitle.value = ''
  action.value = null
  actions.value = []
}

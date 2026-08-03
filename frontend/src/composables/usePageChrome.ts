import { ref } from 'vue'

/**
 * 页面向 App 移动端顶栏注入的次要信息与动作。
 *
 * 用模块级 ref 而非 pinia：与 useSidebar / useTheme 一样属于界面壳层，
 * 不是业务领域状态。页面 onMounted 写入、onBeforeUnmount 清空，
 * 避免离开路由后顶栏仍显示上一页的副标题或按钮。
 *
 * 消费者：Zashboard / AdGuard（副标题、状态徽章、刷新/设置/工具等）。
 */
export type PageChromeIcon = 'settings' | 'tools' | 'tools-open' | 'refresh' | 'external'

export interface PageChromeAction {
  label: string
  /** 仅图标时用 aria-label；有 label 时也作补充 */
  ariaLabel?: string
  /** 有 icon 时 App 顶栏渲染图标按钮，更省宽 */
  icon?: PageChromeIcon
  disabled?: boolean
  onClick: () => void
}

/** 紧凑状态点（如 AdGuard「运行中」），避免再占一整条页面工具条 */
export interface PageChromeBadge {
  label: string
  /** 与 Badge 组件 variant 对齐的语义，App 用 class 着色 */
  tone?: 'ok' | 'warn' | 'info' | 'neutral'
}

const subtitle = ref('')
const badge = ref<PageChromeBadge | null>(null)
/** 兼容旧单按钮；优先用 actions */
const action = ref<PageChromeAction | null>(null)
const actions = ref<PageChromeAction[]>([])

export function usePageChrome() {
  return { subtitle, badge, action, actions }
}

export function setPageChrome(opts: {
  subtitle?: string
  badge?: PageChromeBadge | null
  action?: PageChromeAction | null
  actions?: PageChromeAction[]
}) {
  if (opts.subtitle !== undefined) subtitle.value = opts.subtitle
  if (opts.badge !== undefined) badge.value = opts.badge
  if (opts.actions !== undefined) {
    actions.value = opts.actions
    action.value = opts.actions[0] ?? null
  } else if (opts.action !== undefined) {
    action.value = opts.action
    actions.value = opts.action ? [opts.action] : []
  }
}

export function clearPageChrome() {
  subtitle.value = ''
  badge.value = null
  action.value = null
  actions.value = []
}

import { ref } from 'vue'

/**
 * 页面向 App 移动端顶栏注入的次要信息与动作。
 *
 * 用模块级 ref 而非 pinia：与 useSidebar / useTheme 一样属于界面壳层，
 * 不是业务领域状态。页面 onMounted 写入、onBeforeUnmount 清空，
 * 避免离开路由后顶栏仍显示上一页的副标题或按钮。
 *
 * 目前消费者是 Zashboard（副标题 = 内核 host:port，「新标签页」外开），
 * 其它页不写则顶栏保持「仅标题 + 主题」的默认形态。
 */
export interface PageChromeAction {
  label: string
  disabled?: boolean
  onClick: () => void
}

const subtitle = ref('')
const action = ref<PageChromeAction | null>(null)

export function usePageChrome() {
  return { subtitle, action }
}

export function setPageChrome(opts: {
  subtitle?: string
  action?: PageChromeAction | null
}) {
  if (opts.subtitle !== undefined) subtitle.value = opts.subtitle
  if (opts.action !== undefined) action.value = opts.action
}

export function clearPageChrome() {
  subtitle.value = ''
  action.value = null
}

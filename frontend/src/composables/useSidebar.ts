import { ref, watch } from 'vue'

const STORAGE_KEY = 'aurora_sidebar_collapsed'

/**
 * 侧边栏折叠状态。
 *
 * 用模块级 ref 而非 pinia store，与 useTheme 一致：这是界面偏好而非业务
 * 状态，不参与状态树，也不需要 devtools 里的时间旅行。
 *
 * 只影响 lg 以上的桌面布局。窄屏侧边栏是抽屉（整体滑入滑出），
 * 折叠成图标条在那里没有意义——抽屉本来就是覆盖式的，
 * 收起后一点宽度都不占。
 */
const collapsed = ref(readStored())

function readStored(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    // 隐私模式下 localStorage 不可用，默认展开
    return false
  }
}

watch(collapsed, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    // 存不进去也不影响本次会话的使用，静默即可
  }
})

export function useSidebar() {
  const toggle = () => {
    collapsed.value = !collapsed.value
  }
  const expand = () => {
    collapsed.value = false
  }
  return { collapsed, toggle, expand }
}

import { computed, ref, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'aurora_theme'

/**
 * 主题状态。
 *
 * 用模块级 ref 而非 pinia store：主题不参与业务状态树，且需要在
 * createPinia() 之前就能读取（index.html 的内联脚本已先落定 class，
 * 这里只是接管后续切换），放 pinia 反而多一层无谓的初始化顺序约束。
 *
 * 首屏的 class 由 index.html 的内联同步脚本设置，避免深色偏好下闪白。
 * 两处共用同一个 localStorage 键，改键名时需同步修改。
 */
const mode = ref<ThemeMode>(readStoredMode())

// 系统偏好。system 模式下由它决定最终明暗
const systemDark = ref(false)
let media: MediaQueryList | null = null

function readStoredMode(): ThemeMode {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw
  } catch {
    // 隐私模式下 localStorage 不可用，回落到跟随系统
  }
  return 'system'
}

if (typeof window !== 'undefined' && window.matchMedia) {
  media = window.matchMedia('(prefers-color-scheme: dark)')
  systemDark.value = media.matches
  // 始终监听：system 模式下需要实时跟随，非 system 模式下 isDark 不读它，
  // 留着监听比在模式间反复增删监听更简单，成本也可忽略
  media.addEventListener('change', (e) => {
    systemDark.value = e.matches
  })
}

const isDark = computed(() => (mode.value === 'system' ? systemDark.value : mode.value === 'dark'))

// 立即生效并持续同步。flush: 'sync' 让切换在同一帧完成，
// 避免主题切换时先渲染一帧旧配色
watch(
  isDark,
  (dark) => {
    document.documentElement.classList.toggle('dark', dark)
    // 移动端浏览器的地址栏底色，不同步会与页面顶部割裂
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', dark ? '#0f172a' : '#f8fafc')
  },
  { immediate: true, flush: 'sync' },
)

watch(mode, (m) => {
  try {
    localStorage.setItem(STORAGE_KEY, m)
  } catch {
    // 隐私模式下写入失败，本次会话内仍生效，只是不持久
  }
})

export function useTheme() {
  const setMode = (m: ThemeMode) => {
    mode.value = m
  }

  // 浅色 → 深色 → 跟随系统 → 浅色，供快捷键或单按钮场景使用。
  // 用数组轮转而非 Record 查表：tsconfig 开了 noUncheckedIndexedAccess，
  // 索引结果会带上 undefined，而这里的取值一定落在环内。
  const cycle = () => {
    const order: ThemeMode[] = ['light', 'dark', 'system']
    const i = order.indexOf(mode.value)
    // indexOf 找不到时回落到首项，不会得到 undefined
    mode.value = order[(i + 1) % order.length] ?? 'light'
  }

  return { mode, isDark, setMode, cycle }
}

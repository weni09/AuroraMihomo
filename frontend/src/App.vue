<script setup lang="ts">
import ToastHost from './components/ToastHost.vue'
import AppLogo from './components/AppLogo.vue'
import ThemeToggle from './components/ThemeToggle.vue'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useMihomoStore } from './stores/mihomo'
import { useAdGuardStore } from './stores/adguard'
import {
  Menu,
  X,
  LayoutDashboard,
  Cpu,
  Layers3,
  Settings2,
  GitCompare,
  ScrollText,
  Settings,
  Gauge,
  Shield,
  BookOpen,
  LogOut,
  ChevronsLeft,
  ChevronsRight,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  ExternalLink,
} from 'lucide-vue-next'
import { RouterView, RouterLink, useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useSidebar } from './composables/useSidebar'
import { usePageChrome } from './composables/usePageChrome'
import api from './api'

const route = useRoute()
const router = useRouter()
const mihomoStore = useMihomoStore()
const adguardStore = useAdGuardStore()
const isLogin = computed(() => route.name === 'login')
// 内嵌 iframe 必须吃满「顶栏以下」的剩余高度；若子页再用 h-dvh 会叠在 App 顶栏之下
// 把整屏顶出可视区，面板底栏被浏览器工具条挡住。
const isZashboard = computed(() => route.name === 'zashboard')
// AdGuard 同样内嵌 iframe，需与 Zashboard 一样锁一屏并隐藏顶栏主题三钮
const isAdGuard = computed(() => route.name === 'adguard')
const isEmbedFrame = computed(() => isZashboard.value || isAdGuard.value)
// 页面可选地往移动端顶栏挂副标题/动作（如 Zashboard 的对接信息与「新标签页」）
const {
  subtitle: pageSubtitle,
  badge: pageBadge,
  action: pageAction,
  actions: pageActions,
} = usePageChrome()

const pageBadgeClass = computed(() => {
  switch (pageBadge.value?.tone) {
    case 'ok':
      return 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
    case 'warn':
      return 'bg-amber-500/15 text-amber-800 dark:text-amber-300'
    case 'info':
      return 'bg-sky-500/15 text-sky-800 dark:text-sky-300'
    default:
      return 'bg-elevated text-fg-muted'
  }
})

function pageActionIcon(name: string | undefined) {
  switch (name) {
    case 'settings':
      return Settings2
    case 'tools':
      return ChevronDown
    case 'tools-open':
      return ChevronUp
    case 'refresh':
      return RefreshCw
    case 'external':
      return ExternalLink
    default:
      return null
  }
}

onMounted(() => {
  // /system/status、/adguard/status 需 JWT；登录页无 token 时请求只会 401 刷控制台。
  // 等离开登录页（或已带 token 进主界面）再拉状态。
  if (!isLogin.value && localStorage.getItem('aurora_token')) {
    void mihomoStore.fetchStatus()
    void adguardStore.fetchStatus()
  }
})

// 登录成功进入主界面时补一次状态（onMounted 时还在 /login 会跳过）
watch(isLogin, (login, wasLogin) => {
  if (wasLogin && !login && localStorage.getItem('aurora_token')) {
    void mihomoStore.fetchStatus()
    void adguardStore.fetchStatus()
  }
})

// 登录成功后路由离开 login 页时再补拉 AdGuard（与 isLogin watch 互补，避免时序漏拉）
watch(
  () => route.name,
  (name, prev) => {
    if (prev === 'login' && name !== 'login' && localStorage.getItem('aurora_token')) {
      void adguardStore.fetchStatus()
    }
  },
)

// 侧边栏折叠。只作用于 lg 以上的桌面布局：窄屏侧边栏是覆盖式抽屉，
// 收起后一点宽度都不占，再做一个图标条没有意义。
const { collapsed, toggle: toggleCollapsed } = useSidebar()

// 导航项集中成数组：原先九段 RouterLink 手写重复，active-class 出现了
// 三种不一致的写法（部分项漏了 text-white font-medium），逐项维护易漂移。
// prefix 用于 Sub-Store 这类含子路由的项，需要按前缀而非精确路径判断高亮。
// AdGuard 仅在组件开启时出现（默认关）。
const allNavItems = [
  { to: '/', label: '控制台', icon: LayoutDashboard },
  { to: '/mihomo', label: '内核管理', icon: Cpu },
  { to: '/substore', label: 'Sub-Store 管理', prefix: '/substore', icon: Layers3 },
  { to: '/config', label: '配置中心', icon: Settings2 },
  { to: '/diff', label: '配置差异', icon: GitCompare },
  { to: '/logs', label: '运行日志', icon: ScrollText },
  { to: '/settings', label: '系统设置', icon: Settings },
  { to: '/zashboard', label: 'Zashboard', icon: Gauge },
  { to: '/adguard', label: 'AdGuard', icon: Shield, requireAdGuard: true },
  { to: '/docs', label: '使用文档', icon: BookOpen },
] as const

const navItems = computed(() =>
  allNavItems.filter((item) => !('requireAdGuard' in item && item.requireAdGuard) || adguardStore.status.componentEnabled),
)

const isActive = (item: { to: string; prefix?: string }) =>
  item.prefix ? route.path.startsWith(item.prefix) : route.path === item.to

// 移动端顶栏标题取自路由 meta，含子路由时回落到父级
const pageTitle = computed(() => {
  const matched = [...route.matched].reverse().find((r) => r.meta?.title)
  return (matched?.meta.title as string) || 'AuroraMihomo'
})

// ===== 移动端抽屉 =====
const drawerOpen = ref(false)
// menuButton 现在指向 shadcn Button 组件实例而非原生 <button>，
// 组件实例本身不暴露 .focus()，需经 .$el 拿到底层 DOM 元素再聚焦
const menuButton = ref<{ $el: HTMLElement } | null>(null)
const drawer = ref<HTMLElement | null>(null)

const closeDrawer = () => {
  drawerOpen.value = false
}

// 选中导航项后自动收起，否则用户要多点一次遮罩才能看到目标页面
watch(() => route.fullPath, closeDrawer)

const onKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Escape') closeDrawer()
}

watch(drawerOpen, (open) => {
  // 锁背景滚动：iOS 上不锁的话手指滑动会带动下层页面，抽屉自身反而滚不动
  document.body.classList.toggle('overflow-hidden', open)
  if (open) {
    // 焦点移入抽屉，键盘与读屏用户才能直接遍历导航项
    requestAnimationFrame(() => drawer.value?.focus())
    // 只在打开期间挂监听，避免常驻一个只在窄屏有用的按键处理
    window.addEventListener('keydown', onKeydown)
  } else {
    menuButton.value?.$el.focus()
    window.removeEventListener('keydown', onKeydown)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  document.body.classList.remove('overflow-hidden')
})

const logout = async () => {
  if (!confirm('确定退出登录吗？')) return
  // aurora_session 是 HttpOnly，只能由服务端清 cookie；失败也继续清本地 token
  try {
    await api.post('/auth/logout')
  } catch {
    // ignore
  }
  localStorage.removeItem('aurora_token')
  router.push({ name: 'login' })
}
</script>
<template>
  <ToastHost />
  <!-- 页面本身用浏览器原生滚动条，不再整体锁死一屏——此前 h-dvh + overflow-hidden
       把所有内容塞进一屏，每个页面被迫自造一个内部滚动区，用户会觉得
       「滚动条不是浏览器原生的」。现在只有侧边栏用 sticky 固定在视口内，
       内容随页面一起用原生滚动条滚动。 -->
  <div class="min-h-dvh bg-canvas text-fg flex">
    <!-- 遮罩：仅窄屏抽屉展开时出现，点击关闭 -->
    <div
      v-if="drawerOpen && !isLogin"
      class="fixed inset-0 z-30 bg-black/60 lg:hidden"
      aria-hidden="true"
      @click="closeDrawer"
    ></div>

    <!-- 侧边栏。lg 以上用 sticky 固定在视口内、随页面滚动条保持贴顶
         （而非 fixed 那样脱离布局流，也不是 static 那样被内容推走）；
         lg 以下脱离文档流成为抽屉，靠 translate 滑入滑出。
         sticky 生效的前提是父级链上没有 overflow:hidden/auto，
         因此外层容器与内容列都不能再裁切溢出。 -->
    <aside
      v-if="!isLogin"
      id="app-sidebar"
      ref="drawer"
      tabindex="-1"
      aria-label="主导航"
      class="safe-py safe-pl fixed inset-y-0 left-0 z-40 w-64 shrink-0 bg-surface text-fg border-r border-line flex flex-col shadow-xl transition-transform duration-200 focus:outline-none lg:sticky lg:top-0 lg:inset-y-auto lg:h-dvh lg:z-auto lg:translate-x-0 lg:transition-[width]"
      :class="[
        drawerOpen ? 'translate-x-0' : '-translate-x-full',
        // 折叠只在 lg 以上生效：窄屏抽屉始终是完整宽度，
        // 否则用户在手机上会看到一条只有图标的窄条，反而更难用
        collapsed ? 'lg:w-16' : 'lg:w-64',
      ]"
    >
      <!-- 折叠态下侧边栏 64px，减去 p-2 的左右内边距剩 48px：横排放不下
           32px 的 Logo 加一个 44px 触控热区的按钮（触控下限见 .tap-target），
           因此改为竖排——Logo 在上、折叠开关在下，每行最宽 44px 不溢出。 -->
      <div
        class="border-b border-line shrink-0 flex items-center gap-3"
        :class="collapsed ? 'p-2 lg:flex-col lg:gap-2' : 'p-6'"
      >
        <!-- 固定 32 盒：避免 flex 行把 SVG 压扁；折叠竖排时水平居中 -->
        <div
          class="flex size-8 shrink-0 items-center justify-center"
          :class="collapsed ? 'lg:mx-auto' : ''"
        >
          <AppLogo :size="32" />
        </div>
        <!-- 折叠时隐藏标题。用 v-if 而非视觉隐藏：读屏用户此时也不需要它，
             aside 的 aria-label 已说明这是主导航 -->
        <h1
          v-if="!collapsed"
          class="min-w-0 truncate text-xl font-bold tracking-wider text-primary"
        >
          AuroraMihomo
        </h1>

        <!-- 折叠开关。纯图标：双箭头的指向本身就表达了动作方向
             （« 收起 / » 展开），配文字反而与图标语义重复。
             lg 以下隐藏——窄屏侧边栏是覆盖式抽屉，收起后本就不占宽度。
             展开态用 ml-auto 推到标题右侧；折叠态是竖排，不需要它。 -->
        <TooltipProvider :delay-duration="0">
          <Tooltip>
            <TooltipTrigger as-child>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                class="tap-target hidden text-fg-muted hover:bg-elevated hover:text-fg lg:inline-flex"
                :class="collapsed ? '' : 'ml-auto'"
                :aria-label="collapsed ? '展开侧边栏' : '折叠侧边栏'"
                :aria-expanded="!collapsed"
                aria-controls="app-sidebar"
                @click="toggleCollapsed"
              >
                <component
                  :is="collapsed ? ChevronsRight : ChevronsLeft"
                  class="!h-5 !w-5"
                  aria-hidden="true"
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">
              {{ collapsed ? '展开侧边栏' : '折叠侧边栏' }}
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- 抽屉内的关闭按钮：窄屏下遮罩之外还需要一个明确的关闭入口 -->
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="tap-target ml-auto text-fg-muted hover:text-fg lg:hidden"
          aria-label="关闭导航菜单"
          @click="closeDrawer"
        >
          <!-- shadcn Button 基础样式含 [&_svg]:size-4，会覆盖图标自身的尺寸类
               （选择器特异性比 h-5 w-5 更高），须加 ! 前缀提升优先级才能生效 -->
          <X class="!h-5 !w-5" aria-hidden="true" />
        </Button>
      </div>
      <!-- 导航项较多时侧边栏自身可滚动，避免挤压底部的退出按钮 -->
      <nav
        class="flex-1 min-h-0 overflow-y-auto space-y-2 text-sm"
        :class="collapsed ? 'p-2' : 'p-4'"
      >
        <!-- 折叠态下文字被隐藏，靠 Tooltip 补回可读名称。
             delay-duration=0：这是识别图标的唯一途径，默认 700ms 的延迟
             会让人误以为没有提示。展开态不套 Tooltip，文字已经在那儿了。 -->
        <TooltipProvider :delay-duration="0">
          <template v-for="item in navItems" :key="item.to">
            <Tooltip v-if="collapsed">
              <TooltipTrigger as-child>
                <RouterLink
                  :to="item.to"
                  class="flex items-center rounded transition-colors justify-center px-0 py-2.5"
                  :class="
                    isActive(item)
                      ? 'bg-primary text-primary-foreground font-medium'
                      : 'text-fg-muted hover:bg-elevated hover:text-fg'
                  "
                  :aria-label="item.label"
                >
                  <component :is="item.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
                </RouterLink>
              </TooltipTrigger>
              <TooltipContent side="right">{{ item.label }}</TooltipContent>
            </Tooltip>
            <RouterLink
              v-else
              :to="item.to"
              class="flex items-center gap-3 px-4 py-2 rounded transition-colors"
              :class="
                isActive(item)
                  ? 'bg-primary text-primary-foreground font-medium'
                  : 'text-fg-muted hover:bg-elevated hover:text-fg'
              "
            >
              <component :is="item.icon" class="h-4 w-4 shrink-0" aria-hidden="true" />
              {{ item.label }}
            </RouterLink>
          </template>
        </TooltipProvider>
      </nav>
      <div
        class="border-t border-line shrink-0 space-y-2"
        :class="collapsed ? 'p-2' : 'p-4 space-y-3'"
      >
        <TooltipProvider :delay-duration="0">
          <Tooltip v-if="collapsed">
            <TooltipTrigger as-child>
              <div
                class="flex items-center justify-center py-1 text-[10px] font-mono text-fg-muted cursor-default"
                aria-label="系统版本"
              >
                <span class="px-1.5 py-0.5 rounded bg-elevated border border-line truncate max-w-[48px]">
                  {{ mihomoStore.appVersion }}
                </span>
              </div>
            </TooltipTrigger>
            <TooltipContent side="right">系统版本: {{ mihomoStore.appVersion }}</TooltipContent>
          </Tooltip>
          <div
            v-else
            class="flex items-center justify-between px-3 py-1.5 rounded-lg bg-elevated/40 border border-line/60 text-xs"
          >
            <span class="text-[11px] text-fg-muted font-medium">系统版本</span>
            <span class="px-1.5 py-0.5 rounded bg-surface border border-line text-[10px] font-mono font-semibold text-fg-muted">
              {{ mihomoStore.appVersion }}
            </span>
          </div>
        </TooltipProvider>

        <!-- 折叠态下 ThemeToggle 的分段控件放不进 64px，
             收起为一个图标按钮触发的折叠菜单代价过大，
             这里直接隐藏——展开侧边栏或用窄屏顶栏都能切换主题 -->
        <ThemeToggle v-if="!collapsed" />

        <TooltipProvider :delay-duration="0">
          <Tooltip v-if="collapsed">
            <TooltipTrigger as-child>
              <Button
                variant="ghost"
                size="icon"
                class="tap-target w-full text-fg-muted hover:bg-elevated hover:text-fg"
                aria-label="退出登录"
                @click="logout"
              >
                <LogOut class="!h-5 !w-5 shrink-0" aria-hidden="true" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">退出登录</TooltipContent>
          </Tooltip>
          <Button
            v-else
            variant="ghost"
            class="w-full justify-start gap-3 px-4 py-2 text-sm text-fg-muted hover:bg-elevated hover:text-fg"
            @click="logout"
          >
            <LogOut class="h-4 w-4 shrink-0" aria-hidden="true" />
            退出登录
          </Button>

        </TooltipProvider>
      </div>
    </aside>

    <!-- min-w-0 防止宽内容（如日志长行、表格）把 flex 容器撑破导致横向溢出。
         普通页不设 overflow-hidden：那会让侧边栏 sticky 失效。
         仅 Zashboard 锁一屏：子页 iframe 要 flex 分走「顶栏以下」剩余高度。 -->
    <div
      class="flex-1 min-w-0 flex flex-col"
      :class="isEmbedFrame ? 'h-dvh max-h-dvh overflow-hidden' : ''"
    >
      <!-- 移动端顶栏：承载汉堡按钮、当前页标题与主题切换。
           lg 以上隐藏，桌面布局保持原样。sticky 使其在页面滚动时常驻可见——
           取代此前靠整体锁一屏来「保证顶栏总是看得见」的做法。 -->
      <header
        v-if="!isLogin"
        class="safe-pt sticky top-0 z-20 shrink-0 flex items-center gap-2 sm:gap-3 border-b border-line bg-surface px-3 sm:px-4 py-2 lg:hidden"
      >
        <Button
          ref="menuButton"
          type="button"
          variant="ghost"
          size="icon-sm"
          class="tap-target text-fg-muted hover:bg-elevated hover:text-fg shrink-0"
          aria-label="打开导航菜单"
          aria-controls="app-sidebar"
          :aria-expanded="drawerOpen"
          @click="drawerOpen = true"
        >
          <Menu class="!h-5 !w-5" aria-hidden="true" />
        </Button>
        <!-- 标题 + 可选副标题（如 Zashboard 对接 host:port）叠在同一列，
             比再开一条页面级 header 更省垂直空间。 -->
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1.5 min-w-0">
            <div class="font-semibold truncate leading-tight">{{ pageTitle }}</div>
            <!-- 页内状态点（如 AdGuard 运行中）挂在标题旁，省掉整条页面工具条 -->
            <span
              v-if="pageBadge"
              class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium leading-none"
              :class="pageBadgeClass"
            >
              {{ pageBadge.label }}
            </span>
          </div>
          <div
            v-if="pageSubtitle"
            class="text-[11px] text-fg-subtle truncate leading-tight mt-0.5"
          >
            {{ pageSubtitle }}
          </div>
        </div>
        <div class="ml-auto shrink-0 flex items-center gap-1">
          <template v-if="pageActions.length">
            <Button
              v-for="(act, i) in pageActions"
              :key="i"
              type="button"
              :variant="act.icon ? 'ghost' : i === 0 ? 'secondary' : 'outline'"
              :size="act.icon ? 'icon-sm' : 'sm'"
              class="shrink-0"
              :class="act.icon ? 'h-8 w-8 tap-target' : 'h-8 px-2.5 text-xs'"
              :disabled="act.disabled"
              :aria-label="act.ariaLabel || act.label"
              @click="act.onClick"
            >
              <component
                :is="pageActionIcon(act.icon)"
                v-if="act.icon && pageActionIcon(act.icon)"
                class="!h-4 !w-4"
                aria-hidden="true"
              />
              <span v-else>{{ act.label }}</span>
            </Button>
          </template>
          <Button
            v-else-if="pageAction"
            type="button"
            variant="secondary"
            size="sm"
            class="h-8 px-2.5 text-xs"
            :disabled="pageAction.disabled"
            @click="pageAction.onClick"
          >
            {{ pageAction.label }}
          </Button>
          <!-- Zashboard/AdGuard 顶栏空间紧，主题三钮过宽；改主题可去其它页或侧栏。 -->
          <ThemeToggle v-if="!isEmbedFrame" />
        </div>
      </header>

      <div
        class="flex-1 min-h-0 flex flex-col"
        :class="isEmbedFrame ? 'overflow-hidden' : ''"
      >
        <RouterView />
      </div>
    </div>
  </div>
</template>

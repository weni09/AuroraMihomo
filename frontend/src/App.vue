<script setup lang="ts">
import ToastHost from './components/ToastHost.vue'
import AppLogo from './components/AppLogo.vue'
import ThemeToggle from './components/ThemeToggle.vue'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useMihomoStore } from './stores/mihomo'
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
  BookOpen,
  LogOut,
  ChevronsLeft,
  ChevronsRight,
} from 'lucide-vue-next'
import { RouterView, RouterLink, useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useSidebar } from './composables/useSidebar'

const route = useRoute()
const router = useRouter()
const mihomoStore = useMihomoStore()
const isLogin = computed(() => route.name === 'login')

onMounted(() => {
  mihomoStore.fetchStatus()
})

// 侧边栏折叠。只作用于 lg 以上的桌面布局：窄屏侧边栏是覆盖式抽屉，
// 收起后一点宽度都不占，再做一个图标条没有意义。
const { collapsed, toggle: toggleCollapsed } = useSidebar()

// 导航项集中成数组：原先九段 RouterLink 手写重复，active-class 出现了
// 三种不一致的写法（部分项漏了 text-white font-medium），逐项维护易漂移。
// prefix 用于 Sub-Store 这类含子路由的项，需要按前缀而非精确路径判断高亮。
const navItems = [
  { to: '/', label: '控制台', icon: LayoutDashboard },
  { to: '/mihomo', label: '内核管理', icon: Cpu },
  { to: '/substore', label: 'Sub-Store 管理', prefix: '/substore', icon: Layers3 },
  { to: '/config', label: '配置中心', icon: Settings2 },
  { to: '/diff', label: '配置差异', icon: GitCompare },
  { to: '/logs', label: '运行日志', icon: ScrollText },
  { to: '/settings', label: '系统设置', icon: Settings },
  { to: '/zashboard', label: 'Zashboard', icon: Gauge },
  { to: '/docs', label: '使用文档', icon: BookOpen },
]

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

const logout = () => {
  if (!confirm('确定退出登录吗？')) return
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
      class="fixed inset-0 z-30 bg-slate-900/60 lg:hidden"
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
        <AppLogo :size="32" />
        <!-- 折叠时隐藏标题。用 v-if 而非视觉隐藏：读屏用户此时也不需要它，
             aside 的 aria-label 已说明这是主导航 -->
        <h1 v-if="!collapsed" class="text-xl font-bold tracking-wider text-primary">
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
                class="text-xs text-fg-muted font-mono text-center py-1 truncate cursor-default"
                aria-label="系统版本"
              >
                {{ mihomoStore.appVersion }}
              </div>
            </TooltipTrigger>
            <TooltipContent side="right">版本号: {{ mihomoStore.appVersion }}</TooltipContent>
          </Tooltip>
          <div
            v-else
            class="text-xs text-fg-muted font-mono flex items-center justify-between px-1 py-1"
          >
            <span>版本号</span>
            <span>{{ mihomoStore.appVersion }}</span>
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
         不再设 overflow-hidden：那会让上面侧边栏的 sticky 失效。 -->
    <div class="flex-1 min-w-0 flex flex-col">
      <!-- 移动端顶栏：承载汉堡按钮、当前页标题与主题切换。
           lg 以上隐藏，桌面布局保持原样。sticky 使其在页面滚动时常驻可见——
           取代此前靠整体锁一屏来「保证顶栏总是看得见」的做法。 -->
      <header
        v-if="!isLogin"
        class="safe-pt sticky top-0 z-20 shrink-0 flex items-center gap-3 border-b border-line bg-surface px-4 py-2.5 lg:hidden"
      >
        <Button
          ref="menuButton"
          type="button"
          variant="ghost"
          size="icon-sm"
          class="tap-target text-fg-muted hover:bg-elevated hover:text-fg"
          aria-label="打开导航菜单"
          aria-controls="app-sidebar"
          :aria-expanded="drawerOpen"
          @click="drawerOpen = true"
        >
          <Menu class="!h-5 !w-5" aria-hidden="true" />
        </Button>
        <span class="font-semibold truncate">{{ pageTitle }}</span>
        <div class="ml-auto shrink-0">
          <ThemeToggle />
        </div>
      </header>

      <RouterView />
    </div>
  </div>
</template>

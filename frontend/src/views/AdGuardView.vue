<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useAdGuardStore, type WiringOptions } from '../stores/adguard'
import { clearPageChrome, setPageChrome } from '../composables/usePageChrome'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import ModalDialog from '../components/ModalDialog.vue'
import AdGuardSettingsDialog from '../components/AdGuardSettingsDialog.vue'
import { ExternalLink, Play, Power, RefreshCw, Download, Settings2 } from 'lucide-vue-next'
import type { PageChromeAction } from '../composables/usePageChrome'
import api from '../api'

const store = useAdGuardStore()

const wiringOpen = ref(false)
const settingsOpen = ref(false)
/** 递增以强制重载 iframe（刷新按钮 / 会话对齐后） */
const iframeKey = ref(0)
/**
 * 会话 cookie 已写入且探测 /adguard-ui 可通过后才为 true。
 * 仅 POST /auth/session 成功不够：部分手机浏览器 Set-Cookie 后立刻导航
 * iframe 仍带不上 cookie，表现为空白或 unauthorized，多刷几次才好。
 */
const sessionReady = ref(false)
/** 正在对齐会话 / 探测反代（与 store.isLoading 区分，避免整页误判为「未启用」） */
const sessionPreparing = ref(false)
/** 移动端工具条默认收起，把高度留给 iframe；需要启停/更新时再展开 */
const mobileToolbarOpen = ref(false)
const iframeRef = ref<HTMLIFrameElement | null>(null)
/** 避免 watch(running) 与 onMounted 并发准备会话 */
let prepareGen = 0

const wiringOpts = reactive<WiringOptions>({
  redirectTProxy: true,
  resolveConflict: true,
  patchUpstream: true,
  weakenTunHijack: false,
})

const entryPath = computed(() => store.status.entryPath || '/adguard-ui/')
const busy = computed(() => store.isLoading || store.actionLoading || sessionPreparing.value)
const componentEnabled = computed(() => store.status.componentEnabled)
const installed = computed(() => store.status.installed)
const running = computed(() => store.status.running)
const desiredRunning = computed(() => store.status.desiredRunning === true)
const showIframe = computed(() => running.value && sessionReady.value)
const sessionFailed = ref(false)
/** 首次进入且尚无可用 status 时的整页 loading（已有缓存 status 时不挡界面） */
const bootLoading = computed(
  () => store.isLoading && !installed.value && !running.value && !componentEnabled.value,
)

function openExternally() {
  window.open(entryPath.value, '_blank', 'noopener,noreferrer')
}

function openSettings() {
  settingsOpen.value = true
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

/** Android Edge / 部分 Chromium 移动版对「XHR 写 cookie → 立刻 iframe」特别敏感 */
function isMobileEdgeLike(): boolean {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
  // EdgA = Android Edge；EdgiOS = iOS Edge；同时兜底一般 Android 移动浏览器
  return /EdgA|EdgiOS|Android.*Edg|Mobile.*Edg/i.test(ua) || /Android/i.test(ua)
}

/**
 * 探测反代是否已接受当前 cookie、AGH 是否已可服务。
 * 手机上 XHR Set-Cookie 与随后 iframe 导航之间常有竞态；用同源 fetch 确认后再挂 iframe。
 *
 * Android Edge：优先用 mode:cors 的普通 GET（follow 重定向），读最终 status；
 * redirect:manual 在部分 Chromium 移动版上对同站 302 表现不稳定。
 */
async function probeAdGuardEntry(path: string): Promise<'ok' | 'unauthorized' | 'unavailable' | 'error'> {
  const probeOnce = async (redirect: RequestRedirect) => {
    const res = await fetch(path, {
      method: 'GET',
      credentials: 'include',
      redirect,
      cache: 'no-store',
      headers: { Accept: 'text/html,application/xhtml+xml;q=0.9,*/*;q=0.8' },
    })
    return res
  }

  try {
    // Android Edge 先 follow；桌面/其它仍可用 manual 区分 302
    const res = isMobileEdgeLike()
      ? await probeOnce('follow')
      : await probeOnce('manual')

    if (res.status === 401) return 'unauthorized'
    if (res.status === 503 || res.status === 502) return 'unavailable'
    if (res.status === 0 || res.type === 'opaqueredirect') return 'ok'
    if (res.status >= 200 && res.status < 400) return 'ok'
    if (res.status >= 500) return 'unavailable'
    return 'ok'
  } catch {
    // manual 失败时再试 follow（个别 WebView 不支持 manual）
    try {
      const res = await probeOnce('follow')
      if (res.status === 401) return 'unauthorized'
      if (res.status === 503 || res.status === 502) return 'unavailable'
      if (res.status >= 200 && res.status < 400) return 'ok'
      return 'error'
    } catch {
      return 'error'
    }
  }
}

/**
   * API 用 localStorage Bearer，/adguard-ui 反代只认 aurora_session cookie。
   *
   * 手机慢/空白的主因曾是：挂 iframe 前最多 10 次探测 + 递增 sleep（可卡数秒～十几秒），
   * 且 sessionReady 为 false 时整页只显示「正在准备…」像白屏。
   *
   * 现策略：
   * 1) POST /auth/session 写 cookie（登录时通常已有，再覆盖一次防过期）
   * 2) 立刻 sessionReady=true 挂 iframe（乐观），最多 2 次轻量探测修 401
   * 3) 探测失败不拆掉已显示的 iframe，只标 sessionFailed 给重试条
   */
  async function ensureSessionCookie(): Promise<boolean> {
    const gen = ++prepareGen
    sessionPreparing.value = true
    try {
      await api.post('/auth/session', null, { skipErrorToast: true })
      if (gen !== prepareGen) return false

      // 乐观：先允许挂 iframe，避免「准备中」空等
      sessionReady.value = true
      sessionFailed.value = false

      await sleep(isMobileEdgeLike() ? 50 : 0)
      if (gen !== prepareGen) return false

      let last: 'ok' | 'unauthorized' | 'unavailable' | 'error' = 'error'
      for (let i = 0; i < 2; i++) {
        if (gen !== prepareGen) return false
        if (i > 0) await sleep(100)
        last = await probeAdGuardEntry(entryPath.value)
        if (last === 'ok') {
          sessionFailed.value = false
          return true
        }
        if (last === 'unauthorized') {
          try {
            await api.post('/auth/session', null, { skipErrorToast: true })
          } catch {
            /* 继续 */
          }
        }
      }

      // 仍失败：保留 iframe（可能已能看），角标提示可重试
      sessionFailed.value = true
      console.warn('adguard entry probe soft-fail', last)
      return true
    } catch (e) {
      if (gen !== prepareGen) return false
      console.error('ensure aurora_session failed', e)
      // 即使 session POST 失败也尝试挂 iframe：登录时可能已有 cookie
      sessionReady.value = true
      sessionFailed.value = true
      return true
    } finally {
      if (gen === prepareGen) sessionPreparing.value = false
    }
  }

  async function refreshIframe() {
    await ensureSessionCookie()
    iframeKey.value += 1
  }

/**
 * 同源 iframe 可窥 body：若仍是 unauthorized / not running 文案，自动再对齐一次。
 * 避免用户只看到白屏小字、只能整页刷新。
 */
function onIframeLoad() {
  const el = iframeRef.value
  if (!el || !running.value) return
  try {
    const text = (el.contentDocument?.body?.innerText || '').trim().slice(0, 80).toLowerCase()
    if (!text) return
    if (
      text.includes('unauthorized') ||
      text.includes('adguard not running') ||
      text.includes('bad gateway')
    ) {
      void (async () => {
        await sleep(200)
        await refreshIframe()
      })()
    }
  } catch {
    // 极端情况下 document 不可读则忽略
  }
}

/** bfcache 恢复时补一次会话；普通 pageshow 不再整页重探，避免手机来回切卡死 */
  function onPageShow(ev: PageTransitionEvent) {
    if (!running.value) return
    if (ev.persisted) {
      void refreshIframe()
    }
  }

  function onVisibilityChange() {
    // 仅在明确失败且仍无 iframe 时才自动重试，避免每次回前台都卡「刷新」
    if (
      document.visibilityState === 'visible' &&
      running.value &&
      sessionFailed.value &&
      !sessionReady.value
    ) {
      void ensureSessionCookie()
    }
  }

function syncPageChrome() {
  // 副标题：对接文案 / 期望运行 / 错误；运行态用 badge 表达，避免再占一行页面工具条
  let subtitle = store.status.wiringLabel || ''
  if (!running.value && desiredRunning.value) {
    subtitle = '期望运行 · 面板将自启或启动中'
  }
  if (store.status.lastError && !running.value) {
    subtitle = store.status.lastError
  }

  const badge =
    store.status.installed
      ? {
          label: running.value ? '运行中' : '已停止',
          tone: (running.value ? 'ok' : 'warn') as 'ok' | 'warn',
        }
      : null

  // 已安装：设置 + 工具展开进 App 顶栏；运行中再加刷新
  const actions: PageChromeAction[] = []
  if (store.status.installed) {
    if (running.value) {
      actions.push({
        label: '刷新',
        ariaLabel: '刷新面板',
        icon: 'refresh',
        disabled: false,
        onClick: () => {
          void refreshIframe()
        },
      })
    }
    actions.push({
      label: '设置',
      ariaLabel: 'AdGuard 设置',
      icon: 'settings',
      disabled: busy.value,
      onClick: openSettings,
    })
    actions.push({
      label: mobileToolbarOpen.value ? '收起' : '工具',
      ariaLabel: mobileToolbarOpen.value ? '收起工具' : '展开工具',
      icon: mobileToolbarOpen.value ? 'tools-open' : 'tools',
      disabled: false,
      onClick: () => {
        mobileToolbarOpen.value = !mobileToolbarOpen.value
      },
    })
  }

  setPageChrome({
    subtitle,
    badge,
    actions,
  })
}

async function openWiring() {
  wiringOpen.value = true
  await store.wiringPreview()
}

async function applyWiring() {
  await store.wiringApply({ ...wiringOpts })
  if (store.status.wiring === 'on') wiringOpen.value = false
}

async function rollbackWiring() {
  if (!confirm('将解除 DNS 对接并恢复对接前的配置，确定继续？')) return
  await store.wiringRollback()
}

onMounted(async () => {
  window.addEventListener('pageshow', onPageShow)
  document.addEventListener('visibilitychange', onVisibilityChange)

  await store.fetchStatus()
  // 仅在运行中才对齐会话并探测；未运行时写 cookie 也无妨，但不要阻塞启停 UI
  if (running.value) {
    await ensureSessionCookie()
  }
  syncPageChrome()
})

// AGH 从停止→运行（本页点启动、或 desiredRunning 自启）后必须重新探测，
// 否则 sessionReady 可能仍是旧值，或 iframe 打到 503 白屏。
watch(running, async (isRun, wasRun) => {
  if (isRun && !wasRun) {
    await ensureSessionCookie()
    iframeKey.value += 1
    syncPageChrome()
  }
  if (!isRun) {
    prepareGen += 1
    sessionReady.value = false
    sessionPreparing.value = false
    sessionFailed.value = false
  }
})

watch(
  () => [
    store.status.wiringLabel,
    store.status.installed,
    store.status.running,
    store.status.entryPath,
    store.status.desiredRunning,
    store.status.lastError,
    store.isLoading,
    store.actionLoading,
    sessionPreparing.value,
    mobileToolbarOpen.value,
  ],
  syncPageChrome,
)

onBeforeUnmount(() => {
  prepareGen += 1
  window.removeEventListener('pageshow', onPageShow)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  clearPageChrome()
})
</script>
<template>
  <!-- 高度由 App 在 adguard 路由上锁 h-dvh 后 flex 分给本页，勿再 h-dvh 叠顶栏 -->
  <main class="flex-1 h-full min-h-0 flex flex-col">
    <!-- 已安装时显示桌面顶栏；未安装由全页安装引导承载主 CTA -->
    <div
      v-if="componentEnabled && installed"
      data-testid="adguard-page-header"
      class="hidden lg:flex flex-wrap items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-surface shrink-0"
    >
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h1 class="text-xl font-bold text-fg">AdGuard Home</h1>
          <Badge variant="ok">已安装</Badge>
          <Badge :variant="running ? 'ok' : 'warn'">
            {{ running ? '运行中' : '已停止' }}
          </Badge>
          <Badge
            v-if="!running && desiredRunning"
            variant="info"
            data-testid="adguard-desired-running"
          >
            期望运行
          </Badge>
          <Badge :variant="store.status.wiring === 'on' ? 'info' : 'neutral'">
            {{ store.status.wiringLabel || '未对接' }}
          </Badge>
        </div>
        <p v-if="store.status.version" class="text-xs text-fg-subtle mt-0.5 font-mono truncate">
          {{ store.status.version }}
          <span v-if="store.status.webAddr"> · Web {{ store.status.webAddr }}</span>
          <span v-if="store.status.dnsPort"> · DNS :{{ store.status.dnsPort }}</span>
        </p>
        <p
          v-if="store.status.lastError && !running"
          class="text-xs text-destructive mt-0.5 truncate"
          data-testid="adguard-header-error"
        >
          {{ store.status.lastError }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" :disabled="busy" @click="store.update()">
          <RefreshCw class="h-4 w-4" aria-hidden="true" />
          更新
        </Button>
        <Button
          v-if="!running"
          size="sm"
          :disabled="busy"
          @click="store.start()"
        >
          <Play class="h-4 w-4" aria-hidden="true" />
          启动
        </Button>
        <template v-else>
          <Button size="sm" variant="outline" :disabled="busy" @click="store.restart()">
            <RefreshCw class="h-4 w-4" aria-hidden="true" />
            重启
          </Button>
          <Button size="sm" variant="destructive" :disabled="busy" @click="store.stop()">
            <Power class="h-4 w-4" aria-hidden="true" />
            停止
          </Button>
        </template>
        <Button
          size="sm"
          variant="outline"
          :disabled="busy || !running"
          data-testid="adguard-refresh-btn"
          @click="refreshIframe"
        >
          <RefreshCw class="h-4 w-4" aria-hidden="true" />
          刷新
        </Button>
        <Button size="sm" variant="secondary" :disabled="busy" data-testid="adguard-settings-btn" @click="openSettings">
          <Settings2 class="h-4 w-4" aria-hidden="true" />
          设置
        </Button>
        <Button
          v-if="running"
          size="sm"
          variant="secondary"
          @click="openExternally"
        >
          <ExternalLink class="h-4 w-4" aria-hidden="true" />
          新标签页
        </Button>
      </div>
    </div>

    <!-- 窄屏：状态/设置/工具开关已在 App 顶栏；展开后才露出启停/更新，默认不占高 -->
    <div
      v-if="componentEnabled && installed && mobileToolbarOpen"
      data-testid="adguard-mobile-actions"
      class="lg:hidden border-b bg-surface shrink-0"
    >
      <div
        data-testid="adguard-mobile-toolbar-panel"
        class="flex flex-wrap gap-2 px-3 py-2"
      >
        <Button size="sm" variant="outline" class="h-8" :disabled="busy" @click="store.update()">
          更新
        </Button>
        <Button
          v-if="!running"
          size="sm"
          class="h-8"
          :disabled="busy"
          @click="store.start()"
        >
          启动
        </Button>
        <template v-else>
          <Button size="sm" variant="outline" class="h-8" :disabled="busy" @click="store.restart()">
            重启
          </Button>
          <Button size="sm" variant="destructive" class="h-8" :disabled="busy" @click="store.stop()">
            停止
          </Button>
          <Button size="sm" variant="outline" class="h-8" :disabled="busy" @click="openExternally">
            新标签页
          </Button>
        </template>
      </div>
    </div>

    <p v-if="bootLoading" class="p-4 sm:p-6 text-sm text-fg-muted" data-testid="adguard-boot-loading">
      正在获取 AdGuard 状态…
    </p>

    <!-- 组件未启用：引导去系统设置（直达 /adguard 时仍可看到） -->
    <div
      v-else-if="!componentEnabled"
      class="flex-1 min-h-0 flex items-center justify-center p-4 sm:p-6"
      data-testid="adguard-component-disabled"
    >
      <Card class="max-w-lg w-full">
        <CardHeader>
          <CardTitle>AdGuard Home 未启用</CardTitle>
          <CardDescription>
            请在系统设置中启用 AdGuard Home 组件
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button as="a" href="/settings#components">前往系统设置</Button>
        </CardContent>
      </Card>
    </div>

    <!-- 未安装：完整安装引导 -->
    <div
      v-else-if="!installed"
      class="flex-1 min-h-0 flex items-center justify-center p-4 sm:p-6"
    >
      <Card class="max-w-lg w-full" data-testid="adguard-install-cta">
        <CardHeader>
          <CardTitle>安装 AdGuard Home</CardTitle>
          <CardDescription>
            可选 DNS 过滤组件。Aurora 按需下载官方 release 并作为子进程托管，不链入本程序。
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <p class="text-sm text-fg-muted">
            AdGuard Home 为 <strong class="text-fg">GPL-3.0</strong> 独立程序。安装后 Web 仅监听回环，
            经本面板同源路径 <code class="text-xs">/adguard-ui/</code> 访问。
          </p>
          <div class="rounded border border-line bg-elevated p-3 text-xs text-fg-muted space-y-1">
            <p>落盘路径（相对数据目录）：</p>
            <p>
              可执行文件 <code class="font-mono text-fg">data/bin</code>
              · 工作目录 <code class="font-mono text-fg">data/adguardhome</code>
            </p>
          </div>
          <Button
            size="lg"
            class="w-full sm:w-auto"
            data-testid="adguard-install-btn"
            :disabled="busy"
            @click="store.install()"
          >
            <Download class="h-4 w-4" aria-hidden="true" />
            {{ store.actionLoading ? '下载安装中…' : '下载并安装' }}
          </Button>
          <p v-if="store.status.lastError" class="text-sm text-destructive" data-testid="adguard-install-error">
            {{ store.status.lastError }}
          </p>
        </CardContent>
      </Card>
    </div>

    <!-- 已安装未运行：状态 + 启动 + 设置 -->
    <div
      v-else-if="!running"
      class="flex-1 min-h-0 flex items-center justify-center p-4 sm:p-6"
      data-testid="adguard-start-prompt"
    >
      <Card class="max-w-lg w-full">
        <CardHeader>
          <CardTitle>AdGuard Home 已停止</CardTitle>
          <CardDescription>
            启动后可在本页内嵌官方管理界面。运行参数、端口与 DNS 模式请在设置中配置。
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <div class="text-sm text-fg-muted space-y-1">
            <p v-if="store.status.version">
              版本：<span class="font-mono text-fg">{{ store.status.version }}</span>
            </p>
            <p v-if="store.status.workDir" class="break-all text-xs">
              工作目录：{{ store.status.workDir }}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <Button :disabled="busy" @click="store.start()">
              <Play class="h-4 w-4" aria-hidden="true" />
              {{ store.actionLoading ? '启动中…' : '启动' }}
            </Button>
            <Button variant="secondary" :disabled="busy" data-testid="adguard-settings-btn-start" @click="openSettings">
              <Settings2 class="h-4 w-4" aria-hidden="true" />
              设置
            </Button>
          </div>
          <p v-if="store.status.lastError" class="text-sm text-destructive">
            {{ store.status.lastError }}
          </p>
          <!-- 次要：旧 wiring 向导（T6 将由 DNS 模式取代） -->
          <details class="text-xs text-fg-subtle pt-1">
            <summary class="cursor-pointer select-none hover:text-fg-muted">高级 · 旧版 DNS 对接</summary>
            <div class="mt-2">
              <Button size="sm" variant="ghost" :disabled="busy" @click="openWiring">打开对接向导</Button>
            </div>
          </details>
        </CardContent>
      </Card>
    </div>

    <!-- 运行中：立刻给壳层高度；会话问题用顶条提示，不再整页空白等待 -->
    <div
      v-else-if="running"
      class="relative flex-1 min-h-0 w-full bg-surface"
      data-testid="adguard-iframe-shell"
    >
      <div
        v-if="sessionPreparing && !sessionReady"
        class="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-surface/80 p-4 text-sm text-fg-muted"
        data-testid="adguard-session-pending"
      >
        <p data-testid="adguard-session-preparing">正在打开 AdGuard…</p>
      </div>
      <div
        v-if="sessionFailed"
        class="absolute top-0 inset-x-0 z-20 flex flex-wrap items-center justify-center gap-2 border-b border-line bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:bg-amber-950/40 dark:text-amber-100"
        data-testid="adguard-session-banner"
      >
        <span>会话可能未带上，面板若空白请重试；也可在设置中保存 AdGuard 账号开启免密。</span>
        <Button
          size="sm"
          variant="outline"
          class="h-7"
          :disabled="busy"
          data-testid="adguard-session-retry"
          @click="refreshIframe"
        >
          重试
        </Button>
      </div>
      <iframe
        v-if="showIframe"
        ref="iframeRef"
        data-testid="adguard-iframe"
        :key="iframeKey"
        :src="entryPath"
        class="absolute inset-0 h-full w-full border-0 bg-surface"
        title="AdGuard Home"
        referrerpolicy="same-origin"
        @load="onIframeLoad"
      ></iframe>
      <div class="hidden lg:block absolute bottom-0 left-0 right-0 z-20 px-4 sm:px-6 py-1 border-t bg-surface/95">
        <details class="text-xs text-fg-subtle">
          <summary class="cursor-pointer select-none hover:text-fg-muted">高级 · 旧版 DNS 对接</summary>
          <div class="py-1">
            <Button size="sm" variant="ghost" :disabled="busy" @click="openWiring">打开对接向导</Button>
          </div>
        </details>
      </div>
    </div>

    <!-- AdGuard 设置弹窗：运行 / 端口 / 版本 / DNS 模式 -->
    <AdGuardSettingsDialog v-model:open="settingsOpen" />

    <!-- DNS 一键对接（次要入口；T6 后移除） -->
    <ModalDialog :open="wiringOpen" title="DNS 一键对接" max-width="max-w-lg" @close="wiringOpen = false">
      <div class="space-y-4 text-sm">
        <p class="text-fg-muted">
          将 TProxy/DNS 流量经 AdGuard Home 过滤后再回 mihomo（可选）。默认不改现网；
          应用前会写入快照，可一键回滚。
        </p>

        <div class="space-y-3">
          <label class="flex items-start gap-3 cursor-pointer">
            <Checkbox v-model="wiringOpts.redirectTProxy" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">TProxy DNS 指向 AGH</span>
              <span class="block text-xs text-fg-subtle">透明代理启用时，将 :53 转发到 AdGuard DNS 端口</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <Checkbox v-model="wiringOpts.resolveConflict" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">解决端口冲突</span>
              <span class="block text-xs text-fg-subtle">mihomo 与 AGH 争用同一 DNS 口时，把 mihomo 挪到回环备用口</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <Checkbox v-model="wiringOpts.patchUpstream" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">AGH 上游指向 mihomo DNS</span>
              <span class="block text-xs text-fg-subtle">保留 fake-ip / 策略 DNS；仅过滤时可不勾</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <Checkbox v-model="wiringOpts.weakenTunHijack" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">弱化 TUN dns-hijack</span>
              <span class="block text-xs text-fg-subtle">高级选项，默认关闭；仅在 TUN 劫持与 AGH 冲突时考虑</span>
            </span>
          </label>
        </div>

        <div v-if="store.preview" class="rounded border border-line bg-elevated p-3 space-y-2">
          <p class="text-xs font-medium text-fg">
            预检 · 当前 {{ store.preview.wiring === 'on' ? '已对接' : '未对接' }}
            <span v-if="store.preview.aghDnsPort"> · AGH DNS :{{ store.preview.aghDnsPort }}</span>
          </p>
          <ul v-if="store.preview.actions?.length" class="list-disc pl-4 text-xs text-fg-muted space-y-0.5">
            <li v-for="(a, i) in store.preview.actions" :key="i">{{ a }}</li>
          </ul>
          <ul v-if="store.preview.warnings?.length" class="list-disc pl-4 text-xs text-amber-700 dark:text-amber-300 space-y-0.5">
            <li v-for="(w, i) in store.preview.warnings" :key="'w' + i">{{ w }}</li>
          </ul>
        </div>

        <div class="flex flex-wrap gap-2 pt-1">
          <Button
            :disabled="busy || store.status.wiring === 'on'"
            @click="applyWiring"
          >
            {{ store.actionLoading ? '处理中…' : '应用对接' }}
          </Button>
          <Button
            variant="outline"
            :disabled="busy || store.status.wiring !== 'on'"
            @click="rollbackWiring"
          >
            回滚对接
          </Button>
          <Button variant="ghost" @click="wiringOpen = false">关闭</Button>
        </div>
      </div>
    </ModalDialog>
  </main>
</template>

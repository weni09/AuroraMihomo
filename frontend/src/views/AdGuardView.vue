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


const store = useAdGuardStore()

const wiringOpen = ref(false)
const settingsOpen = ref(false)
// 对接向导默认勾选：与设计 §4 一致（弱化 TUN 默认关）
// 主路径已降级为次要入口；T6 将用 DNS 模式取代
const wiringOpts = reactive<WiringOptions>({
  redirectTProxy: true,
  resolveConflict: true,
  patchUpstream: true,
  weakenTunHijack: false,
})

const entryPath = computed(() => store.status.entryPath || '/adguard-ui/')
const busy = computed(() => store.isLoading || store.actionLoading)
const componentEnabled = computed(() => store.status.componentEnabled)
const installed = computed(() => store.status.installed)
const running = computed(() => store.status.running)

function openExternally() {
  window.open(entryPath.value, '_blank', 'noopener,noreferrer')
}

function openSettings() {
  settingsOpen.value = true
}

/** 把对接状态与「新标签页」交给 App 移动端顶栏，避免本页再叠一条 header。 */
function syncPageChrome() {
  setPageChrome({
    subtitle: store.status.wiringLabel || '',
    action: {
      label: '新标签页',
      // 未安装时入口也无意义；运行中才真正能用
      disabled: !store.status.installed || !store.status.running,
      onClick: openExternally,
    },
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
  await store.fetchStatus()
  syncPageChrome()
})

watch(
  () => [store.status.wiringLabel, store.status.installed, store.status.running, store.status.entryPath],
  syncPageChrome,
)

onBeforeUnmount(clearPageChrome)
</script>

<template>
  <!-- 对标 Zashboard：iframe 无内在高度，用 h-dvh 自持一屏 -->
  <main class="h-dvh flex flex-col min-h-0">
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
          <Badge :variant="store.status.wiring === 'on' ? 'info' : 'neutral'">
            {{ store.status.wiringLabel || '未对接' }}
          </Badge>
        </div>
        <p v-if="store.status.version" class="text-xs text-fg-subtle mt-0.5 font-mono truncate">
          {{ store.status.version }}
          <span v-if="store.status.webAddr"> · Web {{ store.status.webAddr }}</span>
          <span v-if="store.status.dnsPort"> · DNS :{{ store.status.dnsPort }}</span>
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

    <!-- 窄屏操作条：仅已安装时展示启停/设置 -->
    <div
      v-if="componentEnabled && installed"
      data-testid="adguard-mobile-actions"
      class="lg:hidden flex flex-wrap gap-2 px-4 py-2 border-b bg-surface shrink-0"
    >
      <Badge variant="ok">已安装</Badge>
      <Badge :variant="running ? 'ok' : 'warn'">
        {{ running ? '运行中' : '已停止' }}
      </Badge>
      <Badge :variant="store.status.wiring === 'on' ? 'info' : 'neutral'">
        {{ store.status.wiringLabel || '未对接' }}
      </Badge>
      <div class="flex flex-wrap gap-2 w-full mt-1">
        <Button size="sm" variant="outline" :disabled="busy" @click="store.update()">更新</Button>
        <Button
          v-if="!running"
          size="sm"
          :disabled="busy"
          @click="store.start()"
        >
          启动
        </Button>
        <template v-else>
          <Button size="sm" variant="outline" :disabled="busy" @click="store.restart()">重启</Button>
          <Button size="sm" variant="destructive" :disabled="busy" @click="store.stop()">停止</Button>
        </template>
        <Button size="sm" variant="secondary" :disabled="busy" data-testid="adguard-settings-btn-mobile" @click="openSettings">
          设置
        </Button>
      </div>
    </div>

    <p v-if="store.isLoading && !installed && !running" class="p-4 sm:p-6 text-sm text-fg-muted">
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

    <!-- 运行中：同源 iframe -->
    <template v-else>
      <iframe
        data-testid="adguard-iframe"
        :src="entryPath"
        class="flex-1 min-h-0 w-full border-0"
        title="AdGuard Home"
      ></iframe>
      <!-- 次要：旧 wiring 入口（不占主工具栏） -->
      <div class="hidden lg:block px-4 sm:px-6 py-1 border-t bg-surface shrink-0">
        <details class="text-xs text-fg-subtle">
          <summary class="cursor-pointer select-none hover:text-fg-muted">高级 · 旧版 DNS 对接</summary>
          <div class="py-1">
            <Button size="sm" variant="ghost" :disabled="busy" @click="openWiring">打开对接向导</Button>
          </div>
        </details>
      </div>
    </template>

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

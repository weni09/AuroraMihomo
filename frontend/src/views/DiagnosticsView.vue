<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useDiagnosticsStore, type DiagnosticTarget, type ProbeResult } from '../stores/diagnostics'
import { useRealtime } from '../composables/useRealtime'
import { wsStatusLabel } from '../utils/labels'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Badge, type BadgeVariants } from '@/components/ui/badge'

const store = useDiagnosticsStore()

// 出网路径：both 直连+代理对比 / direct 仅直连 / proxy 仅代理。
// 预设目标的直连探测会走 /etc/resolv.conf 的本地 DNS，代理探测走内核代理端口
const path = ref<'direct' | 'proxy' | 'both'>('both')

// 手动输入
const manualTarget = ref('')
const manualType = ref<DiagnosticTarget['type']>('tcp')
// 端口仅对 tcp 有意义（其余类型后端有默认端口）；Input 是封装组件，
// v-model.number 修饰符不会透传，提交时显式 Number() 转换
const manualPort = ref<number>(443)

// WS 实时进度：store 内部按 requestId 过滤，只收本轮自己的事件
const { status: wsStatus } = useRealtime((type, data) => {
  store.handleProgress(type, data)
})
// useRealtime 的 status 是 Ref，模板里直接引用会被自动解包，
// 展示文案统一走 computed，避免解包后 .value 变 undefined
const wsOffline = computed(() => wsStatus.value !== 'live')
const wsStatusText = computed(() => wsStatusLabel(wsStatus.value))

/**
 * 轮询兜底：进度事件可能丢（WS 断连/重连窗口），
 * 结束时用全量结果回填，拿到 done 即停表。
 */
let pollTimer: number | null = null
function stopPolling() {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}
function startPolling() {
  stopPolling()
  pollTimer = window.setInterval(async () => {
    if (!store.requestId) return
    try {
      const done = await store.fetchResult(store.requestId)
      if (done) stopPolling()
    } catch {
      // 拦截器已全局提示，下一轮再试
    }
  }, 2000)
}

// 预设目标一键全测：目标清单来自后端 /diagnostics/targets——
// 除 GitHub API / raw / 公共 DNS 外，还含代理端口 TCP 连通性探测目标
// （依赖当前生效的本地代理地址，前端无法自行推导）。
async function runPreset() {
  try {
    const targets = await store.fetchPresetTargets()
    if (!targets.length) {
      // 后端未返回预设目标（异常情况）：不发起空跑
      console.error('预设目标清单为空')
      return
    }
    await store.run(targets, path.value)
  } catch (e) {
    console.error(e)
    return
  }
  startPolling()
}

async function runManual() {
  const target = manualTarget.value.trim()
  if (!target) return
  const targets: DiagnosticTarget[] = [{ type: manualType.value, target }]
  if (manualType.value === 'tcp') {
    const port = Number(manualPort.value)
    // 端口必须是 1-65535 的整数；非法时直接返回，保留输入供修正
    if (!(Number.isInteger(port) && port >= 1 && port <= 65535)) return
    targets[0].port = port
  }
  try {
    await store.run(targets, path.value)
  } catch (e) {
    console.error(e)
    return
  }
  startPolling()
}

function handleClear() {
  stopPolling()
  store.reset()
}

onUnmounted(stopPolling)

// status → Badge 变体。fail 用 err（badge 的 err 变体即 destructive 配色），
// timeout/error 无法确认是成功还是被墙，统一用中性色
function statusVariant(status: ProbeResult['status']): BadgeVariants['variant'] {
  switch (status) {
    case 'success':
      return 'ok'
    case 'fail':
      return 'err'
    default:
      return 'neutral'
  }
}
</script>

<template>
  <main class="max-w-6xl mx-auto p-4 sm:p-6 lg:p-8 space-y-6">
    <div class="mb-2">
      <h1 class="text-2xl sm:text-3xl font-bold text-fg">网络诊断</h1>
      <p class="text-xs sm:text-sm text-fg-subtle mt-1">
        从面板宿主机视角排查出网问题，支持直连与本地代理对比。
      </p>
    </div>

    <!-- 出网路径选择 -->
    <section class="rounded-xl border border-line bg-surface p-4 sm:p-5">
      <h2 class="text-sm font-semibold mb-3">出网路径</h2>
      <div class="flex flex-wrap items-center gap-3">
        <Select v-model="path">
          <SelectTrigger class="w-52" aria-label="出网路径"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="both">直连 + 代理对比</SelectItem>
            <SelectItem value="direct">仅直连</SelectItem>
            <SelectItem value="proxy">仅代理</SelectItem>
          </SelectContent>
        </Select>
        <span v-if="wsOffline" class="text-xs text-fg-subtle">
          实时通道未连接（{{ wsStatusText }}），结果将轮询获取
        </span>
      </div>
    </section>

    <!-- 预设目标一键全测 -->
    <section class="rounded-xl border border-line bg-surface p-4 sm:p-5">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0">
          <h2 class="text-sm font-semibold">预设目标一键全测</h2>
          <p class="text-xs text-fg-subtle mt-1">
            GitHub API / raw 内容 / 公共 DNS（1.1.1.1、8.8.8.8、223.5.5.5）
          </p>
        </div>
        <Button :disabled="store.running" @click="runPreset">
          {{ store.running ? '诊断中…' : '开始诊断' }}
        </Button>
      </div>
    </section>

    <!-- 手动输入 -->
    <section class="rounded-xl border border-line bg-surface p-4 sm:p-5">
      <h2 class="text-sm font-semibold mb-3">手动输入</h2>
      <div class="flex flex-wrap items-end gap-3">
        <div class="min-w-0 flex-1">
          <Label for="diag-target" class="text-sm text-fg-muted">目标</Label>
          <Input id="diag-target" v-model="manualTarget" placeholder="主机 / 域名 / URL" class="mt-1 font-mono" />
        </div>
        <div>
          <Label for="diag-type" class="text-sm text-fg-muted">类型</Label>
          <Select v-model="manualType">
            <SelectTrigger id="diag-type" class="mt-1 w-36"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="tcp">TCP</SelectItem>
              <SelectItem value="dns">DNS</SelectItem>
              <SelectItem value="http">HTTP</SelectItem>
              <SelectItem value="ping">Ping</SelectItem>
              <SelectItem value="traceroute">Traceroute</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="manualType === 'tcp'">
          <Label for="diag-port" class="text-sm text-fg-muted">端口</Label>
          <Input id="diag-port" v-model="manualPort" type="number" min="1" max="65535" class="mt-1 w-28" />
        </div>
        <Button :disabled="store.running" @click="runManual">诊断</Button>
      </div>
    </section>

    <!-- 诊断结果 -->
    <section
      v-if="store.results.length || store.running"
      class="rounded-xl border border-line bg-surface p-4 sm:p-5"
    >
      <div class="flex items-center justify-between gap-3 mb-3">
        <h2 class="text-sm font-semibold">诊断结果</h2>
        <div class="flex items-center gap-2">
          <span v-if="store.running" class="text-xs text-fg-subtle">进行中…</span>
          <Button variant="ghost" size="sm" @click="handleClear">清空</Button>
        </div>
      </div>
      <div class="space-y-2">
        <!-- 同一目标同一类型在 both 模式下会有 direct/proxy 两条，用下标做 key 即可 -->
        <div
          v-for="(r, i) in store.results"
          :key="i"
          class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-line bg-elevated/50 px-3 py-2 text-sm"
        >
          <Badge :variant="statusVariant(r.status)">{{ r.status }}</Badge>
          <span class="font-mono text-xs text-fg-muted">{{ r.path }}</span>
          <span class="min-w-0 flex-1 truncate">
            {{ r.target }}<span class="text-fg-subtle"> ({{ r.type }})</span>
          </span>
          <span v-if="r.latencyMs !== undefined" class="shrink-0 text-xs text-fg-subtle">
            {{ r.latencyMs }}ms
          </span>
          <span v-if="r.error" class="max-w-64 shrink-0 truncate text-xs text-destructive">
            {{ r.error }}
          </span>
        </div>
      </div>
    </section>
  </main>
</template>

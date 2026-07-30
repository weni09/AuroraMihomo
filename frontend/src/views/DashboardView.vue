<script setup lang="ts">
import { wsStatusLabel, taskStatusLabel } from '../utils/labels'
import { computed, onMounted, ref } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useSubscriptionStore } from '../stores/subscription'
import { useConflictStore } from '../stores/conflict'
import { useTaskStore } from '../stores/task'
import { useRealtime } from '../composables/useRealtime'
import api from '../api'

const mihomoStore = useMihomoStore()
const subStore = useSubscriptionStore()
const conflictStore = useConflictStore()
const taskStore = useTaskStore()

const proxyCount = ref(0)
const configOk = ref<boolean | null>(null)

const { status: wsStatus } = useRealtime((type, data) => {
  if (type === 'mihomo.status' && data) {
    mihomoStore.applyStatus({ status: data.status, version: data.version, pid: data.pid })
  }
  if (type === 'log.message' && data) {
    mihomoStore.pushLog(data)
  }
  if (type === 'config.updated') {
    void refreshConfigState()
  }
})

// 设计 §3：最近一次订阅更新时间
const lastSubUpdate = computed(() => {
  const times = subStore.subscriptions
    .map((s) => s.lastUpdate)
    .filter((t): t is string => !!t)
    .sort()
  const latest = times[times.length - 1]
  return latest ? new Date(latest).toLocaleString() : '尚未更新'
})

async function refreshConfigState() {
  try {
    const res = await api.get<{ content: string }>('/config/base')
    configOk.value = !!res.data?.content
  } catch {
    configOk.value = false
  }
}

// 设计 §3：代理节点数量（取自最新合并结果）
async function refreshProxyCount() {
  try {
    const res = await api.get<Array<{ id: number }>>('/collections')
    const first = res.data?.[0]
    if (first) {
      const built = await api.post(`/collections/${first.id}/build`)
      proxyCount.value = built.data?.count || 0
      return
    }
  } catch { /* 无组合订阅时忽略 */ }
  proxyCount.value = 0
}

onMounted(async () => {
  await Promise.all([
    mihomoStore.fetchStatus(),
    subStore.fetchSubscriptions(),
    conflictStore.fetch(),
    taskStore.fetch(),
    refreshConfigState(),
  ])
  void refreshProxyCount()
  try {
    const res = await api.get('/mihomo/logs')
    ;(res.data || []).slice(-20).forEach((l: any) => mihomoStore.pushLog(l))
  } catch { /* 内核未启动时忽略 */ }
})
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-6">
      <h1 class="text-2xl sm:text-3xl font-bold text-fg">控制台</h1>
      <span class="text-xs px-2 py-1 rounded tint-neutral">实时通道：{{ wsStatusLabel(wsStatus) }}</span>
    </div>

    <!-- 设计 §3 StatusCard：状态 / 版本 / 节点数 / 上次更新 / 配置状态 -->
    <div class="grid gap-3 sm:gap-4 grid-cols-2 lg:grid-cols-5 mb-6">
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">内核状态</div>
        <!-- text-lg(18px)+bold 达到 WCAG 大字号门槛，只需 3:1 对比度，
             success/destructive token 在此背景下都够（3.98~5.77:1）。
             MihomoView 同类文字是 font-bold 但字号更小，未达大字号门槛，
             那边改用了专门调过对比度的固定色阶，两处不要合并 -->
        <div
          class="text-lg font-bold"
          :class="mihomoStore.status === 'running' ? 'text-success' : 'text-destructive'"
        >
          {{ mihomoStore.status === 'running' ? '运行中' : '已停止' }}
        </div>
      </div>
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">内核版本</div>
        <div class="text-sm font-mono text-fg truncate" :title="mihomoStore.version">
          {{ mihomoStore.version }}
        </div>
      </div>
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">代理节点数</div>
        <div class="text-lg font-bold text-fg">{{ proxyCount }}</div>
      </div>
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">上次订阅更新</div>
        <div class="text-xs text-fg">{{ lastSubUpdate }}</div>
      </div>
      <!-- 五张卡片在两列网格下末位落单，让它横跨整行而非留半行空白 -->
      <div class="bg-surface rounded-xl shadow-sm border p-4 col-span-2 lg:col-span-1">
        <div class="text-xs text-fg-muted mb-1">配置状态</div>
        <div class="text-sm font-semibold" :class="configOk ? 'text-success' : 'text-amber-600 dark:text-amber-400'">
          {{ configOk === null ? '检测中' : configOk ? '正常' : '未配置' }}
        </div>
        <div v-if="conflictStore.unresolvedCount > 0" class="text-xs text-amber-600 dark:text-amber-400 mt-1">
          {{ conflictStore.unresolvedCount }} 项冲突待处理
        </div>
      </div>
    </div>

    <div class="grid gap-4 sm:gap-6 grid-cols-1 lg:grid-cols-3">
      <!-- 设计 §3 TaskCard -->
      <div class="bg-surface rounded-xl shadow-sm border p-4 sm:p-5">
        <h2 class="font-bold text-fg mb-3">后台任务</h2>
        <div class="space-y-2">
          <div
            v-for="t in taskStore.labeled"
            :key="t.id"
            class="flex items-center justify-between gap-3 text-sm border-b border-line pb-2 last:border-0"
          >
            <div class="min-w-0">
              <div class="text-fg truncate">{{ t.label }}</div>
              <div class="text-xs text-fg-subtle font-mono">{{ t.cron }}</div>
            </div>
            <span
              class="text-xs px-2 py-0.5 rounded shrink-0"
              :class="t.status === 'ok' ? 'tint-ok' : t.status === 'error' ? 'tint-err' : 'tint-neutral'"
            >
              {{ taskStatusLabel(t.status) }}
            </span>
          </div>
          <div v-if="taskStore.tasks.length === 0" class="text-sm text-fg-subtle">暂无任务</div>
        </div>
      </div>

      <!-- 设计 §3 RuntimeLog -->
      <div class="bg-surface rounded-xl shadow-sm border p-4 sm:p-5 lg:col-span-2">
        <div class="flex items-center justify-between gap-3 mb-3">
          <h2 class="font-bold text-fg">实时日志</h2>
          <RouterLink to="/logs" class="text-sm text-primary hover:underline shrink-0">查看完整日志</RouterLink>
        </div>
        <!-- 日志区两种主题下都保持深色终端观感：这是等宽输出流，
             浅底反而降低可读性。窄屏降低高度，避免整屏只剩日志 -->
        <div class="bg-slate-900 text-slate-100 rounded-lg p-3 h-56 sm:h-72 overflow-auto text-xs font-mono space-y-1">
          <div v-for="(l, i) in mihomoStore.recentLogs" :key="i" class="break-words">
            <span class="text-slate-500">{{ l.time }}</span>
            <span class="text-cyan-400"> [{{ l.stream }}]</span>
            {{ l.message }}
          </div>
          <div v-if="mihomoStore.recentLogs.length === 0" class="text-slate-500">等待内核日志输出…</div>
        </div>
      </div>
    </div>
  </main>
</template>

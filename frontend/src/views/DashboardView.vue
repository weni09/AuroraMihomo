<script setup lang="ts">
import { wsStatusLabel, taskStatusLabel } from '../utils/labels'
import { computed, onMounted, ref } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useSubscriptionStore } from '../stores/subscription'
import { useConflictStore } from '../stores/conflict'
import { useTaskStore } from '../stores/task'
import { useMihomoRealtime } from '../composables/useMihomoRealtime'
import KernelActionBar from '../components/KernelActionBar.vue'
import KernelLogPreview from '../components/KernelLogPreview.vue'
import type { KernelLogLine } from '../components/KernelLogPreview.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import api from '../api'

const mihomoStore = useMihomoStore()
const subStore = useSubscriptionStore()
const conflictStore = useConflictStore()
const taskStore = useTaskStore()

const proxyCount = ref(0)
const configOk = ref<boolean | null>(null)

// 内核 status/log 由 composable 写入 store；此处仅补 config.updated
const { status: wsStatus } = useMihomoRealtime((type) => {
  if (type === 'config.updated') void refreshConfigState()
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
  } catch {
    /* 无组合订阅时忽略 */
  }
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
  // 进入页时补一段历史日志，随后靠 realtime 续写
  try {
    const res = await api.get<KernelLogLine[]>('/mihomo/logs')
    ;(res.data || []).slice(-20).forEach((l) => mihomoStore.pushLog(l))
  } catch {
    /* 内核未启动时忽略 */
  }
})
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8 flex flex-col gap-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-2xl sm:text-3xl font-bold text-fg">控制台</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">
          内核运行概况 · 订阅与任务 · 快捷操作
        </p>
      </div>
      <span class="text-xs px-2 py-1 rounded tint-neutral shrink-0">
        实时通道：{{ wsStatusLabel(wsStatus) }}
      </span>
    </div>

    <!-- 设计 §3 StatusCard：状态 / 版本 / 节点数 / 上次更新 / 配置状态 -->
    <div class="grid gap-3 sm:gap-4 grid-cols-2 lg:grid-cols-5">
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">内核状态</div>
        <!-- 与 MihomoView 统一：emerald/rose 固定色阶，小字大字都满足 WCAG AA。
             不用 text-success/text-destructive：其深色值是为「按钮实色底配白字」调的，
             直接当前景字叠在 surface 上时对比度不足。text-lg+bold 虽可走大字号 3:1，
             仍与内核页保持同一套色，避免两页状态色跳变。 -->
        <div
          class="text-lg font-bold"
          :class="
            mihomoStore.status === 'running'
              ? 'text-emerald-600 dark:text-emerald-400'
              : 'text-rose-600 dark:text-rose-400'
          "
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
        <div
          class="text-sm font-semibold"
          :class="configOk ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'"
        >
          {{ configOk === null ? '检测中' : configOk ? '正常' : '未配置' }}
        </div>
        <div
          v-if="conflictStore.unresolvedCount > 0"
          class="text-xs text-amber-600 dark:text-amber-400 mt-1"
        >
          {{ conflictStore.unresolvedCount }} 项冲突待处理
        </div>
      </div>
    </div>

    <!-- 快捷操作：启停/重启/重载 + 跳转内核管理 -->
    <div class="bg-surface rounded-xl shadow-sm border p-4 sm:p-5">
      <div class="flex flex-wrap items-center justify-between gap-3 mb-3">
        <div class="text-sm font-semibold text-fg">快捷操作</div>
        <RouterLink
          to="/mihomo"
          class="text-sm text-primary hover:underline shrink-0"
        >
          内核管理 →
        </RouterLink>
      </div>
      <KernelActionBar compact />
    </div>

    <div class="grid gap-4 sm:gap-6 grid-cols-1 lg:grid-cols-3">
      <!-- 设计 §3 TaskCard -->
      <Card>
        <CardHeader>
          <CardTitle>后台任务</CardTitle>
        </CardHeader>
        <CardContent>
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
        </CardContent>
      </Card>

      <!-- 设计 §3 RuntimeLog：终端预览下沉到 KernelLogPreview -->
      <Card class="lg:col-span-2">
        <CardHeader class="flex flex-row items-center justify-between gap-3 space-y-0">
          <CardTitle>实时日志</CardTitle>
          <RouterLink to="/logs" class="text-sm text-primary hover:underline shrink-0">
            查看完整日志
          </RouterLink>
        </CardHeader>
        <CardContent>
          <KernelLogPreview
            :lines="mihomoStore.recentLogs"
            height-class="h-56 sm:h-72"
            empty-text="等待内核日志输出…"
          />
        </CardContent>
      </Card>
    </div>
  </main>
</template>

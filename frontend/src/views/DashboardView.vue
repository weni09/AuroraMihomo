<script setup lang="ts">
import { wsStatusLabel, taskStatusLabel } from '../utils/labels'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useSubscriptionStore } from '../stores/subscription'
import { useConflictStore } from '../stores/conflict'
import { useTaskStore } from '../stores/task'
import { useTransparentStore } from '../stores/transparent'
import { useMihomoRealtime } from '../composables/useMihomoRealtime'
import KernelActionBar from '../components/KernelActionBar.vue'
import KernelLogPreview from '../components/KernelLogPreview.vue'
import type { KernelLogLine } from '../components/KernelLogPreview.vue'
import GitHubCorner from '../components/GitHubCorner.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Clock } from 'lucide-vue-next'
import api from '../api'

/** 本项目公开仓库；控制台右上角章鱼猫角标 */
const PROJECT_GITHUB_URL = 'https://github.com/weni09/AuroraMihomo'

const mihomoStore = useMihomoStore()
const subStore = useSubscriptionStore()
const conflictStore = useConflictStore()
const taskStore = useTaskStore()
const tp = useTransparentStore()

const proxyCount = ref(0)
const configOk = ref<boolean | null>(null)

/**
 * 控制台时钟：以宿主 serverTime 为锚，本地每秒推演，避免每秒打状态接口。
 * fetchAtMs / serverAtMs 在拿到状态后对齐；无 serverTime 时退回浏览器本地钟。
 */
const nowTick = ref(Date.now())
let clockTimer: number | null = null
const fetchAtMs = ref(0)
const serverAtMs = ref(0)

const pad2 = (n: number) => String(n).padStart(2, '0')

/** 日期行：2026-08-01 · 周六 */
const clockDateText = computed(() => {
  const d = currentServerDate()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()] ?? ''
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} · 周${week}`
})

/** 时刻行：精确到秒，等宽数字 */
const clockTimeText = computed(() => {
  const d = currentServerDate()
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
})

const timezoneIana = computed(
  () => mihomoStore.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || '',
)

/** 短偏移如 GMT+8 */
const timezoneOffset = computed(() => {
  const iana = timezoneIana.value
  const d = currentServerDate()
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: iana || undefined,
    timeZoneName: 'shortOffset',
  }).formatToParts(d)
  return parts.find((p) => p.type === 'timeZoneName')?.value || ''
})

const timezoneTitle = computed(() => {
  const iana = timezoneIana.value
  const off = timezoneOffset.value
  if (iana && off) return `宿主时间 ${iana}（${off}）`
  return `宿主时间 ${iana || off || ''}`
})

function currentServerDate(): Date {
  if (serverAtMs.value > 0 && fetchAtMs.value > 0) {
    return new Date(serverAtMs.value + (nowTick.value - fetchAtMs.value))
  }
  return new Date(nowTick.value)
}

function syncClockFromStore() {
  const raw = mihomoStore.serverTime
  if (!raw) return
  const t = Date.parse(raw)
  if (Number.isNaN(t)) return
  serverAtMs.value = t
  fetchAtMs.value = Date.now()
  nowTick.value = fetchAtMs.value
}

function startClock() {
  stopClock()
  clockTimer = window.setInterval(() => {
    nowTick.value = Date.now()
  }, 1000)
}

function stopClock() {
  if (clockTimer != null) {
    window.clearInterval(clockTimer)
    clockTimer = null
  }
}

/** 与宿主状态接口的分钟级再对齐定时器 */
let alignTimer: number | null = null

function stopAlign() {
  if (alignTimer != null) {
    window.clearInterval(alignTimer)
    alignTimer = null
  }
}

// 内核 status/log 由 composable 写入 store；此处仅补 config.updated
const { status: wsStatus } = useMihomoRealtime((type) => {
  if (type === 'config.updated') {
    void refreshConfigState()
    // 透明代理开关写在 base.yaml，合并/保存后状态可能变
    void tp.fetch()
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

/** 透明代理开关文案：开启 / 待确认 / 仅端口 / 未开启 */
const transparentOnLabel = computed(() => {
  if (tp.status.pendingConfirm) return '待确认'
  if (tp.status.enabled) return '已开启'
  if (tp.status.portConfiguredOnly) return '仅配置端口'
  return '未开启'
})

/**
 * 模式展示。enabled 时用实际 mode；未启用但 portConfiguredOnly 时仍可能
 * 显示 tproxy（配置里有端口），否则给破折号避免误以为已接管。
 */
const transparentModeLabel = computed(() => {
  const mode = tp.status.mode
  if (mode === 'tun') return 'TUN · 虚拟网卡'
  if (mode === 'tproxy') return 'TProxy · 透明转发'
  return '—'
})

const transparentOnClass = computed(() => {
  if (tp.status.pendingConfirm || tp.status.rulesOutOfSync || tp.status.portConfiguredOnly) {
    return 'text-amber-600 dark:text-amber-400'
  }
  if (tp.status.enabled) return 'text-emerald-600 dark:text-emerald-400'
  return 'text-fg-muted'
})

async function refreshConfigState() {
  try {
    const res = await api.get<{ content: string }>('/config/base')
    configOk.value = !!res.data?.content
  } catch {
    configOk.value = false
  }
}

/** 任务下次运行时间的短展示；解析失败则原样截断 */
function formatTaskTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
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
  startClock()
  await Promise.all([
    mihomoStore.fetchStatus(),
    subStore.fetchSubscriptions(),
    conflictStore.fetch(),
    taskStore.fetch(),
    refreshConfigState(),
    tp.fetch(),
  ])
  syncClockFromStore()
  // 每分钟再对齐一次宿主钟，抑制长期漂移
  stopAlign()
  alignTimer = window.setInterval(() => {
    void mihomoStore.fetchStatus().then(() => syncClockFromStore())
  }, 60_000)
  void refreshProxyCount()
  // 进入页时补一段历史日志，随后靠 realtime 续写
  try {
    const res = await api.get<KernelLogLine[]>('/mihomo/logs')
    ;(res.data || []).slice(-20).forEach((l) => mihomoStore.pushLog(l))
  } catch {
    /* 内核未启动时忽略 */
  }
})

onUnmounted(() => {
  stopClock()
  stopAlign()
})
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8 flex flex-col gap-6">
    <!-- 页头：小屏标题与状态区分行；状态区纵向排列，避免时钟+通道并排撑出横向滚动。
         右侧预留角标宽度（窄屏 56 / ≥sm 88），避免被 fixed Octocat 挡住。 -->
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4 pr-12 sm:pr-[5.5rem]">
      <div class="min-w-0">
        <h1 class="text-2xl sm:text-3xl font-bold text-fg">控制台</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">
          内核运行概况 · 订阅与任务 · 快捷操作
        </p>
      </div>
      <div class="flex flex-col gap-2 w-full min-w-0 sm:w-auto sm:items-end">
        <!-- 宿主时钟：窄屏时区在时刻右侧并用竖线分隔（与桌面一致），
             不再堆到时刻下方以免「时间 / 时区」之间缺分割。 -->
        <div
          class="flex items-center gap-2 sm:gap-3 w-full sm:w-auto min-w-0 max-w-full rounded-xl border border-line bg-surface/80 px-2.5 sm:px-3 py-2 shadow-sm backdrop-blur-sm"
          :title="timezoneTitle"
        >
          <div
            class="flex size-8 sm:size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
            aria-hidden="true"
          >
            <Clock class="size-3.5 sm:size-4" />
          </div>
          <div class="min-w-0 flex-1 leading-tight">
            <div class="text-[11px] text-fg-muted tracking-wide truncate">
              {{ clockDateText }}
            </div>
            <div
              class="font-mono text-base sm:text-xl font-semibold tabular-nums text-fg tracking-tight"
            >
              {{ clockTimeText }}
            </div>
          </div>
          <!-- 时区：全尺寸都在右侧竖排 + border-l，与桌面同一套分隔 -->
          <div
            class="flex flex-col items-end gap-0.5 pl-2 border-l border-line shrink-0 min-w-0"
          >
            <span
              class="inline-flex items-center rounded-md bg-elevated px-1.5 py-0.5 text-[10px] font-medium text-fg-muted"
            >
              {{ timezoneOffset || '—' }}
            </span>
            <span
              class="max-w-[6.5rem] sm:max-w-[7.5rem] truncate text-[10px] text-fg-subtle"
              :title="timezoneIana"
            >
              {{ timezoneIana || 'Local' }}
            </span>
          </div>
        </div>
        <span
          class="inline-flex self-start sm:self-end text-xs px-2.5 py-1.5 rounded-lg border border-line bg-surface text-fg-muted max-w-full"
        >
          <span class="sm:hidden">通道</span>
          <span class="hidden sm:inline">实时通道</span>
          <span class="mx-1 text-fg-subtle">·</span>
          <span class="text-fg font-medium">{{ wsStatusLabel(wsStatus) }}</span>
        </span>
      </div>
    </div>

    <!-- 右上角直角三角半包围 + Octocat 探头；fixed 不占文档流 -->
    <GitHubCorner :href="PROJECT_GITHUB_URL" :size="88" />

    <!-- 状态卡：内核 + 透明代理 + 业务概况。lg 三列，六张卡两行对齐 -->
    <div class="grid gap-3 sm:gap-4 grid-cols-2 lg:grid-cols-3">
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="text-xs text-fg-muted mb-1">内核状态</div>
        <!-- 与 MihomoView 统一：emerald/rose 固定色阶，满足 WCAG AA。
             不用 text-success/text-destructive：其深色值是为按钮实色底配白字调的。 -->
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
      <!-- 透明代理：只读概览，改开关去系统设置 -->
      <div class="bg-surface rounded-xl shadow-sm border p-4">
        <div class="flex items-center justify-between gap-2 mb-1">
          <div class="text-xs text-fg-muted">透明代理</div>
          <RouterLink
            to="/settings#transparent"
            class="text-[11px] text-primary hover:underline shrink-0"
          >
            设置
          </RouterLink>
        </div>
        <div class="text-lg font-bold" :class="transparentOnClass">
          {{ transparentOnLabel }}
        </div>
        <div class="text-xs text-fg mt-0.5">
          {{ transparentModeLabel }}
          <span v-if="tp.status.enabled && tp.status.mode === 'tproxy'" class="text-fg-subtle">
            · :{{ tp.status.tproxyPort }}
          </span>
          <span v-if="tp.status.enabled && tp.status.mode === 'tun'" class="text-fg-subtle">
            · {{ tp.status.tunStack || 'mixed' }}
          </span>
        </div>
        <div
          v-if="tp.status.rulesOutOfSync"
          class="text-xs text-amber-600 dark:text-amber-400 mt-1"
        >
          规则与配置不同步
        </div>
        <div
          v-else-if="tp.status.portConfiguredOnly"
          class="text-xs text-fg-subtle mt-1"
        >
          配置有端口，规则未由面板接管
        </div>
        <div
          v-else-if="tp.status.pendingConfirm"
          class="text-xs text-amber-600 dark:text-amber-400 mt-1"
        >
          {{ tp.status.secondsLeft }}s 内确认，否则回滚
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
      <div class="bg-surface rounded-xl shadow-sm border p-4">
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
          <p class="text-xs text-fg-subtle font-normal mt-1">
            含配置/内核任务与系统设置中的定时调度（日志清理、自动更新、远程拉取）
          </p>
        </CardHeader>
        <CardContent>
          <div class="space-y-2 max-h-72 overflow-y-auto">
            <div
              v-for="t in taskStore.labeled"
              :key="t.id"
              class="flex items-center justify-between gap-3 text-sm border-b border-line pb-2 last:border-0"
            >
              <div class="min-w-0">
                <div class="text-fg truncate">{{ t.label }}</div>
                <div class="text-xs text-fg-subtle font-mono truncate">{{ t.cron || '—' }}</div>
                <div v-if="t.message" class="text-[11px] text-fg-subtle truncate">{{ t.message }}</div>
              </div>
              <div class="flex flex-col items-end gap-1 shrink-0">
                <span
                  class="text-xs px-2 py-0.5 rounded"
                  :class="
                    t.status === 'ok' || t.status === 'scheduled'
                      ? 'tint-ok'
                      : t.status === 'error'
                        ? 'tint-err'
                        : t.status === 'disabled' || !t.enabled
                          ? 'tint-neutral'
                          : 'tint-neutral'
                  "
                >
                  {{ t.enabled ? taskStatusLabel(t.status) : '已关闭' }}
                </span>
                <span v-if="t.nextRun" class="text-[10px] text-fg-subtle max-w-[9rem] text-right truncate" :title="t.nextRun">
                  下次 {{ formatTaskTime(t.nextRun) }}
                </span>
              </div>
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

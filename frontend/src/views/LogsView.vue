<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import api from '../api'
import { useRealtime } from '../composables/useRealtime'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { wsStatusLabel, appLogLevelLabel } from '../utils/labels'

/** 内核日志：mihomo 子进程的 stdout/stderr */
type KernelLine = { time: string; stream: string; message: string }
/** 应用日志：本项目自身（go-zero logx）的输出，分级别、带调用位置 */
type AppLine = { time: string; level: string; message: string; caller?: string }

type Tab = 'kernel' | 'app'
const tab = ref<Tab>('kernel')

// 两路日志各自独立缓存：混成一条流就无法区分"内核说的"与"本程序说的"，
// 排查时反而更费劲。切换 Tab 只切换展示，两边都在后台持续接收。
const kernelLogs = ref<KernelLine[]>([])
const appLogs = ref<AppLine[]>([])
const loadError = ref('')

/** 应用日志的级别筛选，空字符串表示全部 */
const levelFilter = ref('')

// shadcn Select 的 SelectItem 不接受空字符串 value，"全部"用 __all__ 占位，
// 转换只在这个 computed 里做，levelFilter 本身仍按空字符串语义读写
const levelFilterSelectValue = computed({
  get: () => levelFilter.value || '__all__',
  set: (v: string) => { levelFilter.value = v === '__all__' ? '' : v },
})

// 前端保留上限。后端内存缓冲是 1000 条，这里 500 足够回溯；
// 再多会让 DOM 节点数明显拖慢滚动（当前无虚拟滚动）。
const MAX_LINES = 500

const logBox = ref<HTMLElement | null>(null)
// 跟随开关：用户主动上滑查看历史时暂停自动滚动，回到底部后恢复
const following = ref(true)

const onScroll = () => {
  const el = logBox.value
  if (!el) return
  following.value = el.scrollHeight - el.scrollTop - el.clientHeight < 40
}

const scrollToBottom = () => {
  const el = logBox.value
  if (el) el.scrollTop = el.scrollHeight
}

/** 只有当前正在看的那一路才需要自动滚动 */
const autoScrollIfActive = (which: Tab) => {
  if (which === tab.value && following.value) nextTick(scrollToBottom)
}

const pushKernel = (line: KernelLine) => {
  kernelLogs.value.push(line)
  if (kernelLogs.value.length > MAX_LINES) {
    kernelLogs.value.splice(0, kernelLogs.value.length - MAX_LINES)
  }
  autoScrollIfActive('kernel')
}

const pushApp = (line: AppLine) => {
  appLogs.value.push(line)
  if (appLogs.value.length > MAX_LINES) {
    appLogs.value.splice(0, appLogs.value.length - MAX_LINES)
  }
  autoScrollIfActive('app')
}

/** 应用日志按级别筛选后的视图 */
const visibleAppLogs = computed(() =>
  levelFilter.value ? appLogs.value.filter((l) => l.level === levelFilter.value) : appLogs.value,
)

const isEmpty = computed(() =>
  tab.value === 'kernel' ? kernelLogs.value.length === 0 : visibleAppLogs.value.length === 0,
)

// 清空只作用于当前 Tab：用户清的是眼前这份列表，
// 不该顺手把另一路的历史也丢掉。
const clearLogs = async () => {
  if (tab.value === 'kernel') {
    kernelLogs.value = []
  } else {
    appLogs.value = []
    // 后端内存缓冲一并清掉，否则刷新页面又会被历史快照填满，
    // 用户会以为"清空"没生效。内核日志无对应接口，故仅清前端。
    try {
      await api.delete('/system/logs')
    } catch {
      // 拦截器已提示过，不重复打扰；前端列表已清空，观感上是生效的
    }
  }
  following.value = true
}

// stdout/stderr/system 是内核侧的技术标识，界面上给出中文标签
const streamLabel = (stream: string) => {
  switch (stream) {
    case 'stdout':
      return '输出'
    case 'stderr':
      return '错误'
    case 'system':
      return '系统'
    default:
      return stream || '日志'
  }
}

const streamClass = (stream: string) =>
  stream === 'stderr' ? 'text-rose-400' : stream === 'system' ? 'text-amber-400' : 'text-cyan-400'

/** 级别配色：error/severe 醒目，info 中性，debug/stat 次要 */
const levelClass = (level: string) => {
  switch (level) {
    case 'error':
    case 'severe':
      return 'text-rose-400'
    case 'slow':
      return 'text-amber-400'
    case 'debug':
    case 'stat':
      return 'log-meta-dim'
    default:
      return 'text-cyan-400'
  }
}

// 复用统一的实时通道，自带重连与服务端重启识别
const { status } = useRealtime((type, data, raw) => {
  if (type === 'log.message' && data) {
    pushKernel({
      time: data.time || raw?.at || '',
      stream: data.stream || '',
      message: data.message || '',
    })
    return
  }
  if (type === 'applog.message' && data) {
    pushApp({
      time: data.time || raw?.at || '',
      level: data.level || 'info',
      message: data.message || '',
      caller: data.caller || '',
    })
  }
})

const statusText = computed(() => wsStatusLabel(status.value))

const statusClass = computed(() =>
  status.value === 'live' ? 'tint-ok' : status.value === 'connecting' ? 'tint-neutral' : 'tint-warn',
)

// 切 Tab 后滚到底部：两路列表长度不同，沿用上一个 Tab 的 scrollTop
// 会停在一个无意义的位置
watch(tab, async () => {
  following.value = true
  await nextTick()
  scrollToBottom()
})

onMounted(async () => {
  // 两路历史并行拉取，任一失败只影响它自己
  const [kernel, app] = await Promise.allSettled([
    api.get<KernelLine[]>('/mihomo/logs'),
    api.get<{ logs: AppLine[]; total: number }>('/system/logs?limit=500'),
  ])

  const failed: string[] = []
  if (kernel.status === 'fulfilled') {
    kernelLogs.value = kernel.value.data || []
  } else {
    failed.push('内核日志')
  }
  if (app.status === 'fulfilled') {
    appLogs.value = app.value.data?.logs || []
  } else {
    failed.push('应用日志')
  }
  if (failed.length) {
    loadError.value = `${failed.join('、')}的历史记录加载失败，仅显示后续实时输出`
  }

  // 历史日志按时间正序，加载后需滚到底部，否则用户看到的是最旧的几行
  await nextTick()
  scrollToBottom()
})
</script>

<template>
  <!-- 页面本身随浏览器原生滚动条滚动；日志区是自带“跟随滚动到底部”逻辑的
       实时终端，需要一个不随外层滚动而变化的固定高度容器，因此用 dvh
       显式给高度，而不再依赖外壳的一屏 flex 链条来撑出 flex-1。 -->
  <main class="p-4 sm:p-6 lg:p-8">
    <div class="flex flex-wrap items-center justify-between gap-2 mb-4 shrink-0">
      <h1 class="text-2xl sm:text-3xl font-bold">运行日志</h1>
      <div class="flex flex-wrap items-center gap-2">
        <span v-if="!following" class="text-xs text-amber-600 dark:text-amber-400">
          已暂停跟随（滑到底部恢复）
        </span>
        <Button variant="outline" size="sm" @click="clearLogs">清空</Button>
        <span class="text-sm px-2.5 py-1 rounded" :class="statusClass">
          实时连接：{{ statusText }}
        </span>
      </div>
    </div>

    <!-- 两路日志分开展示：一路是内核说的，一路是本程序说的 -->
    <div class="flex flex-wrap items-center gap-2 mb-3 shrink-0">
      <div class="inline-flex rounded border border-line overflow-hidden" role="tablist">
        <Button
          type="button"
          role="tab"
          :aria-selected="tab === 'kernel'"
          :variant="tab === 'kernel' ? 'default' : 'ghost'"
          size="sm"
          class="rounded-none"
          @click="tab = 'kernel'"
        >
          内核日志
        </Button>
        <Button
          type="button"
          role="tab"
          :aria-selected="tab === 'app'"
          :variant="tab === 'app' ? 'default' : 'ghost'"
          size="sm"
          class="rounded-none border-l border-line"
          @click="tab = 'app'"
        >
          应用日志
        </Button>
      </div>

      <!-- 级别筛选只对应用日志有意义：内核的 stdout/stderr 不是级别 -->
      <label v-if="tab === 'app'" class="flex items-center gap-1.5 text-sm">
        <span class="text-fg-muted">级别</span>
        <Select v-model="levelFilterSelectValue">
          <!-- h-7 配默认 px-3 py-2 会把内容挤出边框，同时收紧 padding 才够放下
               图标与文字（SelectTrigger 是固定高度+padding撑内容，不像 Button
               靠 flex 居中自适应） -->
          <SelectTrigger class="h-8 w-auto py-1 px-2 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部</SelectItem>
            <SelectItem value="error">错误</SelectItem>
            <SelectItem value="severe">严重</SelectItem>
            <SelectItem value="slow">慢调用</SelectItem>
            <SelectItem value="info">信息</SelectItem>
            <SelectItem value="debug">调试</SelectItem>
          </SelectContent>
        </Select>
      </label>
    </div>

    <p v-if="loadError" class="mb-3 text-sm note-warn border rounded px-3 py-2 shrink-0">
      {{ loadError }}
    </p>

    <!-- 终端观感在两种主题下都保留：等宽日志流浅底反而更难读。
         显式高度而非 flex-1：外壳不再锁一屏，没有父级高度可撑。 -->
    <div
      ref="logBox"
      class="log-terminal rounded p-3 sm:p-4 h-[65dvh] overflow-auto text-xs font-mono space-y-1"
      @scroll="onScroll"
    >
      <div v-if="isEmpty" class="log-meta-dim italic">暂无日志输出。</div>

      <template v-if="tab === 'kernel'">
        <div v-for="(l, i) in kernelLogs" :key="i" class="break-words">
          <span class="log-meta">{{ l.time }}</span>
          <span :class="streamClass(l.stream)"> [{{ streamLabel(l.stream) }}]</span>
          {{ l.message }}
        </div>
      </template>

      <template v-else>
        <div v-for="(l, i) in visibleAppLogs" :key="i" class="break-words">
          <span class="log-meta">{{ l.time }}</span>
          <span :class="levelClass(l.level)"> [{{ appLogLevelLabel(l.level) }}]</span>
          <span v-if="l.caller" class="log-meta-dim"> {{ l.caller }}</span>
          {{ l.message }}
        </div>
      </template>
    </div>
  </main>
</template>

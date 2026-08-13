<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Progress } from '@/components/ui/progress'
import { ArrowDownToLine, ArrowUpFromLine, Clock, Cpu, HardDrive, MemoryStick } from 'lucide-vue-next'
import api from '../api'
import { useSettingsStore } from '../stores/settings'

/** 与 GET /api/v1/system/stats 的返回结构对齐（见 backend/api/AuroraMihomo-Go-Zero-API.api） */
interface DiskVolume {
  path: string
  total: number
  used: number
  percent: number
  fstype: string
}

interface SystemStats {
  cpuPercent: number
  memTotal: number
  memUsed: number
  memPercent: number
  netUpRate: number
  netDownRate: number
  netUpTotal: number
  netDownTotal: number
  diskTotal: number
  diskUsed: number
  diskPercent: number
  diskPath?: string
  diskVolumes?: DiskVolume[]
  uptimeSeconds: number
}

const settingsStore = useSettingsStore()

/** 开关与间隔都从设置读取。间隔以秒计，转毫秒给定时器用 */
const enabled = computed(() => settingsStore.settings?.monitorEnabled !== false)
const intervalMs = computed(() => {
  const sec = settingsStore.settings?.monitorIntervalSec || 3
  return sec * 1000
})

const stats = ref<SystemStats | null>(null)
let pollTimer: number | null = null

/**
 * 拉取资源快照。失败时保留上一次数据（轮询场景下旧值比占位符有用）；
 * 首次失败 stats 保持 null，界面显示「—」。错误 toast 由 api 拦截器统一处理。
 */
async function fetchStats() {
  try {
    const res = await api.get<SystemStats>('/system/stats')
    stats.value = res.data
  } catch {
    /* 保持旧值 */
  }
}

function startPolling() {
  stopPolling()
  void fetchStats()
  pollTimer = window.setInterval(() => {
    void fetchStats()
  }, intervalMs.value)
}

function stopPolling() {
  if (pollTimer != null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

onMounted(() => {
  // 设置未就绪时先按默认节奏轮询，settings 返回后由 watcher 校准
  startPolling()
  void settingsStore.fetch()
})

onUnmounted(stopPolling)

// 开关或间隔在设置页保存后变化：开关关 → 停轮询；间隔变 → 按新间隔重启
watch([enabled, intervalMs], () => {
  if (enabled.value) {
    startPolling()
  } else {
    stopPolling()
  }
})

/** 进度条与百分比共用同一 clamp：gopsutil 的 UsedPercent 偶发略超 100 */
function pct(v: number | undefined): number {
  if (v == null || !Number.isFinite(v)) return 0
  return Math.min(100, Math.max(0, v))
}

/** 百分比文本；无数据时给「—」占位，不闪 0% 误导 */
function pctText(v: number | undefined): string {
  return v == null || !Number.isFinite(v) ? '—' : `${Math.round(v)}%`
}

/** 字节数自适应单位；无数据时给「—」 */
function bytesText(v: number | undefined): string {
  if (v == null || !Number.isFinite(v) || v < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = v
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  // 三位数以上不再保留小数，避免卡片数字过宽
  return `${n >= 100 ? Math.round(n) : n.toFixed(1)} ${units[i]}`
}

/** 速率文本：字节/秒 → 自适应单位 */
function rateText(v: number | undefined): string {
  if (v == null || !Number.isFinite(v) || v < 0) return '—'
  return `${bytesText(v)}/s`
}

/** 运行时长：只取前两个有意义的单位（12 天 3 小时 / 5 小时 23 分），
 *  超过两个单位会让卡片数字过宽 */
function uptimeText(sec: number | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec < 0) return '—'
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  const mins = Math.floor((sec % 3600) / 60)
  if (days > 0) return `${days} 天 ${hours} 小时`
  if (hours > 0) return `${hours} 小时 ${mins} 分`
  return `${mins} 分`
}

/** 「已用 X / 共 Y」副信息；磁盘附分区路径 tooltip */
function usedTotalText(used: number | undefined, total: number | undefined): string {
  if (used == null || total == null) return '—'
  return `已用 ${bytesText(used)} / 共 ${bytesText(total)}`
}
</script>

<template>
  <section class="bg-surface rounded-xl shadow-sm border p-4 sm:p-5">
    <div class="flex flex-wrap items-center justify-between gap-2 mb-3">
      <div class="text-sm font-semibold text-fg">服务器资源</div>
      <div class="text-[11px] text-fg-muted">{{ Math.round(intervalMs / 1000) }} 秒刷新</div>
    </div>
    <div class="grid gap-3 sm:gap-4 grid-cols-2 lg:grid-cols-3">
      <!-- CPU：百分比 + 进度条，无副行（grid 行内自动等高对齐） -->
      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <Cpu class="size-3.5" aria-hidden="true" /> CPU
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ pctText(stats?.cpuPercent) }}</div>
        <Progress
          :model-value="pct(stats?.cpuPercent)"
          class="mt-2 h-1.5"
          aria-label="CPU 使用率"
        />
      </div>

      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <MemoryStick class="size-3.5" aria-hidden="true" /> 内存
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ pctText(stats?.memPercent) }}</div>
        <Progress
          :model-value="pct(stats?.memPercent)"
          class="mt-2 h-1.5"
          aria-label="内存使用率"
        />
        <div class="text-xs text-fg-subtle mt-1.5 tabular-nums">
          {{ usedTotalText(stats?.memUsed, stats?.memTotal) }}
        </div>
      </div>

      <!-- 磁盘：合计所有常规文件系统（排除 tmpfs/overlay 等），明细列在副行 -->
      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <HardDrive class="size-3.5" aria-hidden="true" /> 磁盘
          <span v-if="stats?.diskPath" class="font-normal text-fg-subtle truncate">{{ stats.diskPath }}</span>
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ pctText(stats?.diskPercent) }}</div>
        <Progress
          :model-value="pct(stats?.diskPercent)"
          class="mt-2 h-1.5"
          aria-label="磁盘使用率"
        />
        <div class="text-xs text-fg-subtle mt-1.5 tabular-nums">
          {{ usedTotalText(stats?.diskUsed, stats?.diskTotal) }}
        </div>
        <ul
          v-if="stats?.diskVolumes && stats.diskVolumes.length > 1"
          class="mt-1.5 space-y-0.5 text-[11px] text-fg-subtle"
        >
          <li
            v-for="vol in stats.diskVolumes"
            :key="vol.path"
            class="flex items-baseline justify-between gap-2 tabular-nums"
            :title="vol.fstype ? `${vol.path} (${vol.fstype})` : vol.path"
          >
            <span class="truncate min-w-0">{{ vol.path }}</span>
            <span class="shrink-0">{{ bytesText(vol.used) }}/{{ bytesText(vol.total) }}</span>
          </li>
        </ul>
      </div>

      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <ArrowUpFromLine class="size-3.5" aria-hidden="true" /> 上行
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ rateText(stats?.netUpRate) }}</div>
        <div class="text-xs text-fg-subtle mt-2 tabular-nums">
          累计 {{ bytesText(stats?.netUpTotal) }}
        </div>
      </div>

      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <ArrowDownToLine class="size-3.5" aria-hidden="true" /> 下行
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ rateText(stats?.netDownRate) }}</div>
        <div class="text-xs text-fg-subtle mt-2 tabular-nums">
          累计 {{ bytesText(stats?.netDownTotal) }}
        </div>
      </div>

      <!-- 运行时长：宿主开机时长，与面板自身进程无关 -->
      <div class="bg-elevated rounded-xl p-3 sm:p-4">
        <div class="flex items-center gap-1.5 text-xs text-fg-muted mb-1.5">
          <Clock class="size-3.5" aria-hidden="true" /> 运行时长
        </div>
        <div class="text-lg font-bold text-fg tabular-nums">{{ uptimeText(stats?.uptimeSeconds) }}</div>
        <div class="text-xs text-fg-subtle mt-2">主机开机运行</div>
      </div>
    </div>
  </section>
</template>

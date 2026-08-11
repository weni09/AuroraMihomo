<script setup lang="ts">
import { onMounted } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useMihomoRealtime } from '../composables/useMihomoRealtime'
import KernelActionBar from '../components/KernelActionBar.vue'
import KernelLogPreview from '../components/KernelLogPreview.vue'
import type { KernelLogLine } from '../components/KernelLogPreview.vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import api from '../api'
import { wsStatusLabel } from '../utils/labels'

const store = useMihomoStore()
// 与控制台共用同一订阅，避免一边实时一边过期
const { status: wsStatus } = useMihomoRealtime()

onMounted(async () => {
  await store.fetchStatus()
  // 进入页时补一段历史日志，随后靠 realtime 续写
  try {
    const res = await api.get<KernelLogLine[]>('/mihomo/logs')
    ;(res.data || []).slice(-40).forEach((l) => store.pushLog(l))
  } catch {
    /* 内核未起时忽略 */
  }
})

// 切换内核守护：由 store 落库并 toast
const onToggleBoot = (next: boolean | 'indeterminate') => {
  if (next === 'indeterminate') return
  void store.setBoot(next)
}
</script>

<template>
  <!-- 全宽布局（与控制台一致），不再 max-w-5xl 收窄 -->
  <main class="p-4 sm:p-6 lg:p-8 flex flex-col gap-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-2xl sm:text-3xl font-bold text-fg">内核管理</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">启动、停止、重载与更新 mihomo 内核</p>
      </div>
      <span class="text-xs px-2 py-1 rounded tint-neutral shrink-0">
        实时通道：{{ wsStatusLabel(wsStatus) }}
      </span>
    </div>

    <Card>
      <CardHeader>
        <CardTitle>运行状态</CardTitle>
        <CardDescription>进程状态随实时通道更新</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 sm:gap-4">
          <div class="bg-elevated rounded-lg p-4">
            <div class="text-xs text-fg-muted mb-1">状态</div>
            <!-- 不用 text-success/text-destructive：这两个 token 的深色值是为
                 「按钮实色底配白字」调的对比度，直接当小字叠在 bg-elevated
                 上时不够 WCAG AA 的 4.5:1，这里维持专门调过的色阶 -->
            <div
              class="font-bold"
              :class="
                store.status === 'running'
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-rose-600 dark:text-rose-400'
              "
            >
              {{ store.status === 'running' ? '运行中' : '已停止' }}
            </div>
          </div>
          <div class="bg-elevated rounded-lg p-4">
            <div class="text-xs text-fg-muted mb-1">版本</div>
            <div class="text-sm font-mono text-fg break-all">{{ store.version }}</div>
          </div>
          <div class="bg-elevated rounded-lg p-4">
            <div class="text-xs text-fg-muted mb-1">PID</div>
            <div class="font-mono text-fg">{{ store.pid || '—' }}</div>
          </div>
        </div>

        <!-- 启停/重启确认与更新逻辑已下沉到 KernelActionBar -->
        <KernelActionBar show-update />

        <!-- 内核守护（期望运行）：
             开启 = 检测到停止自动拉起（限次）、面板重启按期望拉回；
             关闭 = 手动停止后不再自动拉、面板重启也不拉。 -->
        <div class="mt-4 border-t border-line pt-4">
          <label class="flex items-center gap-3" data-testid="kernel-boot-switch">
            <Switch
              :model-value="store.desiredRunning === true"
              @update:model-value="onToggleBoot"
            />
            <span class="text-sm font-medium">内核守护（期望运行）</span>
          </label>
          <p class="text-xs text-fg-subtle mt-1">
            开启时，面板检测到内核异常停止会自动拉起（短时间内限次重试）；
            关闭后手动停止的内核不再自动拉，面板重启也不会拉起，直到手动启动或重新开启。
          </p>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader class="flex flex-row items-center justify-between gap-3 space-y-0">
        <CardTitle>运行日志</CardTitle>
        <RouterLink to="/logs" class="text-sm text-primary hover:underline shrink-0">
          完整日志面板
        </RouterLink>
      </CardHeader>
      <CardContent>
        <KernelLogPreview :lines="store.recentLogs" empty-text="暂无日志" />
      </CardContent>
    </Card>
  </main>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useNotifyStore } from '../stores/notify'
import api from '../api'
import { Button } from '@/components/ui/button'

const store = useMihomoStore()
const notify = useNotifyStore()
const updating = ref(false)

onMounted(() => store.fetchStatus())

// 设计 §4：版本更新
const updateCore = async () => {
  updating.value = true
  // 下载可能持续较久，先给一条提示，免得用户以为没响应；
  // 按钮本身也会切到"更新中…"并禁用
  notify.push('info', '正在下载最新内核…')
  try {
    const res = await api.post('/update/mihomo')
    const text = res.data?.message || '更新完成'
    if (res.data?.success === false) notify.error(text)
    else notify.success(text)
    await store.fetchStatus()
  } catch (e: any) {
    notify.error(e?.response?.data?.message || '更新失败')
  } finally {
    updating.value = false
  }
}

// 停止/重启会中断当前所有代理连接，需二次确认
const confirmStop = () => {
  if (confirm('停止内核将中断所有正在进行的代理连接，确定停止？')) store.stop()
}
const confirmRestart = () => {
  if (confirm('重启内核会短暂中断所有代理连接，确定重启？')) store.restart()
}
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8 max-w-5xl mx-auto space-y-6">
    <h1 class="text-2xl sm:text-3xl font-bold text-fg">内核管理</h1>

    <div class="bg-surface rounded-xl shadow-sm border p-4 sm:p-6">
      <h2 class="font-bold text-fg mb-4">运行状态</h2>
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 sm:gap-4 mb-6">
        <div class="bg-elevated rounded-lg p-4">
          <div class="text-xs text-fg-muted mb-1">状态</div>
          <!-- 不用 text-destructive/text-success：这两个 token 的深色值是为
               「按钮实色底配白字」调的对比度，直接当纯文字叠在 bg-elevated
               上时（2.82:1）不够 WCAG AA 的 4.5:1，这里维持专门调过的色阶 -->
          <div class="font-bold" :class="store.status === 'running' ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'">
            {{ store.status === 'running' ? '运行中' : '已停止' }}
          </div>
        </div>
        <div class="bg-elevated rounded-lg p-4">
          <div class="text-xs text-fg-muted mb-1">版本</div>
          <div class="text-sm font-mono text-fg break-all">{{ store.version }}</div>
        </div>
        <div class="bg-elevated rounded-lg p-4">
          <div class="text-xs text-fg-muted mb-1">进程号</div>
          <div class="font-mono text-fg">{{ store.pid || '—' }}</div>
        </div>
      </div>

      <!-- 窄屏收窄内边距，五个按钮才能排成两行而不是五行 -->
      <!-- 五个内核操作原先是五种实色按钮（绿/红/琥珀/蓝/靛），
           整块像调色板且分不出主次。改为：启动是此处主操作，
           停止属破坏性操作用 danger，其余为常规描边。 -->
      <div class="flex flex-wrap gap-2">
        <Button @click="store.start()">启动</Button>
        <Button variant="destructive" @click="confirmStop()">停止</Button>
        <Button variant="outline" @click="confirmRestart()">重启</Button>
        <Button variant="outline" @click="store.reload()">重载配置</Button>
        <Button variant="outline" :disabled="updating" @click="updateCore">
          {{ updating ? '更新中…' : '更新内核版本' }}
        </Button>
      </div>

      <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->
    </div>

    <div class="bg-surface rounded-xl shadow-sm border p-4 sm:p-6">
      <div class="flex items-center justify-between gap-3 mb-3">
        <h2 class="font-bold text-fg">运行日志</h2>
        <RouterLink to="/logs" class="text-sm text-primary hover:underline shrink-0">完整日志面板</RouterLink>
      </div>
      <!-- 终端观感在两种主题下都保留，见 DashboardView 同处说明 -->
      <div class="bg-slate-900 text-slate-100 rounded-lg p-3 h-64 sm:h-80 overflow-auto text-xs font-mono space-y-1">
        <div v-for="(l, i) in store.recentLogs" :key="i" class="break-words">
          <span class="text-slate-500">{{ l.time }}</span>
          <span class="text-cyan-400"> [{{ l.stream }}]</span>
          {{ l.message }}
        </div>
        <div v-if="store.recentLogs.length === 0" class="text-slate-500">暂无日志</div>
      </div>
    </div>
  </main>
</template>

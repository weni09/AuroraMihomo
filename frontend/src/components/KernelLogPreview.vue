<script setup lang="ts">
export interface KernelLogLine {
  time: string
  stream: string
  message: string
}

withDefaults(
  defineProps<{
    lines: KernelLogLine[]
    heightClass?: string
    emptyText?: string
  }>(),
  {
    heightClass: 'h-64 sm:h-80',
    emptyText: '暂无日志',
  },
)
</script>

<template>
  <!-- 终端观感：等宽深色底，两主题都保留。slate 仅用于本终端区（与 LogsView/Dashboard 同源），勿扩散到状态卡 -->
  <div
    class="bg-slate-900 text-slate-100 rounded-lg p-3 overflow-auto text-xs font-mono flex flex-col gap-1"
    :class="heightClass"
    role="log"
    aria-live="polite"
  >
    <div v-for="(l, i) in lines" :key="i" class="break-words">
      <span class="text-slate-500">{{ l.time }}</span>
      <span class="text-cyan-400"> [{{ l.stream }}]</span>
      {{ l.message }}
    </div>
    <div v-if="lines.length === 0" class="text-slate-500 italic">{{ emptyText }}</div>
  </div>
</template>

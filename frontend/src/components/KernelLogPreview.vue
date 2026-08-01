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
  <!-- 终端观感：两主题下都用深色等宽底（可读性优先）。
       不用 slate/gray 等中性色工具类（FE1）；色值用任意值，语义仍是「终端面板」。 -->
  <div
    class="rounded-lg p-3 overflow-auto text-xs font-mono flex flex-col gap-1 bg-[#0f172a] text-[#f1f5f9]"
    :class="heightClass"
    role="log"
    aria-live="polite"
  >
    <div v-for="(l, i) in lines" :key="i" class="break-words">
      <span class="text-[#64748b]">{{ l.time }}</span>
      <span class="text-cyan-400"> [{{ l.stream }}]</span>
      {{ l.message }}
    </div>
    <div v-if="lines.length === 0" class="text-[#64748b] italic">{{ emptyText }}</div>
  </div>
</template>

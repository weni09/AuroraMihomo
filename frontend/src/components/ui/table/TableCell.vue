<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string; label?: string }>()
</script>

<template>
  <!-- label 落到 data-label：窄屏卡片形态下由 CSS 取它作为行首列名。
       lg:px-4 而非 px-6：桌面表格在 1280px 视口（容器约 880px）要并排放下
       订阅表 7 列，px-6 的 48px 单列内距会让窄列（节点数/状态）几乎没有
       内容空间。px-4 的 32px 是视觉与空间的中值。 -->
  <td :data-label="props.label" :class="cn('align-top lg:px-4 lg:py-4', props.class)">
    <slot />
  </td>
</template>

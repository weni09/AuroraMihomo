<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string; label?: string }>()
</script>

<template>
  <!-- label 落到 data-label：窄屏卡片形态下由 CSS 取它作为行首列名 -->
  <td :data-label="props.label" :class="cn('align-top lg:px-6 lg:py-4', props.class)">
    <slot />
  </td>
</template>

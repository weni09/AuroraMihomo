<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <!-- responsive-table 是本项目既有的窄屏形态（lg 以下每行折成卡片，
       列名取 td 的 data-label），规则见 assets/main.css。
       七列表格在手机上无论如何压缩都读不了，故保留这一机制。 -->
  <table :class="cn('responsive-table w-full caption-bottom text-sm', props.class)">
    <slot />
  </table>
</template>

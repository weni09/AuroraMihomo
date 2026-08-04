<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <!-- 外包横向滚动容器：桌面形态下列总宽超视口时（如订阅表 7 列 + 固定操作列）
       可以横向滚动查看，而不是被外层 Card 的 overflow-hidden 裁掉最右列。
       responsive-table 的卡片形态（<1280px，见 assets/main.css）下每个 td
       是 block，列内元素不会撑出横向滚动。 -->
  <div class="w-full overflow-x-auto">
    <table :class="cn('responsive-table w-full caption-bottom text-sm', props.class)">
      <slot />
    </table>
  </div>
</template>

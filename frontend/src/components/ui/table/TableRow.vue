<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <tr :class="cn('transition-colors hover:bg-elevated/50', props.class)">
    <slot />
  </tr>
</template>

<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <div :class="cn('flex items-center p-4 pt-0 sm:p-5 sm:pt-0', props.class)">
    <slot />
  </div>
</template>

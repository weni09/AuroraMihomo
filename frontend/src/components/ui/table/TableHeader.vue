<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <thead :class="cn('bg-elevated', props.class)">
    <slot />
  </thead>
</template>

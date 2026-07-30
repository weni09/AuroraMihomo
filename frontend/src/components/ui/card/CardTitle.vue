<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <h3 :class="cn('font-semibold leading-tight tracking-tight text-fg', props.class)">
    <slot />
  </h3>
</template>

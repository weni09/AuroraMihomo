<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <tbody :class="cn('lg:divide-y lg:divide-line', props.class)">
    <slot />
  </tbody>
</template>

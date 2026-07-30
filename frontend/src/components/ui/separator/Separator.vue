<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string; orientation?: 'horizontal' | 'vertical' }>()
</script>

<template>
  <div
    role="separator"
    :aria-orientation="props.orientation ?? 'horizontal'"
    :class="
      cn(
        'shrink-0 bg-line',
        (props.orientation ?? 'horizontal') === 'horizontal' ? 'h-px w-full' : 'h-full w-px',
        props.class,
      )
    "
  />
</template>

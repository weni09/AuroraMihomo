<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string }>()
</script>

<template>
  <th
    scope="col"
    :class="
      cn(
        'px-4 py-3 text-left align-middle text-xs font-semibold uppercase tracking-wider text-fg-muted lg:px-6',
        props.class,
      )
    "
  >
    <slot />
  </th>
</template>

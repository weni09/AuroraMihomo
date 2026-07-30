<script setup lang="ts">
import { cn } from '@/lib/utils'

const props = defineProps<{ class?: string; colspan?: number }>()
</script>

<template>
  <tr>
    <td
      :colspan="props.colspan ?? 1"
      :class="cn('px-4 py-12 text-center text-sm italic text-fg-subtle', props.class)"
    >
      <slot />
    </td>
  </tr>
</template>

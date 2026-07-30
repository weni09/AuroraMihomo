<script setup lang="ts">
import { Sun, Moon, Monitor } from 'lucide-vue-next'
import { useTheme, type ThemeMode } from '../composables/useTheme'
import { Button } from '@/components/ui/button'

const { mode, setMode } = useTheme()

const options: Array<{ value: ThemeMode; label: string; icon: typeof Sun }> = [
  { value: 'light', label: '浅色', icon: Sun },
  { value: 'dark', label: '深色', icon: Moon },
  { value: 'system', label: '跟随系统', icon: Monitor },
]
</script>

<template>
  <!-- radiogroup 而非一排独立按钮：三个选项互斥，屏幕阅读器需要知道
       当前选中项与可选范围。用 aria-checked 而非 aria-pressed 与之对应。
       侧边栏与移动端顶栏现在都是 bg-surface 底，不再需要按位置分两套配色。 -->
  <div
    role="radiogroup"
    aria-label="界面主题"
    class="flex items-center gap-0.5 rounded-lg p-0.5 bg-elevated"
  >
    <Button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      role="radio"
      variant="ghost"
      size="sm"
      :aria-checked="mode === opt.value"
      :title="opt.label"
      class="flex-1 rounded-md h-auto py-1.5"
      :class="
        mode === opt.value
          ? 'bg-surface text-primary shadow-sm hover:bg-surface'
          : 'text-fg-muted hover:text-fg'
      "
      @click="setMode(opt.value)"
    >
      <component :is="opt.icon" class="h-4 w-4" aria-hidden="true" />
      <span class="sr-only">{{ opt.label }}</span>
    </Button>
  </div>
</template>

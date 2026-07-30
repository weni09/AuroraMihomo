<script setup lang="ts">
import { computed } from 'vue'
// lucide 0.300 用的是旧命名（AlertCircle / CheckCircle），
// 不是新版的 CircleX / CircleCheck
import { AlertCircle, CheckCircle, Info, X } from 'lucide-vue-next'
import { useNotifyStore, type Toast } from '../stores/notify'
import { Button } from '@/components/ui/button'

const notify = useNotifyStore()

// note-* 在 assets/main.css 里定义，自带深浅两套配色
const style = (level: Toast['level']) =>
  level === 'error' ? 'note-err' : level === 'success' ? 'note-ok' : 'note-neutral'

const icon = (level: Toast['level']) =>
  level === 'error' ? AlertCircle : level === 'success' ? CheckCircle : Info

/**
 * 按级别分成两组渲染。
 *
 * 错误走 role="alert"（assertive，立即打断当前朗读），成功与提示走
 * role="status"（polite，等朗读间隙）。混在一个 region 里只能二选一：
 * 全 assertive 会让每条成功提示都打断用户，全 polite 则请求失败可能
 * 迟迟不播报——而 API 失败经拦截器汇入这里，是唯一的失败信号。
 */
// live 需标注为字面量类型，否则推导成 string 而与 aria-live 的取值不兼容
const groups = computed(
  (): Array<{ key: string; role: string; live: 'assertive' | 'polite'; items: Toast[] }> => [
    {
      key: 'error',
      role: 'alert',
      live: 'assertive',
      items: notify.toasts.filter((t) => t.level === 'error'),
    },
    {
      key: 'other',
      role: 'status',
      live: 'polite',
      items: notify.toasts.filter((t) => t.level !== 'error'),
    },
  ],
)
</script>

<template>
  <!-- z-[60] 高于移动端抽屉（z-40）、其遮罩（z-30）与弹窗（z-50 / z-[55]）：
       抽屉展开或弹窗打开时若触发请求失败，提示不能被遮住 -->
  <div class="safe-inset-tr fixed z-[60] space-y-2 w-[min(92vw,26rem)]">
    <!-- 两个 live region 必须常驻挂载（不随 toast 一起 v-if）：辅助技术只
         播报已存在 region 内的增量变化，region 与内容同时出现时首条往往不播报 -->
    <div
      v-for="g in groups"
      :key="g.key"
      :role="g.role"
      :aria-live="g.live"
      class="space-y-2"
    >
      <TransitionGroup
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0 translate-x-4"
        leave-active-class="transition duration-150 ease-in"
        leave-to-class="opacity-0"
      >
        <div
          v-for="t in g.items"
          :key="t.id"
          class="border rounded-lg shadow-sm px-4 py-3 flex items-start gap-3"
          :class="style(t.level)"
        >
          <component :is="icon(t.level)" class="h-5 w-5 shrink-0" aria-hidden="true" />
          <span class="text-sm flex-1 break-words">{{ t.text }}</span>
          <!-- icon-sm 默认 36x36，配 items-start 的单行短文案会把卡片撑高；
               收紧到贴合图标的紧凑尺寸，保留 tap-target 承担触屏热区 -->
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="tap-target h-auto w-auto shrink-0 p-1 text-current opacity-50 hover:opacity-100 hover:bg-transparent focus-visible:ring-current"
            aria-label="关闭提示"
            @click="notify.dismiss(t.id)"
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </TransitionGroup>
    </div>
  </div>
</template>

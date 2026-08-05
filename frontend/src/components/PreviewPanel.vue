<script setup lang="ts">
import { computed, ref } from 'vue'
import { X } from 'lucide-vue-next'
import CodeEditor from './CodeEditor.vue'
import ModalDialog from './ModalDialog.vue'
import type { PreviewResult } from '../stores/preview'
import { useCopy } from '../composables/useCopy'
import { Button } from '@/components/ui/button'
import { DialogTitle } from '@/components/ui/dialog'

/**
 * 预览结果对话框：展示处理前后的内容对照。
 *
 * 三个页面（单个订阅 / 组合订阅 / 模板文件）共用，避免各写一份
 * 而在字段含义上产生分歧。
 *
 * 用弹窗而非页面内嵌面板：此前预览结果直接显示在列表下方，
 * 用户点「预览」后要往下滚才能看到，且在新建/编辑弹窗里点预览时，
 * 结果出现在弹窗背后的页面上，视觉上完全对不上。改成弹窗后
 * 预览永远浮在最上层，点哪里预览、结果就在哪里弹出。
 *
 * loading/error/result 三者任一非空就代表“预览应该显示”，
 * 因此不需要额外的 open prop，直接复用 preview store 的状态即可。
 */
const props = defineProps<{
  result: PreviewResult | null
  loading?: boolean
  error?: string
}>()

const emit = defineEmits<{ close: [] }>()

const isOpen = computed(() => !!(props.loading || props.error || props.result))

// Escape 关闭、焦点管理与背景滚动锁定统一由 ModalDialog 承担，此处不再自建

// 默认看处理结果：多数时候用户关心的是最终产物
const tab = ref<'processed' | 'original'>('processed')

const body = computed(() => {
  if (!props.result) return ''
  return tab.value === 'processed' ? props.result.processed : props.result.original
})

// 前后内容一致时提示一句，免得用户以为管道没生效是页面出错
const unchanged = computed(
  () => !!props.result && props.result.original === props.result.processed,
)

const { copy } = useCopy()

const onCopy = () => copy(body.value, '内容')
</script>

<template>
  <!-- 套用 ModalDialog 以复用其 role/aria-modal、焦点陷阱与滚动锁定。
       这里的“标题栏”承载格式/节点数/切换 tab 等预览专属操作，
       故走 #header slot 而非默认标题结构。
       z-[55]：高于 ModalDialog/ShareDialog 默认的 z-50。
       新建/编辑表单里的「即时预览」按钮就在那层弹窗内部触发，
       预览结果必须盖在它上面，否则会被表单弹窗挡住看不见。 -->
  <ModalDialog
    :open="isOpen"
    max-width="max-w-3xl"
    z-index-class="z-[55]"
    @close="emit('close')"
  >
    <template #header>
      <!-- 头部布局：移动端两行式，桌面单行。
           原单行 flex-wrap + ml-auto 在窄屏换行后错位：关闭钮被挤到
           新行、复制钮位置漂浮。用 order 重排——order-2 关闭钮紧随
           标题（ml-auto 推右），order-3 操作组 w-full 强制换行为第二行；
           lg 以上 w-auto 回到同一行、关闭钮经 lg:order-4 排回最右。 -->
      <div class="flex flex-wrap items-center gap-3 px-4 py-3 border-b border-line bg-elevated rounded-t-xl shrink-0">
        <DialogTitle class="order-1 text-sm font-semibold text-fg">即时预览</DialogTitle>

        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="order-2 lg:order-4 tap-target ml-auto text-fg-muted hover:text-fg"
          aria-label="关闭"
          @click="emit('close')"
        >
          <X class="!h-5 !w-5" aria-hidden="true" />
        </Button>

        <div
          v-if="result"
          class="order-3 flex w-full flex-wrap items-center gap-2 lg:w-auto lg:gap-3"
        >
          <div class="flex rounded-md border border-line-strong overflow-hidden text-xs" role="tablist">
            <Button
              type="button"
              role="tab"
              :aria-selected="tab === 'processed'"
              :variant="tab === 'processed' ? 'default' : 'ghost'"
              size="sm"
              class="rounded-none"
              @click="tab = 'processed'"
            >
              处理后
            </Button>
            <Button
              type="button"
              role="tab"
              :aria-selected="tab === 'original'"
              :variant="tab === 'original' ? 'default' : 'ghost'"
              size="sm"
              class="rounded-none border-l border-line-strong"
              @click="tab = 'original'"
            >
              处理前
            </Button>
          </div>
          <span class="text-xs text-fg-muted bg-surface border border-line rounded px-2 py-0.5">
            格式: <span class="font-mono">{{ result.format }}</span>
          </span>
          <span
            v-if="result.count > 0"
            class="text-xs text-fg-muted bg-surface border border-line rounded px-2 py-0.5"
          >
            节点数: <strong class="text-fg">{{ result.count }}</strong>
          </span>
          <Button variant="outline" size="sm" @click="onCopy">复制内容</Button>
        </div>
      </div>
    </template>

    <!-- 正文区：ModalDialog 已提供内边距与滚动，这里只关心内容 -->
    <p v-if="loading" class="text-sm text-fg-muted">正在生成预览…</p>
    <p v-else-if="error" class="text-sm text-rose-600 dark:text-rose-400 whitespace-pre-wrap">{{ error }}</p>
    <template v-else-if="result">
      <ul v-if="result.warnings?.length" class="mb-3 space-y-1">
        <li v-for="w in result.warnings" :key="w" class="text-xs text-amber-600 dark:text-amber-400">{{ w }}</li>
      </ul>
      <p v-if="unchanged && tab === 'processed'" class="mb-2 text-xs text-fg-subtle">
        处理前后内容一致（未配置处理管道，或管道未改变任何内容）。
      </p>
      <!-- 只读编辑器而非 pre：预览产物动辄上千行，需要行号与语法高亮。
           不指定 dark，跟随全局主题 -->
      <CodeEditor :model-value="body" language="yaml" height="min(60vh, 500px)" readonly />
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, useSlots } from 'vue'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

/**
 * 通用弹窗外壳，供订阅/组合/文件的「新建、编辑」表单与分享、预览面板复用。
 *
 * 基于 shadcn-vue 的 Dialog（reka-ui DialogRoot/DialogContent）封装，不再自建
 * 焦点陷阱/Escape监听/滚动锁定——这些能力由 reka-ui 的 FocusScope、
 * DismissableLayer、Presence 原生提供，此前手写的等价逻辑已删除。
 *
 * 正文与操作按钮都放在默认 slot 里，不单独拆 footer：
 * 各表单本身的按钮布局差异较大（订阅只有保存/预览/取消，
 * 文件还要看是否处于编辑态决定要不要显示取消），拆 footer slot
 * 反而要在调用处重复传参数，不如直接把按钮写在正文末尾。
 */
withDefaults(
  defineProps<{
    open: boolean
    /** 标题文本。使用 #header slot 时可留空，由调用方自行提供 DialogTitle 承担可访问名称 */
    title?: string
    /** 弹窗宽度，Tailwind 的 max-w-* 类名 */
    maxWidth?: string
    /**
     * 遮罩与面板层级。默认 z-50。
     * 预览面板需要更高的值：它从表单弹窗内部触发，必须盖在那一层之上。
     */
    zIndexClass?: string
  }>(),
  { title: '', maxWidth: 'max-w-2xl', zIndexClass: 'z-50' },
)

const emit = defineEmits<{ close: [] }>()

const slots = useSlots()
// 使用 #header slot 的调用方（ShareDialog/PreviewPanel）会自带关闭按钮，
// 这时隐藏 DialogContent 内置的右上角关闭按钮，避免同一弹窗出现两个入口。
const hasCustomHeader = computed(() => !!slots.header)

// Dialog 的 open 由调用方持有并透传（如 dialogOpen、!!shareTarget），本组件
// 不直接翻转它——reka-ui 在受控模式下只会转发 update:open，实际关闭仍需
// 调用方把自己的状态置为 false，这与迁移前 @close 的契约一致。
function onOpenChange(next: boolean) {
  if (!next) emit('close')
}
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent
      :class="cn('flex flex-col p-0 gap-0 max-h-[85dvh] sm:max-h-[90dvh]', zIndexClass, maxWidth)"
      :overlay-class="zIndexClass"
      :hide-close="hasCustomHeader"
    >
      <slot name="header">
        <DialogHeader class="px-4 sm:px-6 py-4 border-b border-line shrink-0 space-y-0 text-left">
          <DialogTitle class="text-lg font-bold text-fg truncate">{{ title }}</DialogTitle>
        </DialogHeader>
      </slot>

      <div class="px-4 sm:px-6 py-5 overflow-y-auto min-h-0 flex-1">
        <slot />
      </div>
    </DialogContent>
  </Dialog>
</template>

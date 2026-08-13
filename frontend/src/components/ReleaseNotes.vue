<script setup lang="ts">
import { ref, watch } from 'vue'
import MarkdownIt from 'markdown-it'

/**
 * 主程序升级确认弹窗里的变更日志渲染。
 *
 * 独立成组件并整体懒加载（SettingsView 用 defineAsyncComponent 引入）：
 * markdown-it 约 40KB，只有打开升级弹窗才需要，静态引入会让设置页首屏
 * 为它付费。DocsView 用路由懒加载做同样的事，这里组件级分包。
 *
 * 渲染时关闭 html 选项：release notes 来自远端仓库，关闭让外部注入不成立。
 */
const props = defineProps<{
  /** GitHub release notes（markdown 源文本） */
  notes: string
}>()

const md = new MarkdownIt({ html: false, linkify: true, typographer: false })

const html = ref('')
watch(
  () => props.notes,
  (notes) => {
    if (!notes) {
      html.value = ''
      return
    }
    try {
      html.value = md.render(notes)
    } catch (e) {
      console.error('渲染变更日志失败', e)
      html.value = ''
    }
  },
  { immediate: true },
)
</script>

<template>
  <div class="release-notes text-xs text-fg-subtle max-h-64 overflow-y-auto" v-html="html" />
</template>

<style scoped>
/* 变更日志（markdown 渲染结果）样式。内容经 v-html 注入，
   scoped 选择器默认作用不到，须用 :deep。与 DocsView 的 .doc-body
   保持同套视觉，但只覆盖 release notes 可能出现的元素子集。 */
.release-notes :deep(h1) {
  @apply mb-2 mt-0 text-base font-bold;
}
.release-notes :deep(h2) {
  @apply mb-2 mt-4 text-sm font-bold;
}
.release-notes :deep(h3),
.release-notes :deep(h4) {
  @apply mb-1 mt-3 font-semibold;
}
.release-notes :deep(p) {
  @apply my-2 leading-relaxed;
}
.release-notes :deep(ul) {
  @apply my-2 list-disc space-y-1 pl-5;
}
.release-notes :deep(ol) {
  @apply my-2 list-decimal space-y-1 pl-5;
}
.release-notes :deep(li) {
  @apply leading-relaxed;
}
.release-notes :deep(a) {
  @apply text-accent-text underline underline-offset-2;
}
.release-notes :deep(strong) {
  @apply font-semibold;
}
.release-notes :deep(code) {
  @apply rounded bg-elevated px-1 py-0.5 font-mono text-[0.85em];
}
.release-notes :deep(pre) {
  @apply my-3 overflow-x-auto rounded bg-elevated p-2;
}
.release-notes :deep(pre code) {
  @apply bg-transparent p-0 text-xs;
}
.release-notes :deep(blockquote) {
  @apply my-3 border-l-4 border-accent/40 bg-elevated/50 py-1 pl-3 pr-2;
}
.release-notes :deep(blockquote p) {
  @apply my-1;
}
.release-notes :deep(hr) {
  @apply my-4 border-line;
}
.release-notes :deep(table) {
  @apply w-full border-collapse text-xs;
}
.release-notes :deep(th),
.release-notes :deep(td) {
  @apply border border-line px-2 py-1 text-left align-top;
}
</style>

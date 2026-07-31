<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import { EditorView, lineNumbers } from '@codemirror/view'
import { MergeView } from '@codemirror/merge'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { yaml as yamlLang } from '@codemirror/lang-yaml'
import { json as jsonLang } from '@codemirror/lang-json'
import { oneDark } from '@codemirror/theme-one-dark'
import { useTheme } from '../composables/useTheme'

/**
 * 并排差异视图（CodeMirror 6 的 MergeView）。
 *
 * 与 CodeEditor 分开而非在其中加分支：MergeView 管理的是两个
 * EditorView 且构造参数完全不同（a/b 而非单个 doc），塞进同一个组件
 * 会让两条互不相干的初始化路径缠在一起。
 *
 * 两侧都只读：这里是核对用的视图，改配置去配置中心。
 */
const props = withDefaults(
  defineProps<{
    /** 左侧（原始/基准）文本 */
    original: string
    /** 右侧（对照）文本 */
    modified: string
    language?: 'yaml' | 'json'
    /** 期望高度，CSS 尺寸值 */
    height?: string
    /** 折叠无差异的区域，只留少量上下文行 */
    collapseUnchanged?: boolean
  }>(),
  {
    language: 'yaml',
    height: 'min(600px, 60vh)',
    collapseUnchanged: true,
  },
)

const { isDark } = useTheme()

const host = ref<HTMLDivElement | null>(null)
let view: MergeView | null = null

// 主题要能热替换。MergeView 的两个子视图各自需要一个 Compartment——
// 同一个 Compartment 实例不能装进两个 EditorState，否则 reconfigure
// 只会作用到其中一个，切换主题时会出现"一半深色一半浅色"。
const themeA = new Compartment()
const themeB = new Compartment()

function languageExtension(): Extension[] {
  return props.language === 'json' ? [jsonLang()] : [yamlLang()]
}

function themeExtension(): Extension {
  return isDark.value ? oneDark : syntaxHighlighting(defaultHighlightStyle)
}

function baseExtensions(themeCompartment: Compartment): Extension[] {
  return [
    lineNumbers(),
    ...languageExtension(),
    themeCompartment.of(themeExtension()),
    EditorView.editable.of(false),
    EditorState.readOnly.of(true),
    EditorView.lineWrapping,
  ]
}

function create() {
  if (!host.value) return
  view = new MergeView({
    a: {
      doc: props.original,
      extensions: baseExtensions(themeA),
    },
    b: {
      doc: props.modified,
      extensions: baseExtensions(themeB),
    },
    parent: host.value,
    // 高亮改动的具体字符而非整行，便于看清"值改了哪一部分"
    highlightChanges: true,
    gutter: true,
    // 相同区域折叠成一行提示，留 2 行上下文。
    // 配置文件里绝大多数内容是相同的，不折叠的话要滚很久才能翻到下一处差异。
    collapseUnchanged: props.collapseUnchanged ? { margin: 2, minSize: 4 } : undefined,
  })
}

function destroy() {
  view?.destroy()
  view = null
}

onMounted(create)
onBeforeUnmount(destroy)

// 文本变化时直接改写文档，而不是重建 MergeView：
// 重建会丢掉滚动位置与折叠状态，用户点"重新加载"后又得重新找到刚看的位置。
watch(
  () => ({ a: props.original, b: props.modified }),
  ({ a, b }) => {
    if (!view) return
    const setDoc = (v: EditorView, text: string) => {
      if (v.state.doc.toString() === text) return
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: text } })
    }
    setDoc(view.a, a)
    setDoc(view.b, b)
  },
)

// 语言与折叠开关的变化需要重建：它们不在 Compartment 里，
// 而这两项改动本身就意味着换了一份对比对象，丢掉滚动位置可以接受。
watch(
  () => [props.language, props.collapseUnchanged],
  () => {
    destroy()
    create()
  },
)

watch(isDark, () => {
  if (!view) return
  view.a.dispatch({ effects: themeA.reconfigure(themeExtension()) })
  view.b.dispatch({ effects: themeB.reconfigure(themeExtension()) })
})

const hostStyle = computed(() => ({ height: props.height }))
</script>

<template>
  <div ref="host" class="diff-host overflow-auto text-xs" :style="hostStyle" />
</template>

<style scoped>
/* MergeView 默认两栏各占一半，但内层 .cm-editor 需要显式撑满高度，
   否则内容少时编辑器只有几行高、差异高亮的背景断在中间。 */
.diff-host :deep(.cm-mergeView),
.diff-host :deep(.cm-mergeViewEditors) {
  height: 100%;
}
.diff-host :deep(.cm-editor) {
  height: 100%;
}
/* 折叠占位行用主题 token，避免在深浅两套主题下都是同一种灰 */
.diff-host :deep(.cm-collapsedLines) {
  background-color: rgb(var(--c-elevated));
  color: rgb(var(--c-fg-muted));
}
</style>

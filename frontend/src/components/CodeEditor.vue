<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import { EditorView, keymap, lineNumbers, highlightActiveLine, placeholder as cmPlaceholder } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { syntaxHighlighting, defaultHighlightStyle, indentUnit, bracketMatching, StreamLanguage } from '@codemirror/language'
import { yaml as yamlLang } from '@codemirror/lang-yaml'
import { javascript as jsLang } from '@codemirror/lang-javascript'
import { json as jsonLang } from '@codemirror/lang-json'
// CodeMirror 6 官方没有独立的 ini 语言包，legacy-modes 的 properties 模式
// 语法上与 ini 基本一致（key=value + [section] + # 注释），是标准的替代选择
import { properties as iniMode } from '@codemirror/legacy-modes/mode/properties'
import { oneDark } from '@codemirror/theme-one-dark'
import { useTheme } from '../composables/useTheme'

/**
 * 通用代码编辑组件（CodeMirror 6）。
 *
 * 统一替换项目里用于编辑 YAML / JavaScript 的裸 textarea，提供语法高亮、
 * 行号、括号匹配、撤销重做与 Tab 缩进。
 *
 * 用 v-model 双向绑定文本内容，行为上尽量贴近原来的 textarea，
 * 以便各处调用方改动最小。
 */
const props = withDefaults(
  defineProps<{
    modelValue: string
    /** 语言模式，决定语法高亮规则。'text' 为纯文本，不挂任何语言扩展 */
    language?: 'yaml' | 'javascript' | 'json' | 'ini' | 'text'
    /** 期望高度，CSS 尺寸值 */
    height?: string
    /** 强制深色主题。不传时跟随全局主题 */
    dark?: boolean
    placeholder?: string
    readonly?: boolean
  }>(),
  {
    language: 'yaml',
    height: '260px',
    dark: undefined,
    placeholder: '',
    readonly: false,
  },
)

const { isDark } = useTheme()

// 未显式指定 dark 时跟随全局主题：浅色面板里嵌一块深色编辑器（或反之）
// 会显得割裂，而个别调用方仍需强制深色
const useDarkTheme = computed(() => props.dark ?? isDark.value)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const host = ref<HTMLDivElement | null>(null)
let view: EditorView | null = null

// 主题需要在运行时热替换。CodeMirror 的扩展集合是不可变的，
// 只有装进 Compartment 才能用 reconfigure 局部替换；
// 重建整个 EditorView 会丢掉光标位置与撤销栈。
const themeCompartment = new Compartment()

// 语言同理需要热替换：FilesView 里切换「文件格式」下拉时，同一个
// CodeEditor 实例会换用不同的 language prop，若不重配置，编辑器会
// 一直按挂载时的旧语言高亮，直到组件被销毁重建（用户完全看不出切换生效了）。
const languageCompartment = new Compartment()

function languageExtension(): Extension[] {
  switch (props.language) {
    case 'javascript':
      return [jsLang()]
    case 'json':
      return [jsonLang()]
    case 'ini':
      return [StreamLanguage.define(iniMode)]
    // 纯文本不挂语言扩展：没有对应语法可高亮，硬套一个只会显示误导性的着色
    case 'text':
      return []
    default:
      // YAML 缩进敏感，禁止用 Tab 字符，统一两空格
      return [yamlLang()]
  }
}

// 深色用 oneDark；浅色用默认高亮样式。fallback 保证没有语言支持时也有基础着色
function themeExtension(): Extension {
  return useDarkTheme.value
    ? oneDark
    : syntaxHighlighting(defaultHighlightStyle, { fallback: true })
}

function buildExtensions(): Extension[] {
  const ext: Extension[] = [
    lineNumbers(),
    highlightActiveLine(),
    history(),
    bracketMatching(),
    // indentWithTab 放在默认 keymap 之前，保证 Tab 用于缩进而非移动焦点。
    // 注意：这会影响键盘用户的 Tab 跳转，因此下面额外提供 Esc 释放焦点。
    keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
    indentUnit.of('  '),
    languageCompartment.of(languageExtension()),
    EditorView.lineWrapping,
    EditorState.readOnly.of(props.readonly),
    themeCompartment.of(themeExtension()),
    // 内容变化时同步回 v-model
    EditorView.updateListener.of((u) => {
      if (!u.docChanged) return
      emit('update:modelValue', u.state.doc.toString())
    }),
  ]

  if (props.placeholder) ext.push(cmPlaceholder(props.placeholder))

  return ext
}

onMounted(() => {
  if (!host.value) return
  view = new EditorView({
    state: EditorState.create({ doc: props.modelValue ?? '', extensions: buildExtensions() }),
    parent: host.value,
  })
})

// 主题切换：只重配置 Compartment，编辑器实例与文档状态原样保留
watch(useDarkTheme, () => {
  view?.dispatch({ effects: themeCompartment.reconfigure(themeExtension()) })
})

// language prop 变化时同样走 Compartment 热替换，不重建编辑器实例，
// 保留用户当前的光标位置与撤销栈
watch(
  () => props.language,
  () => {
    view?.dispatch({ effects: languageCompartment.reconfigure(languageExtension()) })
  },
)

// 外部改值时同步进编辑器；跳过与当前文档相同的值，避免打断用户输入与光标位置
watch(
  () => props.modelValue,
  (val) => {
    if (!view) return
    const cur = view.state.doc.toString()
    if (val === cur) return
    view.dispatch({ changes: { from: 0, to: cur.length, insert: val ?? '' } })
  },
)

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})
</script>

<template>
  <!-- Esc 让键盘用户能退出编辑器继续 Tab 导航，避免 Tab 被缩进独占造成焦点陷阱。
       必须 .stop：本组件常渲染在 ModalDialog 内（配置中心、文件编辑等表单），
       Esc 冒泡上去会被弹窗当成「关闭」，用户本想退出编辑器却丢掉整份草稿。 -->
  <div
    ref="host"
    class="cm-host border border-line rounded overflow-hidden text-sm"
    :style="{ height }"
    @keydown.esc.stop="($event.target as HTMLElement)?.blur()"
  ></div>
</template>

<style scoped>
/* 让 CodeMirror 撑满容器并在超长内容时内部滚动 */
.cm-host :deep(.cm-editor) {
  height: 100%;
}
.cm-host :deep(.cm-scroller) {
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  line-height: 1.6;
}
.cm-host :deep(.cm-editor.cm-focused) {
  outline: none;
}
</style>

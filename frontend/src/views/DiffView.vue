<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useConfigLabStore } from '../stores/configlab'
import CodeEditor from '../components/CodeEditor.vue'
import DiffEditor from '../components/DiffEditor.vue'
import { Button } from '@/components/ui/button'
import { normalizeYamlPairForDiff } from '../utils/yaml'

const store = useConfigLabStore()
onMounted(() => store.fetchConfigs())

/**
 * 三份配置的核对页面。
 *
 * 本地配置 = 用户在配置中心填写的原始内容；
 * 最终配置 = 合并后 mihomo 内核实际加载的内容；
 * 远程订阅 = 当前远程来源拉取后的原始层（合并前）。
 *
 * 两种看法：
 *   - 三栏对照：各看全文，确认内容本身符合预期
 *   - 并排差异：算出两份之间到底哪里不同，回答"合并改了什么"
 *
 * 都是只读展示，改动请去配置中心。
 */
type View = 'panels' | 'local-final' | 'final-remote'
const view = ref<View>('panels')

/**
 * 差异对比前先规范化。
 *
 * 本地配置是手写原文（带注释、自定义键序），最终配置是后端 yaml.Marshal
 * 的产物（注释丢失、键序重排）。直接比原始文本，差异列表会被这类无意义的
 * 行淹没，真正的内容变化反而找不到。规范化把两侧过同一遍解析 + 重序列化 +
 * 键排序，只留真实差异。
 *
 * 留成开关而非固定行为：排查"为什么我的注释没了"这类序列化问题时，
 * 仍需要看原始逐行差异。
 */
const normalize = ref(true)

const panels = computed(() => [
  { key: 'local', title: '本地配置', content: store.localContent, filename: 'local-config.yaml' },
  { key: 'final', title: '最终配置', content: store.finalContent, filename: 'final-config.yaml' },
  { key: 'remote', title: '远程订阅', content: store.remoteContent, filename: 'remote-config.yaml' },
])

/** 当前对比的两侧；三栏对照模式下为 null */
const pair = computed(() => {
  if (view.value === 'local-final') {
    return {
      leftTitle: '本地配置',
      rightTitle: '最终配置',
      left: store.localContent,
      right: store.finalContent,
      // 说明这组差异该怎么读，否则容易误解成"合并把我的配置改坏了"
      hint: '右侧多出的内容来自远程订阅与合并策略；左侧独有的项若在右侧消失，说明被远程层覆盖，或因引用失效被清理。',
    }
  }
  if (view.value === 'final-remote') {
    return {
      leftTitle: '最终配置',
      rightTitle: '远程订阅',
      left: store.finalContent,
      right: store.remoteContent,
      hint: '远程订阅是合并前的原始层。左侧独有的内容来自本地配置，以及合并时的补充（如透明代理注入）。',
    }
  }
  return null
})

/** 对比两侧缺内容时给出针对性提示，而不是渲染一个空的差异视图 */
const pairMissing = computed(() => {
  const p = pair.value
  if (!p) return ''
  const missing: string[] = []
  if (!p.left.trim()) missing.push(p.leftTitle)
  if (!p.right.trim()) missing.push(p.rightTitle)
  if (!missing.length) return ''
  return `${missing.join('、')}尚无内容，无法对比。`
})

/**
 * 交给 DiffEditor 的两侧文本。
 *
 * 成对规范化而非各自独立处理：只要有一侧解析失败，两侧都退回原文，
 * 否则会拿规范化文本去比原始文本，噪声全部回到差异里（详见
 * utils/yaml.ts 的 normalizeYamlPairForDiff）。
 */
const prepared = computed(() => {
  const p = pair.value
  if (!p) return null
  if (!normalize.value) return { left: p.left, right: p.right, normalized: false }
  return normalizeYamlPairForDiff(p.left, p.right)
})

/** 勾了规范化却没生效（有一侧解析不了），需要如实告知，否则用户以为开关坏了 */
const normalizeFailed = computed(
  () => normalize.value && !pairMissing.value && prepared.value?.normalized === false,
)

/** 两侧完全一致时明确告知——空的差异视图看起来像没加载出来 */
const identical = computed(() => {
  const p = prepared.value
  if (!p || pairMissing.value) return false
  return p.left === p.right
})

const download = (content: string, filename: string) => {
  const blob = new Blob([content], { type: 'text/yaml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 立即撤销会有小概率打断下载：click() 触发的保存动作是异步的，
  // 延迟撤销几乎不占内存又更保险
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

const tabs: { key: View; label: string }[] = [
  { key: 'panels', label: '三栏对照' },
  { key: 'local-final', label: '本地 → 最终' },
  { key: 'final-remote', label: '最终 → 远程' },
]
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
      <h1 class="text-2xl sm:text-3xl font-bold">配置差异</h1>
      <Button
        variant="outline"
        size="sm"
        :disabled="store.loadingConfigs"
        @click="store.fetchConfigs()"
      >
        {{ store.loadingConfigs ? '加载中…' : '重新加载' }}
      </Button>
    </div>

    <p v-if="store.configsError" class="mb-4 text-sm note-err border rounded px-3 py-2">
      {{ store.configsError }}
    </p>

    <div class="flex flex-wrap items-center gap-3 mb-4">
      <div class="inline-flex rounded border border-line overflow-hidden" role="tablist">
        <button
          v-for="(t, i) in tabs"
          :key="t.key"
          type="button"
          role="tab"
          :aria-selected="view === t.key"
          class="px-3 py-1.5 text-sm transition-colors"
          :class="[
            view === t.key ? 'bg-primary text-primary-foreground' : 'hover:bg-elevated',
            i > 0 ? 'border-l border-line' : '',
          ]"
          @click="view = t.key"
        >
          {{ t.label }}
        </button>
      </div>

      <!-- 规范化只对差异视图有意义；三栏对照展示的是原文 -->
      <label v-if="pair" class="flex items-center gap-1.5 text-sm">
        <input v-model="normalize" type="checkbox" class="rounded border-line" />
        <span>忽略注释与键序差异</span>
      </label>
    </div>

    <!-- 三栏对照 -->
    <div v-if="!pair" class="grid grid-cols-1 lg:grid-cols-3 gap-4">
      <section
        v-for="p in panels"
        :key="p.key"
        class="bg-surface border border-line rounded-xl overflow-hidden"
      >
        <div
          class="flex items-center justify-between gap-2 px-3 py-2 border-b border-line bg-elevated"
        >
          <span class="text-sm font-semibold text-fg">{{ p.title }}</span>
          <Button
            variant="outline"
            size="sm"
            class="h-7 px-2 text-xs"
            :disabled="!p.content"
            @click="download(p.content, p.filename)"
          >
            下载
          </Button>
        </div>
        <!-- 远程订阅没配置来源、或本地/最终尚未生成过时都会是空字符串，
             此时不渲染空编辑器（看起来像加载卡住），改为提示文字 -->
        <CodeEditor
          v-if="p.content"
          :model-value="p.content"
          language="yaml"
          height="min(600px, 60vh)"
          readonly
        />
        <p v-else class="p-4 text-sm text-fg-subtle">
          {{
            p.key === 'remote'
              ? '未配置远程来源，或尚未拉取过。'
              : '尚未生成，请先在配置中心保存并应用。'
          }}
        </p>
      </section>
    </div>

    <!-- 并排差异 -->
    <section v-else class="bg-surface border border-line rounded-xl overflow-hidden">
      <div class="px-3 py-2 border-b border-line bg-elevated">
        <div class="flex items-center gap-2 text-sm font-semibold text-fg">
          <span>{{ pair.leftTitle }}</span>
          <span class="text-fg-subtle">→</span>
          <span>{{ pair.rightTitle }}</span>
        </div>
        <p class="text-xs text-fg-subtle mt-1">{{ pair.hint }}</p>
      </div>

      <p v-if="pairMissing" class="p-4 text-sm text-fg-subtle">{{ pairMissing }}</p>
      <template v-else>
        <p v-if="normalizeFailed" class="px-3 py-2 text-xs note-warn border-b border-line">
          有一侧的 YAML 无法解析，本次按原文对比（注释与键序差异会一并显示）。
        </p>
        <p v-if="identical" class="p-4 text-sm text-fg-muted">
          两者内容一致{{ normalize && !normalizeFailed ? '（已忽略注释与键序差异）' : '' }}。
        </p>
        <DiffEditor
          v-else-if="prepared"
          :original="prepared.left"
          :modified="prepared.right"
          language="yaml"
          height="min(680px, 68vh)"
        />
      </template>
    </section>
  </main>
</template>

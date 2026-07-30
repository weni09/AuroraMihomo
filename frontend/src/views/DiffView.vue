<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useConfigLabStore } from '../stores/configlab'
import CodeEditor from '../components/CodeEditor.vue'
import { Button } from '@/components/ui/button'

const store = useConfigLabStore()
onMounted(() => store.fetchConfigs())

/**
 * 三栏对照：本地配置（用户在配置中心填写的原始内容）、
 * 最终配置（合并后 mihomo 内核实际加载的内容）、远程订阅
 * （当前远程来源拉取后的原始层，合并前）。
 *
 * 都是只读展示，改动请去配置中心；这里只用于核对三者是否符合预期
 * （例如远程层没把本地某项覆盖掉、最终产物确实包含了远程节点）。
 */
const panels = computed(() => [
  { key: 'local', title: '本地配置', content: store.localContent, filename: 'local-config.yaml' },
  { key: 'final', title: '最终配置', content: store.finalContent, filename: 'final-config.yaml' },
  { key: 'remote', title: '远程订阅', content: store.remoteContent, filename: 'remote-config.yaml' },
])

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

    <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
      <section v-for="p in panels" :key="p.key" class="bg-surface border border-line rounded-xl overflow-hidden">
        <div class="flex items-center justify-between gap-2 px-3 py-2 border-b border-line bg-elevated">
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
          {{ p.key === 'remote' ? '未配置远程来源，或尚未拉取过。' : '尚未生成，请先在配置中心保存并应用。' }}
        </p>
      </section>
    </div>
  </main>
</template>

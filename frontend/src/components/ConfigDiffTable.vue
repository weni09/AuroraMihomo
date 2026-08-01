<script setup lang="ts">
import { computed, ref } from 'vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import DiffEditor from './DiffEditor.vue'
import type { ConfigDiff, DiffStatus, KeyDiff } from '../utils/configDiff'

/**
 * 语义对比的展示层：按顶层键列出「保留 / 改写 / 丢弃 / 新增」。
 *
 * 为什么不是逐行文本：本地配置几十行、最终配置上千行，逐行算法只能产出
 * 一个横跨上千行的巨块（等于说「整份都是新增」），用户真正要问的
 * 「我设的键还在不在、有没有被合并改掉」一条都看不到。详见 utils/configDiff.ts。
 *
 * 逐行对比仍然有用，但要限定到单个键：那时两侧规模相当，才值得逐行看。
 * 所以每行展开后提供一个「逐行对比」入口。
 */
const props = defineProps<{
  diff: ConfigDiff
  leftTitle: string
  rightTitle: string
}>()

/** 状态 → 展示用的中文与徽标变体。用既有 Badge 变体，避免同一套语义在本页出现第二种配色 */
const STATUS_META: Record<DiffStatus, { text: string; variant: 'ok' | 'err' | 'warn' | 'neutral' }> =
  {
    changed: { text: '已改写', variant: 'warn' },
    removed: { text: '被丢弃', variant: 'err' },
    added: { text: '新增', variant: 'ok' },
    same: { text: '原样保留', variant: 'neutral' },
  }

/** 有差异的键。原样保留的默认不展开——十几条噪声会盖住两三条真问题 */
const changedKeys = computed(() => props.diff.keys.filter((k) => k.status !== 'same'))
const sameKeys = computed(() => props.diff.keys.filter((k) => k.status === 'same'))

/** 展开明细的键；键名唯一，可直接当集合元素 */
const expanded = ref<Set<string>>(new Set())
const toggle = (key: string) => {
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  // 换一个 Set 实例，否则原地增删不会触发重渲染
  expanded.value = next
}

/** 切到逐行对比的键。同时只看一个，避免一屏里挂好几个 CodeMirror 实例 */
const inlineDiffKey = ref('')
const toggleInlineDiff = (key: string) => {
  inlineDiffKey.value = inlineDiffKey.value === key ? '' : key
}

const showSame = ref(false)

/**
 * 明细条目的显示上限。
 *
 * 一次合并可以引入上千条规则，全部渲染会让页面卡住，而「前 200 条长什么样」
 * 已经足够判断这批变化是否符合预期；要逐条核对应该用下面的逐行对比。
 */
const ENTRY_LIMIT = 200
const visibleEntries = (k: KeyDiff) => k.entries.slice(0, ENTRY_LIMIT)
const hiddenEntryCount = (k: KeyDiff) => Math.max(0, k.entries.length - ENTRY_LIMIT)

/** 明细里各状态的条数，如「新增 75」——比只说「明细 75 条」更能说明方向 */
const entrySummary = (k: KeyDiff): string => {
  const counts = new Map<DiffStatus, number>()
  for (const e of k.entries) counts.set(e.status, (counts.get(e.status) ?? 0) + 1)
  return [...counts]
    .map(([s, n]) => `${STATUS_META[s].text} ${n}`)
    .join('，')
}
</script>

<template>
  <div>
    <!-- 汇总：先给结论，再让用户决定看哪一条 -->
    <div class="flex flex-wrap items-center gap-2 px-3 py-2 border-b border-line bg-elevated">
      <span class="text-xs text-fg-muted">按顶层键对比：</span>
      <Badge v-if="diff.counts.changed" variant="warn">已改写 {{ diff.counts.changed }}</Badge>
      <Badge v-if="diff.counts.removed" variant="err">被丢弃 {{ diff.counts.removed }}</Badge>
      <Badge v-if="diff.counts.added" variant="ok">新增 {{ diff.counts.added }}</Badge>
      <Badge variant="neutral">原样保留 {{ diff.counts.same }}</Badge>
    </div>

    <Table>
      <TableHeader>
        <TableRow class="hover:bg-transparent">
          <TableHead>配置项</TableHead>
          <TableHead>状态</TableHead>
          <TableHead>{{ leftTitle }}</TableHead>
          <TableHead>{{ rightTitle }}</TableHead>
          <TableHead class="lg:w-32">明细</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableEmpty v-if="!changedKeys.length && !diff.counts.same" :colspan="5">
          两份配置都没有内容可对比。
        </TableEmpty>
        <TableEmpty v-else-if="!changedKeys.length" :colspan="5">
          没有差异，{{ diff.counts.same }} 个配置项全部原样保留。
        </TableEmpty>

        <template v-for="k in changedKeys" :key="k.key">
          <TableRow>
            <TableCell label="配置项">
              <!-- 键名作主体：用户是拿它去对 YAML 的。中文标签作副文本 -->
              <div class="font-mono text-sm font-semibold text-fg break-all">{{ k.key }}</div>
              <div v-if="k.label !== k.key" class="text-xs text-fg-subtle mt-0.5">
                {{ k.label }}
              </div>
            </TableCell>
            <TableCell label="状态">
              <Badge :variant="STATUS_META[k.status].variant">
                {{ STATUS_META[k.status].text }}
              </Badge>
            </TableCell>
            <TableCell label="左侧">
              <span class="font-mono text-xs break-all" :class="k.leftBrief ? 'text-fg' : 'text-fg-subtle'">
                {{ k.leftBrief || '(无此项)' }}
              </span>
            </TableCell>
            <TableCell label="右侧">
              <span class="font-mono text-xs break-all" :class="k.rightBrief ? 'text-fg' : 'text-fg-subtle'">
                {{ k.rightBrief || '(无此项)' }}
              </span>
            </TableCell>
            <TableCell label="明细">
              <div class="flex flex-wrap gap-1.5">
                <Button
                  v-if="k.entries.length"
                  variant="outline"
                  size="sm"
                  class="h-7 px-2 text-xs"
                  :aria-expanded="expanded.has(k.key)"
                  @click="toggle(k.key)"
                >
                  {{ expanded.has(k.key) ? '收起' : `${k.entries.length} 条` }}
                </Button>
                <!-- 新增/丢弃整个键时没有可对齐的明细，但看全文仍有意义 -->
                <Button
                  variant="outline"
                  size="sm"
                  class="h-7 px-2 text-xs"
                  @click="toggleInlineDiff(k.key)"
                >
                  {{ inlineDiffKey === k.key ? '关闭逐行' : '逐行' }}
                </Button>
              </div>
            </TableCell>
          </TableRow>

          <!-- 明细行：跨整行展开，列出对齐后的条目 -->
          <TableRow v-if="expanded.has(k.key)" class="hover:bg-transparent">
            <TableCell :colspan="5" class="bg-elevated">
              <p class="text-xs text-fg-muted mb-2">{{ entrySummary(k) }}</p>
              <ul class="space-y-1">
                <li
                  v-for="(e, i) in visibleEntries(k)"
                  :key="`${e.name}-${i}`"
                  class="flex flex-wrap items-baseline gap-2 text-xs"
                >
                  <Badge :variant="STATUS_META[e.status].variant" class="shrink-0">
                    {{ STATUS_META[e.status].text }}
                  </Badge>
                  <span class="font-mono font-semibold text-fg break-all">{{ e.name }}</span>
                  <span v-if="e.status === 'changed'" class="font-mono text-fg-muted break-all">
                    {{ e.left }} → {{ e.right }}
                  </span>
                  <span v-else class="font-mono text-fg-muted break-all">
                    {{ e.left || e.right }}
                  </span>
                </li>
              </ul>
              <p v-if="hiddenEntryCount(k)" class="text-xs text-fg-subtle mt-2">
                还有 {{ hiddenEntryCount(k) }} 条未显示，逐条核对请用「逐行」对比。
              </p>
            </TableCell>
          </TableRow>

          <!-- 逐行对比行：限定到单个键，两侧规模相当，此时逐行才有意义 -->
          <TableRow v-if="inlineDiffKey === k.key" class="hover:bg-transparent">
            <TableCell :colspan="5" class="p-0">
              <p
                v-if="!k.leftText && !k.rightText"
                class="p-3 text-xs text-fg-subtle"
              >
                两侧都没有内容可逐行对比。
              </p>
              <DiffEditor
                v-else
                :original="k.leftText"
                :modified="k.rightText"
                language="yaml"
                height="min(420px, 50dvh)"
              />
            </TableCell>
          </TableRow>
        </template>

        <!-- 原样保留的键收成一行，点开才列出 -->
        <TableRow v-if="sameKeys.length" class="hover:bg-transparent">
          <TableCell :colspan="5">
            <Button
              variant="ghost"
              size="sm"
              class="h-7 px-2 text-xs"
              :aria-expanded="showSame"
              @click="showSame = !showSame"
            >
              {{ showSame ? '收起' : '展开' }} {{ sameKeys.length }} 个原样保留的配置项
            </Button>
            <div v-if="showSame" class="flex flex-wrap gap-1.5 mt-2">
              <span
                v-for="k in sameKeys"
                :key="k.key"
                class="font-mono text-xs text-fg-muted border border-line rounded px-1.5 py-0.5"
              >
                {{ k.key }}
              </span>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

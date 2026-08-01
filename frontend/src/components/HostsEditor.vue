<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Plus, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { dumpYaml } from '../utils/yaml'

/**
 * 顶层 `hosts` 段的键值行编辑器。
 *
 * mihomo 的 hosts 是「域名 -> 指向」的映射，指向可以是单个 IP、多个 IP
 * （数组，内核会做负载/随机）或另一个域名（别名）。此前它只能在「高级参数」
 * 的 YAML 兜底框里手写，而 DNS 分组里的 `use-hosts` 开关又写着「使用下方
 * 自定义 hosts 映射」——开关和它控制的数据不在一处，这个组件补上这个入口。
 *
 * 为什么不直接把 map 绑到输入框上：域名是 map 的**键**，逐字符编辑键
 * 意味着每敲一个字都要删旧键建新键，中途还会与其他键碰撞（把 `a.com`
 * 改成 `b.com` 的过程会经过 `b.co`，若该键已存在就把它的值覆盖掉）。
 * 所以编辑期的唯一真源是下面的 rows 数组，map 只在回传时按行序重建。
 */

/** 一行映射。uid 用于 v-for 的稳定 key——用索引会让删除中间行后输入框错位 */
interface HostRow {
  uid: number
  domain: string
  /** 指向的文本形态：多个目标以逗号分隔 */
  target: string
  /**
   * 无法用「单值/逗号分隔」表达的原始值（例如用户手写的嵌套结构）。
   * 有值时该行只读并原样回传，避免把看不懂的配置静默改形或丢掉。
   */
  raw?: unknown
}

const props = defineProps<{
  /** 当前 hosts 映射；未配置时传 undefined */
  modelValue?: Record<string, unknown>
  /** 关联 label 的 id，供读屏定位（本组件不是单个原生控件，label 的 for 到不了内部） */
  labelledby?: string
  describedby?: string
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, unknown> | undefined): void }>()

let uidSeq = 0

/** 值 -> 输入框文本。数组用逗号分隔；其余形态归入 raw 走只读行 */
const toRow = (domain: string, value: unknown): HostRow => {
  if (typeof value === 'string') return { uid: ++uidSeq, domain, target: value }
  if (typeof value === 'number' || typeof value === 'boolean') {
    // YAML 里 `1.1.1.1` 是字符串，但 `hosts: { a: 127 }` 这类写法会解析成数字。
    // 转成文本可编辑即可，回传时也按文本写回（内核对 hosts 的值按字符串理解）
    return { uid: ++uidSeq, domain, target: String(value) }
  }
  if (Array.isArray(value) && value.every((v) => typeof v === 'string')) {
    return { uid: ++uidSeq, domain, target: (value as string[]).join(', ') }
  }
  return { uid: ++uidSeq, domain, target: '', raw: value }
}

const rows = ref<HostRow[]>([])

/** 行 -> map。空域名行不参与：新增行必然先经过「域名还没填」的状态 */
const rowsToMap = (list: HostRow[]): Record<string, unknown> => {
  const out: Record<string, unknown> = {}
  for (const row of list) {
    const domain = row.domain.trim()
    if (!domain) continue
    if (row.raw !== undefined) {
      out[domain] = row.raw
      continue
    }
    const parts = row.target.split(',').map((s) => s.trim()).filter(Boolean)
    // 单目标写成标量而不是单元素数组：与用户手写的常见形态一致，
    // 也让 diff 页面不会因为形态变化而报出一堆假差异
    if (parts.length === 0) out[domain] = ''
    else if (parts.length === 1) out[domain] = parts[0]
    else out[domain] = parts
  }
  return out
}

/**
 * 用序列化结果判断两份 map 是否等价。
 *
 * 需要这个比较是因为下面的 watch 会在每次自身回传后被重新触发（父组件把
 * 我们刚写出去的值又传回来）。若不做等价判断就重新播种 rows，正在输入的
 * 那一行会被替换成新对象，输入框失去焦点、光标跳回开头。
 * 逐键深比较也能做，但 hosts 的值形态杂（字符串/数组/嵌套），
 * 直接比规范化后的文本更短且不会漏分支。
 */
const sameMap = (a: Record<string, unknown>, b: Record<string, unknown>): boolean => {
  try {
    return dumpYaml(a) === dumpYaml(b)
  } catch {
    return false
  }
}

/**
 * 外部值变化时播种 rows。
 *
 * immediate：首次挂载就要把已有配置铺开。
 * 只在「外部值与当前 rows 表示的 map 不等价」时才重建，理由见 sameMap。
 * 这同时覆盖了「放弃修改（重新加载）」——那条路径会换掉整个 model，
 * 内容必然与当前 rows 不等价，于是正常重新播种。
 */
watch(
  () => props.modelValue,
  (val) => {
    const next = val && typeof val === 'object' ? val : {}
    if (sameMap(next, rowsToMap(rows.value))) return
    rows.value = Object.entries(next).map(([domain, value]) => toRow(domain, value))
  },
  { immediate: true, deep: true },
)

/**
 * 回传改动。
 *
 * 全空时传 undefined 而不是 {}：调用方据此删除整个 hosts 键，
 * 否则会在 base.yaml 里写出一个空的 `hosts: {}`——那是「显式配置了空映射」，
 * 与「没配过 hosts」在语义上不同（前者也让配置文件多出无意义的噪声）。
 */
const commit = () => {
  const map = rowsToMap(rows.value)
  emit('update:modelValue', Object.keys(map).length === 0 ? undefined : map)
}

const onDomainInput = (row: HostRow, value: string) => {
  row.domain = value
  commit()
}

const onTargetInput = (row: HostRow, value: string) => {
  row.target = value
  commit()
}

const addRow = () => {
  rows.value.push({ uid: ++uidSeq, domain: '', target: '' })
  // 不 commit：空行不产生任何键，回传只会触发一次无意义的等价判断
}

const removeRow = (index: number) => {
  rows.value.splice(index, 1)
  commit()
}

/**
 * 重复域名的行号集合（0 基）。
 *
 * map 的键唯一，重复域名里只有最后一条生效，前面的会被静默吃掉。
 * 这个提示不阻止输入——用户可能正在改其中一行的域名，中途撞车是正常的。
 */
const duplicateRows = computed(() => {
  const seen = new Map<string, number[]>()
  rows.value.forEach((row, idx) => {
    const domain = row.domain.trim()
    if (!domain) return
    const list = seen.get(domain)
    if (list) list.push(idx)
    else seen.set(domain, [idx])
  })
  const dup = new Set<number>()
  for (const list of seen.values()) {
    if (list.length > 1) list.forEach((idx) => dup.add(idx))
  }
  return dup
})
</script>

<template>
  <div :aria-labelledby="labelledby" :aria-describedby="describedby" role="group">
    <div v-if="rows.length" class="space-y-2">
      <div v-for="(row, idx) in rows" :key="row.uid" class="flex flex-wrap items-start gap-2">
        <div class="flex-1 min-w-[160px]">
          <Input
            :model-value="row.domain"
            :disabled="row.raw !== undefined"
            class="font-mono text-sm"
            :aria-label="`第 ${idx + 1} 条映射的域名`"
            placeholder="router.local"
            @update:model-value="onDomainInput(row, String($event))"
          />
          <p v-if="duplicateRows.has(idx)" class="text-xs text-amber-600 dark:text-amber-400 mt-1">
            域名重复，只有最后一条会生效
          </p>
        </div>
        <div class="flex-1 min-w-[160px]">
          <Input
            v-if="row.raw === undefined"
            :model-value="row.target"
            class="font-mono text-sm"
            :aria-label="`第 ${idx + 1} 条映射指向的地址`"
            placeholder="192.168.1.1"
            @update:model-value="onTargetInput(row, String($event))"
          />
          <!-- 形态特殊的条目（嵌套结构等）不提供编辑：把它塞进两个输入框
               必然要改形，而改形等于悄悄替用户改了配置。这里只说明它存在。 -->
          <p v-else class="text-xs text-fg-subtle py-2.5">
            该条目结构特殊，请在「高级参数」中编辑；保存时原样保留。
          </p>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="tap-target mt-0.5 text-fg-subtle hover:text-destructive"
          :aria-label="`删除第 ${idx + 1} 条映射`"
          title="删除此映射"
          @click="removeRow(idx)"
        >
          <!-- shadcn Button 的 [&_svg]:size-4 特异性高于 h-4 w-4，须加 ! 前缀 -->
          <X class="!h-4 !w-4" aria-hidden="true" />
        </Button>
      </div>
    </div>
    <p v-else class="text-xs text-fg-subtle">尚未配置任何 hosts 映射。</p>

    <Button type="button" variant="outline" size="sm" class="mt-2" @click="addRow">
      <Plus class="!h-4 !w-4 mr-1" aria-hidden="true" />
      添加映射
    </Button>
  </div>
</template>

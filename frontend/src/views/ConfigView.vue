<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useConfigStore } from '../stores/config'
import { baseConfigSchema, getByPath, setByPath, deleteByPath, normalizeOptions, advancedExcludedKeys, type FieldOption } from '../schemas/baseConfig'
import CodeEditor from '../components/CodeEditor.vue'
import HostsEditor from '../components/HostsEditor.vue'
import { loadYaml, dumpYaml } from '../utils/yaml'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

const store = useConfigStore()
const active = ref(baseConfigSchema[0]?.id || 'general')

// 文本类字段的输入草稿。
// 这些字段的值在 model 里是数组/对象，若在 @input 里立刻解析再回灌 :value，
// 用户刚敲的换行、逗号会被 trim/filter 当场吃掉（回车丢失、逗号消失，
// 导致第二条规则或第二个 DNS 根本填不进去）。
// 因此输入期间只更新草稿、不回灌，等失焦时再统一解析写入 model。
const drafts = ref<Record<string, string>>({})

const isDraftType = (type: string) => type === 'textarea' || type === 'string-array' || type === 'yaml-object'

// 输入框实际显示值：有草稿时优先用草稿，避免规范化结果打断输入
const fieldValue = (key: string, type: string) => {
  if (isDraftType(type) && drafts.value[key] !== undefined) return drafts.value[key]
  return displayValue(key, type)
}

const onDraftInput = (key: string, value: string) => {
  drafts.value[key] = value
}

/**
 * 表单元素 id。
 *
 * f.key 在 schema 内唯一，但含点号（如 dns.enable）与斜杠，
 * 直接当 id 会让 CSS 选择器与 querySelector 需要转义，统一替换成连字符。
 * 前缀避免与页面上其他元素的 id 相撞。
 */
const slugify = (key: string) => key.replace(/[^a-zA-Z0-9_-]/g, '-')
const fieldId = (key: string) => `cfg-${slugify(key)}`
const labelId = (key: string) => `cfg-${slugify(key)}-label`
const helpId = (key: string) => `cfg-${slugify(key)}-help`

/**
 * 源码编辑器（CodeMirror）的输入处理。
 *
 * 与 textarea 不同，CodeEditor 自己维护文档状态、不会被规范化后的值回灌打断，
 * 所以这里边记草稿边解析写入 model，用户无需失焦即可生效
 * （编辑器没有可靠的 blur 时机，等失焦提交会导致内容丢失）。
 *
 * 草稿仍要保留：YAML 写到中间态时解析会失败，此时 model 不更新，
 * 但编辑器需要继续显示用户正在输入的原文。
 *
 * 提交经过防抖：advanced-raw 每次提交都要全量 loadYaml 整篇 YAML 并重建
 * model 的非受管键（见 onInput），逐字符触发在内容较长时会明显卡顿。
 */
const CODE_COMMIT_DELAY = 250
// key -> { timer, 待提交的值与类型 }，flush 时需要原样重放
const codePending: Record<string, { timer: number; value: string; type: string }> = {}

const onCodeInput = (key: string, type: string, value: string) => {
  drafts.value[key] = value
  const prev = codePending[key]
  if (prev) window.clearTimeout(prev.timer)
  const timer = window.setTimeout(() => {
    delete codePending[key]
    onInput(key, value, type)
  }, CODE_COMMIT_DELAY)
  codePending[key] = { timer, value, type }
}

/**
 * 立即提交所有挂起的编辑器输入。
 *
 * 防抖留下一个窗口：用户改完立刻点保存（250ms 内）时 model 还没更新，
 * 保存的是上一次的内容，且界面上看不出差别。所有写操作前必须先 flush。
 */
const flushCodeInputs = () => {
  for (const [key, p] of Object.entries(codePending)) {
    window.clearTimeout(p.timer)
    delete codePending[key]
    onInput(key, p.value, p.type)
  }
}

/** 丢弃挂起的编辑器输入（放弃修改、卸载时用），不写回 model */
const discardCodeInputs = () => {
  for (const [key, p] of Object.entries(codePending)) {
    window.clearTimeout(p.timer)
    delete codePending[key]
  }
}

// 卸载时清掉未触发的定时器，避免在已销毁的组件上写 store
onBeforeUnmount(discardCodeInputs)

/** 保存前先落盘挂起的编辑器输入，避免丢掉最后 250ms 内的修改 */
const saveBase = () => {
  flushCodeInputs()
  return store.saveBase()
}

const saveAndMerge = () => {
  flushCodeInputs()
  return store.saveAndMerge()
}

// 失焦时提交草稿并清除，让显示值回落到 model 的规范化结果
const onDraftBlur = (key: string, type: string) => {
  const draft = drafts.value[key]
  if (draft === undefined) return
  onInput(key, draft, type)
  delete drafts.value[key]
}

onMounted(() => {
  store.fetchBase()
  store.fetchRemoteSource()
})

/**
 * TUN 是否已开启。
 *
 * 直接读 store.model（fetchBase 已经加载），不额外请求 /transparent/status：
 * 那个接口每次都会做一次实时环境检测（读一堆 /proc、还要 exec 出 ip 与
 * iptables 子进程），只为决定一条提示横幅是否显示，代价不成比例。
 * 而开关状态本身就存在 base.yaml 里，本页的 model 就是它。
 */
const transparentTUNActive = computed(() => !!store.model?.tun?.enable)

// 仅这三类需要再选一个具体实体；none/all/url 不需要
const needsEntity = computed(() =>
  ['subscription', 'collection', 'file'].includes(store.remoteSourceType),
)

// 按当前来源类型过滤可选项
const currentOptions = computed(() =>
  store.remoteSourceOptions.filter((o) => o.type === store.remoteSourceType),
)

// 切换来源类型时清空已选 ID，否则会把上一类型的 ID 带过去，
// 提交后被后端判定为「实体不存在」
const onSourceTypeChange = () => {
  store.remoteSourceId = 0
}

// shadcn Select 的 SelectItem 不接受空字符串 value（reka-ui 用空串表示
// "未选中"）。schema 里大量 select 字段把空串用作合法业务值（"未设置，使用
// 内核默认"），因此这里统一用 __unset__ 占位，转换只在 select 字段的
// model-value/update:model-value 这一层做，onInput/displayValue 仍按空
// 字符串语义读写 model，不需要跟着改
const UNSET_SELECT_VALUE = '__unset__'
const CUSTOM_SELECT_VALUE = '__custom__'
const selectDisplayValue = (key: string, type: string) => displayValue(key, type) || UNSET_SELECT_VALUE
const onSelectChange = (key: string, type: string, value: string) =>
  onInput(key, value === UNSET_SELECT_VALUE ? '' : value, type)

/**
 * url-preset（下拉预设 + 输入）字段的下拉回显：
 * 当前值命中某个预设时选中它；非空但没命中（自定义地址）显示「自定义」；
 * 未设置显示「未设置」。
 */
const presetSelectValue = (key: string, presets?: (string | FieldOption)[]) => {
  const v = displayValue(key, 'text')
  if (!v) return UNSET_SELECT_VALUE
  return normalizeOptions(presets).some((o) => o.value === v) ? v : CUSTOM_SELECT_VALUE
}

/** url-preset 下拉选择：预设直接写入；「未设置」删键；「自定义」不动（由输入框编辑） */
const onPresetSelect = (key: string, presets: (string | FieldOption)[] | undefined, value: string) => {
  if (value === UNSET_SELECT_VALUE) {
    deleteByPath(store.model, key)
    return
  }
  if (value === CUSTOM_SELECT_VALUE) return
  // 预设值非空，直接写入（normalizeOptions 保证 value 与下拉 item 一致）
  setByPath(store.model, key, value)
}

// 常用调度预设。6 段式（含秒），与后端规范化后的存储形态一致，
// 便于高亮当前选中项
const cronPresets = [
  { label: '每 30 分钟', expr: '0 */30 * * * *' },
  { label: '每小时', expr: '0 0 * * * *' },
  { label: '每 6 小时', expr: '0 0 */6 * * *' },
  { label: '每天 4:00', expr: '0 0 4 * * *' },
]

// 手动拉取远程并合并。成功后刷新本地配置视图，
// 因为合并可能因冲突解决策略改动了最终内容
const onPullMerge = async () => {
  const ok = await store.pullAndMerge()
  if (ok) await store.fetchBase()
}

// 重新加载必须同时清空草稿，否则残留草稿会在下次失焦时把旧内容写回 model
const reloadBase = async () => {
  if (!confirm('将放弃所有未保存的修改并重新加载配置，确定继续？')) return
  // 挂起的编辑器提交必须一并丢弃：否则它们会在 fetchBase 之后触发，
  // 把刚放弃的修改重新写回 model
  discardCodeInputs()
  drafts.value = {}
  await store.fetchBase()
  // 订阅/组合/文件可能在别处被增删，来源列表需一并刷新
  await store.fetchRemoteSource()
}

const onSwitch = (key: string, checked: boolean) => setByPath(store.model, key, checked)

/**
 * hosts-map 字段（顶层 hosts）的写回。
 *
 * 编辑器传 undefined 表示「一条映射都不剩」，此时删键而不是写 {}：
 * 空映射会在 base.yaml 里留下一个 `hosts: {}`，那是「显式配置了空映射」，
 * 与「没配过 hosts」不是一回事，也让配置文件多出无意义的噪声。
 */
const onHostsMap = (key: string, value: Record<string, unknown> | undefined) => {
  if (!value || Object.keys(value).length === 0) {
    deleteByPath(store.model, key)
    return
  }
  setByPath(store.model, key, value)
}

/** hosts-map 的当前值；未配置时给编辑器一个空对象，免得它在模板里判空 */
const hostsMapValue = (key: string): Record<string, unknown> => {
  const v = getByPath(store.model, key)
  return v && typeof v === 'object' && !Array.isArray(v) ? v : {}
}
const onInput = (key: string, value: string, type: string) => {
  if (type === 'number') {
    // 清空或非法输入时移除该键，而不是写入 0：
    // 对 port / mtu / keep-alive-interval 这类字段，0 与「不设置」语义完全不同
    // （写 0 会被内核按 0 生效甚至拒绝加载，留空才是回落到默认值）
    const trimmed = value.trim()
    const n = Number(trimmed)
    if (trimmed === '' || !Number.isFinite(n)) {
      deleteByPath(store.model, key)
      return
    }
    setByPath(store.model, key, n)
    return
  }
  // 策略组以 YAML 数组维护，单独解析
  if (key === 'proxy-groups-raw') {
    // 空文本不写回：全选删除后重新输入必然经过「空」这个中间态，
    // 若此时就把 proxy-groups 置空，用户还没输入完策略组就已经丢了。
    // 真要清空策略组，走下面的「清空」按钮显式确认。
    if (!value.trim()) return
    try {
      const parsed = loadYaml(value)
      setByPath(store.model, 'proxy-groups', Array.isArray(parsed) ? parsed : [])
    } catch {
      // YAML 语法错误时暂不写回，避免破坏已有配置
    }
    return
  }
  // 高级参数兜底：以 YAML 对象维护所有未被表单显式建模的顶层字段
  // （listeners / proxy-providers / sub-rules / tls / experimental 等）。
  // 该文本域是这些字段的唯一来源，因此先清空旧的非受管键再写入解析结果，
  // 这样用户在此处删除某个键时也能同步从 model 中移除。
  if (key === 'advanced-raw') {
    // 与 proxy-groups-raw 同理：空文本会让下面的「先删后写」把所有非受管键
    // （listeners / proxy-providers / tls / experimental …）一次删光，
    // 而这些键此后只要解析一直失败就再也回不来。清空须走显式按钮。
    if (!value.trim()) return
    try {
      const parsed = loadYaml(value)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        for (const k of Object.keys(store.model)) {
          if (!advancedExcludedKeys.has(k)) delete store.model[k]
        }
        for (const [k, v] of Object.entries(parsed as Record<string, any>)) {
          if (!advancedExcludedKeys.has(k)) store.model[k] = v
        }
      }
    } catch {
      // YAML 语法错误时暂不写回，避免破坏已有配置
    }
    return
  }
  // yaml-object：值本身是嵌套结构（如 sniffer.sniff 的按协议配置），
  // 用 YAML 文本编辑；留空表示移除该键
  if (type === 'yaml-object') {
    const text = value.trim()
    if (!text) {
      deleteByPath(store.model, key)
      return
    }
    try {
      const parsed = loadYaml(text)
      if (parsed && typeof parsed === 'object') {
        setByPath(store.model, key, parsed)
      }
    } catch {
      // YAML 语法错误时不写回，保留用户草稿供其继续修正
    }
    return
  }
  if (type === 'textarea') {
    setByPath(store.model, key, value.split('\n').map((s) => s.trim()).filter(Boolean))
    return
  }
  if (type === 'string-array') {
    setByPath(store.model, key, value.split(',').map((s) => s.trim()).filter(Boolean))
    return
  }
  // 下拉框选择「默认/未设置」时移除该键，而不是写入空字符串：
  // mihomo 对 enhanced-mode / cache-algorithm 这类枚举字段遇到 '' 会拒绝加载配置
  if (type === 'select' && value === '') {
    deleteByPath(store.model, key)
    return
  }
  // url-preset / text 地址字段：清空输入框应等同「未设置」，删键而不是写 ''。
  // 否则会留下 geox-url.geoip: ""，下拉显示「自定义」、保存后 YAML 也是空串噪声。
  if ((type === 'url-preset' || type === 'text') && value.trim() === '') {
    deleteByPath(store.model, key)
    return
  }
  setByPath(store.model, key, value)
}

/**
 * 显式清空 advanced-raw / proxy-groups-raw 承载的配置。
 *
 * onInput 里这两个字段的空文本会被忽略（否则编辑过程中经过的空状态就把内容
 * 删光了），因此需要这条独立路径来表达「我确实要清空」。
 * 二次确认是必要的：这两项覆盖 listeners / proxy-providers / tls /
 * experimental 与全部策略组，误清后只能靠重新加载找回未保存前的值。
 */
const clearRawField = (key: string) => {
  const label = key === 'proxy-groups-raw' ? '全部策略组' : '全部高级参数'
  if (!confirm(`将清空${label}，保存后生效。确定继续？`)) return
  // 先取消该字段挂起的提交，否则清空后它会在 250ms 内把旧内容写回
  const pending = codePending[key]
  if (pending) {
    window.clearTimeout(pending.timer)
    delete codePending[key]
  }
  if (key === 'proxy-groups-raw') {
    setByPath(store.model, 'proxy-groups', [])
  } else {
    for (const k of Object.keys(store.model)) {
      if (!advancedExcludedKeys.has(k)) delete store.model[k]
    }
  }
  // 清掉草稿，让编辑器回落到 model 的（现已为空的）规范化结果
  delete drafts.value[key]
}

const displayValue = (key: string, type: string) => {
  if (type === 'yaml-object') {
    const v = getByPath(store.model, key)
    if (!v || typeof v !== 'object') return ''
    try {
      return dumpYaml(v)
    } catch {
      return ''
    }
  }
  if (key === 'proxy-groups-raw') {
    const groups = getByPath(store.model, 'proxy-groups')
    if (!groups || !Array.isArray(groups) || groups.length === 0) return ''
    try {
      return dumpYaml(groups)
    } catch {
      return ''
    }
  }
  if (key === 'advanced-raw') {
    const rest: Record<string, any> = {}
    for (const [k, v] of Object.entries(store.model)) {
      if (!advancedExcludedKeys.has(k)) rest[k] = v
    }
    if (Object.keys(rest).length === 0) return ''
    try {
      return dumpYaml(rest)
    } catch {
      return ''
    }
  }
  const v = getByPath(store.model, key)
  if (type === 'switch') return !!v
  if (Array.isArray(v)) {
    // textarea 类型（rules / authentication / fake-ip-filter 等）按行编辑，
    // 需要用换行拼接才能和 onInput 里 split('\n') 的解析逻辑对应；
    // string-array 类型走逗号分隔的单行展示。
    return type === 'textarea' ? v.join('\n') : v.join(', ')
  }
  return v ?? ''
}
</script>

<template>
  <!-- 页面整体随浏览器原生滚动条滚动。桌面端（lg+）的分组导航改用 sticky
       固定在视口内，随页面滚动“跟随”而不必再把表单区关进一个自造的
       内部滚动容器——此前那套结构依赖外壳锁一屏高度分配 flex-1，
       外壳已不再锁屏，这里改用 sticky 达到同样的“导航常驻可见”效果。 -->
  <main class="p-4 sm:p-6 lg:p-8">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4 sm:mb-6">
      <div>
        <h1 class="text-2xl sm:text-3xl font-bold">配置中心</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">管理 Mihomo 内核的基础配置、远程订阅与规则关联</p>
      </div>
    </div>
    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <!-- 远程订阅（可选）：决定最终配置里的「远程层」从哪来。
         默认不填 —— 此时最终配置就等于本页的本地配置，不会被订阅内容改写。
         填了才会把远程内容按合并策略并进来。 -->
    <section class="bg-surface border rounded p-4 mb-4 shrink-0">
      <div class="flex flex-wrap items-center gap-3">
        <div>
          <h2 class="text-sm font-semibold text-fg">远程订阅（可选）</h2>
          <p class="text-xs text-fg-subtle mt-0.5">不填则只使用下方的本地配置</p>
        </div>
        <Select v-model="store.remoteSourceType" @update:model-value="onSourceTypeChange">
          <SelectTrigger class="text-sm w-auto"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="none">不使用（仅本地配置）</SelectItem>
            <SelectItem value="subscription">本地订阅：单条</SelectItem>
            <SelectItem value="collection">本地订阅：组合</SelectItem>
            <SelectItem value="file">本地文件模板</SelectItem>
            <SelectItem value="url">外部订阅链接</SelectItem>
            <!-- 「全部聚合」已移除：多机场聚合会把互相冲突的节点、策略组与规则
                 一起并进最终配置，结果难以预期；需要多机场请建「组合订阅」，
                 在其中显式配置筛选与处理管道。仅存量数据可能仍是该值。 -->
            <SelectItem v-if="store.remoteSourceType === 'all'" value="all">
              本地订阅：全部聚合（已弃用）
            </SelectItem>
          </SelectContent>
        </Select>

        <Select v-if="needsEntity" v-model="store.remoteSourceId">
          <SelectTrigger class="text-sm w-auto min-w-[220px]"><SelectValue placeholder="请选择…" /></SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="opt in currentOptions"
              :key="`${opt.type}-${opt.id}`"
              :value="opt.id"
              :disabled="opt.disabled"
            >
              {{ opt.name }}{{ opt.disabled ? `（${opt.reason}）` : '' }}
            </SelectItem>
          </SelectContent>
        </Select>

        <Input
          v-if="store.remoteSourceType === 'url'"
          v-model="store.remoteSourceUrl"
          type="url"
          class="text-sm font-mono flex-1 min-w-[280px]"
          placeholder="https://example.com/sub?token=... （别人分享给你的订阅地址）"
        />

        <!-- 「保存」持久化来源设置，「立即拉取并合并」直接触发一次回源，
             两者都是立刻生效的操作，放在一起更符合操作习惯；
             拉取前提是已配置远程来源，type 为 none 时不渲染。 -->
        <Button :disabled="store.loading" @click="store.saveRemoteSource()">保存</Button>
        <Button
          v-if="store.remoteSourceType !== 'none'"
          variant="outline"
          :disabled="store.pulling"
          @click="onPullMerge"
        >
          {{ store.pulling ? '拉取中…' : '立即拉取并合并' }}
        </Button>
      </div>

      <!-- 定时拉取。保存本地配置不会触发远程拉取，
           远程内容的更新只由「立即拉取并合并」与这里的定时调度驱动。 -->
      <div v-if="store.remoteSourceType !== 'none'" class="mt-3 pt-3 border-t border-line space-y-2">
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-1.5 text-xs font-semibold text-fg-muted cursor-pointer select-none">
            <Checkbox v-model="store.remoteSourceCronEnabled" />
            定时拉取
          </label>
          <Input
            v-model="store.remoteSourceCron"
            class="text-sm font-mono w-44"
            :disabled="!store.remoteSourceCronEnabled"
            placeholder="0 0 * * * *"
          />
          <div class="flex flex-wrap gap-1.5">
            <Button
              v-for="p in cronPresets"
              :key="p.expr"
              variant="outline"
              size="sm"
              :class="store.remoteSourceCron === p.expr ? 'bg-primary/10 border-primary/40 text-primary hover:bg-primary/10' : ''"
              :disabled="!store.remoteSourceCronEnabled"
              @click="store.remoteSourceCron = p.expr"
            >
              {{ p.label }}
            </Button>
          </div>
        </div>
        <p class="text-xs text-fg-subtle">
          Cron 语法与系统设置里的自动更新一致：支持 5 段（分 时 日 月 周）或 6 段（秒 分 时 日 月 周），随「保存」生效。
          <span v-if="!store.remoteSourceCronEnabled" class="text-amber-600 dark:text-amber-400">已关闭定时拉取，只能手动拉取。</span>
        </p>
      </div>

      <p v-if="store.remoteSourceType === 'none'" class="text-xs text-fg-subtle mt-2">
        当前不使用任何远程配置，合并后的最终配置与本页填写的内容一致。
      </p>
      <p v-if="store.remoteSourceType === 'url'" class="text-xs text-fg-subtle mt-2">
        支持 Clash/Mihomo YAML、Base64 订阅与明文分享链接。该地址不会存为订阅，也不参与订阅的定时刷新，每次合并都会重新拉取。
      </p>
      <p v-if="store.remoteSourceType === 'file'" class="text-xs text-fg-subtle mt-2">
        仅列出「配置类型 = Mihomo 配置」的文件模板，原样输出型文件无法作为配置来源。
      </p>
      <p v-if="store.remoteSourceType === 'all'" class="text-xs text-amber-600 dark:text-amber-400 mt-2">
        「全部聚合」已弃用：它把所有启用订阅的节点、策略组与规则不加筛选地并在一起，
        多机场场景下结果难以预期。建议改用「本地订阅：组合」，在组合里显式选择订阅与处理管道。
      </p>
      <p v-if="needsEntity && currentOptions.length === 0" class="text-xs text-amber-600 dark:text-amber-400 mt-2">
        暂无可选项，请先在 Sub-Store 管理中创建。
      </p>
    </section>

    <!-- 本地配置：加载/保存下方分组表单，与上面的「远程订阅」区块分开，
         避免用户把两者的按钮混为一谈——远程那组按钮只管远程来源与拉取，
         这里的按钮才是真正读写下方的本地基础配置。 -->
    <section class="bg-surface border rounded p-4 mb-4 shrink-0">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="text-sm font-semibold text-fg">本地配置</h2>
          <!-- 点明这里填的是 mihomo 内核配置：本页同时还有「远程订阅」区块，
               而下方分组表单的字段名（dns / tun / sniffer 等）都是 mihomo 的，
               不说明的话容易被当成本平台自身的设置（那些在「系统设置」里）。 -->
          <p class="text-xs text-fg-subtle mt-0.5">加载、保存下方分组表单中的本地基础配置，即 mihomo 内核配置</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button variant="outline" :disabled="store.loading" @click="reloadBase()">放弃修改（重新加载）</Button>
          <!-- 未加载成功时禁用两个写按钮：此时表单显示的是 model 初值（空），
               保存会把服务端配置整份覆盖成空。禁用而非仅靠 store 内部拦截，
               是为了让用户在点击前就看到「不可保存」，而不是点完才收到报错。 -->
          <Button
            :disabled="store.loading || !store.baseLoaded"
            :title="store.baseLoaded ? '' : '基础配置未加载成功，无法保存'"
            @click="saveBase()"
          >
            保存基础配置
          </Button>
          <!-- 只用已缓存的远程层重新生成最终配置，不回源拉取。
               需要拉取远程时用上方「远程订阅」区块里的「立即拉取并合并」。 -->
          <Button
            variant="secondary"
            :disabled="store.loading || !store.baseLoaded"
            :title="store.baseLoaded ? '保存本地配置并重新生成最终配置（不会拉取远程）' : '基础配置未加载成功，无法保存'"
            @click="saveAndMerge()"
          >
            保存并应用
          </Button>
        </div>
      </div>

      <!-- 用户只点击了「保存基础配置」而尚未合并下发给内核时的提示条 -->
      <div
        v-if="store.baseLoaded && store.unmergedChanges"
        class="mt-3 note-warn border rounded p-3 text-sm flex flex-col sm:flex-row sm:items-center justify-between gap-2"
        role="status"
      >
        <div>
          <div class="font-semibold">
            本地基础配置已保存，但尚未应用生效
          </div>
          <p class="text-xs opacity-90 mt-0.5">
            改动已保存到数据库，但尚未合并并作用于 mihomo 内核配置。点击右侧「应用并生效」使其生效。
          </p>
        </div>
        <Button
          size="sm"
          variant="secondary"
          class="shrink-0"
          :disabled="store.loading"
          @click="saveAndMerge()"
        >
          应用并生效
        </Button>
      </div>
    </section>

    <!-- 加载失败时给出明确错误态并提供重试入口。
         不能直接渲染下方表单：model 是空的，表单看起来像「这台机器还没配过」，
         用户很容易照着填一遍再保存，等于用空配置覆盖真实配置。 -->
    <section v-if="!store.baseLoaded && !store.loading" class="note-err border rounded p-4 mb-4" role="alert">
      <p class="text-sm font-semibold">基础配置加载失败</p>
      <p class="text-xs mt-1">{{ store.baseLoadError || '未能读取服务端配置' }}</p>
      <p class="text-xs mt-1">已隐藏配置表单以防误将空配置写回服务端。请重试加载。</p>
      <Button variant="outline" class="mt-3" @click="store.fetchBase()">
        重新加载
      </Button>
    </section>

    <!-- 手机上这个 div 只是普通块级元素，随页面一起自然滚动；
         lg 上分组导航改用 sticky 固定在视口内，随页面滚动“跟随”，
         两栏都不再各自内滚，统一交给浏览器原生滚动条。
         baseLoaded 为假时整块隐藏，理由见上方错误态注释。 -->
    <div v-if="store.baseLoaded" class="grid gap-3 sm:gap-4 lg:grid-cols-4">
      <!-- 十一个分组：窄屏横向滑动的 chip 行，桌面端恢复为竖直侧栏。
           lg:top-4 留出与页面顶部的间距，避免贴死在视口边缘。 -->
      <aside class="bg-surface border rounded p-2 lg:p-3 flex lg:flex-col gap-1 overflow-x-auto lg:overflow-x-visible no-scrollbar lg:sticky lg:top-4 lg:self-start shrink-0">
        <Button
          v-for="sec in baseConfigSchema"
          :key="sec.id"
          variant="ghost"
          class="shrink-0 lg:w-full justify-start text-left whitespace-nowrap lg:mb-1"
          :class="active === sec.id ? 'bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground' : 'hover:bg-elevated'"
          @click="active = sec.id"
        >
          {{ sec.title }}
        </Button>
      </aside>

      <section class="lg:col-span-3 bg-surface border rounded p-3 sm:p-5 space-y-4">
        <div v-for="sec in baseConfigSchema.filter((s) => s.id === active)" :key="sec.id">
          <h2 class="text-xl font-semibold mb-4">{{ sec.title }}</h2>
          
          <!-- TUN 分组提示：这一段与「系统设置 → 透明代理」的开关是同一份数据，
               用户需要知道在哪改都算改、以及何时才真正生效。
               配色沿用 SettingsView 里待确认横幅的写法（当前没有可用的 info token）。 -->
          <div
            v-if="sec.id === 'tun' && transparentTUNActive"
            class="mb-4 rounded-lg border border-blue-300 bg-blue-50 p-3 dark:border-blue-500/40 dark:bg-blue-500/10"
            role="note"
          >
            <div class="text-sm font-semibold text-blue-800 dark:text-blue-300">
              TUN 当前已开启
            </div>
            <p class="mt-1 text-xs text-blue-700 dark:text-blue-400">
              这里的 tun.enable 与
              <RouterLink to="/settings" class="underline hover:text-blue-900 dark:hover:text-blue-200">
                系统设置 → 透明代理
              </RouterLink>
              的开关是同一项配置，两边改的都是它。在本页修改后需点「保存并应用」才会生效，
              系统设置里的开关状态也在那时同步。只点「保存基础配置」不会重新下发配置。
              若开启 Auto Redirect 后看不到 Meta 网卡，请先查内核日志是否
              netlink “file exists”（Alpine 常见）：确认 mihomo 进程带有
              DISABLE_NFTABLES=1 后冷启动内核；旁路由场景一般应保持 Auto Redirect 开启。
            </p>
          </div>
          
          <div class="space-y-4">
            <!-- items-start 而非 center：字段说明文字换行后，
                 居中会让标签浮在输入框中间 -->
            <div v-for="f in sec.fields" :key="f.key" class="grid md:grid-cols-3 gap-1.5 md:gap-3 md:items-center">
              <!-- for/id 用 f.key 生成（schema 内唯一）：不关联的话读屏只会
                   念出「输入框」而听不到字段名，switch 那种只有标签的更是完全无从判断。
                   f.help 经 aria-describedby 一并关联，作为补充说明朗读。 -->
              <label :id="labelId(f.key)" :for="fieldId(f.key)" class="text-sm text-fg-muted">{{ f.title }}</label>
              <div class="md:col-span-2 min-w-0">
                <Checkbox
                  v-if="f.type === 'switch'"
                  :id="fieldId(f.key)"
                  :aria-describedby="f.help ? helpId(f.key) : undefined"
                  :model-value="!!displayValue(f.key, f.type)"
                  @update:model-value="onSwitch(f.key, $event === true)"
                />
                <Select
                  v-else-if="f.type === 'select'"
                  :model-value="selectDisplayValue(f.key, f.type)"
                  @update:model-value="onSelectChange(f.key, f.type, $event as string)"
                >
                  <SelectTrigger :id="fieldId(f.key)" class="w-full" :aria-describedby="f.help ? helpId(f.key) : undefined">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="opt in normalizeOptions(f.options)"
                      :key="opt.value"
                      :value="opt.value || UNSET_SELECT_VALUE"
                    >
                      {{ opt.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <!-- url-preset：预设地址下拉 + 自定义输入框。下拉负责「国内镜像 /
                     官方源」一键填入与「未设置」清空，输入框始终可编辑当前值。 -->
                <div v-else-if="f.type === 'url-preset'" class="flex flex-col sm:flex-row gap-2">
                  <Select
                    :model-value="presetSelectValue(f.key, f.presets)"
                    @update:model-value="onPresetSelect(f.key, f.presets, $event as string)"
                  >
                    <SelectTrigger :id="fieldId(f.key)" class="w-full sm:w-44 shrink-0" :aria-describedby="f.help ? helpId(f.key) : undefined">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="opt in normalizeOptions(f.presets)" :key="opt.value" :value="opt.value">
                        {{ opt.label }}
                      </SelectItem>
                      <SelectItem :value="CUSTOM_SELECT_VALUE">自定义…</SelectItem>
                      <SelectItem :value="UNSET_SELECT_VALUE">未设置（使用内核默认）</SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    type="text"
                    class="flex-1 min-w-0"
                    :placeholder="f.placeholder"
                    :aria-label="f.title + '（自定义地址）'"
                    :model-value="displayValue(f.key, f.type)"
                    @input="onInput(f.key, ($event.target as HTMLInputElement).value, f.type)"
                  />
                </div>
                <!-- 顶层 hosts：键值行编辑器。它自己维护编辑期状态，
                     不走 drafts / 失焦提交那套（域名是 map 的键，逐字符
                     改键会碰撞，理由见 HostsEditor.vue 的文件头注释）。 -->
                <HostsEditor
                  v-else-if="f.type === 'hosts-map'"
                  :model-value="hostsMapValue(f.key)"
                  :labelledby="labelId(f.key)"
                  :describedby="f.help ? helpId(f.key) : undefined"
                  @update:model-value="onHostsMap(f.key, $event)"
                />
                <!-- 源码类字段（YAML/JS）用 CodeMirror 编辑，带语法高亮与行号。
                     它不像 textarea 那样会被规范化回灌打断输入，因此直接用
                     草稿值 + 变更事件，失焦时统一提交。 -->
                <template v-else-if="f.code">
                  <!-- CodeMirror 不是原生控件，label 的 for 到不了它内部的
                       contenteditable，只能反向用 aria-labelledby 指回标签 -->
                  <CodeEditor
                    :model-value="fieldValue(f.key, f.type)"
                    :language="f.code"
                    :height="f.codeHeight || '260px'"
                    :placeholder="f.placeholder"
                    :aria-labelledby="labelId(f.key)"
                    @update:model-value="onCodeInput(f.key, f.type, $event)"
                  />
                  <!-- 这两个字段清空编辑器不再等于清空配置（否则输入过程必经的
                       「空」中间态就会把内容删光），所以清空要有显式入口。 -->
                  <Button
                    v-if="f.key === 'advanced-raw' || f.key === 'proxy-groups-raw'"
                    type="button"
                    variant="outline"
                    size="sm"
                    class="mt-2"
                    @click="clearRawField(f.key)"
                  >
                    清空此项
                  </Button>
                </template>
                <Textarea
                  v-else-if="f.type === 'yaml-object'"
                  :id="fieldId(f.key)"
                  class="font-mono text-sm"
                  rows="6"
                  :placeholder="f.placeholder"
                  :aria-describedby="f.help ? helpId(f.key) : undefined"
                  :model-value="fieldValue(f.key, f.type)"
                  @input="onDraftInput(f.key, ($event.target as HTMLTextAreaElement).value)"
                  @blur="onDraftBlur(f.key, f.type)"
                />
                <Textarea
                  v-else-if="f.type === 'textarea'"
                  :id="fieldId(f.key)"
                  class="font-mono text-sm"
                  rows="10"
                  :placeholder="f.placeholder"
                  :aria-describedby="f.help ? helpId(f.key) : undefined"
                  :model-value="fieldValue(f.key, f.type)"
                  @input="onDraftInput(f.key, ($event.target as HTMLTextAreaElement).value)"
                  @blur="onDraftBlur(f.key, f.type)"
                />
                <Input
                  v-else-if="f.type === 'string-array'"
                  :id="fieldId(f.key)"
                  type="text"
                  :placeholder="f.placeholder"
                  :aria-describedby="f.help ? helpId(f.key) : undefined"
                  :model-value="fieldValue(f.key, f.type)"
                  @input="onDraftInput(f.key, ($event.target as HTMLInputElement).value)"
                  @blur="onDraftBlur(f.key, f.type)"
                />
                <Input
                  v-else
                  :id="fieldId(f.key)"
                  :type="f.type === 'number' ? 'number' : 'text'"
                  :placeholder="f.placeholder"
                  :aria-describedby="f.help ? helpId(f.key) : undefined"
                  :model-value="displayValue(f.key, f.type)"
                  @input="onInput(f.key, ($event.target as HTMLInputElement).value, f.type)"
                />
                <p v-if="f.help" :id="helpId(f.key)" class="text-xs text-fg-subtle mt-1">{{ f.help }}</p>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  </main>
</template>

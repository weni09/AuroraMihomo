<script setup lang="ts">
import { fileTypeLabel, templateLangLabel } from '../utils/labels'
import { computed, onMounted, reactive, ref } from 'vue'
import { loadYaml, dumpYaml } from '../utils/yaml'
import { useFilesStore } from '../stores/files'
import { useSubscriptionStore } from '../stores/subscription'
import { useConfigLabStore } from '../stores/configlab'
import { usePreviewStore } from '../stores/preview'
import CodeEditor from '../components/CodeEditor.vue'
import PreviewPanel from '../components/PreviewPanel.vue'
import ShareDialog from '../components/ShareDialog.vue'
import ModalDialog from '../components/ModalDialog.vue'
import { useNotifyStore } from '../stores/notify'
import { useCopy } from '../composables/useCopy'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Copy, Eye, ExternalLink, MoreHorizontal, Pencil, Plus, RefreshCw, Save, Share2, Trash2, Wand2, X } from 'lucide-vue-next'

const notify = useNotifyStore()
const { copy } = useCopy()
const store = useFilesStore()
const subs = useSubscriptionStore()
const lab = useConfigLabStore()
const preview = usePreviewStore()

const form = reactive({
  name: '',
  content: '',
  type: 'raw',
  // 远程正文地址，支持多行、每行一个
  syncUrl: '',
  // local=正文取自编辑器；remote=取自上面的地址
  sourceMode: 'local',
  // 空=不合并；localFirst / remoteFirst 决定拼接顺序
  mergeSources: '',
  // ''=失败即报错；enabled=跳过并提示；quiet=静默跳过
  ignoreFailedRemote: '',
  userAgent: '',
  // file=原样输出；mihomo=作为模板套用节点渲染
  configType: 'file',
  sourceType: 'subscription',
  sourceId: 0,
  // 仅 configType=mihomo 时生效：yaml（覆写深合并）/ gotemplate（Go 模板）/ javascript（脚本覆写）。
  // 新建默认 yaml：直接粘贴现成 mihomo 配置就能用，是最常见的用法；
  // 存量数据没有该字段时后端按 gotemplate 解释，行为不变。
  templateLang: 'yaml',
  trafficUrl: '',
})
const editing = ref<any>(null)
const dialogOpen = ref(false)

// shadcn Select 的 SelectItem 不接受空字符串 value（reka-ui 用空串表示
// "未选中"）。form.mergeSources / form.ignoreFailedRemote 的空串是有效业务值
// （"不合并" / "失败即报错"），用 __none__ 占位，转换只在这两个 computed 里做，
// 其余逻辑仍按空字符串语义读写 form 字段
const mergeSourcesSelectValue = computed({
  get: () => form.mergeSources || '__none__',
  set: (v: string) => { form.mergeSources = v === '__none__' ? '' : v },
})
const ignoreFailedRemoteSelectValue = computed({
  get: () => form.ignoreFailedRemote || '__none__',
  set: (v: string) => { form.ignoreFailedRemote = v === '__none__' ? '' : v },
})

const isTemplate = computed(() => form.configType === 'mihomo')

// 合并开启时两侧都要用，因此本地编辑器与远程地址栏都得显示
const merging = computed(() => form.mergeSources !== '')
const usesLocal = computed(() => form.sourceMode === 'local' || merging.value)
const usesRemote = computed(() => form.sourceMode === 'remote' || merging.value)

// Go 模板语法、YAML 覆写的 {{}} 无关字符与 Vue 插值都用双花括号，
// 放进 JS 常量避免被 Vue 编译
const goTemplatePlaceholder = [
  '# 文件内容即 Go 模板，可用 .Nodes 遍历所选订阅的节点',
  'proxies:',
  '{{ range .Nodes }}  - name: "{{ .Name }}"',
  '    type: {{ .Type }}',
  '    server: {{ .Server }}',
  '    port: {{ .Port }}',
  '{{ end }}',
  'proxy-groups:',
  '  - name: Proxy',
  '    type: select',
  '    proxies:',
  '{{ range .Nodes }}      - "{{ .Name }}"',
  '{{ end }}',
  'rules:',
  '  - MATCH,Proxy',
].join('\n')

// YAML 覆写：正文与自动生成的基础配置（proxies/proxy-groups/rules）深度合并，
// +key 前插 / key+ 追加 / key! 强制整体覆盖，对齐官方 Sub-Store 的 YAML 覆写语义
const yamlOverridePlaceholder = [
  '# 与自动生成的基础配置（proxies/proxy-groups/rules）深度合并',
  '# +key: 前插；key+: 追加；key!: 强制整体覆盖（不合并）',
  'rules+:',
  '  - DOMAIN,example.com,DIRECT',
].join('\n')

// JS 脚本覆写：接收完整配置对象，返回值覆盖原配置，对齐官方 Sub-Store 的 JavaScript 覆写
const jsOverridePlaceholder = [
  '// config 为自动生成的基础配置（proxies/proxy-groups/rules），必须 return',
  'function main(config) {',
  '  config.rules.unshift("DOMAIN,example.com,DIRECT")',
  '  return config',
  '}',
].join('\n')

/**
 * 三种模板语法的完整参考示例（公开仓库，可对照抄写/改写）。
 * 与下方 templateLang 选项一一对应，编辑器旁会按当前语言高亮对应链接。
 */
// 示例必须是 raw 直链：blob 页面返回 HTML，粘贴进「远程地址」后
// 预览/同步会把整页当配置解析而 400。后端虽会把 github.com/.../blob/
// 改写成 raw.githubusercontent.com，示例本身仍应给可直接下载的地址，
// 避免用户照抄后仍踩坑（其它托管没有自动改写）。
const TEMPLATE_EXAMPLES = [
  {
    lang: 'yaml' as const,
    title: 'YAML 覆写',
    url: 'https://raw.githubusercontent.com/weni09/clash_my_conf/main/mihomo.yaml',
  },
  {
    lang: 'gotemplate' as const,
    title: 'Go 模板',
    url: 'https://raw.githubusercontent.com/weni09/clash_my_conf/main/mihomo-gotemplate.yaml',
  },
  {
    lang: 'javascript' as const,
    title: 'JS 脚本覆写',
    url: 'https://raw.githubusercontent.com/weni09/clash_my_conf/main/mihomo-jstemplate.yaml',
  },
] as const

const currentTemplateExample = computed(
  () => TEMPLATE_EXAMPLES.find((e) => e.lang === form.templateLang) ?? TEMPLATE_EXAMPLES[0],
)
// 占位示例随类型切换，降低「不知道该填什么」的成本
const contentPlaceholder = computed(() => {
  if (isTemplate.value) {
    if (form.templateLang === 'yaml') return yamlOverridePlaceholder
    if (form.templateLang === 'javascript') return jsOverridePlaceholder
    return goTemplatePlaceholder
  }
  if (form.type === 'script') {
    return `// 节点处理脚本：传入 proxies 数组，返回处理后的数组\nreturn proxies.filter((p) => p.type !== 'ss')`
  }
  if (form.type === 'json') {
    return `{\n  "payload": ["DOMAIN-SUFFIX,example.com"]\n}`
  }
  if (form.type === 'yaml') {
    return `payload:\n  - DOMAIN-SUFFIX,example.com\n  - DOMAIN-KEYWORD,example`
  }
  if (form.type === 'ini') {
    return `[General]\nbypass-system = true\nskip-proxy = 127.0.0.1, 192.168.0.0/16`
  }
  return '在此输入文件内容，如 Surge 模块、规则片段等'
})

// 文件格式/模板语言 -> CodeEditor 语言模式，概念不完全重合：
// raw 在编辑器里按纯文本处理（无语法可高亮），script 对应 javascript
const editorLanguage = computed(() => {
  if (isTemplate.value) {
    if (form.templateLang === 'javascript') return 'javascript'
    return 'yaml' // gotemplate 与 yaml 覆写都按 YAML 高亮
  }
  if (form.type === 'script') return 'javascript'
  if (form.type === 'json' || form.type === 'yaml' || form.type === 'ini') return form.type
  return 'text'
})

// 本地内容的真实格式校验：不只是编辑器语法高亮的装饰，
// JSON/YAML 用真实的解析器校验，INI 没有现成库，退化为逐行结构检查，
// JavaScript 只做语法检查（new Function 只编译不执行函数体，无副作用）
const validateContentFormat = (type: string, content: string): string => {
  const text = content.trim()
  if (!text) return ''
  switch (type) {
    case 'json':
      try {
        JSON.parse(text)
      } catch (e: any) {
        return `JSON 格式错误：${e?.message || '无法解析'}`
      }
      break
    case 'yaml':
      try {
        loadYaml(text)
      } catch (e: any) {
        return `YAML 格式错误：${e?.message || '无法解析'}`
      }
      break
    case 'ini':
      for (const [i, rawLine] of text.split('\n').entries()) {
        const line = rawLine.trim()
        if (!line || line.startsWith(';') || line.startsWith('#')) continue
        const isSection = /^\[.+\]$/.test(line)
        const isKeyValue = /^[^=\s][^=]*=.*$/.test(line)
        if (!isSection && !isKeyValue) {
          return `INI 格式错误：第 ${i + 1} 行不是合法的 [section] 或 key = value（内容：${line}）`
        }
      }
      break
    case 'script':
      try {
        new Function(content)
      } catch (e: any) {
        return `JavaScript 语法错误：${e?.message || '无法解析'}`
      }
      break
  }
  return ''
}

// 仅 JSON/YAML 有确定的规范化写法，值得提供「格式化」；
// INI 没有统一规范，script/raw 不需要
const canFormat = computed(() => !isTemplate.value && (form.type === 'json' || form.type === 'yaml'))

const formatContent = () => {
  const text = form.content.trim()
  if (!text) return
  try {
    if (form.type === 'json') {
      form.content = JSON.stringify(JSON.parse(text), null, 2)
    } else if (form.type === 'yaml') {
      // 注意：格式化会把锚点引用就地展开成实际内容（YAML 解析的固有行为，
      // 锚点信息在解析阶段就已丢失），原本靠 <<: *anchor 复用的写法会变成
      // 逐条铺开的完整配置。功能上等价，但会失去手写模板的简洁性，
      // 所以这里先提示一次再改，避免用户以为只是重排缩进。
      const hasAnchor = /(^|\s)[&*][A-Za-z0-9_-]+|<<\s*:/.test(text)
      if (hasAnchor && !confirm('该内容使用了 YAML 锚点（& / * / <<:）。格式化会把锚点引用展开为实际内容，无法还原，确定继续？')) {
        return
      }
      form.content = dumpYaml(loadYaml(text))
    }
  } catch (e: any) {
    notify.error(`格式化失败：${e?.message || '内容不是合法的 ' + form.type.toUpperCase()}`)
  }
}

const sourceOptions = computed(() =>
  form.sourceType === 'collection'
    ? lab.collections.map((c: any) => ({ id: c.id, name: c.name, enabled: c.enabled }))
    : subs.subscriptions.map((s: any) => ({ id: s.id, name: s.name, enabled: s.enabled })),
)

onMounted(async () => {
  await Promise.all([store.fetch(), subs.fetchSubscriptions(), lab.fetchCollections()])
})

// 表单校验集中一处：来源方式、合并方式与地址三者相互影响，
// 分散判断容易漏掉组合情形
const validate = (): string => {
  if (!form.name) return '文件名不能为空'
  if (usesLocal.value && !form.content.trim()) {
    return merging.value ? '合并模式下本地内容不能为空' : '文件内容不能为空'
  }
  if (usesRemote.value && !form.syncUrl.trim()) {
    return '使用远程内容时必须填写至少一个远程地址'
  }
  // 前端先挡一道，避免建出「声明为配置模板但没有节点来源」的文件——
  // 那样直到别人访问分享链接时才会报错
  if (isTemplate.value && !form.sourceId) {
    return '配置类型为 Mihomo 配置时，必须选择节点来源'
  }
  // 本地内容按所选格式做真实校验：不只是编辑器高亮，保存/预览前就该拦下格式错误。
  // 模板正文（Go 模板/YAML 覆写/JS 脚本）不在此校验：Go 模板前端没有解析器，
  // 交由后端保存时校验；YAML 覆写与 JS 脚本覆写在下面单独处理
  if (!isTemplate.value && usesLocal.value) {
    const err = validateContentFormat(form.type, form.content)
    if (err) return err
  }
  if (isTemplate.value && form.templateLang !== 'gotemplate' && usesLocal.value) {
    const err = validateContentFormat(form.templateLang === 'yaml' ? 'yaml' : 'script', form.content)
    if (err) return err
  }
  return ''
}

const onSave = async () => {
  const err = validate()
  if (err) return notify.error(err)
  if (editing.value) {
    await store.update(editing.value.id, { ...form })
  } else {
    await store.create({ ...form })
  }
  closeDialog()
}

const onEdit = (f: any) => {
  editing.value = f
  form.name = f.name
  form.content = f.content
  form.type = f.type
  form.syncUrl = f.syncUrl
  form.sourceMode = f.sourceMode || 'local'
  form.mergeSources = f.mergeSources || ''
  form.ignoreFailedRemote = f.ignoreFailedRemote || ''
  form.userAgent = f.userAgent || ''
  form.configType = f.configType || 'file'
  form.sourceType = f.sourceType || 'subscription'
  form.sourceId = f.sourceId || 0
  // 这里的兜底刻意是 gotemplate 而非新建时的 yaml：存量文件没有该字段时，
  // 后端同样按 gotemplate 解释其正文。若跟着改成 yaml，打开旧文件会把
  // 一份 Go 模板当成 YAML 覆写来解释，等于静默改变它的渲染结果。
  form.templateLang = f.templateLang || 'gotemplate'
  form.trafficUrl = f.trafficUrl || ''
  dialogOpen.value = true
}

// ===== 即时预览 =====
const onPreview = async () => {
  const err = validate()
  if (err) return notify.error(err)
  await preview.run({
    kind: 'file',
    content: form.content,
    syncUrl: form.syncUrl,
    sourceMode: form.sourceMode,
    mergeSources: form.mergeSources,
    ignoreFailedRemote: form.ignoreFailedRemote,
    userAgent: form.userAgent,
    configType: form.configType,
    sourceType: form.sourceType,
    sourceId: form.sourceId,
    templateLang: form.templateLang,
  })
}

const onPreviewSaved = async (f: any) => {
  await preview.run({
    kind: 'file',
    content: f.content,
    syncUrl: f.syncUrl,
    sourceMode: f.sourceMode || 'local',
    mergeSources: f.mergeSources || '',
    ignoreFailedRemote: f.ignoreFailedRemote || '',
    userAgent: f.userAgent || '',
    configType: f.configType || 'file',
    sourceType: f.sourceType || 'subscription',
    sourceId: f.sourceId || 0,
    templateLang: f.templateLang || 'gotemplate',
  })
}

// ===== 分享设置 =====
const shareTarget = ref<any>(null)

const onDelete = async (id: number) => {
  if (!confirm('确定删除此文件？删除后其分享链接立即失效。')) return
  await store.remove(id)
}

const syncingId = ref<number | null>(null)

const onSync = async (f: any) => {
  if (!f.syncUrl) return notify.error('该文件未配置远程地址')
  if (!confirm(`将拉取远程内容并覆盖「${f.name}」的本地正文，确定继续？`)) return
  syncingId.value = f.id
  try {
    await store.sync(f.id)
  } catch {
    // 失败提示由 store 统一 toast
  } finally {
    syncingId.value = null
  }
}

const resetForm = () => {
  editing.value = null
  form.name = ''
  form.content = ''
  form.type = 'raw'
  form.syncUrl = ''
  form.sourceMode = 'local'
  form.mergeSources = ''
  form.ignoreFailedRemote = ''
  form.userAgent = ''
  form.configType = 'file'
  form.sourceType = 'subscription'
  form.sourceId = 0
  form.templateLang = 'yaml'
  form.trafficUrl = ''
}

// 弹窗关闭时一并清空表单与预览结果，避免下次打开时残留上一次的草稿或预览内容
const closeDialog = () => {
  dialogOpen.value = false
  resetForm()
  preview.close()
}

const openCreate = () => {
  resetForm()
  dialogOpen.value = true
}

// 切换来源类型时清空已选项，避免把订阅 ID 当成组合 ID 提交
const onSourceTypeChange = () => {
  form.sourceId = 0
}

const origin = window.location.origin

const shareUrl = (f: any) => (f.shareToken ? `${origin}/api/v1/file/${f.shareToken}` : '')

const copyShare = (f: any) => copy(shareUrl(f), '分享链接')

const sourceLabel = (f: any) => {
  if (f.configType !== 'mihomo') return ''
  const list = f.sourceType === 'collection' ? lab.collections : subs.subscriptions
  const hit = (list as any[]).find((x) => x.id === f.sourceId)
  const kind = f.sourceType === 'collection' ? '组合' : '订阅'
  return hit ? `${kind}：${hit.name}` : `${kind}：已失效(#${f.sourceId})`
}
</script>

<template>
  <main class="max-w-6xl mx-auto space-y-4 sm:space-y-6">
    <div class="flex justify-between items-center gap-3 flex-wrap">
      <div class="min-w-0">
        <h1 class="text-2xl sm:text-3xl font-bold text-fg">模板转换</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1 flex flex-wrap items-center gap-x-1 gap-y-0.5">
          <span>原样输出或 Mihomo 配置模板。语法参考：</span>
          <template v-for="(ex, i) in TEMPLATE_EXAMPLES" :key="ex.lang">
            <a
              :href="ex.url"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary hover:underline inline-flex items-center gap-0.5"
            >
              {{ ex.title }}
              <ExternalLink class="size-3 opacity-70" aria-hidden="true" />
            </a>
            <span v-if="i < TEMPLATE_EXAMPLES.length - 1" class="text-fg-subtle" aria-hidden="true">·</span>
          </template>
        </p>
        <!-- 参考模板不只是语法示例：它们的 raw 直链可直接作为模板的远程内容来源。
             提示放在语法参考旁，让用户不必在「本地编辑」与「远程拉取」两个入口间
             来回试错才知道这条用法。 -->
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">
          以上示例也可直接当作模板使用：把对应 raw 链接填进「远程地址」，选择远程拉取即可。
        </p>
      </div>
      <Button @click="openCreate"><Plus class="h-4 w-4" aria-hidden="true" />新建模板转换</Button>
    </div>

    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <!-- 模板转换列表。卡片与徽标改用 components/ui 下的 shadcn 组件，
         与订阅页、组合页共享同一套排版与状态配色。 -->
    <div class="space-y-3 sm:space-y-4">
      <Card v-if="store.files.length === 0" class="border-dashed shadow-none">
        <CardContent class="p-8 sm:p-10 text-center text-sm text-fg-subtle flex flex-col gap-3 items-center">
          <p class="italic">当前暂无模板转换。</p>
          <p class="text-xs max-w-md">
            新建时可选择 YAML 覆写、Go 模板或 JS 脚本。三种语法的完整示例：
          </p>
          <div class="flex flex-wrap justify-center gap-2">
            <a
              v-for="ex in TEMPLATE_EXAMPLES"
              :key="ex.lang"
              :href="ex.url"
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex items-center gap-1 rounded-md border border-line bg-surface px-2.5 py-1 text-xs text-primary hover:bg-elevated"
            >
              <ExternalLink class="size-3" aria-hidden="true" />
              {{ ex.title }}
            </a>
          </div>
        </CardContent>
      </Card>

      <Card v-for="f in store.files" :key="f.id" class="transition-shadow hover:shadow-md">
        <div class="flex flex-col gap-3 p-4 sm:p-5">
          <!-- 窄屏纵向堆叠：文件名较长、徽标较多时和右侧按钮挤在一行会互相挤压 -->
          <div class="flex flex-col gap-2 sm:flex-row sm:justify-between">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <CardTitle class="break-all text-lg">{{ f.name }}</CardTitle>
              <Badge :variant="f.configType === 'mihomo' ? 'accent' : 'neutral'">
                {{ f.configType === 'mihomo' ? 'Mihomo 配置模板' : '原样输出' }}
              </Badge>
              <Badge v-if="f.configType !== 'mihomo'" variant="outline">
                {{ fileTypeLabel(f.type) }}
              </Badge>
              <Badge v-if="f.configType === 'mihomo'" variant="outline">
                {{ templateLangLabel(f.templateLang) }}
              </Badge>
              <span v-if="f.configType === 'mihomo'" class="text-xs text-fg-muted">{{ sourceLabel(f) }}</span>
            </div>
            <!-- 只留预览/编辑两个高频操作，其余（立即同步/分享设置/删除）
                 收进「更多」下拉菜单——此前 5 个按钮换行会撑高数据行，
                 与订阅/组合两页保持一致的收纳方式 -->
            <div class="flex flex-wrap items-center gap-1.5 sm:shrink-0">
              <Button variant="secondary" size="sm" :disabled="preview.loading" @click="onPreviewSaved(f)"><Eye class="h-4 w-4" aria-hidden="true" />预览</Button>
              <Button variant="outline" size="sm" @click="onEdit(f)"><Pencil class="h-4 w-4" aria-hidden="true" />编辑</Button>
              <DropdownMenu>
                <DropdownMenuTrigger as-child>
                  <Button variant="ghost" size="icon-sm" class="tap-target" aria-label="更多操作">
                    <MoreHorizontal class="h-4 w-4" aria-hidden="true" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem v-if="f.syncUrl" :disabled="syncingId === f.id" @click="onSync(f)">
                    <RefreshCw class="h-4 w-4" aria-hidden="true" />
                    {{ syncingId === f.id ? '同步中…' : '立即同步' }}
                  </DropdownMenuItem>
                  <DropdownMenuItem @click="shareTarget = f">
                    <Share2 class="h-4 w-4" aria-hidden="true" />
                    分享设置
                  </DropdownMenuItem>
                  <DropdownMenuItem class="text-rose-600 focus:text-rose-600 dark:text-rose-400 dark:focus:text-rose-400" @click="onDelete(f.id)">
                    <Trash2 class="h-4 w-4" aria-hidden="true" />
                    删除
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>

          <div class="line-clamp-3 rounded-md border border-line bg-elevated p-2 font-mono text-xs text-fg-muted">
            {{ f.content }}
          </div>

          <div v-if="f.syncUrl" class="break-all text-xs text-fg-muted">
            <span>远程地址：</span>
            <span class="whitespace-pre-line font-mono">{{ f.syncUrl }}</span>
            <span v-if="f.mergeSources" class="ml-2 text-fg-subtle">
              （{{ f.mergeSources === 'localFirst' ? '本地在前合并' : '远程在前合并' }}）
            </span>
            <span v-else-if="f.sourceMode === 'remote'" class="ml-2 text-fg-subtle">（纯远程）</span>
          </div>

          <div v-if="f.shareToken" class="space-y-1.5 border-t border-line pt-3">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-xs text-fg-muted">分享链接</span>
              <Button variant="outline" size="sm" @click="copyShare(f)"><Copy class="h-4 w-4" aria-hidden="true" />复制链接</Button>
              <Badge v-if="f.expiresAt" variant="warn">
                有效期至 {{ new Date(f.expiresAt).toLocaleString() }}
              </Badge>
            </div>
            <code class="block break-all select-all rounded-md border border-line bg-elevated px-3 py-1.5 font-mono text-xs text-primary">
              {{ shareUrl(f) }}
            </code>
            <p class="text-xs text-fg-subtle">
              该链接无需登录即可访问，凭据即链接本身。点上方「分享设置」可改名、设有效期或重置。
            </p>
          </div>
          <p v-else class="border-t border-line pt-3 text-xs text-fg-subtle">
            分享已撤销，点上方「分享设置」可重新生成链接。
          </p>
        </div>
      </Card>
    </div>

    <!-- 预览结果用弹窗展示（PreviewPanel 内部自带遮罩），不再嵌在页面里；
         列表行「预览」与新建/编辑弹窗内「即时预览」共用同一份，
         由 preview store 的状态决定是否显示 -->
    <PreviewPanel
      :result="preview.result"
      :loading="preview.loading"
      :error="preview.error"
      @close="preview.close()"
    />

    <ModalDialog
      :open="dialogOpen"
      :title="editing ? '编辑模板转换' : '新建模板转换'"
      max-width="max-w-4xl"
      @close="closeDialog"
    >
      <p class="text-xs text-fg-muted mb-4">
        文件可原样对外提供（规则片段、Surge 模块等），也可声明为 Mihomo 配置模板——
        由所选订阅的节点套用模板渲染，产出可直接订阅的配置。
      </p>

      <div class="grid md:grid-cols-2 gap-4">
        <div class="space-y-1">
          <Label for="file-name" class="text-sm font-semibold text-fg-muted">文件名</Label>
          <Input id="file-name" v-model="form.name" placeholder="必须唯一" :disabled="!!editing" />
        </div>
        <div class="space-y-1">
          <Label for="file-config-type" class="text-sm font-semibold text-fg-muted">配置类型</Label>
          <Select v-model="form.configType">
            <SelectTrigger id="file-config-type" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="file">文件（原样输出）</SelectItem>
              <SelectItem value="mihomo">Mihomo 配置（模板转换）</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <template v-if="isTemplate">
          <div class="space-y-1">
            <Label for="file-source-type" class="text-sm font-semibold text-fg-muted">节点来源类型</Label>
            <Select v-model="form.sourceType" @update:model-value="onSourceTypeChange">
              <SelectTrigger id="file-source-type" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="subscription">单条订阅</SelectItem>
                <SelectItem value="collection">组合订阅</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="space-y-1">
            <Label for="file-source-id" class="text-sm font-semibold text-fg-muted">选择来源</Label>
            <Select v-model="form.sourceId">
              <SelectTrigger id="file-source-id" class="w-full"><SelectValue placeholder="请选择…" /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="o in sourceOptions" :key="o.id" :value="o.id">
                  {{ o.name }}{{ o.enabled === false ? '（已停用）' : '' }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="space-y-1 md:col-span-2">
            <Label for="file-template-lang" class="text-sm font-semibold text-fg-muted">模板语言</Label>
            <!-- YAML 覆写排在最前：直接粘贴一份现成的 mihomo 配置即可用，
                 是从 Sub-Store 迁配置过来最常走的路径；Go 模板要从零手写整份
                 结构，门槛更高，作为兼容存量数据的选项排在后面。 -->
            <Select v-model="form.templateLang">
              <SelectTrigger id="file-template-lang" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="yaml">YAML 覆写（在自动生成的基础配置上打补丁）</SelectItem>
                <SelectItem value="gotemplate">Go 模板（从零手写整份配置）</SelectItem>
                <SelectItem value="javascript">JS 脚本覆写（function main(config)）</SelectItem>
              </SelectContent>
            </Select>
            <p class="text-xs text-fg-subtle">
              Go 模板需要自己写全 proxies/proxy-groups/rules；YAML 覆写与 JS 脚本覆写则是在系统按所选节点来源
              自动生成的基础配置上做增量修改，对齐官方 Sub-Store 的「覆写」用法。
            </p>
            <!-- 三种语法各给一条可打开的完整示例，降低从零开写的门槛 -->
            <div class="rounded-lg border border-line bg-elevated/60 px-3 py-2 text-xs text-fg-muted flex flex-col gap-1.5">
              <div class="font-medium text-fg">参考完整示例（GitHub）</div>
              <ul class="flex flex-col gap-1">
                <li v-for="ex in TEMPLATE_EXAMPLES" :key="ex.lang">
                  <a
                    :href="ex.url"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex items-center gap-1 hover:underline"
                    :class="form.templateLang === ex.lang ? 'text-primary font-medium' : 'text-fg-muted'"
                  >
                    <ExternalLink class="size-3 shrink-0" aria-hidden="true" />
                    {{ ex.title }}
                    <span v-if="form.templateLang === ex.lang" class="text-[10px] text-primary/80">（当前）</span>
                  </a>
                </li>
              </ul>
            </div>
          </div>
          <div class="space-y-1 md:col-span-2">
            <Label for="file-traffic-url" class="text-sm font-semibold text-fg-muted">流量显示链接（可选）</Label>
            <Input id="file-traffic-url" v-model="form.trafficUrl" class="font-mono text-sm" placeholder="https://机场提供的订阅地址，用于向客户端展示剩余流量" />
            <p class="text-xs text-fg-subtle">
              留空时自动取来源订阅的流量信息。文件模板本身没有流量概念，此项仅影响客户端显示。
            </p>
          </div>
        </template>

        <div v-else class="space-y-1">
          <Label for="file-format" class="text-sm font-semibold text-fg-muted">文件格式</Label>
          <Select v-model="form.type">
            <SelectTrigger id="file-format" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="raw">纯文本</SelectItem>
              <SelectItem value="script">JavaScript</SelectItem>
              <SelectItem value="json">JSON</SelectItem>
              <SelectItem value="yaml">YAML</SelectItem>
              <SelectItem value="ini">INI</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="space-y-1">
          <!-- RadioGroup 内置 role=radiogroup，aria-labelledby 由 span 承担 -->
          <span id="file-source-mode-label" class="text-sm font-semibold text-fg-muted">正文来源</span>
          <RadioGroup
            v-model="form.sourceMode"
            aria-labelledby="file-source-mode-label"
            class="flex items-center gap-4 border rounded p-2"
          >
            <label class="flex items-center gap-1.5 text-sm cursor-pointer select-none">
              <RadioGroupItem value="local" /> 本地编辑
            </label>
            <label class="flex items-center gap-1.5 text-sm cursor-pointer select-none">
              <RadioGroupItem value="remote" /> 远程拉取
            </label>
          </RadioGroup>
        </div>

        <div class="space-y-1">
          <Label for="file-merge-sources" class="text-sm font-semibold text-fg-muted">本地与远程合并</Label>
          <Select v-model="mergeSourcesSelectValue">
            <SelectTrigger id="file-merge-sources" class="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">不合并（只用上面选的一侧）</SelectItem>
              <SelectItem value="localFirst">合并：本地在前</SelectItem>
              <SelectItem value="remoteFirst">合并：远程在前</SelectItem>
            </SelectContent>
          </Select>
          <p class="text-xs text-fg-subtle">合并后两侧内容按所选顺序拼接，段间自动补换行</p>
        </div>

        <template v-if="usesRemote">
          <div class="space-y-1 md:col-span-2">
            <Label for="file-remote-urls" class="text-sm font-semibold text-fg-muted">远程地址（每行一个）</Label>
            <Textarea id="file-remote-urls"
              v-model="form.syncUrl"
              rows="4"
              class="font-mono text-sm"
              placeholder="https://example.com/rules.yaml&#10;https://example.com/more.yaml&#10;# 以 # 开头的行会被忽略，便于临时停用某个地址"
            />
            <p class="text-xs text-fg-subtle">
              多个地址并发拉取，但按此处的先后顺序拼接。可在下方列表点「立即同步」把远程内容固化到本地正文。
              想直接试用时，也可粘贴页面上方参考模板的 raw 链接。
            </p>
          </div>
          <div class="space-y-1">
            <Label for="file-remote-fallback" class="text-sm font-semibold text-fg-muted">远程失败处理</Label>
            <Select v-model="ignoreFailedRemoteSelectValue">
              <SelectTrigger id="file-remote-fallback" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">失败即报错（推荐）</SelectItem>
                <SelectItem value="enabled">跳过失败地址并提示</SelectItem>
                <SelectItem value="quiet">静默跳过失败地址</SelectItem>
              </SelectContent>
            </Select>
            <p class="text-xs text-fg-subtle">
              选择跳过时，缺失的内容不会有明显提示，客户端可能拿到不完整的配置
            </p>
          </div>
          <div class="space-y-1">
            <Label for="file-user-agent" class="text-sm font-semibold text-fg-muted">拉取 User-Agent（可选）</Label>
            <Input id="file-user-agent" v-model="form.userAgent" class="font-mono text-sm" placeholder="如：ClashForWindows/0.20.39" />
            <p class="text-xs text-fg-subtle">部分上游按 UA 返回不同格式</p>
          </div>
        </template>
      </div>

      <!-- 纯远程且不合并时没有本地正文可编辑，隐藏编辑器避免误以为它会生效 -->
      <div v-if="usesLocal" class="space-y-1">
        <div class="flex items-center justify-between">
          <!-- CodeMirror 不是原生控件，label 的 for 到不了它内部的 contenteditable，
               改用 span + 编辑器侧的 aria-labelledby 反向关联 -->
          <span id="file-content-label" class="text-sm font-semibold text-fg-muted">
            {{ isTemplate ? `模板内容（${templateLangLabel(form.templateLang)}）` : '文件内容' }}
          </span>
          <Button v-if="canFormat" type="button" variant="ghost" size="sm" @click="formatContent">
            <Wand2 class="h-4 w-4" aria-hidden="true" />格式化
          </Button>
        </div>
        <!-- 语言高亮跟随所选文件格式/模板语言 -->
        <CodeEditor
          v-model="form.content"
          :language="editorLanguage"
          height="300px"
          :placeholder="contentPlaceholder"
          aria-labelledby="file-content-label"
        />
        <p v-if="isTemplate && form.templateLang === 'gotemplate'" class="text-xs text-fg-subtle">
          可用变量：<code class="text-fg-muted">.Nodes</code>（节点数组，每项含 Name / Type / Server / Port / UDP / Extra）。
          完整写法见
          <a
            :href="currentTemplateExample.url"
            target="_blank"
            rel="noopener noreferrer"
            class="text-primary hover:underline inline-flex items-center gap-0.5"
          >Go 模板示例<ExternalLink class="size-3" aria-hidden="true" /></a>。
        </p>
        <p v-else-if="isTemplate && form.templateLang === 'yaml'" class="text-xs text-fg-subtle">
          与自动生成的基础配置深度合并：标量/对象递归合并，数组默认整体替换；
          <code class="text-fg-muted">+key</code> 前插、<code class="text-fg-muted">key+</code> 追加、
          <code class="text-fg-muted">key!</code> 强制整体覆盖。
          完整写法见
          <a
            :href="currentTemplateExample.url"
            target="_blank"
            rel="noopener noreferrer"
            class="text-primary hover:underline inline-flex items-center gap-0.5"
          >YAML 覆写示例<ExternalLink class="size-3" aria-hidden="true" /></a>。
        </p>
        <p v-else-if="isTemplate && form.templateLang === 'javascript'" class="text-xs text-fg-subtle space-y-1">
          <span class="block">
            必须定义 <code class="text-fg-muted">function main(config)</code> 并 return，config 为自动生成的基础配置对象。
            完整写法见
            <a
              :href="currentTemplateExample.url"
              target="_blank"
              rel="noopener noreferrer"
              class="text-primary hover:underline inline-flex items-center gap-0.5"
            >JS 脚本示例<ExternalLink class="size-3" aria-hidden="true" /></a>。
          </span>
          <span class="block text-amber-700 dark:text-amber-400/90">
            注意：JS 覆写仅在登录后的预览与配置合并中执行；文件分享链接 / 公开直链会拒绝输出，请改用 Go 模板或 YAML 覆写对外分发。
          </span>
        </p>
      </div>
      <p v-else class="text-xs text-fg-muted bg-elevated border border-line rounded p-3">
        当前为纯远程来源，正文完全取自上面的远程地址。若想同时保留自己写的内容，请选择一种合并方式。
      </p>

      <div class="flex flex-wrap gap-2 mt-5">
        <Button @click="onSave"><Save class="h-4 w-4" aria-hidden="true" />保存文件</Button>
        <Button variant="outline" :disabled="preview.loading" @click="onPreview">
          <Eye class="h-4 w-4" aria-hidden="true" />{{ preview.loading ? '预览中…' : '即时预览' }}
        </Button>
        <Button variant="ghost" @click="closeDialog"><X class="h-4 w-4" aria-hidden="true" />取消</Button>
      </div>
    </ModalDialog>

    <ShareDialog
      v-if="shareTarget"
      :open="!!shareTarget"
      kind="file"
      :id="shareTarget.id"
      :name="shareTarget.name"
      :share-token="shareTarget.shareToken"
      @close="shareTarget = null"
      @changed="store.fetch()"
    />
  </main>
</template>

<script setup lang="ts">
import { subStatusLabel } from '../utils/labels'
import { onMounted, reactive, ref } from 'vue'
import { useSubscriptionStore } from '../stores/subscription'
import { usePreviewStore } from '../stores/preview'
import PipelineEditor from '../components/PipelineEditor.vue'
import CodeEditor from '../components/CodeEditor.vue'
import PreviewPanel from '../components/PreviewPanel.vue'
import ShareDialog from '../components/ShareDialog.vue'
import ModalDialog from '../components/ModalDialog.vue'
import { toBackendOperators, toUiOperators } from '../utils/operators'
import { shareTargetOptions, buildShareUrl } from '../utils/shareTargets'
import { useNotifyStore } from '../stores/notify'
import { useCopy } from '../composables/useCopy'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Copy, Eye, MoreHorizontal, Pencil, Plus, RefreshCw, Save, Share2, Trash2, X } from 'lucide-vue-next'

const store = useSubscriptionStore()
const preview = usePreviewStore()
const notify = useNotifyStore()
const { copy } = useCopy()
const form = reactive({
  name: '',
  // 远程订阅填 url，手动粘贴节点填 content
  sourceMode: 'url' as 'url' | 'content',
  url: '',
  content: '',
  enabled: true,
  userAgent: '',
  operators: [] as any[],
})
const editingId = ref<number | null>(null)
const dialogOpen = ref(false)
const origin = window.location.origin

const resetForm = () => {
  editingId.value = null
  form.name = ''
  form.sourceMode = 'url'
  form.url = ''
  form.content = ''
  form.userAgent = ''
  form.enabled = true
  form.operators = []
}

onMounted(async () => {
  await store.fetchSubscriptions()
})

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

const onSave = async () => {
  if (!form.name) return notify.error('请填写订阅名称')
  if (form.sourceMode === 'url' && !form.url) return notify.error('请填写订阅地址')
  if (form.sourceMode === 'content' && !form.content.trim()) return notify.error('请粘贴节点内容')

  const payload: any = {
    name: form.name,
    enabled: form.enabled,
    userAgent: form.userAgent,
    operators: toBackendOperators(form.operators),
    // 切换来源方式时清空另一侧，避免两者同时存在造成歧义
    url: form.sourceMode === 'url' ? form.url : '',
    content: form.sourceMode === 'content' ? form.content : '',
  }

  if (editingId.value !== null) {
    await store.updateSubscription(editingId.value, payload)
  } else {
    await store.createSubscription(payload)
  }
  closeDialog()
}

const onEdit = (sub: any) => {
  editingId.value = sub.id
  form.name = sub.name
  form.sourceMode = sub.url ? 'url' : 'content'
  form.url = sub.url || ''
  form.content = sub.content || ''
  form.enabled = sub.enabled
  form.userAgent = sub.userAgent || ''
  form.operators = toUiOperators(sub.operators)
  dialogOpen.value = true
}

const onUpdate = async (id: number) => {
  await store.updateNow(id)
}

const onDelete = async (id: number) => {
  if (!confirm('确定要删除此订阅吗？')) return
  await store.deleteSubscription(id)
}

// ===== 即时预览 =====
// 预览的是当前表单，不必先保存，因此新建时也能看到处理结果
const onPreview = async () => {
  if (form.sourceMode === 'url' && !form.url) return notify.error('请先填写订阅地址')
  if (form.sourceMode === 'content' && !form.content.trim()) return notify.error('请先粘贴节点内容')
  await preview.run({
    kind: 'subscription',
    url: form.sourceMode === 'url' ? form.url : '',
    content: form.sourceMode === 'content' ? form.content : '',
    userAgent: form.userAgent,
    operators: toBackendOperators(form.operators),
  })
}

// 预览已保存的订阅：从列表直接看某条订阅当前的产出
const onPreviewSaved = async (sub: any) => {
  await preview.run({
    kind: 'subscription',
    url: sub.url || '',
    content: sub.content || '',
    userAgent: sub.userAgent || '',
    operators: sub.operators || [],
  })
}

// ===== 分享设置 =====
const shareTarget = ref<any>(null)

// ===== 流量信息展示 =====
const formatBytes = (n: number): string => {
  if (!n || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

const usedPercent = (t: any): number => {
  if (!t?.total) return 0
  return Math.min(100, Math.round((t.used / t.total) * 100))
}

const expireText = (t: any): string => {
  if (!t?.expire) return '不限期'
  const d = new Date(t.expire * 1000)
  const days = Math.ceil((d.getTime() - Date.now()) / 86400000)
  if (days < 0) return `已过期（${d.toLocaleDateString()}）`
  return `${d.toLocaleDateString()}（剩 ${days} 天）`
}

// ===== 分享链接 =====
// 与组合订阅共用 buildShareUrl 与格式选项，避免两处行为再次分叉。
// 列表不再展示链接文本或记忆上次选的格式：直接在下拉菜单里选格式即复制，
// 省去列表空间，也不需要每条订阅各自维护一份「当前选中格式」的状态。
const copyShareAs = (sub: any, target: string) =>
  copy(buildShareUrl(origin, sub.shareToken, target), '订阅链接')
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8 max-w-7xl mx-auto space-y-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl sm:text-3xl font-bold text-fg">单个订阅</h1>
      <Button @click="openCreate"><Plus class="h-4 w-4" aria-hidden="true" />新建订阅</Button>
    </div>

    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <!-- 订阅列表。
         表格与徽标改用 components/ui 下的 shadcn 组件，
         使三个 substore 页面共享同一套排版与状态配色。
         窄屏形态仍由 Table 内置的 responsive-table 承担（见其注释）。 -->
    <Card class="overflow-hidden lg:rounded-xl">
      <Table>
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>名称</TableHead>
            <TableHead>地址</TableHead>
            <TableHead>节点数</TableHead>
            <TableHead>缓存状态</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>流量</TableHead>
            <!-- 只保留编辑/预览两个高频操作在行内，其余（刷新缓存/分享设置/
                 删除）收进「更多」下拉菜单——原先 5 个按钮撑成两行，
                 让数据行比其它列高出一倍，视觉上很不协调。
                 16rem 按「编辑+预览+更多」三个按钮（均带图标）紧挨一行算出
                 （含单元格 px-6 内距），留出安全余量，避免「更多」被 Card 的
                 overflow-hidden 裁掉（下方 flex-wrap 是双重保险，正常不会
                 触发）。lg: 限定是必须的：窄屏下 responsive-table 把单元格
                 变成 block，固定宽度会撑破卡片形态。 -->
            <TableHead class="lg:w-64">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableEmpty v-if="store.subscriptions.length === 0" :colspan="7">
            当前暂无任何订阅。
          </TableEmpty>
          <TableRow v-for="sub in store.subscriptions" :key="sub.id">
            <TableCell label="名称">
              <div class="text-sm font-semibold text-fg break-all">{{ sub.name }}</div>
              <!-- 外部分享链接：不再展示链接文本或单独的格式选择框——那会常驻
                   占用列表空间，而多数人一次只需要复制一种格式。改为单个按钮，
                   点开菜单选格式即直接复制，选完即关，不需要额外点「复制链接」。 -->
              <DropdownMenu v-if="sub.shareToken">
                <DropdownMenuTrigger as-child>
                  <!-- 图标在 shadcn Button 内被 [&_svg]:size-4 统一收到 16px，
                       此处写 h-4 w-4 与实际渲染尺寸一致，避免误导 -->
                  <Button variant="outline" size="sm" class="mt-2 h-6 gap-1 px-2 text-[10px]">
                    <Copy class="h-4 w-4" aria-hidden="true" />
                    复制外部分享链接
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  <DropdownMenuItem
                    v-for="o in shareTargetOptions"
                    :key="o.value"
                    @click="copyShareAs(sub, o.value)"
                  >
                    {{ o.label }}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </TableCell>
            <TableCell label="地址">
              <!-- 卡片形态下没有列宽约束，改为换行显示完整地址；
                   桌面端仍截断，否则长 URL 会把表格撑宽。
                   xl 上限从 max-w-xl(576px) 收到 max-w-md(448px)：
                   前者在 1280px 视口下会把操作列挤成多行，而地址本来就
                   截断显示、完整值可由 title 查看，让出这部分宽度更划算。 -->
              <div v-if="sub.url" class="text-xs font-mono text-fg-muted break-all lg:break-normal lg:max-w-xs xl:max-w-md lg:truncate" :title="sub.url">{{ sub.url }}</div>
              <Badge v-else variant="accent">手动粘贴的节点</Badge>
              <div class="mt-1 flex flex-wrap gap-1">
                <Badge v-if="sub.operators && sub.operators.length" variant="outline">
                  独立管道 {{ sub.operators.length }} 步
                </Badge>
                <Badge v-if="sub.userAgent" variant="outline">UA {{ sub.userAgent }}</Badge>
              </div>
            </TableCell>
            <TableCell label="节点数">
              <span class="text-sm font-semibold text-fg">{{ sub.nodeCount ?? 0 }}</span>
            </TableCell>
            <TableCell label="缓存状态">
              <div class="flex flex-col items-start gap-1.5">
                <Badge :variant="sub.status === 'ok' ? 'ok' : sub.status === 'error' ? 'err' : 'neutral'">
                  {{ subStatusLabel(sub.status) }}
                </Badge>
                <span v-if="sub.lastUpdate" class="text-[10px] text-fg-subtle">{{ new Date(sub.lastUpdate).toLocaleString() }}</span>
                <span
                  v-if="sub.status === 'error' && sub.errorMessage"
                  class="text-[10px] text-rose-600 dark:text-rose-400 max-w-[220px] break-words"
                  :title="sub.errorMessage"
                >
                  {{ sub.errorMessage }}
                </span>
              </div>
            </TableCell>
            <TableCell label="状态">
              <Badge :variant="sub.enabled ? 'info' : 'warn'">
                {{ sub.enabled ? '已启用' : '已禁用' }}
              </Badge>
            </TableCell>
            <TableCell label="流量">
              <div v-if="sub.traffic" class="min-w-[140px]">
                <div class="text-xs font-medium text-fg">
                  {{ formatBytes(sub.traffic.used) }}<span class="text-fg-subtle"> / {{ sub.traffic.total ? formatBytes(sub.traffic.total) : '不限' }}</span>
                </div>
                <div v-if="sub.traffic.total" class="mt-1 h-1.5 overflow-hidden rounded-full bg-elevated">
                  <div class="h-full rounded-full transition-all"
                    :class="usedPercent(sub.traffic) >= 90 ? 'bg-rose-500' : usedPercent(sub.traffic) >= 70 ? 'bg-amber-500' : 'bg-emerald-500'"
                    :style="{ width: usedPercent(sub.traffic) + '%' }"></div>
                </div>
                <div class="mt-1 text-[10px] text-fg-subtle">{{ expireText(sub.traffic) }}</div>
              </div>
              <span v-else class="text-xs text-fg-subtle">—</span>
            </TableCell>
            <!-- 操作区：只留编辑/预览两个高频操作在行内，其余次要操作
                 （刷新缓存/分享设置/删除）收进「更多」下拉菜单——
                 5 个按钮横向换行会撑成两行，让数据行比其它列高出一倍。
                 删除单独标红，与下拉菜单里的其它常规项区分。 -->
            <TableCell label="操作">
              <!-- flex-wrap 作为安全网：列宽按三个按钮一行算好，正常不会触发换行，
                   但换行远比被 Card 的 overflow-hidden 裁掉更安全 -->
              <div class="flex flex-wrap items-center gap-1.5">
                <Button variant="outline" size="sm" @click="onEdit(sub)"><Pencil class="h-4 w-4" aria-hidden="true" />编辑</Button>
                <Button variant="secondary" size="sm" @click="onPreviewSaved(sub)"><Eye class="h-4 w-4" aria-hidden="true" />预览</Button>
                <DropdownMenu>
                  <DropdownMenuTrigger as-child>
                    <Button variant="ghost" size="icon-sm" class="tap-target" aria-label="更多操作">
                      <MoreHorizontal class="h-4 w-4" aria-hidden="true" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      title="回源拉取该订阅的最新节点并更新缓存，供分享链接与预览使用。不会改动最终配置，如需应用到内核请到配置中心「拉取远程并合并」。"
                      @click="onUpdate(sub.id)"
                    >
                      <RefreshCw class="h-4 w-4" aria-hidden="true" />
                      刷新缓存
                    </DropdownMenuItem>
                    <DropdownMenuItem @click="shareTarget = sub">
                      <Share2 class="h-4 w-4" aria-hidden="true" />
                      分享设置
                    </DropdownMenuItem>
                    <DropdownMenuItem class="text-rose-600 focus:text-rose-600 dark:text-rose-400 dark:focus:text-rose-400" @click="onDelete(sub.id)">
                      <Trash2 class="h-4 w-4" aria-hidden="true" />
                      删除
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>

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
      :title="editingId !== null ? '编辑订阅' : '新建订阅'"
      max-width="max-w-3xl"
      @close="closeDialog"
    >
      <div class="grid md:grid-cols-2 gap-4">
        <div class="space-y-1">
          <Label for="sub-name" class="text-sm font-semibold text-fg-muted">订阅名称</Label>
          <Input id="sub-name" v-model="form.name" placeholder="如：机场A" />
        </div>
        <div class="space-y-1">
          <Label for="sub-user-agent" class="text-sm font-semibold text-fg-muted">自定义 User-Agent (可选)</Label>
          <Input id="sub-user-agent" v-model="form.userAgent" placeholder="如：ClashForWindows/0.20.39" />
        </div>
        <div class="md:col-span-2 space-y-2">
          <!-- RadioGroup 内置 role=radiogroup，aria-labelledby 由 span 承担 -->
          <RadioGroup v-model="form.sourceMode" aria-labelledby="sub-source-mode-label" class="flex items-center gap-4">
            <span id="sub-source-mode-label" class="text-sm font-semibold text-fg-muted">节点来源</span>
            <label class="flex items-center gap-1.5 text-sm cursor-pointer select-none">
              <RadioGroupItem value="url" /> 远程订阅地址
            </label>
            <label class="flex items-center gap-1.5 text-sm cursor-pointer select-none">
              <RadioGroupItem value="content" /> 手动粘贴节点
            </label>
          </RadioGroup>
          <Input v-if="form.sourceMode === 'url'" v-model="form.url" class="font-mono text-sm" placeholder="https://..." />
          <div v-else>
            <!-- 内容可能是分享链接、Base64 订阅或 Clash YAML，按 YAML 高亮对三者都不至于误导 -->
            <CodeEditor
              v-model="form.content"
              language="yaml"
              height="220px"
              placeholder="每行一条分享链接（ss:// vmess:// vless:// trojan:// hysteria2:// ...），也可直接粘贴 Base64 订阅或 Clash YAML"
            />
            <p class="text-xs text-fg-subtle mt-1">手动节点无需回源，保存后即可使用</p>
          </div>
        </div>
        <!-- 此处原有「自动更新间隔（秒）」。已移除：订阅不再各自定时回源，
             刷新时机统一由配置中心的定时拉取与外部分享的即时拉取决定。 -->
        <div class="space-y-1 md:col-span-2">
          <!-- 「状态」只是分区小标题，真正的控件名由下面那个包裹 checkbox 的
               label 承担（隐式关联）。用 span 而非 label，避免一个指不到控件的空 label。 -->
          <span class="block text-sm font-semibold text-fg-muted">状态</span>
          <label class="flex items-center gap-2 border rounded-md px-3 py-2 cursor-pointer select-none hover:bg-elevated transition-colors">
            <Checkbox v-model="form.enabled" />
            <span class="text-sm text-fg">启用此订阅</span>
          </label>
          <p class="text-xs text-fg-subtle">禁用后不参与组合与配置合并，其分享链接也会失效</p>
        </div>
      </div>
      <div class="mt-5">
        <PipelineEditor v-model="form.operators" title="该订阅的独立处理管道（先于组合管道执行）" />
      </div>

      <!-- 弹窗底部：保存是此处的主操作，故用 primary；
           窄屏下换行而不横向溢出 -->
      <div class="mt-4 flex flex-wrap gap-2">
        <Button @click="onSave">
          <Save class="h-4 w-4" aria-hidden="true" />{{ editingId !== null ? '保存修改' : '添加订阅' }}
        </Button>
        <Button variant="outline" :disabled="preview.loading" @click="onPreview">
          <Eye class="h-4 w-4" aria-hidden="true" />{{ preview.loading ? '预览中…' : '即时预览' }}
        </Button>
        <Button variant="ghost" @click="closeDialog"><X class="h-4 w-4" aria-hidden="true" />取消</Button>
      </div>
    </ModalDialog>

    <ShareDialog
      v-if="shareTarget"
      :open="!!shareTarget"
      kind="subscription"
      :id="shareTarget.id"
      :name="shareTarget.name"
      :share-token="shareTarget.shareToken"
      @close="shareTarget = null"
      @changed="store.fetchSubscriptions()"
    />
  </main>
</template>

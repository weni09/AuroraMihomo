<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useConfigLabStore } from '../stores/configlab'
import { useSubscriptionStore } from '../stores/subscription'
import { usePreviewStore } from '../stores/preview'
import PipelineEditor from '../components/PipelineEditor.vue'
import PreviewPanel from '../components/PreviewPanel.vue'
import ShareDialog from '../components/ShareDialog.vue'
import ModalDialog from '../components/ModalDialog.vue'
import { toBackendOperators, toUiOperators } from '../utils/operators'
import { shareTargetOptions, buildShareUrl } from '../utils/shareTargets'
import { useNotifyStore } from '../stores/notify'
import { useCopy } from '../composables/useCopy'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Copy, Eye, MoreHorizontal, Pencil, Plus, Save, Share2, Trash2, X } from 'lucide-vue-next'
const store = useConfigLabStore()
const subs = useSubscriptionStore()
const preview = usePreviewStore()
const notify = useNotifyStore()
const { copy } = useCopy()

const form = reactive({ 
  name: '', 
  enabled: true,
  subIds: [] as number[],
  operators: [] as any[]
})

const origin = window.location.origin
const editingId = ref<number | null>(null)
const dialogOpen = ref(false)
// 每个组合当前选择的分享格式；key 为空串表示"默认"，与 shareTargetOptions
// 的空串取值语义一致
const shareFormat = reactive<Record<number, string>>({})

// shadcn Select 的 SelectItem 不接受空字符串 value，"默认" 选项用 __default__
// 占位；shareFormat 本身仍按 shareTargetOptions 的空串语义存取，转换只在
// Select 的 model-value/update:model-value 这一层做（模板里直接调用，
// 而非 v-model，因为每个组合要各自映射到 shareFormat[c.id]）
const shareFormatSelectValue = (id: number) => shareFormat[id] || '__default__'
const setShareFormat = (id: number, v: string) => {
  shareFormat[id] = v === '__default__' ? '' : v
}

onMounted(async () => {
  await Promise.all([store.fetchCollections(), subs.fetchSubscriptions()])
})

const toggle = (id: number) => {
  const i = form.subIds.indexOf(id)
  if (i >= 0) form.subIds.splice(i, 1)
  else form.subIds.push(id)
}

const resetForm = () => {
  editingId.value = null
  form.name = ''
  form.enabled = true
  form.subIds = []
  form.operators = []
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

const startEdit = (c: any) => {
  editingId.value = c.id
  form.name = c.name
  form.enabled = c.enabled !== false
  form.subIds = [...(c.subIds || [])]
  form.operators = toUiOperators(c.operators)
  dialogOpen.value = true
}

const save = async () => {
  if (!form.name || form.subIds.length === 0) return notify.error('请填写名称并至少勾选一个底层订阅')

  const payload = {
    name: form.name,
    enabled: form.enabled,
    subIds: form.subIds,
    operators: toBackendOperators(form.operators),
  }

  if (editingId.value !== null) {
    await store.updateCollection(editingId.value, payload)
  } else {
    await store.createCollection(payload)
  }
  closeDialog()
}

const removeCollection = async (c: any) => {
  if (!confirm(`确定删除组合「${c.name}」吗？其分享链接将立即失效。`)) return
  await store.deleteCollection(c.id)
  if (editingId.value === c.id) closeDialog()
}

// 分享链接：不带 target 时由后端按默认的 mihomo-yaml 输出
const shareUrl = (c: any) => buildShareUrl(origin, c.shareToken, shareFormat[c.id])

const copyShare = (c: any) => copy(shareUrl(c), '分享链接')

// 预览当前表单：不必先保存，改完管道即可看效果
// 组合统一按 mihomo-yaml 渲染，不再传 target
const onPreview = async () => {
  if (form.subIds.length === 0) return notify.error('请先勾选至少一个底层订阅')
  await preview.run({
    kind: 'collection',
    subIds: form.subIds,
    operators: toBackendOperators(form.operators),
  })
}

// 预览已保存的组合
const onPreviewSaved = async (c: any) => {
  await preview.run({
    kind: 'collection',
    subIds: c.subIds || [],
    operators: c.operators || [],
  })
}

// ===== 分享设置 =====
const shareTarget = ref<any>(null)
</script>

<template>
  <main class="max-w-6xl mx-auto space-y-4 sm:space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl sm:text-3xl font-bold text-fg">组合订阅</h1>
      <Button @click="openCreate"><Plus class="h-4 w-4" aria-hidden="true" />新建组合</Button>
    </div>

    <!-- 组合列表。卡片与徽标改用 components/ui 下的 shadcn 组件，
         与订阅页、模板转换页共享同一套排版与状态配色。 -->
    <div class="space-y-3 sm:space-y-4">
      <!-- 空态此前缺失，只会显示一片空白，看不出是没数据还是没加载出来 -->
      <Card v-if="store.collections.length === 0" class="border-dashed shadow-none">
        <CardContent class="p-10 text-center text-sm italic text-fg-subtle">
          当前暂无组合订阅。
        </CardContent>
      </Card>

      <Card v-for="c in store.collections" :key="c.id" class="transition-shadow hover:shadow-md">
        <!-- 窄屏纵向堆叠：右侧四个按钮的竖列会把左侧信息压成窄条 -->
        <div class="flex flex-col items-stretch justify-between gap-4 p-4 sm:p-5 lg:flex-row lg:items-start">
          <div class="w-full min-w-0 space-y-2 lg:pr-6">
            <div class="flex flex-wrap items-center gap-2">
              <CardTitle class="break-all text-lg">{{ c.name }}</CardTitle>
              <Badge v-if="c.enabled === false" variant="warn">已禁用</Badge>
            </div>
            <div class="flex flex-wrap items-center gap-1.5">
              <Badge variant="neutral">关联源 {{ c.subIds?.length || 0 }}</Badge>
              <Badge variant="neutral">处理步骤 {{ c.operators ? c.operators.length : 0 }}</Badge>
            </div>

            <!-- 处理管道概览：按顺序列出各步类型 -->
            <div v-if="c.operators && c.operators.length > 0" class="flex flex-wrap items-center gap-1.5 pt-0.5">
              <span class="text-xs text-fg-muted">处理管道</span>
              <template v-for="(op, i) in c.operators" :key="i">
                <Badge variant="accent" class="font-mono">{{ op.type }}</Badge>
                <span v-if="Number(i) < c.operators.length - 1" class="text-xs text-fg-subtle">→</span>
              </template>
            </div>

            <!-- 分享已撤销时必须整块隐藏：否则会渲染出以 /share/ 结尾的空 token
                 地址，看着能复制，实际访问必定 404 -->
            <div v-if="c.shareToken" class="mt-4 space-y-1.5 border-t border-line pt-3">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-xs text-fg-muted">外部分享链接</span>
                <Select
                  :model-value="shareFormatSelectValue(c.id)"
                  @update:model-value="(v) => setShareFormat(c.id, v as string)"
                >
                  <SelectTrigger class="h-7 w-auto text-xs px-2 py-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="o in shareTargetOptions"
                      :key="o.value"
                      :value="o.value || '__default__'"
                    >
                      {{ o.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <!-- 与左侧紧凑 Select 同高，避免一行内一高一矮 -->
                <Button variant="outline" size="sm" class="h-7 px-2 text-xs" @click="copyShare(c)"><Copy class="h-4 w-4" aria-hidden="true" />复制链接</Button>
              </div>
              <code class="block break-all select-all rounded-md border border-line bg-elevated px-3 py-1.5 font-mono text-xs text-primary">
                {{ shareUrl(c) }}
              </code>
              <p class="text-xs text-fg-subtle">
                可追加 <code class="text-fg-muted">&amp;filter=关键词</code> 临时筛选节点
              </p>
            </div>
            <p v-else class="mt-4 border-t border-line pt-3 text-xs text-fg-subtle">
              分享已撤销，点右侧「分享设置」可重新生成链接。
            </p>
          </div>
          <!-- 只留即时预览/编辑两个高频操作，分享设置/删除收进「更多」下拉菜单——
               4 个按钮撑成的竖列比左侧信息区高得多，与订阅/文件两页保持一致。
               窄屏横向铺开成一排，桌面端恢复右侧竖列 -->
          <div class="flex shrink-0 flex-wrap gap-2 border-t border-line pt-3 lg:flex-col lg:border-t-0 lg:pt-0">
            <Button variant="secondary" class="flex-1 lg:flex-none" :disabled="preview.loading" @click="onPreviewSaved(c)">
              <Eye class="h-4 w-4" aria-hidden="true" />即时预览
            </Button>
            <Button variant="outline" class="flex-1 lg:flex-none" @click="startEdit(c)"><Pencil class="h-4 w-4" aria-hidden="true" />编辑</Button>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="ghost" size="icon" class="tap-target lg:self-center" aria-label="更多操作">
                  <MoreHorizontal class="h-4 w-4" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem @click="shareTarget = c">
                  <Share2 class="h-4 w-4" aria-hidden="true" />
                  分享设置
                </DropdownMenuItem>
                <DropdownMenuItem class="text-rose-600 focus:text-rose-600 dark:text-rose-400 dark:focus:text-rose-400" @click="removeCollection(c)">
                  <Trash2 class="h-4 w-4" aria-hidden="true" />
                  删除
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </Card>
    </div>

    <!-- 预览结果用弹窗展示（PreviewPanel 内部自带遮罩），不再嵌在页面里；
         列表行「即时预览」与新建/编辑弹窗内「即时预览」共用同一份，
         由 preview store 的状态决定是否显示 -->
    <PreviewPanel
      :result="preview.result"
      :loading="preview.loading"
      :error="preview.error"
      @close="preview.close()"
    />

    <ModalDialog
      :open="dialogOpen"
      :title="editingId !== null ? '编辑组合' : '新建组合'"
      max-width="max-w-3xl"
      @close="closeDialog"
    >
      <div>
        <Label for="coll-name" class="block text-sm font-semibold text-fg-muted mb-1.5">组合名称</Label>
        <Input id="coll-name" v-model="form.name" placeholder="如：我的优质节点" />
      </div>

      <div class="mt-4">
        <!-- 一组多选：role=group + aria-labelledby 表达整组，
             label 的 for 指不了多个 checkbox。内层 label 各自包裹自己的 checkbox。 -->
        <span id="coll-subs-label" class="block text-sm font-semibold text-fg-muted mb-2">选择基础订阅源</span>
        <div role="group" aria-labelledby="coll-subs-label" class="flex flex-wrap gap-2">
          <label v-for="s in subs.subscriptions" :key="s.id" class="flex items-center text-sm border rounded-md px-3 py-2 cursor-pointer hover:bg-elevated transition-colors select-none" :class="form.subIds.includes(s.id) ? 'border-primary/50 bg-primary/10 text-primary font-medium shadow-sm' : 'border-line text-fg'">
            <Checkbox class="mr-2" :model-value="form.subIds.includes(s.id)" @update:model-value="toggle(s.id)" />
            {{ s.name }}
          </label>
        </div>
      </div>

      <div class="mt-5">
        <PipelineEditor v-model="form.operators" />
      </div>

      <div class="pt-3 border-t mt-5 flex flex-wrap items-center gap-3">
        <Button @click="save">
          <Save class="h-4 w-4" aria-hidden="true" />{{ editingId !== null ? '保存修改' : '保存组合订阅' }}
        </Button>
        <Button variant="outline" :disabled="preview.loading" @click="onPreview">
          <Eye class="h-4 w-4" aria-hidden="true" />{{ preview.loading ? '预览中…' : '即时预览' }}
        </Button>
        <Button variant="ghost" @click="closeDialog"><X class="h-4 w-4" aria-hidden="true" />取消</Button>
        <!-- ml-auto 在换行后会把复选框推到独占一行的最右侧，
             窄屏下改为常规流式排布 -->
        <label class="flex items-center gap-2 text-sm text-fg-muted cursor-pointer select-none sm:ml-auto">
          <Checkbox v-model="form.enabled" />
          启用（关闭后分享链接立即失效）
        </label>
      </div>
    </ModalDialog>

    <ShareDialog
      v-if="shareTarget"
      :open="!!shareTarget"
      kind="collection"
      :id="shareTarget.id"
      :name="shareTarget.name"
      :share-token="shareTarget.shareToken"
      @close="shareTarget = null"
      @changed="store.fetchCollections()"
    />
  </main>
</template>

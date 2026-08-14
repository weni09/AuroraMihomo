<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useSharesStore, type ShareItem, type ShareKind } from '../stores/shares'
import { useCopy } from '../composables/useCopy'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Ban, Copy, Infinity as InfinityIcon, RotateCcw, Save, Settings, X } from 'lucide-vue-next'

const store = useSharesStore()
const { copy } = useCopy()

const origin = window.location.origin

const kindLabel = (k: ShareKind) =>
  k === 'subscription' ? '订阅' : k === 'collection' ? '组合' : '文件'

const kindClass = (k: ShareKind) =>
  k === 'subscription'
    ? 'bg-blue-50 text-blue-700 dark:text-blue-300 border-blue-100'
    : k === 'collection'
      ? 'bg-purple-50 text-purple-700 dark:text-purple-300 border-purple-100'
      : 'bg-amber-50 text-amber-700 dark:text-amber-300 border-amber-100'

// 过滤器：分享一多就需要按类型收窄
const filterKind = ref<'' | ShareKind>('')
// shadcn Select 的 SelectItem 不接受空字符串 value（reka-ui 内部用空串表示
// "未选中"），"全部类型"用 __all__ 占位，两侧转换集中在这里，其余逻辑仍按
// 空字符串语义工作
const filterKindSelectValue = computed({
  get: () => filterKind.value || '__all__',
  set: (v: string) => { filterKind.value = v === '__all__' ? '' : (v as ShareKind) },
})

const visible = computed(() =>
  filterKind.value ? store.items.filter((i) => i.kind === filterKind.value) : store.items,
)

const activeCount = computed(() => store.items.filter((i) => !i.revoked && !i.expired).length)

onMounted(() => store.fetch())

const fullUrl = (item: ShareItem) => (item.url ? origin + item.url : '')

// 同一列表里订阅/组合/文件的 id 会重复，用 kind-id 作唯一键
const keyOf = (item: ShareItem) => `${item.kind}-${item.id}`

const copyLink = (item: ShareItem) => copy(fullUrl(item), '分享链接')

// ===== 编辑分享（名称 / 有效期）=====
const editingKey = ref('')
const editForm = reactive({ shareName: '', expiresAt: '' })

// datetime-local 控件需要 "YYYY-MM-DDTHH:mm" 格式，且要用本地时间而非 UTC，
// 直接切 toISOString() 会平移时区、显示成错误时刻
const toLocalInput = (iso: string) => {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const startEdit = (item: ShareItem) => {
  editingKey.value = keyOf(item)
  editForm.shareName = item.shareName || ''
  editForm.expiresAt = toLocalInput(item.expiresAt)
}

const cancelEdit = () => {
  editingKey.value = ''
  editForm.shareName = ''
  editForm.expiresAt = ''
}

const saveEdit = async (item: ShareItem) => {
  try {
    await store.update(item.kind, item.id, {
      shareName: editForm.shareName,
      expiresAt: editForm.expiresAt,
    })
    cancelEdit()
  } catch {
    // 失败提示由 store 统一 toast
  }
}

// 快捷设置有效期：从现在起 N 天
const setDays = (days: number) => {
  const d = new Date(Date.now() + days * 24 * 3600 * 1000)
  editForm.expiresAt = toLocalInput(d.toISOString())
}

const onReset = async (item: ShareItem) => {
  if (!confirm(`将为「${item.shareName || item.sourceName}」生成新链接，旧链接立即失效。确定继续？`)) return
  try {
    await store.reset(item.kind, item.id)
  } catch {
    // 失败提示由 store 统一 toast
  }
}

const onRevoke = async (item: ShareItem) => {
  if (!confirm(`将撤销「${item.shareName || item.sourceName}」的分享链接，访问者立即无法获取内容。确定继续？`)) return
  try {
    await store.revoke(item.kind, item.id)
  } catch {
    // 失败提示由 store 统一 toast
  }
}

// 分享的实际可用状态，决定列表上的状态标签
const statusOf = (item: ShareItem) => {
  if (item.revoked) return { text: '已撤销', cls: 'bg-elevated text-fg-muted' }
  if (item.expired) return { text: '已过期', cls: 'bg-rose-100 text-rose-700 dark:text-rose-300' }
  if (!item.enabled) return { text: '来源已停用', cls: 'bg-amber-100 text-amber-700 dark:text-amber-300' }
  return { text: '生效中', cls: 'bg-emerald-100 text-emerald-700 dark:text-emerald-300' }
}
</script>

<template>
  <main class="max-w-6xl mx-auto space-y-6">
    <div class="bg-surface border rounded-xl p-5 shadow-sm">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="font-semibold text-fg">分享管理</h2>
          <p class="text-xs text-fg-muted mt-1">
            集中管理由订阅、组合与文件创建的对外分享链接。共 {{ store.items.length }} 项，{{ activeCount }} 项生效中。
          </p>
        </div>
        <Select v-model="filterKindSelectValue">
          <SelectTrigger class="w-auto text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部类型</SelectItem>
            <SelectItem value="subscription">仅订阅</SelectItem>
            <SelectItem value="collection">仅组合</SelectItem>
            <SelectItem value="file">仅文件</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="text-xs text-amber-600 dark:text-amber-400 mt-3 bg-amber-50 dark:bg-amber-950/30 border border-amber-100 dark:border-amber-900/50 rounded px-3 py-2 space-y-1">
        <p>分享链接无需登录即可访问，凭据即链接本身。若链接可能已外泄，请使用「重置」生成新凭据。</p>
        <p>
          安全限制：公开拉取时<strong class="font-medium">不会执行</strong>处理管道中的
          JS 脚本算子；文件若使用 JS 覆写模板，直链/分享会拒绝输出。
          脚本仍可在登录后的即时预览与配置合并中运行。
        </p>
      </div>
    </div>

    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <div v-if="store.loading && store.items.length === 0" class="text-sm text-fg-muted py-6 text-center">
      正在加载…
    </div>

    <div v-else-if="visible.length === 0" class="text-sm text-fg-subtle italic py-10 text-center bg-elevated rounded-lg border border-dashed">
      暂无分享。可在单个订阅、组合订阅或模板转换中创建。
    </div>

    <div v-else class="space-y-3">
      <div v-for="item in visible" :key="keyOf(item)" class="bg-surface border rounded-lg p-5 space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-xs px-2 py-0.5 rounded border" :class="kindClass(item.kind)">
              {{ kindLabel(item.kind) }}
            </span>
            <span class="font-semibold text-fg">{{ item.shareName || item.sourceName }}</span>
            <span v-if="item.shareName" class="text-xs text-fg-subtle">（来源：{{ item.sourceName }}）</span>
            <span class="text-xs px-2 py-0.5 rounded-full" :class="statusOf(item).cls">
              {{ statusOf(item).text }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-1.5 shrink-0">
            <Button variant="outline" size="sm" @click="startEdit(item)">
              <Settings class="h-4 w-4" aria-hidden="true" />设置
            </Button>
            <!-- 重置会让旧链接立即失效，用 warning token 提示这是有代价的
                 操作，但不到 destructive 的程度（不涉及数据丢失）。
                 描边而非实色：与「设置」保持同等视觉重量，仅靠颜色区分 -->
            <Button variant="outline" size="sm" class="border-warning/40 text-amber-700 hover:bg-warning/10 dark:text-amber-400" @click="onReset(item)">
              <RotateCcw class="h-4 w-4" aria-hidden="true" />{{ item.revoked ? '重新启用' : '重置链接' }}
            </Button>
            <Button v-if="!item.revoked" variant="destructive" size="sm" @click="onRevoke(item)">
              <Ban class="h-4 w-4" aria-hidden="true" />撤销
            </Button>
          </div>
        </div>

        <div v-if="item.url">
          <div class="flex items-center gap-2 mb-1.5">
            <Button variant="outline" size="sm" @click="copyLink(item)"><Copy class="h-4 w-4" aria-hidden="true" />复制链接</Button>
            <span class="text-xs text-fg-muted">
              {{ item.expiresAt ? `有效期至 ${new Date(item.expiresAt).toLocaleString()}` : '永不过期' }}
            </span>
          </div>
          <code class="bg-elevated text-primary py-1.5 px-3 rounded-md block font-mono text-xs border border-line break-all select-all">
            {{ fullUrl(item) }}
          </code>
        </div>
        <p v-else class="text-xs text-fg-subtle">该分享已撤销，无可用链接。</p>

        <!-- 设置面板 -->
        <div v-if="editingKey === keyOf(item)" class="border-t border-line pt-3 space-y-3">
          <div class="grid md:grid-cols-2 gap-3">
            <div class="space-y-1">
              <Label for="share-name" class="text-xs font-semibold text-fg-muted">分享名称</Label>
              <Input id="share-name" v-model="editForm.shareName" class="text-sm" :placeholder="item.sourceName" />
              <p class="text-xs text-fg-subtle">留空时显示来源名称</p>
            </div>
            <div class="space-y-1">
              <Label for="share-expires" class="text-xs font-semibold text-fg-muted">有效期</Label>
              <Input id="share-expires" v-model="editForm.expiresAt" type="datetime-local" class="text-sm" />
              <div class="flex flex-wrap gap-1.5 pt-0.5">
                <Button variant="outline" size="sm" @click="setDays(7)">7 天</Button>
                <Button variant="outline" size="sm" @click="setDays(30)">30 天</Button>
                <Button variant="outline" size="sm" @click="setDays(90)">90 天</Button>
                <Button variant="outline" size="sm" @click="editForm.expiresAt = ''">
                  <InfinityIcon class="h-4 w-4" aria-hidden="true" />永不过期
                </Button>
              </div>
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <Button :disabled="store.loading" @click="saveEdit(item)"><Save class="h-4 w-4" aria-hidden="true" />保存</Button>
            <Button variant="ghost" @click="cancelEdit"><X class="h-4 w-4" aria-hidden="true" />取消</Button>
          </div>
        </div>
      </div>
    </div>
  </main>
</template>

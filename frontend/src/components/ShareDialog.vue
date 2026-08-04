<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { X } from 'lucide-vue-next'
import { useSharesStore, type ShareKind, type ShareItem } from '../stores/shares'
import { useCopy } from '../composables/useCopy'
import ModalDialog from './ModalDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

/**
 * 分享设置弹窗。
 *
 * 订阅 / 组合 / 文件在创建时就已自动生成分享凭据，所以这里做的是
 * 「管理已有分享」而非「从零创建」：改展示名、设有效期、选输出格式、
 * 复制链接，以及重置或撤销凭据。
 *
 * 复用 shares store 与 /api/v1/shares 接口，与「分享管理」页同一套逻辑，
 * 避免两处对有效期、撤销语义的理解产生分歧。
 */
const props = defineProps<{
  open: boolean
  kind: ShareKind
  id: number
  /** 所属实体名称，用于标题与占位提示 */
  name: string
  /** 当前凭据；为空表示分享已撤销 */
  shareToken: string
}>()

const emit = defineEmits<{ close: []; changed: [] }>()

const store = useSharesStore()
const { copy } = useCopy()
const form = reactive({ shareName: '', expiresAt: '', target: '' })

// 输出格式：订阅与组合的链接支持 ?target= 临时换格式；
// 文件是原样输出或固定模板渲染，没有这个概念。
const supportsTarget = computed(() => props.kind !== 'file')

const targetOptions = [
  { value: '__default__', label: '默认（跟随自身设置）' },
  { value: 'clash', label: 'Clash / Mihomo' },
  { value: 'base64', label: 'Base64 订阅' },
  { value: 'plain', label: '明文分享链接' },
  { value: 'surge', label: 'Surge' },
  { value: 'loon', label: 'Loon' },
  { value: 'qx', label: 'QuantumultX' },
  { value: 'singbox', label: 'sing-box' },
  { value: 'stash', label: 'Stash' },
  { value: 'shadowrocket', label: 'Shadowrocket' },
]

// shadcn Select 的 SelectItem 不接受空字符串作为 value（reka-ui 内部用空串
// 表示"未选中"），原逻辑里 form.target === '' 代表"默认"，因此用 __default__
// 作占位值，两侧转换在这里集中处理，其余代码仍按空字符串语义工作。
const targetSelectValue = computed({
  get: () => form.target || '__default__',
  set: (v: string) => { form.target = v === '__default__' ? '' : v },
})

// datetime-local 控件要 "YYYY-MM-DDTHH:mm" 且必须是本地时间，
// 直接切 toISOString() 会平移时区、显示成错误时刻
const toLocalInput = (iso: string) => {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 弹窗里的分享状态取自 shares 列表：本组件只拿到实体的 token，
// 展示名与有效期存在分享侧，需要单独拉一次
const current = computed<ShareItem | undefined>(() =>
  store.items.find((it) => it.kind === props.kind && it.id === props.id),
)

const shareUrl = computed(() => {
  const token = current.value?.shareToken || props.shareToken
  if (!token) return ''
  const base = props.kind === 'file'
    ? `${window.location.origin}/api/v1/file/${token}`
    : `${window.location.origin}/api/v1/share/${token}`
  return form.target ? `${base}?target=${form.target}` : base
})

const revoked = computed(() => !shareUrl.value)

const statusText = computed(() => {
  const it = current.value
  if (!it) return revoked.value ? '已撤销' : '生效中'
  if (it.revoked) return '已撤销'
  if (it.expired) return '已过期'
  if (!it.enabled) return '来源已停用'
  return '生效中'
})

// 打开时同步一次：分享信息可能在别处被改过
watch(
  () => props.open,
  async (open) => {
    if (!open) return
    form.target = ''
    await store.fetch()
    form.shareName = current.value?.shareName || ''
    form.expiresAt = toLocalInput(current.value?.expiresAt || '')
  },
  { immediate: true },
)

const setDays = (days: number) => {
  const d = new Date(Date.now() + days * 24 * 3600 * 1000)
  form.expiresAt = toLocalInput(d.toISOString())
}

const onSave = async () => {
  try {
    await store.update(props.kind, props.id, {
      shareName: form.shareName,
      expiresAt: form.expiresAt,
    })
    emit('changed')
    // 保存是终结性操作，成功即关闭弹窗（结果由 toast 告知）。
    // 失败时刻意不关：用户需要留在表单里改完再提交。
    //
    // 重置与撤销不在此列——重置后用户要看新链接、撤销后可能想立即
    // 重新生成，关掉反而打断操作。
    emit('close')
  } catch {
    // 失败提示由 shares store 统一 toast
  }
}

const onReset = async () => {
  if (!confirm(`将为「${props.name}」生成新链接，旧链接立即失效。确定继续？`)) return
  try {
    await store.reset(props.kind, props.id)
    emit('changed')
  } catch {
    // 失败提示由 shares store 统一 toast
  }
}

const onRevoke = async () => {
  if (!confirm(`将撤销「${props.name}」的分享链接，访问者立即无法获取内容。确定继续？`)) return
  try {
    await store.revoke(props.kind, props.id)
    emit('changed')
  } catch {
    // 失败提示由 shares store 统一 toast
  }
}

const onCopy = () => copy(shareUrl.value, '分享链接')
</script>

<template>
  <!-- 套用 ModalDialog 而不再自建外壳：焦点管理、Escape 关闭、背景滚动锁定
       都由 ModalDialog 内部的 shadcn Dialog 统一承担。标题栏带状态徽标，
       故用 #header slot 而非默认标题结构。 -->
  <ModalDialog :open="open" max-width="max-w-2xl" @close="emit('close')">
    <template #header>
      <div class="flex items-center justify-between gap-3 px-4 sm:px-6 py-4 border-b border-line shrink-0">
        <div class="min-w-0">
          <DialogTitle class="text-lg font-bold text-fg">分享设置</DialogTitle>
          <p class="text-xs text-fg-muted mt-0.5 truncate">{{ name }}</p>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span
            class="text-xs px-2 py-0.5 rounded-full"
            :class="statusText === '生效中' ? 'tint-ok' : 'tint-warn'"
          >
            {{ statusText }}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="tap-target text-fg-muted hover:text-fg"
            aria-label="关闭"
            @click="emit('close')"
          >
            <X class="!h-5 !w-5" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </template>

    <div class="space-y-5">
        <div class="grid md:grid-cols-2 gap-4">
          <div class="space-y-1">
            <Label for="share-dlg-name" class="text-sm font-semibold text-fg-muted">分享展示名（可选）</Label>
            <Input id="share-dlg-name" v-model="form.shareName" :placeholder="name" />
            <p class="text-xs text-fg-subtle">留空时在分享管理里显示为来源名称</p>
          </div>
          <div class="space-y-1">
            <Label for="share-dlg-expires" class="text-sm font-semibold text-fg-muted">有效期</Label>
            <Input id="share-dlg-expires" v-model="form.expiresAt" type="datetime-local" />
            <div class="flex flex-wrap gap-1.5 pt-0.5">
              <Button v-for="d in [7, 30, 90]" :key="d" variant="outline" size="sm" @click="setDays(d)">
                {{ d }} 天
              </Button>
              <Button variant="outline" size="sm" @click="form.expiresAt = ''">永不过期</Button>
            </div>
          </div>
        </div>

        <div v-if="revoked" class="text-sm text-fg-muted bg-elevated border border-line rounded p-3">
          该分享已撤销，当前没有可用链接。点下方「重新生成链接」可再次启用。
        </div>
        <div v-else class="space-y-2">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-sm font-semibold text-fg-muted">分享链接</span>
            <Select v-if="supportsTarget" v-model="targetSelectValue">
              <SelectTrigger class="h-7 w-auto text-xs px-2 py-1">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="o in targetOptions" :key="o.value" :value="o.value">{{ o.label }}</SelectItem>
              </SelectContent>
            </Select>
            <!-- 与左侧紧凑 Select 同高，避免一行内一高一矮 -->
            <Button variant="outline" size="sm" class="h-7 px-2 text-xs" @click="onCopy">复制链接</Button>
          </div>
          <code
            class="bg-elevated text-primary py-1.5 px-3 rounded-md block font-mono text-xs border border-line break-all select-all"
          >
            {{ shareUrl }}
          </code>
          <p class="text-xs text-fg-subtle">
            该链接无需登录即可访问，凭据即链接本身。
            <template v-if="supportsTarget">可追加 <code>&amp;filter=关键词</code> 临时筛选节点。</template>
          </p>
          <p class="text-xs text-amber-700 dark:text-amber-400/90 bg-amber-50 dark:bg-amber-950/30 border border-amber-100 dark:border-amber-900/40 rounded px-2.5 py-1.5">
            <template v-if="kind === 'file'">
              公开直链不会执行源订阅上的 JS 脚本算子；若模板语言为 JavaScript，公开访问会返回错误（请改用 Go 模板或 YAML，或仅用登录后预览）。
            </template>
            <template v-else>
              公开拉取时不会执行处理管道中的 JS 脚本算子（过滤/改名等其它算子仍生效）。需要脚本效果时请用面板内「即时预览」。
            </template>
          </p>
        </div>

        <!-- 操作结果统一走 toast（见 stores/notify.ts），不在弹窗里占位 -->

      <!-- 四个按钮在手机上排不下一行，允许换行。
           操作区随正文一起滚动（ModalDialog 的正文区自带滚动），
           不再是贴底的独立栏 -->
      <div class="flex flex-wrap items-center gap-3 -mx-4 sm:-mx-6 mt-5 px-4 sm:px-6 pt-4 border-t border-line">
        <Button :disabled="store.loading" @click="onSave">保存设置</Button>
        <Button variant="outline" :disabled="store.loading" @click="onReset">
          {{ revoked ? '重新生成链接' : '重置链接' }}
        </Button>
        <Button v-if="!revoked" variant="destructive" :disabled="store.loading" @click="onRevoke">
          撤销分享
        </Button>
        <Button variant="ghost" class="ml-auto" @click="emit('close')">关闭</Button>
      </div>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAdGuardStore } from '../stores/adguard'
import { useSettingsStore } from '../stores/settings'
import ModalDialog from './ModalDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Play, Power, RefreshCw, Download } from 'lucide-vue-next'

/**
 * AdGuard 设置弹窗：运行控制、Web 端口、版本更新、DNS 模式、升级链接。
 * 账号密码区留给 T8；出网策略只读展示系统设置。
 */
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const store = useAdGuardStore()
const settings = useSettingsStore()

const webPortInput = ref('3000')
const cdnText = ref('')
const dnsModeValue = ref('0')

const busy = computed(() => store.isLoading || store.actionLoading)
const running = computed(() => store.status.running)
const version = computed(() => store.status.version || '未知')
const pid = computed(() => store.status.pid || 0)
const webAddr = computed(() => store.status.webAddr || '127.0.0.1:3000')

const useMihomoProxy = computed(() => settings.settings?.useMihomoProxy !== false)
const mihomoProxyUrl = computed(() => settings.settings?.mihomoProxyUrl || '')

function parsePortFromAddr(addr: string): string {
  const m = String(addr || '').match(/:(\d+)\s*$/)
  if (m?.[1]) return m[1]
  const n = Number(addr)
  if (Number.isFinite(n) && n > 0) return String(n)
  return '3000'
}

function syncFromStore() {
  webPortInput.value = parsePortFromAddr(store.status.webAddr)
  cdnText.value = (store.status.cdnProviders || []).join('\n')
  dnsModeValue.value = String(store.status.dnsMode ?? 0)
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    // 打开时刷新状态与系统出网设置
    await Promise.all([store.fetchStatus(), settings.fetch().catch(() => undefined)])
    syncFromStore()
  },
  { immediate: true },
)


watch(
  () => [store.status.webAddr, store.status.dnsMode, store.status.cdnProviders],
  () => {
    if (props.open) syncFromStore()
  },
  { deep: true },
)

function close() {
  emit('update:open', false)
}

async function saveWebPort() {
  const port = Number(String(webPortInput.value).trim())
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return
  }
  await store.setWebPort(port)
}

async function saveCdn() {
  const providers = cdnText.value
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
  await store.setCdnProviders(providers)
}

async function onDnsModeChange(v: unknown) {
  if (v === null || v === undefined) return
  const mode = Number(v)
  if (!Number.isInteger(mode) || mode < 0 || mode > 2) return
  if (mode === store.status.dnsMode) return
  await store.setDnsMode(mode)
}

</script>

<template>
  <ModalDialog
    :open="open"
    title="AdGuard 设置"
    max-width="max-w-lg"
    data-testid="adguard-settings-dialog"
    @close="close"
  >
    <div class="space-y-6 text-sm" data-testid="adguard-settings-dialog-body">
      <!-- 运行状态 -->
      <section class="space-y-3" data-testid="adguard-settings-runtime">
        <h3 class="text-sm font-semibold text-fg">运行状态</h3>
        <div class="flex flex-wrap items-center gap-2">
          <Badge :variant="running ? 'ok' : 'warn'">
            {{ running ? '运行中' : '已停止' }}
          </Badge>
          <span v-if="running && pid" class="text-xs text-fg-subtle font-mono">PID {{ pid }}</span>
          <span class="text-xs text-fg-subtle font-mono truncate">{{ webAddr }}</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button
            v-if="!running"
            size="sm"
            :disabled="busy || !store.status.installed"
            @click="store.start()"
          >
            <Play class="h-4 w-4" aria-hidden="true" />
            启动
          </Button>
          <template v-else>
            <Button size="sm" variant="outline" :disabled="busy" @click="store.restart()">
              <RefreshCw class="h-4 w-4" aria-hidden="true" />
              重启
            </Button>
            <Button size="sm" variant="destructive" :disabled="busy" @click="store.stop()">
              <Power class="h-4 w-4" aria-hidden="true" />
              停止
            </Button>
          </template>
        </div>
      </section>

      <!-- Web 端口 -->
      <section class="space-y-3" data-testid="adguard-settings-webport">
        <h3 class="text-sm font-semibold text-fg">网页管理端口</h3>
        <p class="text-xs text-fg-subtle">
          仅绑定回环 <code class="font-mono">127.0.0.1</code>，外网经面板
          <code class="font-mono">/adguard/</code> 反代访问。改端口后若在运行会自动重启。
        </p>
        <div class="flex flex-wrap items-end gap-2">
          <div class="space-y-1.5 min-w-[8rem]">
            <Label for="agh-web-port">端口</Label>
            <Input
              id="agh-web-port"
              v-model="webPortInput"
              type="number"
              min="1"
              max="65535"
              class="font-mono w-28"
              :disabled="busy"
            />
          </div>
          <Button size="sm" :disabled="busy" @click="saveWebPort">保存端口</Button>
        </div>
      </section>

      <!-- 版本与更新 -->
      <section class="space-y-3" data-testid="adguard-settings-version">
        <h3 class="text-sm font-semibold text-fg">版本与更新</h3>
        <p class="text-xs text-fg-muted">
          本地版本：
          <span class="font-mono text-fg">{{ version }}</span>
        </p>
        <div class="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" :disabled="busy" @click="store.checkUpdate()">
            检查更新
          </Button>
          <Button size="sm" :disabled="busy || !store.status.installed" @click="store.update()">
            <Download class="h-4 w-4" aria-hidden="true" />
            立即更新
          </Button>
        </div>
        <div class="space-y-1.5">
          <Label for="agh-cdn">升级链接（可选，按序回落；空则用系统全局 CDN）</Label>
          <Textarea
            id="agh-cdn"
            v-model="cdnText"
            rows="3"
            class="font-mono text-xs"
            placeholder="每行一个镜像前缀，例如&#10;https://ghproxy.example/"
            :disabled="busy"
          />
          <Button size="sm" variant="secondary" :disabled="busy" @click="saveCdn">
            保存升级链接
          </Button>
        </div>
        <p class="text-xs text-fg-subtle rounded border border-line bg-elevated p-2" data-testid="adguard-egress-note">
          下载出网遵循系统设置 → 下载与更新出网
          <span v-if="useMihomoProxy">
            （当前优先经 mihomo 代理
            <span v-if="mihomoProxyUrl" class="font-mono">{{ mihomoProxyUrl }}</span>
            <span v-else>，探测中或未就绪</span>）
          </span>
          <span v-else>（当前不强制走 mihomo 代理）</span>
        </p>
      </section>

      <!-- DNS 模式 -->
      <section class="space-y-3" data-testid="adguard-settings-dnsmode">
        <h3 class="text-sm font-semibold text-fg" id="agh-dns-mode-label">DNS 服务模式</h3>
        <RadioGroup
          v-model="dnsModeValue"
          aria-labelledby="agh-dns-mode-label"
          class="space-y-2"
          :disabled="busy"
          @update:model-value="onDnsModeChange"
        >
          <label class="flex items-start gap-3 cursor-pointer">
            <RadioGroupItem value="0" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">未托管</span>
              <span class="block text-xs text-fg-subtle">不劫持系统 53；AGH 可用高位端口</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <RadioGroupItem value="1" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">使用 53 端口</span>
              <span class="block text-xs text-fg-subtle">AGH 直接监听 :53；需端口空闲与权限</span>
            </span>
          </label>
          <label class="flex items-start gap-3 cursor-pointer">
            <RadioGroupItem value="2" class="mt-0.5" />
            <span>
              <span class="font-medium text-fg">重定向 53→AdGuard</span>
              <span class="block text-xs text-fg-subtle">AGH 高位端口；TProxy/规则把 53 转到该口</span>
            </span>
          </label>
        </RadioGroup>
      </section>

      <div class="flex justify-end pt-1">
        <Button variant="ghost" @click="close">关闭</Button>
      </div>
    </div>
  </ModalDialog>
</template>

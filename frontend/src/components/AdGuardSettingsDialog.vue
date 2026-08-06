<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAdGuardStore } from '../stores/adguard'
import { useSettingsStore } from '../stores/settings'
import { useNotifyStore } from '../stores/notify'
import ModalDialog from './ModalDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Play, Power, RefreshCw, Download } from 'lucide-vue-next'

/**
 * AdGuard 设置弹窗：账号、运行控制、端口、版本/自动更新、升级链接。
 * 出网策略只读展示系统设置。
 */
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const store = useAdGuardStore()
const settings = useSettingsStore()
const notify = useNotifyStore()

const webHostInput = ref('127.0.0.1')
const webPortInput = ref('3000')
const dnsPortDraft = ref('5353')
const cdnText = ref('')
const usernameInput = ref('admin')
const passwordInput = ref('')
const autoUpdateEnabled = ref(false)
const autoUpdateCron = ref('0 0 4 * * *')

const busy = computed(() => store.isLoading || store.actionLoading)
const running = computed(() => store.status.running)
const version = computed(() => store.status.version || '未知')
const pid = computed(() => store.status.pid || 0)
const webAddr = computed(() => store.status.webAddr || '127.0.0.1:3000')

/** 运行形态文案：服务模式下进程由系统服务看护，面板退出不影响 DNS */
const managedByLabel = computed(() => {
  switch (store.status.managedBy) {
    case 'systemd':
      return '由 systemd 服务看护：面板退出后 DNS 过滤仍常驻'
    case 'openrc':
      return '由 OpenRC 服务看护：面板退出后 DNS 过滤仍常驻'
    default:
      return ''
  }
})

/** 开机自启开关的语义提示：exec 模式与系统服务模式不同 */
const bootHint = computed(() => {
  if (store.status.managedBy === 'process') {
    return '开启后，面板重启时自动拉起 AdGuard'
  }
  return '开启后由系统服务随开机自启；手动「停止」只临时停止，不会关闭自启'
})

function onToggleBoot(enabled: boolean) {
  void store.setBoot(enabled)
}

const useMihomoProxy = computed(() => settings.settings?.useMihomoProxy !== false)
const mihomoProxyUrl = computed(() => settings.settings?.mihomoProxyUrl || '')

function parsePortFromAddr(addr: string): string {
  const m = String(addr || '').match(/:(\d+)\s*$/)
  if (m?.[1]) return m[1]
  const n = Number(addr)
  if (Number.isFinite(n) && n > 0) return String(n)
  return '3000'
}

function parseHostFromAddr(addr: string): string {
  const raw = String(addr || '').trim().replace(/^https?:\/\//, '')
  if (!raw) return '127.0.0.1'
  // [ipv6]:port
  const m6 = raw.match(/^\[([^\]]+)\]:\d+$/)
  if (m6?.[1]) return m6[1]
  const m = raw.match(/^(.*):(\d+)\s*$/)
  if (m?.[1]) return m[1]
  // bare host
  if (!/^\d+$/.test(raw)) return raw
  return '127.0.0.1'
}

function syncFromStore() {
  webHostInput.value = parseHostFromAddr(store.status.webAddr)
  webPortInput.value = parsePortFromAddr(store.status.webAddr)
  const dp = store.status.dnsPort
  dnsPortDraft.value = dp && dp > 0 ? String(dp) : '5353'
  cdnText.value = (store.status.cdnProviders || []).join('\n')
  usernameInput.value = store.status.username || 'admin'
  // 密码不回填
  passwordInput.value = ''
  autoUpdateEnabled.value = store.status.autoUpdate === true
  autoUpdateCron.value = store.status.autoUpdateCron || '0 0 4 * * *'
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
  () => [store.status.webAddr, store.status.dnsPort, store.status.cdnProviders, store.status.username, store.status.autoUpdate, store.status.autoUpdateCron],
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
    notify.error('Web 端口须为 1–65535 的整数')
    return
  }
  const host = String(webHostInput.value || '').trim() || '127.0.0.1'
  await store.setWebPort(port, host)
}


async function saveCdn() {
  const providers = cdnText.value
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
  await store.setCdnProviders(providers)
}

async function saveCredentials() {
  const password = passwordInput.value
  if (!password) {
    notify.error('请输入 AdGuard 管理员密码')
    return
  }
  await store.setCredentials({
    username: usernameInput.value.trim() || 'admin',
    password,
  })
  passwordInput.value = ''
}

async function saveDnsPort() {
  const port = Number(String(dnsPortDraft.value).trim())
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    notify.error('DNS 端口须为 1–65535 的整数')
    return
  }
  // 与当前配置相同则跳过（自身占用也算已成功）
  if (port === store.status.dnsPort) {
    return
  }
  await store.setDnsPort(port)
}

async function onDnsPortBlur() {
  if (busy.value) return
  await saveDnsPort()
}

async function applyEntryPreset() {
  if (!confirm('将写入 AdGuard :53 与 mihomo 0.0.0.0:1053 等入口 DNS 方案，并可能重启 AdGuard。确定继续？')) {
    return
  }
  await store.applyEntryDNSPreset()
  // 端口草稿与状态对齐
  dnsPortDraft.value = String(store.status.dnsPort || 53)
}

async function saveAutoUpdate() {
  await store.setAutoUpdate({
    enabled: autoUpdateEnabled.value,
    cron: String(autoUpdateCron.value || '').trim() || '0 0 4 * * *',
  })
}

</script>

<template>
  <ModalDialog
    :open="open"
    title="AdGuard 设置"
    max-width="max-w-3xl"
    data-testid="adguard-settings-dialog"
    @close="close"
  >
    <div class="space-y-6 text-sm" data-testid="adguard-settings-dialog-body">
      <!-- 账号 -->
      <section class="space-y-3" data-testid="adguard-settings-account">
        <h3 class="text-sm font-semibold text-fg">账号</h3>
        <p class="text-xs text-fg-subtle">
          写入 AdGuard Home 管理员账号（yaml 存 bcrypt；面板另以可逆加密保存一份，供重启后面板免密接管）。
        </p>
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="space-y-1.5">
            <Label for="agh-username">用户名</Label>
            <Input
              id="agh-username"
              v-model="usernameInput"
              autocomplete="username"
              class="font-mono"
              :disabled="busy"
            />
          </div>
          <div class="space-y-1.5">
            <Label for="agh-password">密码</Label>
            <Input
              id="agh-password"
              v-model="passwordInput"
              type="password"
              autocomplete="new-password"
              placeholder="输入新密码后保存"
              :disabled="busy"
            />
          </div>
        </div>
        <Button size="sm" :disabled="busy || !passwordInput" @click="saveCredentials">
          保存账号
        </Button>
      </section>

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
        <!-- 运行形态：服务模式下进程由系统服务看护，面板退出后 DNS 仍常驻 -->
        <p v-if="managedByLabel" class="text-xs text-fg-subtle" data-testid="agh-managed-by">
          {{ managedByLabel }}
        </p>
        <!-- 开机自启：服务模式驱动 systemctl enable/disable，exec 模式写面板自启 -->
        <label class="flex items-center gap-3" data-testid="agh-boot-switch">
          <Switch
            :model-value="store.status.desiredRunning === true"
            :disabled="busy || !store.status.installed"
            @update:model-value="onToggleBoot"
          />
          <span class="text-sm font-medium">开机自启</span>
        </label>
        <p class="text-xs text-fg-subtle">
          {{ bootHint }}
        </p>
      </section>

      <!-- Web 监听 -->
      <section class="space-y-3" data-testid="adguard-settings-webport">
        <h3 class="text-sm font-semibold text-fg">网页管理监听</h3>
        <p class="text-xs text-fg-subtle">
          默认 <code class="font-mono">127.0.0.1</code>（仅本机，经面板
          <code class="font-mono">/adguard-ui/</code> 反代）。服务化后可改为
          <code class="font-mono">0.0.0.0</code> 或网卡 IP，让 AGH 管理面在面板之外仍可直连。
          改动后若在运行会自动重启。
        </p>
        <div class="flex flex-wrap items-end gap-2">
          <div class="space-y-1.5 min-w-[10rem]">
            <Label for="agh-web-host">监听地址</Label>
            <Input
              id="agh-web-host"
              v-model="webHostInput"
              placeholder="127.0.0.1"
              class="font-mono w-40"
              list="agh-web-host-presets"
              :disabled="busy"
              data-testid="agh-web-host"
            />
            <datalist id="agh-web-host-presets">
              <option value="127.0.0.1" />
              <option value="0.0.0.0" />
            </datalist>
          </div>
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
          <Button size="sm" :disabled="busy" @click="saveWebPort">保存监听</Button>
        </div>
        <p class="text-xs text-fg-subtle">
          安全提示：绑 <code class="font-mono">0.0.0.0</code> 会把 AGH 登录页暴露到可达网络，请设强密码或继续只用面板反代。
        </p>
      </section>

      <!-- DNS 端口（取代原「服务模式」） -->
      <section class="space-y-3" data-testid="adguard-settings-dnsport">
        <h3 class="text-sm font-semibold text-fg" id="agh-dns-port-label">DNS 端口</h3>
        <p class="text-xs text-fg-subtle">
          AdGuard 监听的 DNS 端口。失焦或点击保存时校验：空闲或已被 AdGuard 自身占用则成功；被其它进程占用则失败。
          常用 <span class="font-mono">5353</span>（默认，避免与 mihomo <span class="font-mono">1053</span> 冲突）或
          <span class="font-mono">53</span>（作入口 DNS 时，请确保无其它 DNS 占用）。
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <Label for="agh-dns-port" class="text-xs text-fg-muted sr-only">DNS 端口</Label>
          <Input
            id="agh-dns-port"
            v-model="dnsPortDraft"
            type="number"
            min="1"
            max="65535"
            class="w-32 font-mono text-sm"
            :disabled="busy"
            aria-labelledby="agh-dns-port-label"
            @blur="onDnsPortBlur"
            @keydown.enter.prevent="saveDnsPort"
          />
          <Button size="sm" variant="outline" :disabled="busy" @click="saveDnsPort">保存</Button>
        </div>
        <p class="text-xs text-fg-subtle">
          当前配置端口：
          <span class="font-mono">{{ store.status.dnsPort || '—' }}</span>
          ；保存后写入配置，若 AdGuard 在跑会重启以应用。
        </p>
        <div
          class="rounded border border-line bg-elevated p-3 space-y-2"
          data-testid="adguard-dns-entry-preset"
        >
          <p class="text-xs font-medium text-fg">一键方案 · 入口 DNS（TUN / TProxy 通用）</p>
          <ul class="text-[11px] text-fg-subtle space-y-0.5 font-mono leading-relaxed">
            <li>AdGuard 端口 <span class="text-fg">53</span></li>
            <li>上游 <span class="text-fg">127.0.0.1:1053</span></li>
            <li>后备 <span class="text-fg">127.0.0.1:1053</span>（仅 mihomo，不用裸 8.8.8.8）</li>
            <li>Bootstrap <span class="text-fg">223.5.5.5 / 119.29.29.29</span>（国内纯 IP）</li>
            <li>mihomo <span class="text-fg">dns.enable=true</span>、<span class="text-fg">dns.listen 0.0.0.0:1053</span></li>
          </ul>
          <p class="text-[11px] text-fg-subtle">
            会尽量清空 <code class="font-mono">tun.dns-hijack</code>，避免 TUN 抢走 53；合并时也不再自动补
            <code class="font-mono">any:53</code>。
            请确保本机 53 端口空闲（或仅被 AdGuard 占用）。
          </p>
          <Button
            size="sm"
            :disabled="busy || !store.status.installed"
            data-testid="adguard-dns-entry-preset-btn"
            @click="applyEntryPreset"
          >
            一键应用入口 DNS
          </Button>
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
        <div class="space-y-3 rounded border border-line bg-elevated p-3" data-testid="adguard-auto-update">
          <label class="flex items-start gap-2 cursor-pointer">
            <Checkbox v-model="autoUpdateEnabled" class="mt-0.5" :disabled="busy" />
            <span class="text-sm text-fg">
              启用 AdGuard Home 自动更新
              <span class="block text-xs text-fg-subtle">独立调度，与系统「组件自动更新」分开；组件关闭时不执行</span>
            </span>
          </label>
          <div class="space-y-1.5">
            <Label for="agh-auto-cron">时间表达式（Cron）</Label>
            <Input
              id="agh-auto-cron"
              v-model="autoUpdateCron"
              class="font-mono text-xs w-full"
              placeholder="0 0 4 * * *"
              :disabled="busy"
            />
            <p class="text-[11px] text-fg-subtle">
              5 段（分 时 日 月 周）或 6 段（含秒）。默认每天 4 点。
            </p>
          </div>
          <div class="flex justify-end pt-0.5">
            <Button size="sm" variant="outline" class="min-w-[7.5rem]" :disabled="busy" @click="saveAutoUpdate">
              保存自动更新
            </Button>
          </div>
        </div>
        <div class="space-y-3 rounded border border-line bg-elevated p-3" data-testid="adguard-cdn-settings">
          <div class="space-y-1.5">
            <Label for="agh-cdn">升级链接（按序回落；支持变量）</Label>
            <Textarea
              id="agh-cdn"
              v-model="cdnText"
              rows="4"
              class="font-mono text-xs"
              placeholder="https://static.adguard.com/adguardhome/beta/AdGuardHome_${GOOS}_${Arch}.tar.gz&#10;https://github.com/AdguardTeam/AdGuardHome/releases/download/${latest_ver}/AdGuardHome_${GOOS}_${Arch}.tar.gz&#10;https://static.adguard.com/adguardhome/release/AdGuardHome_${GOOS}_${Arch}.tar.gz"
              :disabled="busy"
            />
            <p class="text-[11px] text-fg-subtle leading-relaxed">
              变量：<code class="font-mono">${Arch}</code>（amd64/arm64…）、
              <code class="font-mono">${latest_ver}</code>（GitHub 最新 tag）、
              <code class="font-mono">${GOOS}</code>。留空则使用上述默认三源。下载出网仍遵循系统设置。
            </p>
          </div>
          <div class="flex justify-end pt-0.5">
            <Button size="sm" variant="outline" class="min-w-[7.5rem]" :disabled="busy" @click="saveCdn">
              保存升级链接
            </Button>
          </div>
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

      <div class="flex justify-end pt-1">
        <Button variant="ghost" @click="close">关闭</Button>
      </div>
    </div>
  </ModalDialog>
</template>

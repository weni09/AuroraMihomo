<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useSettingsStore } from '../stores/settings'
import { useNotifyStore } from '../stores/notify'
import { useTransparentStore, type TransparentMode } from '../stores/transparent'
import { useCopy } from '../composables/useCopy'
import api from '../api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'

const store = useSettingsStore()
const notify = useNotifyStore()
const tp = useTransparentStore()
const { copy: copyText } = useCopy()
const form = reactive({
  autoUpdateEnabled: false,
  autoUpdateCron: '0 0 4 * * *',
  cdnText: '',
  // 出网优先走本地内核代理，失败再回落镜像/直连
  useMihomoProxy: true,
  // 应用日志归档的保留天数
  logRetentionDays: 7,
  logCleanupEnabled: true,
  logCleanupCron: '0 30 3 * * *',
})

onMounted(async () => {
  await store.fetch()
  syncForm()
  await loadPolicy()
  await tp.fetch()
})

// 倒计时定时器属于本页面，离开时必须停掉；
// 真正的回滚计时在后端且已持久化，前端停表不影响它
onUnmounted(() => tp.stopCountdown())

watch(
  () => store.settings,
  () => syncForm(),
)

function syncForm() {
  if (!store.settings) return
  form.autoUpdateEnabled = store.settings.autoUpdateEnabled
  form.autoUpdateCron = store.settings.autoUpdateCron
  form.cdnText = (store.settings.cdnProviders || []).join('\n')
  form.useMihomoProxy = store.settings.useMihomoProxy !== false
  form.logRetentionDays = store.settings.logRetentionDays || 7
  form.logCleanupEnabled = store.settings.logCleanupEnabled !== false
  form.logCleanupCron = store.settings.logCleanupCron || '0 30 3 * * *'
}

const defaultCDNText = computed(() => (store.settings?.defaultCDN || []).join(', '))

// 代理地址为空表示内核未运行或未开放 mixed-port/port，此时会自动回落
const proxyReady = computed(() => !!store.settings?.mihomoProxyUrl)

// 版本未知时不编造内容：mihomo 需成功探测过一次，zashboard 需经由本平台更新过
const mihomoVersionText = computed(() => store.settings?.mihomoVersion || '版本未知')
const zashboardVersionText = computed(
  () => store.settings?.zashboardVersion || '版本未知（更新一次后可获取）',
)

// 设计 §16：合并策略用户可配置
const mergePolicy = reactive({ proxyPriority: 'local', rulePriority: 'local', dnsPriority: 'local', tunPriority: 'local', generalPriority: 'local' })
async function loadPolicy() {
  try {
    const res = await api.get('/settings/merge-policy')
    Object.assign(mergePolicy, res.data)
  } catch { /* 忽略 */ }
}

async function savePolicy() {
  try {
    const res = await api.put('/settings/merge-policy', { ...mergePolicy })
    Object.assign(mergePolicy, res.data)
    notify.success('合并策略已保存')
  } catch (e: any) {
    notify.error(e?.response?.data?.message || '保存失败')
  }
}

const parseList = (text: string) =>
  text
    .split(/\r?\n|,/)
    .map((s) => s.trim())
    .filter(Boolean)

async function onSave() {
  await store.save({
    autoUpdateEnabled: form.autoUpdateEnabled,
    autoUpdateCron: form.autoUpdateCron,
    cdnProviders: parseList(form.cdnText),
    useMihomoProxy: form.useMihomoProxy,
    logRetentionDays: form.logRetentionDays,
    logCleanupEnabled: form.logCleanupEnabled,
    logCleanupCron: form.logCleanupCron,
  })
}

function useDefaultCDN() {
  if (!store.settings) return
  form.cdnText = (store.settings.defaultCDN || []).join('\n')
}

// 修改管理员密码
const pwdForm = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })
const pwdSaving = ref(false)

async function onChangePassword() {
  if (!pwdForm.oldPassword || !pwdForm.newPassword) {
    return notify.error('请填写当前密码与新密码')
  }
  if (pwdForm.newPassword.length < 8) {
    return notify.error('新密码长度不得少于 8 位')
  }
  if (pwdForm.newPassword !== pwdForm.confirmPassword) {
    return notify.error('两次输入的新密码不一致')
  }
  pwdSaving.value = true
  try {
    const res = await api.post('/auth/password', {
      oldPassword: pwdForm.oldPassword,
      newPassword: pwdForm.newPassword,
    })
    notify.success(res.data?.message || '密码已更新')
    pwdForm.oldPassword = ''
    pwdForm.newPassword = ''
    pwdForm.confirmPassword = ''
  } catch (e: any) {
    // 改密失败的原因（旧密码错误等）由后端返回，直接透出
    notify.error(e?.response?.data?.message || '修改失败')
  } finally {
    pwdSaving.value = false
  }
}

// 更新内核会替换二进制并重启进程，中断当前所有代理连接
const confirmUpdateMihomo = () => {
  if (confirm('更新内核需要下载新版本并重启进程，期间代理会中断，确定继续？')) store.updateMihomo()
}
const confirmUpdateZashboard = () => {
  if (confirm('将下载并替换面板静态资源，确定继续？')) store.updateZashboard()
}

// ---- 透明代理 ----

const MODE_LABELS: Record<string, string> = {
  tun: 'TUN（虚拟网卡）',
  tproxy: 'TProxy（透明转发）',
  off: '关闭',
}
const transparentModeLabel = (m: string) => MODE_LABELS[m] || m

// 界面上选中的模式。开关关着时也要有个值，否则打开开关无从提交，
// 默认取第一个可用模式。
const transparentMode = ref<TransparentMode>('tun')
watch(
  () => tp.status,
  () => {
    if (tp.status.enabled && tp.status.mode !== 'off') {
      transparentMode.value = tp.status.mode
    } else if (tp.availableModes.length > 0) {
      transparentMode.value = tp.availableModes[0]!.mode
    }
  },
  { deep: true, immediate: true },
)

const transparentModeHelp = computed(() => {
  if (transparentMode.value === 'tun') {
    return 'mihomo 自己管理路由与防火墙规则，退出时自动清理。支持 TCP/UDP/ICMP，风险较低，推荐优先使用。'
  }
  if (transparentMode.value === 'tproxy') {
    return '由面板配置策略路由与防火墙规则（仅 Linux）。规则配错可能导致主机失联，建议在有物理或控制台访问的机器上先验证。'
  }
  return ''
})

const transparentEnvRows = computed(() => {
  const e = tp.status.env
  const yn = (v: boolean) => (v ? '是' : '否')
  const rows = [
    { label: '系统', value: `${e.os}/${e.arch}` },
    { label: '内核', value: e.kernel || '—' },
    { label: '发行版', value: e.distro || '—' },
    { label: '包管理器', value: e.packageManager || '—' },
    { label: 'root', value: yn(e.root) },
    { label: 'NET_ADMIN', value: yn(e.capNetAdmin) },
    { label: 'TUN 设备', value: e.tunDevice || '未找到' },
    { label: '容器内', value: yn(e.inContainer) },
  ]
  if (e.inContainer) {
    rows.push({ label: 'host 网络', value: yn(e.hostNetwork) })
  }
  // 只在"给了但拿不到"时显示：这种情况的修复方式与单纯缺权限不同
  if (e.capNetAdminBounding && !e.capNetAdmin) {
    rows.push({ label: 'NET_ADMIN(bounding)', value: '已授予但当前进程未持有' })
  }
  return rows
})

async function onToggleTransparent(next: boolean | 'indeterminate') {
  const enabled = next === true
  if (enabled) {
    // 开启前明确告知风险。TProxy 会改宿主防火墙，配错可能断掉 SSH，
    // 用户需要事先知道"必须在 90 秒内确认"这件事。
    const mode = transparentMode.value
    const extra =
      mode === 'tproxy'
        ? '\n\nTProxy 会修改本机的防火墙规则与路由表。若配置不当，可能导致 SSH 与面板都无法访问。'
        : ''
    if (
      !confirm(
        `即将启用透明代理（${transparentModeLabel(mode)}）。${extra}\n\n` +
          '启用后需在 90 秒内点击"网络正常，确认"，否则将自动回滚。\n确定继续？',
      )
    ) {
      return
    }
    await tp.update({ enabled: true, mode, tunStack: tp.status.tunStack })
  } else {
    await tp.update({ enabled: false, mode: 'off' })
  }
}

const onConfirmTransparent = () => tp.confirm()
</script>

<template>
  <!-- max-w 必须配 mx-auto：否则内容被压在宽屏左侧，右半边整片空白。
       宽度与内核管理、冲突处理等页面保持一致。
       用浏览器原生滚动条滚动本页，不再自造内部滚动区。 -->
  <main class="p-4 sm:p-6 lg:p-8 max-w-6xl mx-auto">
    <h1 class="text-2xl sm:text-3xl font-bold mb-6">系统设置</h1>

    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <div class="bg-surface rounded-lg shadow border border-line p-4 sm:p-6 space-y-6">
      <section>
        <h2 class="text-lg font-semibold mb-3">管理员密码</h2>
        <div class="grid md:grid-cols-3 gap-3">
          <div>
            <Label for="pwd-old" class="block text-sm text-fg-muted mb-1">当前密码</Label>
            <Input id="pwd-old" v-model="pwdForm.oldPassword" type="password" autocomplete="current-password" />
          </div>
          <div>
            <Label for="pwd-new" class="block text-sm text-fg-muted mb-1">新密码（至少 8 位）</Label>
            <Input id="pwd-new" v-model="pwdForm.newPassword" type="password" autocomplete="new-password" />
          </div>
          <div>
            <Label for="pwd-confirm" class="block text-sm text-fg-muted mb-1">确认新密码</Label>
            <Input id="pwd-confirm" v-model="pwdForm.confirmPassword" type="password" autocomplete="new-password" />
          </div>
        </div>
        <div class="mt-3">
          <Button :disabled="pwdSaving" @click="onChangePassword">
            {{ pwdSaving ? '提交中…' : '修改密码' }}
          </Button>
        </div>
      </section>

      <section>
        <h2 class="text-lg font-semibold mb-3">组件状态</h2>
        <div class="grid md:grid-cols-2 gap-3 text-sm">
          <div class="p-3 rounded bg-elevated">
            <div class="text-fg-muted">Mihomo 内核</div>
            <div class="font-medium">{{ store.settings?.mihomoPresent ? '已安装' : '未安装' }}</div>
            <div v-if="store.settings?.mihomoPresent" class="mt-1 text-xs">
              <span class="text-fg-muted">版本：</span>
              <span class="font-mono" :class="store.settings?.mihomoVersion ? 'text-fg' : 'text-fg-subtle'">
                {{ mihomoVersionText }}
              </span>
            </div>
            <div class="text-xs text-fg-subtle break-all mt-1">{{ store.settings?.mihomoPath }}</div>
          </div>
          <div class="p-3 rounded bg-elevated">
            <div class="text-fg-muted">Zashboard 面板</div>
            <div class="font-medium">{{ store.settings?.zashboardPresent ? '已安装' : '未安装' }}</div>
            <div v-if="store.settings?.zashboardPresent" class="mt-1 text-xs">
              <span class="text-fg-muted">版本：</span>
              <span class="font-mono" :class="store.settings?.zashboardVersion ? 'text-fg' : 'text-fg-subtle'">
                {{ zashboardVersionText }}
              </span>
            </div>
            <div class="text-xs text-fg-subtle break-all mt-1">{{ store.settings?.zashboardDir }}</div>
          </div>
        </div>
        <p class="mt-2 text-xs text-fg-subtle">
          内核版本由 <code>mihomo -v</code> 探测。面板是纯静态资源、本地无法反查版本，
          只能记录经本平台更新的那一次，手工放入或旧版本安装的面板会显示为未知。
        </p>
        <div class="mt-3 flex flex-wrap gap-2">
          <Button variant="outline" :disabled="store.updating" @click="store.checkUpdate()">
            {{ store.checkingUpdate ? '检查中…' : '检查更新' }}
          </Button>
          <Button variant="outline" :disabled="store.updating" @click="confirmUpdateMihomo()">
            {{ store.updatingMihomo ? '处理中…' : '更新 Mihomo' }}
          </Button>
          <Button variant="outline" :disabled="store.updating" @click="confirmUpdateZashboard()">
            {{ store.updatingZashboard ? '处理中…' : '更新 Zashboard' }}
          </Button>
        </div>
      </section>

      <section>
        <h2 class="text-lg font-semibold mb-3">自动更新</h2>
        <label class="flex items-center gap-3 mb-4">
          <Checkbox v-model="form.autoUpdateEnabled" />
          <span>启用 Mihomo / Zashboard 自动更新</span>
        </label>

        <Label for="settings-cron" class="block text-sm text-fg-muted mb-1">Cron 表达式（支持 5 位或 6 位，6 位含秒）</Label>
        <Input id="settings-cron"
          v-model="form.autoUpdateCron"
          class="font-mono text-sm"
          placeholder="0 0 4 * * *"
        />
        <p class="text-xs text-fg-subtle mt-1">
          示例：每天 04:00 => <code>0 0 4 * * *</code>；每 12 小时 => <code>0 0 */12 * * *</code>
        </p>
      </section>

      <!-- 透明代理。让局域网设备无需各自设置代理即可分流。
           环境不具备条件时开关禁用并说明缺什么；启用后必须在时限内确认
           网络正常，否则后端自动回滚（规则配错可能导致主机失联）。 -->
      <section>
        <h2 class="text-lg font-semibold mb-3">透明代理</h2>

        <!-- 待确认横幅放在最前：此刻用户的网络可能正在中断，
             必须第一眼就看到倒计时与确认按钮 -->
        <div
          v-if="tp.status.pendingConfirm"
          class="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-3 dark:border-amber-500/40 dark:bg-amber-500/10"
        >
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div class="text-sm">
              <div class="font-semibold text-amber-800 dark:text-amber-300">
                请确认网络是否正常（{{ tp.status.secondsLeft }} 秒后自动回滚）
              </div>
              <p class="mt-1 text-xs text-amber-700 dark:text-amber-400">
                建议用另一台设备访问本面板或目标网站验证。未确认则自动拆除规则并关闭开关。
              </p>
            </div>
            <Button class="shrink-0" @click="onConfirmTransparent">网络正常，确认</Button>
          </div>
        </div>

        <label class="mb-1 flex items-center gap-3">
          <Switch
            :model-value="tp.status.enabled"
            :disabled="!tp.anyAvailable || tp.saving"
            @update:model-value="onToggleTransparent"
          />
          <span>启用透明代理</span>
        </label>
        <p v-if="!tp.anyAvailable" class="mb-3 text-xs text-fg-subtle">
          当前环境不具备条件，开关不可用。原因与解决办法见下方各模式说明。
        </p>

        <div v-if="tp.anyAvailable" class="mb-3">
          <Label class="mb-1 block text-sm text-fg-muted">模式</Label>
          <Select v-model="transparentMode" :disabled="tp.saving">
            <SelectTrigger class="w-full sm:w-72">
              <SelectValue placeholder="选择模式" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in tp.availableModes" :key="m.mode" :value="m.mode">
                {{ transparentModeLabel(m.mode) }}
              </SelectItem>
            </SelectContent>
          </Select>
          <p class="mt-1 text-xs text-fg-subtle">{{ transparentModeHelp }}</p>
        </div>

        <!-- 各模式的可用性与修复办法。不可用时这里是用户唯一的行动依据，
             所以安装命令要能直接复制。 -->
        <div class="space-y-2">
          <div
            v-for="m in tp.status.env.modes"
            :key="m.mode"
            class="rounded-md border border-line bg-elevated p-3 text-sm"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-medium">{{ transparentModeLabel(m.mode) }}</span>
              <Badge :variant="m.available ? 'ok' : 'neutral'">
                {{ m.available ? '可用' : '不可用' }}
              </Badge>
            </div>
            <p v-if="m.reason" class="mt-1 text-xs text-fg-muted">{{ m.reason }}</p>
            <div v-if="m.installHint" class="mt-2 flex flex-wrap items-center gap-2">
              <code class="flex-1 break-all rounded bg-surface px-2 py-1 font-mono text-xs">{{ m.installHint }}</code>
              <button class="btn btn-secondary btn-sm" @click="copyText(m.installHint, '命令')">复制</button>
            </div>
          </div>
        </div>

        <details class="mt-3">
          <summary class="cursor-pointer text-xs text-fg-muted">环境检测详情</summary>
          <dl class="mt-2 grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
            <div v-for="row in transparentEnvRows" :key="row.label" class="flex gap-2">
              <dt class="shrink-0 text-fg-subtle">{{ row.label }}</dt>
              <dd class="break-all font-mono">{{ row.value }}</dd>
            </div>
          </dl>
          <ul v-if="tp.status.env.warnings.length" class="mt-2 space-y-1">
            <li v-for="(w, i) in tp.status.env.warnings" :key="i" class="text-xs text-amber-600 dark:text-amber-400">
              {{ w }}
            </li>
          </ul>
        </details>
      </section>

      <section>
        <h2 class="text-lg font-semibold mb-3">下载与更新出网</h2>

        <label class="flex items-start gap-3 mb-2">
          <Checkbox v-model="form.useMihomoProxy" class="mt-0.5" />
          <span>
            <span class="text-sm">优先经由本地 Mihomo 代理出网（推荐）</span>
            <span class="block text-xs text-fg-muted mt-0.5">
              内核已在运行时，下载与版本查询都先走它。拿到的是 GitHub 官方原始文件，
              不必担心第三方镜像返回被篡改或截断的内容。
            </span>
          </span>
        </label>
        <p class="text-xs mb-4" :class="proxyReady ? 'text-emerald-600 dark:text-emerald-400' : 'text-fg-subtle'">
          <template v-if="proxyReady">
            当前代理：<code>{{ store.settings?.mihomoProxyUrl }}</code>
          </template>
          <template v-else>
            代理当前不可用（内核未运行，或配置里没开放 mixed-port / port），本次会直接走下面的下载源。
          </template>
        </p>

        <div class="flex items-center justify-between mb-1">
          <Label for="cdn-release" class="text-sm font-semibold text-fg">
            下载源（按优先级，每行一个）
          </Label>
          <Button variant="ghost" size="sm" @click="useDefaultCDN">恢复默认</Button>
        </div>
        <p class="text-xs text-fg-subtle mb-1.5">
          用于内核与面板的二进制下载（<code>github.com/.../releases/download</code>）。
          代理不可用或失败时按此顺序回落，官方源始终作为最后兜底。
        </p>
        <Textarea
          id="cdn-release"
          v-model="form.cdnText"
          rows="8"
          class="font-mono text-xs"
          placeholder="每行一个，如 ghproxy.com"
        />
        <p class="text-xs text-fg-subtle mt-1">默认：{{ defaultCDNText }}</p>

        <p class="text-xs text-fg-subtle mt-3">
          自定义源支持两种写法：完整前缀（<code>https://mirror.example.com</code>，会拼在官方地址前）
          或含 <code>%s</code> 的模板（<code>%s</code> 替换为完整官方地址）。裸域名无法判断拼法，会被忽略。
          jsdelivr 只镜像仓库内文件、代理不了 Release 资产，填进来会被跳过。
        </p>
        <p class="text-xs text-fg-subtle mt-2">
          检查更新（<code>api.github.com</code>）不使用这些源：没有镜像代理 GitHub 的 REST API，
          套上前缀只会得到 404。该请求一律直连官方，网络不通时由上面的 Mihomo 代理兜底。
        </p>
      </section>

      <section>
        <h2 class="text-lg font-semibold mb-3">日志</h2>
        <label class="block text-sm mb-1">日志保留天数</label>
        <input
          v-model.number="form.logRetentionDays"
          type="number"
          min="1"
          max="365"
          class="input-base w-32"
        />
        <p class="text-xs text-fg-subtle mt-1">
          超过该天数的日志归档会被自动删除，范围 1~365 天，默认 7 天。
        </p>

        <label class="flex items-start gap-2 text-sm mt-4">
          <Checkbox v-model="form.logCleanupEnabled" class="mt-0.5" />
          <span>
            启用定时清理
            <span class="block text-xs text-fg-subtle mt-0.5">
              关闭后仅靠大小轮转限制磁盘占用（单个 8MB、最多 5 份），
              过期归档不会被删除。
            </span>
          </span>
        </label>

        <label class="block text-sm mt-3 mb-1">清理时间（Cron）</label>
        <input
          v-model="form.logCleanupCron"
          :disabled="!form.logCleanupEnabled"
          class="input-base w-full sm:w-72 font-mono text-sm disabled:opacity-50"
          placeholder="0 30 3 * * *"
        />
        <p class="text-xs text-fg-subtle mt-1">
          支持 5 段（分 时 日 月 周）或 6 段（含秒）。默认
          <code>0 30 3 * * *</code> 即每天凌晨 3:30——清理要遍历目录删文件，
          放在低峰期。保存后即时生效，无需重启。
        </p>
        <p class="text-xs text-fg-subtle mt-2">
          清理只影响已归档的历史文件；当前正在写的日志文件由大小轮转管理，
          不会被按时间删除。界面上「运行日志」页的清空按钮只清内存中的实时列表，
          不动磁盘文件。
        </p>
      </section>

      <section class="border-t pt-5">
        <h2 class="text-lg font-semibold mb-3">配置合并策略</h2>
        <p class="text-xs text-fg-muted mb-4">
          当本地配置与订阅内容冲突时，默认采用哪一方。可选：本地优先 / 远程优先 / 自动合并 / 手动确认。
        </p>
        <div class="grid md:grid-cols-3 gap-4">
          <div>
            <Label for="policy-proxyPriority" class="block text-sm text-fg-muted mb-1">节点冲突</Label>
            <Select v-model="mergePolicy.proxyPriority">
              <SelectTrigger id="policy-proxyPriority" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地优先</SelectItem>
                <SelectItem value="remote">远程优先</SelectItem>
                <SelectItem value="merge">自动合并</SelectItem>
                <SelectItem value="manual">手动确认</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label for="policy-rulePriority" class="block text-sm text-fg-muted mb-1">规则冲突</Label>
            <Select v-model="mergePolicy.rulePriority">
              <SelectTrigger id="policy-rulePriority" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地优先</SelectItem>
                <SelectItem value="remote">远程优先</SelectItem>
                <SelectItem value="manual">手动确认</SelectItem>
              </SelectContent>
            </Select>
            <p class="mt-1 text-xs text-fg-subtle">规则有先后顺序语义，无法安全地自动合并，因此不提供「自动合并」。</p>
          </div>
          <div>
            <Label for="policy-dnsPriority" class="block text-sm text-fg-muted mb-1">DNS 冲突</Label>
            <Select v-model="mergePolicy.dnsPriority">
              <SelectTrigger id="policy-dnsPriority" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地优先</SelectItem>
                <SelectItem value="remote">远程优先</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label for="policy-tunPriority" class="block text-sm text-fg-muted mb-1">虚拟网卡(TUN)冲突</Label>
            <Select v-model="mergePolicy.tunPriority">
              <SelectTrigger id="policy-tunPriority" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地优先</SelectItem>
                <SelectItem value="remote">远程优先</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label for="policy-generalPriority" class="block text-sm text-fg-muted mb-1">其余通用参数冲突</Label>
            <Select v-model="mergePolicy.generalPriority">
              <SelectTrigger id="policy-generalPriority" class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">本地优先</SelectItem>
                <SelectItem value="remote">远程优先</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <p class="mt-2 text-xs text-fg-muted">
          「其余通用参数」覆盖运行模式、各监听端口、Geo 数据、外部控制、认证、嗅探等所有其他顶层配置项。
          无论选择哪种策略，只要订阅中没有声明某个配置项，就一定沿用你的本地设置，不会被清空。
        </p>
        <Button variant="outline" class="mt-3" @click="savePolicy">保存合并策略</Button>
      </section>

      <div class="pt-2">
        <Button :disabled="store.loading" @click="onSave">保存设置</Button>
      </div>
    </div>
  </main>
</template>

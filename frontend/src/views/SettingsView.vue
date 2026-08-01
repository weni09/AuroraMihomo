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
    // 提示必须按模式区分：只有 TProxy 会改宿主防火墙并进入 90 秒确认窗口。
    // 对 TUN 也说"不确认就自动回滚"是假承诺——后端不会为它开窗口，
    // 界面上既不会出现确认横幅也没有按钮，用户会一直等一个不存在的东西。
    const mode = transparentMode.value
    const message =
      mode === 'tproxy'
        ? `即将启用透明代理（${transparentModeLabel(mode)}）。\n\n` +
          'TProxy 会修改本机的防火墙规则与路由表。若配置不当，可能导致 SSH 与面板都无法访问。\n\n' +
          '启用后需在 90 秒内点击"网络正常，确认"，否则将自动回滚。\n确定继续？'
        : `即将启用透明代理（${transparentModeLabel(mode)}）。\n\n` +
          'TUN 由 mihomo 自行管理路由与防火墙规则，进程退出时会自动清理，' +
          '随时可以再关掉。\n\n' +
          '该操作会写入基础配置的 tun.enable，并立即重新下发配置。\n确定继续？'
    if (!confirm(message)) {
      return
    }
    await tp.update({ enabled: true, mode, tunStack: tp.status.tunStack })
  } else {
    await tp.update({ enabled: false, mode: 'off' })
  }
}

const onConfirmTransparent = () => tp.confirm()

// ---- 透明代理：自动准备环境 ----

// 两类操作分开勾选：用户可能只想装依赖而不愿意让面板碰内核参数。
const provisionOpts = reactive({ installPackages: true, applySysctl: true })

// 勾选项跟随实际可做的事：没有缺包时把装包那项关掉，
// 否则用户以为点了会装点什么，结果只得到一句"已满足，跳过"
watch(
  () => [tp.canInstallPackages, tp.canApplySysctl] as const,
  ([canInstall, canSysctl]) => {
    provisionOpts.installPackages = canInstall
    provisionOpts.applySysctl = canSysctl
  },
  { immediate: true },
)

const provisionDisabled = computed(
  () => tp.provisioning || (!provisionOpts.installPackages && !provisionOpts.applySysctl),
)

async function onProvision() {
  const actions: string[] = []
  if (provisionOpts.installPackages) {
    actions.push(`· 安装软件包（${tp.status.env.packageManager || '系统包管理器'}）`)
  }
  if (provisionOpts.applySysctl) {
    actions.push('· 写入 /etc/sysctl.d/99-auroramihomo.conf 并使其生效')
  }
  // 会改动系统，先说清楚做什么。与启用开关的风险提示保持同一种风格。
  const containerNote = tp.status.env.inContainer
    ? '\n\n注意：容器内安装的软件包在容器重建后会丢失，长期使用请在镜像里预装。' +
      '容器内不会修改 sysctl。'
    : ''
  if (
    !confirm(
      `即将对本机执行以下操作：\n\n${actions.join('\n')}\n\n` +
        '只会安装程序内置的固定软件包，sysctl 只写本程序专用的配置文件，' +
        '不会改动防火墙规则，也不会自动打开透明代理开关。' +
        containerNote +
        '\n\n确定继续？',
    )
  ) {
    return
  }
  await tp.provision({ ...provisionOpts })
}

/** 步骤样式：跳过与成功要能一眼区分，否则"跳过"看着像什么都没干成 */
function stepBadge(s: { success: boolean; skipped: boolean }) {
  if (s.skipped) return { variant: 'neutral' as const, text: '已满足' }
  return s.success
    ? { variant: 'ok' as const, text: '成功' }
    : { variant: 'err' as const, text: '失败' }
}

// ---- 分组导航 ----

/**
 * 本页分组，供左侧导航渲染。id 同时是各 section 的锚点，
 * 顺序必须与模板中 section 的出现顺序一致（导航是锚点跳转，
 * 不是标签切换，顺序不一致会让高亮与实际位置错位）。
 */
const sections = [
  { id: 'password', title: '管理员密码' },
  { id: 'components', title: '组件状态' },
  { id: 'auto-update', title: '自动更新' },
  { id: 'transparent', title: '透明代理' },
  { id: 'network', title: '下载与更新出网' },
  { id: 'logs', title: '日志' },
  { id: 'merge-policy', title: '配置合并策略' },
] as const

const activeSection = ref<string>(sections[0].id)

/**
 * 用锚点跳转而不是标签式切换（只渲染当前分组）。
 *
 * 关键原因是透明代理那一节：启用 TProxy 后有 90 秒确认窗口，未确认就自动回滚。
 * 若切走分组会把待确认横幅与倒计时按钮一起卸载，而那是规则配错、
 * 主机即将失联时唯一的补救入口。全部保持挂载则不管用户滚到哪一节，
 * 横幅都还在 DOM 里。（TUN 不进入该窗口，见 onToggleTransparent。）
 *
 * 代价是长页面仍需滚动，由 sticky 导航消化。
 */
let observer: IntersectionObserver | null = null

onMounted(() => {
  // section 由 v-if 控制的内容较多，等一帧让 DOM 稳定再挂观察器
  requestAnimationFrame(() => {
    const targets = sections
      .map((s) => document.getElementById(s.id))
      .filter((el): el is HTMLElement => el !== null)
    if (targets.length === 0) return

    observer = new IntersectionObserver(
      (entries) => {
        // 取当前可见的最靠上那一节，与 DocsView 的目录高亮同一套判定
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible.length > 0) activeSection.value = visible[0]!.target.id
      },
      // 顶部留出 96px：标题刚滑到视口顶端就该高亮，而非等它到屏幕中间。
      // 窄屏顶部有 sticky header（约 48px），这里一并让位。
      { rootMargin: '-96px 0px -70% 0px', threshold: 0 },
    )
    targets.forEach((el) => observer!.observe(el))
  })
})

onUnmounted(() => observer?.disconnect())

function jumpToSection(id: string) {
  const el = document.getElementById(id)
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  // 先行高亮：平滑滚动期间 observer 要等滚完才回调，不抢先设置的话
  // 点了导航却看不到任何反馈
  activeSection.value = id
  navOpen.value = false
}

// 窄屏导航默认折叠：七项展开后会把正文顶下去一大截
const navOpen = ref(false)
</script>

<template>
  <!-- max-w 必须配 mx-auto：否则内容被压在宽屏左侧，右半边整片空白。
       宽度与内核管理、冲突处理等页面保持一致。
       用浏览器原生滚动条滚动本页，不再自造内部滚动区。 -->
  <!-- pb-28 给底部悬浮操作条让位：否则滚到最底时它会盖住
       「配置合并策略」的最后一行内容与那一节自己的保存按钮 -->
  <main class="p-4 pb-28 sm:p-6 sm:pb-28 lg:p-8 lg:pb-28 max-w-7xl mx-auto">
    <h1 class="text-2xl sm:text-3xl font-bold mb-4 sm:mb-6">系统设置</h1>

    <!-- 操作结果统一走 toast（见 stores/notify.ts），不在页面里占位 -->

    <!-- 窄屏导航开关 -->
    <Button
      variant="secondary"
      class="mb-3 w-full lg:hidden"
      :aria-expanded="navOpen"
      aria-controls="settings-nav"
      @click="navOpen = !navOpen"
    >
      {{ navOpen ? '收起分组导航' : '展开分组导航' }}（{{ sections.length }} 项）
    </Button>

    <div class="flex flex-col gap-4 sm:gap-6 lg:flex-row lg:items-start">
      <!-- 分组导航。锚点跳转而非标签切换，理由见 script 里 jumpToSection 上方注释。
           lg 上 sticky 常驻可见；父级链上不能出现 overflow:hidden/auto，
           否则 sticky 失效（App.vue 侧边栏踩过同一个坑）。 -->
      <nav
        id="settings-nav"
        :class="[
          'thin-scrollbar shrink-0 rounded-lg border border-line bg-surface p-2 lg:sticky lg:top-6 lg:w-56 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto',
          navOpen ? 'block' : 'hidden lg:block',
        ]"
        aria-label="设置分组导航"
      >
        <ul class="space-y-0.5 text-sm">
          <li v-for="sec in sections" :key="sec.id">
            <button
              :class="[
                // 选中项用左侧色条 + 强调文字色。不能用 text-accent：
                // 本项目里 --accent 是背景色 token，配淡色底就是同色压同色
                // （见 tests/color-contrast.spec.ts）。文字要用 accent-text。
                'relative block w-full rounded-md py-2 pl-3 pr-2 text-left font-medium transition-colors',
                activeSection === sec.id
                  ? 'bg-accent-solid/10 font-semibold text-accent-text dark:bg-accent-solid/20'
                  : 'text-fg hover:bg-elevated',
              ]"
              :aria-current="activeSection === sec.id ? 'location' : undefined"
              @click="jumpToSection(sec.id)"
            >
              <span
                v-if="activeSection === sec.id"
                class="absolute inset-y-1 left-0 w-0.5 rounded-full bg-accent-text"
                aria-hidden="true"
              />
              {{ sec.title }}
            </button>
          </li>
        </ul>
      </nav>

      <div class="min-w-0 flex-1 bg-surface rounded-lg shadow border border-line p-4 sm:p-6 space-y-6">
        <!-- 各 section 带 id 供导航锚点定位；scroll-mt 让跳转后标题不被
             窄屏顶部 sticky header 盖住（App.vue 里那条，约 48px）。 -->
        <section id="password" class="scroll-mt-20">
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

        <section id="components" class="scroll-mt-20">
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

        <section id="auto-update" class="scroll-mt-20">
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
             环境不具备条件时开关禁用并说明缺什么。
             启用 TProxy 后必须在时限内确认网络正常，否则后端自动回滚
             （面板下发的规则配错可能导致主机失联）；TUN 由 mihomo 自管规则
             并在退出时清理，不需要确认。
             开关状态写入基础配置的 tun.enable / tproxy-port，
             与「配置中心」编辑的是同一份数据。 -->
        <section id="transparent" class="scroll-mt-20">
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

          <!-- 「配置了端口但未接管」的提示。
               放在开关上方而不是折叠进下面的说明里：用户此刻看到的是一个关着的
               开关，而基础配置里明明写着 tproxy-port，不解释清楚就会以为面板出错。
               措辞刻意不判定对错——他可能本就自己在管规则（本项目支持的用法），
               也可能以为填了端口就等于开启。面板无从区分，只陈述事实。 -->
          <div
            v-if="tp.status.portConfiguredOnly"
            class="mb-3 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-500/40 dark:bg-amber-500/10"
          >
            <div class="font-medium text-amber-800 dark:text-amber-300">
              基础配置里有 tproxy-port（{{ tp.status.tproxyPort }}），但流量未被本面板接管
            </div>
            <p class="mt-1 text-xs text-amber-700 dark:text-amber-400">
              TProxy 需要两部分才生效：端口让内核监听（已配置），以及防火墙规则与策略路由把流量引到该端口（当前不是本面板下发的）。
              如果你在自行维护这些规则，忽略此提示即可；否则请在下方选择 TProxy 模式并打开开关，由面板下发规则。
            </p>
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

          <!-- 自动准备环境。只在确实有东西可做时出现：缺 /dev/net/tun 那种
               装包也解决不了（要改 compose 映射设备），给按钮反而误导。 -->
          <div
            v-if="tp.canProvision"
            class="mt-3 rounded-md border border-line bg-elevated p-3 text-sm"
          >
            <div class="font-medium">尝试自动准备环境</div>
            <p class="mt-1 text-xs text-fg-muted">
              由面板代为补齐缺失条件。只安装程序内置的固定软件包，sysctl 只写本程序专用的
              配置文件；不会改动防火墙规则，也不会自动打开上面的开关。
            </p>

            <div class="mt-2 space-y-1">
              <label v-if="tp.canInstallPackages" class="flex items-start gap-2 text-xs">
                <Checkbox v-model="provisionOpts.installPackages" class="mt-0.5" />
                <span>
                  安装缺失的软件包<span v-if="tp.status.env.packageManager">
                    （{{ tp.status.env.packageManager }}）</span
                  >
                </span>
              </label>
              <label v-if="tp.canApplySysctl" class="flex items-start gap-2 text-xs">
                <Checkbox v-model="provisionOpts.applySysctl" class="mt-0.5" />
                <span>调整系统参数并持久化到 /etc/sysctl.d/</span>
              </label>
            </div>

            <!-- 容器内的两点不同必须写在按钮旁边，而不是藏在结果里：
                 用户需要在点之前就知道装的包不持久 -->
            <p v-if="tp.status.env.inContainer" class="mt-2 text-xs text-amber-600 dark:text-amber-400">
              容器内安装的软件包在容器重建后会丢失，长期使用请在镜像里预装；容器内不会修改
              sysctl（非特权容器会被拒绝，host 网络下会直接改动宿主）。
            </p>
            <p v-else-if="!tp.status.env.root" class="mt-2 text-xs text-amber-600 dark:text-amber-400">
              当前不是以 root 运行，自动准备会被拒绝。请用上面/下面的手动命令在宿主上执行。
            </p>

            <Button class="mt-2" size="sm" :disabled="provisionDisabled" @click="onProvision">
              {{ tp.provisioning ? '正在准备…' : '开始准备' }}
            </Button>

            <!-- 结果逐步展示。失败项给出命令原始输出——apt 报的是源不可达
                 还是磁盘满，只有原文说得清。 -->
            <div v-if="tp.provisionResult" class="mt-3 space-y-2">
              <p class="text-xs" :class="tp.provisionResult.success ? 'text-success' : 'text-destructive'">
                {{ tp.provisionResult.message }}
              </p>
              <div
                v-for="(s, i) in tp.provisionResult.steps"
                :key="i"
                class="rounded border border-line bg-surface p-2"
              >
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-xs font-medium">{{ s.name }}</span>
                  <Badge :variant="stepBadge(s).variant">{{ stepBadge(s).text }}</Badge>
                </div>
                <code v-if="s.command" class="mt-1 block break-all font-mono text-xs text-fg-subtle">
                  {{ s.command }}
                </code>
                <pre
                  v-if="s.detail"
                  class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-elevated p-1.5 font-mono text-xs"
                  >{{ s.detail }}</pre
                >
              </div>

              <!-- 手动命令无论成功失败都给：失败时是兜底，成功时便于记进部署脚本 -->
              <div v-if="tp.provisionResult.manualCommands.length" class="mt-2">
                <div class="mb-1 text-xs text-fg-muted">等价手动命令</div>
                <div
                  v-for="(c, i) in tp.provisionResult.manualCommands"
                  :key="i"
                  class="mb-1 flex flex-wrap items-center gap-2"
                >
                  <code class="flex-1 break-all rounded bg-surface px-2 py-1 font-mono text-xs">{{ c }}</code>
                  <button class="btn btn-secondary btn-sm" @click="copyText(c, '命令')">复制</button>
                </div>
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

        <section id="network" class="scroll-mt-20">
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

        <section id="logs" class="scroll-mt-20">
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

        <section id="merge-policy" class="border-t pt-5 scroll-mt-20">
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
      </div>
    </div>

    <!-- 整页级操作条：底部居中悬浮，滚到哪都能保存。
         各分组自己的按钮（改密码、保存合并策略等）仍留在原处——
         它们各自打一个独立接口，挪上来会让「这一条按钮管哪些改动」变得含混。

         z-20 与 App.vue 的窄屏顶栏同层：两者一上一下不会重叠，
         而抽屉(z-40)/弹窗(z-50)/toast(z-60) 都在其上，展开时不会被这条压住。 -->
    <!-- lg:left-64 让位给 App.vue 的侧边栏（w-64）：fixed 是相对视口定位的，
         不补这一段，"居中"会算上侧边栏那 256px，看起来明显偏左 -->
    <div
      class="safe-inset-b pointer-events-none fixed inset-x-0 z-20 flex justify-center px-4 lg:left-64"
    >
      <div
        class="pointer-events-auto flex items-center gap-3 rounded-full border border-line bg-surface/95 px-4 py-2 shadow-lg backdrop-blur"
      >
        <Button :disabled="store.loading" @click="onSave">
          {{ store.loading ? '保存中…' : '保存设置' }}
        </Button>
      </div>
    </div>
  </main>
</template>

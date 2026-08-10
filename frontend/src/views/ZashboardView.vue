<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import api from '../api'
import { Button } from '@/components/ui/button'
import { clearPageChrome, setPageChrome } from '../composables/usePageChrome'

// 内嵌 zashboard。
//
// zashboard 与本管理端同源（后端把它挂在 /ui/，路径不同但协议/主机/端口
// 与管理端一致），因此可以直接 iframe 内嵌；也正因为同源，两者的
// localStorage 本来就是浏览器里同一份存储，父页面不必等 iframe 加载完成
// 再跨 frame 访问，直接用 window.localStorage 即可。
//
// 面板访问内核不再直连 external-controller：公网场景该地址不可达（内核绑
// 127.0.0.1）或裸 http（被浏览器按混合内容拦截），因此 /dashboard/entry
// 拼出经同源反代 /mihomo-api 的入口（hostname=当前主机&port=当前端口&
// https&secondaryPath=/mihomo-api&secret=...），API 与 WebSocket 都走这条
// 同源路径。见 backend/internal/mihomo/proxy.go 与 dashboardEntryLogic.go。
//
// 但 zashboard 只在“首次配置”时才会读取 URL 参数：它把后端信息存进自己的
// localStorage["setup/api-list"]，一旦存过一条，启动时只走“URL 参数与已存
// 记录逐字段比对，完全相同才切换”这条路径——外部控制端口一变，比对必然
// 失败，它既不会新增也不会更新，只会继续用旧端口的记录，看起来就是
// “改端口后面板连不上”。见 data/zashboard/assets/*.js 内 dbe/HX 等函数
// （已读源码确认此行为）。
//
// 因此这里在拿到最新 host/port/secret、但还没设置 frameSrc（即 iframe
// 还没开始加载）之前，先把 zashboard 自己的 localStorage 更新好，
// 它下次初始化读到的就已经是新端口，不需要用户手动重新走一遍配置向导。
// 顺序很重要：如果等 iframe 加载完再改，zashboard 的启动脚本可能已经用
// 旧值发起过一次连接尝试。
const frameSrc = ref('')
const loading = ref(true)
const errorMsg = ref('')
const entryHost = ref('')
const entryPort = ref('')

// 同源反代前缀，必须与后端 internal/mihomo.MihomoAPIPrefix 保持一致。
const MIHOMO_API_PREFIX = '/mihomo-api'

interface ZashboardBackend {
  uuid: string
  type?: string
  protocol?: string
  host: string
  port: string
  password?: string
  secondaryPath?: string
  label?: string
  [key: string]: unknown
}

/**
 * 面板访问 authority 以浏览器 location 为准。
 *
 * 后端 /dashboard/entry 在 nginx 终结 TLS 且 TrustedProxies 未配齐时，可能把
 * 公网入口误判成 http + 80；前端仍处在 https 页，若照抄后端 port 会写成
 * https://host:80，zashboard 全挂。location.host/port/protocol 是用户真实
 * 打开面板的地址，同源反代也必须指回这里。
 */
function browserAuthority(): { host: string; port: string; protocol: string } {
  const protocol = location.protocol === 'https:' ? 'https' : 'http'
  const host = location.hostname || ''
  const port =
    location.port ||
    (protocol === 'https' ? '443' : '80')
  return { host, port, protocol }
}

function syncZashboardBackend(host: string, port: string, secret: string) {
  try {
    // 优先浏览器真实 authority；后端 host/port 仅在 location 不可用时兜底
    const auth = browserAuthority()
    const finalHost = auth.host || host
    const finalPort = auth.port || port
    const protocol = auth.protocol
    if (!finalHost || !finalPort) return
    const listRaw = window.localStorage.getItem('setup/api-list')
    const uuid = window.localStorage.getItem('setup/active-uuid')
    // 从未配置：写入一条默认后端，避免 zashboard 仍连旧/错地址刷 Network Error
    if (!listRaw || !uuid) {
      const id =
        typeof crypto !== 'undefined' && 'randomUUID' in crypto
          ? crypto.randomUUID()
          : `aurora-${Date.now()}`
      const entry: ZashboardBackend = {
        uuid: id,
        // zashboard 的类型体系只有 clash / singbox 两类，协议由 protocol
        // 字段决定（见其 SetupPage 默认值 {type:'clash',protocol:'http'}）。
        // 此前误写 type:'http' 且漏写 protocol，zashboard 拼请求地址时
        // 得到 undefined://host:port，全部 API 请求失败（手机端首次配置
        // 必踩，桌面端靠旧记录侥幸正常）。这里对齐向导结构。
        type: 'clash',
        protocol,
        secondaryPath: MIHOMO_API_PREFIX,
        host: finalHost,
        port: finalPort,
        password: secret,
        label: 'AuroraMihomo 本地内核',
      }
      window.localStorage.setItem('setup/api-list', JSON.stringify([entry]))
      window.localStorage.setItem('setup/active-uuid', id)
      return
    }

    const list = JSON.parse(listRaw) as ZashboardBackend[]
    if (!Array.isArray(list)) return
    let cur = list.find((b) => b.uuid === uuid)
    if (!cur) {
      cur = {
        uuid,
        type: 'clash',
        protocol,
        secondaryPath: MIHOMO_API_PREFIX,
        host: finalHost,
        port: finalPort,
        password: secret,
        label: 'AuroraMihomo 本地内核',
      }
      list.push(cur)
    }

    // 兼容此前写入的坏记录（type 非法或缺 protocol）：非 singbox 一律收敛
    // 为 clash + 当前页面协议 + 同源反代路径，否则 zashboard 继续用
    // undefined 协议拼地址报错。singbox 记录保留用户手动配置的协议。
    if (cur.type !== 'singbox') {
      cur.type = 'clash'
      cur.protocol = protocol
      cur.secondaryPath = MIHOMO_API_PREFIX
    }

    const changed =
      cur.host !== finalHost ||
      cur.port !== finalPort ||
      (cur.password || '') !== secret ||
      (cur.protocol || '') !== protocol ||
      (cur.secondaryPath || '') !== MIHOMO_API_PREFIX
    if (!changed) return // 已经是最新值，不必写回（避免每次加载都触发 localStorage 变更事件）
    cur.host = finalHost
    cur.port = finalPort
    cur.password = secret
    cur.protocol = protocol
    cur.secondaryPath = MIHOMO_API_PREFIX
    window.localStorage.setItem('setup/api-list', JSON.stringify(list))
  } catch {
    // 读写失败（localStorage 被禁用、字段格式在 zashboard 未来版本变化等）
    // 静默放弃自动同步，用户仍可在面板内手动改后端设置
  }
}

/**
 * API 用 localStorage Bearer，/mihomo-api 反代只认 aurora_session cookie。
 * 与 AdGuardView 同策略：挂 iframe 前再覆盖一次 cookie，避免手机浏览器
 * （尤其 Android Edge）长期留着过期/缺失会话导致面板全 401。
 * 失败也继续：登录时通常已有 cookie，探测由 zashboard 自己重试。
 */
async function ensureSessionCookie() {
  try {
    await api.post('/auth/session', null, { skipErrorToast: true })
  } catch {
    // 保留既有 cookie 继续尝试挂载
  }
}

/** 把对接信息与「新标签页」交给 App 移动端顶栏，避免本页再叠一条 header。 */
function syncPageChrome() {
  const hostLine =
    entryHost.value && entryPort.value
      ? `已对接内核 ${entryHost.value}:${entryPort.value}`
      : ''
  setPageChrome({
    subtitle: hostLine,
    action: {
      label: '新标签页',
      disabled: !frameSrc.value,
      onClick: openExternally,
    },
  })
}

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    // 先对齐 HttpOnly 会话，再拉入口 / 挂 iframe：反代鉴权只认 cookie
    await ensureSessionCookie()
    // 错误展示在页内 errorMsg，避免再弹全局 toast
    const res = await api.get('/dashboard/entry', { skipErrorToast: true })
    if (!res.data?.available) {
      errorMsg.value = res.data?.message || '内核未启用外部控制接口（external-controller），面板无法连接。'
      frameSrc.value = ''
      entryHost.value = ''
      entryPort.value = ''
      return
    }
    // UI 展示与 zashboard 记录都优先浏览器真实 authority，避免后端误判 :80
    const auth = browserAuthority()
    entryHost.value = auth.host || res.data.host || ''
    entryPort.value = auth.port || res.data.port || ''
    const secret = new URL(res.data.url, window.location.origin).searchParams.get('secret') || ''
    syncZashboardBackend(entryHost.value, entryPort.value, secret)
    frameSrc.value = res.data.url
  } catch (e: unknown) {
    const { apiErrorMessage } = await import('../utils/apiError')
    errorMsg.value = apiErrorMessage(e, '获取面板入口失败')
    frameSrc.value = ''
    entryHost.value = ''
    entryPort.value = ''
  } finally {
    loading.value = false
    syncPageChrome()
  }
}

// 在新标签页打开，供需要独立窗口的场景使用
function openExternally() {
  if (frameSrc.value) window.open(frameSrc.value, '_blank', 'noopener,noreferrer')
}

watch([frameSrc, entryHost, entryPort], syncPageChrome)

onMounted(load)
onBeforeUnmount(clearPageChrome)
</script>

<template>
  <!-- 内嵌完整应用，iframe 必须有明确高度。
       高度由 App 在 Zashboard 路由上锁 h-dvh 后 flex 分给本页（顶栏已占一部分），
       这里用 flex-1/h-full 吃剩余空间，不再自己 h-dvh——否则会叠在 App 顶栏之下
       把底栏顶出可视区（手机浏览器里表现为上下都被挡）。 -->
  <main class="flex-1 h-full min-h-0 flex flex-col">
    <!-- 仅桌面（lg+）保留完整页面 header：标题 + 对接信息 + 重新加载/新标签页。
         小屏改由 App 顶栏承载标题/副标题/「新标签页」，这里 hidden 避免叠两层。 -->
    <div
      data-testid="zashboard-page-header"
      class="hidden lg:flex flex-wrap items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-surface shrink-0"
    >
      <div>
        <h1 class="text-xl font-bold">Zashboard</h1>
        <p v-if="entryHost" class="text-xs text-fg-subtle">
          已对接内核 {{ entryHost }}:{{ entryPort }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button size="sm" @click="load">重新加载</Button>
        <Button variant="secondary" size="sm" :disabled="!frameSrc" @click="openExternally">
          在新标签页打开
        </Button>
      </div>
    </div>

    <p v-if="loading" class="p-4 sm:p-6 text-sm text-fg-muted">正在获取面板入口…</p>

    <div v-else-if="errorMsg" class="p-4 sm:p-6">
      <div class="rounded border border-amber-300 bg-amber-50 p-4 text-sm text-amber-800">
        <p class="font-medium mb-1">面板暂时无法打开</p>
        <p>{{ errorMsg }}</p>
        <p class="mt-2 text-xs text-amber-700 dark:text-amber-300">
          请在「配置中心 → 外部控制」中设置 external-controller（如 127.0.0.1:9090）后重新合并配置。
          若尚未安装面板资源，可到「系统设置」中执行「更新 Zashboard」。
        </p>
      </div>
    </div>

    <!-- 同源内嵌，无需 sandbox 放行清单；面板需要访问 localStorage 保存自身设置。
         上方 load() 里已在设置 frameSrc 之前调用 syncZashboardBackend，
         这里加载时读到的就是同步过的值，不需要再监听 @load 二次处理 -->
    <!-- absolute 填满：手机（含安卓 Edge）上 flex-1 直接挂 iframe 常高度为 0 白屏 -->
    <div
      v-else
      class="relative flex-1 min-h-0 w-full bg-surface"
      data-testid="zashboard-iframe-shell"
    >
      <iframe
        :src="frameSrc"
        class="absolute inset-0 h-full w-full border-0"
        title="Zashboard 控制面板"
      ></iframe>
    </div>
  </main>
</template>

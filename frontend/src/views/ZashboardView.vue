<script setup lang="ts">
import { onMounted, ref } from 'vue'
import api from '../api'
import { Button } from '@/components/ui/button'

// 内嵌 zashboard。
//
// zashboard 与本管理端同源（后端把它挂在 /ui/，路径不同但协议/主机/端口
// 与管理端一致），因此可以直接 iframe 内嵌；也正因为同源，两者的
// localStorage 本来就是浏览器里同一份存储，父页面不必等 iframe 加载完成
// 再跨 frame 访问，直接用 window.localStorage 即可。
//
// /dashboard/entry 会用当前内核的 external-controller 拼出带
// ?hostname=&port=&secret= 的地址，但 zashboard 只在“首次配置”时才会读取
// 这几个 URL 参数：它把后端信息存进自己的 localStorage["setup/api-list"]，
// 一旦存过一条，启动时只走“URL 参数与已存记录逐字段比对，完全相同才切换”
// 这条路径——外部控制端口一变，比对必然失败，它既不会新增也不会更新，
// 只会继续用旧端口的记录，看起来就是“改端口后面板连不上”。
// 见 data/zashboard/assets/*.js 内 dbe/HX 等函数（已读源码确认此行为）。
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

interface ZashboardBackend {
  uuid: string
  type?: string
  protocol?: string
  host: string
  port: string
  password?: string
  label?: string
  [key: string]: unknown
}

function syncZashboardBackend(host: string, port: string, secret: string) {
  try {
    const listRaw = window.localStorage.getItem('setup/api-list')
    const uuid = window.localStorage.getItem('setup/active-uuid')
    if (!listRaw || !uuid) return // 从未配置过，交给 zashboard 自己的首次配置逻辑

    const list = JSON.parse(listRaw) as ZashboardBackend[]
    if (!Array.isArray(list)) return
    const cur = list.find((b) => b.uuid === uuid)
    if (!cur) return

    if (cur.host === host && cur.port === port && (cur.password || '') === secret) {
      return // 已经是最新值，不必写回（避免每次加载都触发 localStorage 变更事件）
    }
    cur.host = host
    cur.port = port
    cur.password = secret
    window.localStorage.setItem('setup/api-list', JSON.stringify(list))
  } catch {
    // 读写失败（localStorage 被禁用、字段格式在 zashboard 未来版本变化等）
    // 静默放弃自动同步，用户仍可在面板内手动改后端设置
  }
}

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await api.get('/dashboard/entry')
    if (!res.data?.available) {
      errorMsg.value = res.data?.message || '内核未启用外部控制接口（external-controller），面板无法连接。'
      frameSrc.value = ''
      return
    }
    entryHost.value = res.data.host || ''
    entryPort.value = res.data.port || ''
    const secret = new URL(res.data.url, window.location.origin).searchParams.get('secret') || ''
    syncZashboardBackend(entryHost.value, entryPort.value, secret)
    frameSrc.value = res.data.url
  } catch (e: any) {
    errorMsg.value = e?.response?.data?.message || e?.message || '获取面板入口失败'
    frameSrc.value = ''
  } finally {
    loading.value = false
  }
}

// 在新标签页打开，供需要独立窗口的场景使用
function openExternally() {
  if (frameSrc.value) window.open(frameSrc.value, '_blank', 'noopener,noreferrer')
}

onMounted(load)
</script>

<template>
  <!-- 内嵌的是另一个完整应用，iframe 没有内在高度，必须自己撑出一屏；
       外壳已不再锁一屏高度，这里改用 h-dvh 自持，而不是依赖父级传下来的
       h-full。iframe 内部有自己的滚动，不影响本页用浏览器滚动条。 -->
  <main class="h-dvh flex flex-col min-h-0">
    <!-- 小屏与 App 顶栏（菜单+标题）叠两层会挤掉 iframe 高度；
         ≥lg 再显示本页标题、对接信息与「重新加载 / 新标签页」。 -->
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
    <iframe
      v-else
      :src="frameSrc"
      class="flex-1 min-h-0 w-full border-0"
      title="Zashboard 控制面板"
    ></iframe>
  </main>
</template>

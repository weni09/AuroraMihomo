<script setup lang="ts">
import { onMounted, ref } from 'vue'
import api from '../api'
import { Button } from '@/components/ui/button'

// 内嵌 zashboard。
//
// zashboard 与本管理端同源（后端把它挂在 /ui/），因此可以直接 iframe 内嵌，
// 不受同源策略限制；后端的安全响应头也已放开为 SAMEORIGIN / frame-src 'self'。
//
// /dashboard/entry 会用当前内核的 external-controller 拼出带
// ?hostname=&port=&secret= 的地址，面板据此自动对接内核，用户无需手填。
const frameSrc = ref('')
const loading = ref(true)
const errorMsg = ref('')
const entryHost = ref('')
const entryPort = ref('')

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
    <div class="flex flex-wrap items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-surface shrink-0">
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

    <!-- 同源内嵌，无需 sandbox 放行清单；面板需要访问 localStorage 保存自身设置 -->
    <iframe
      v-else
      :src="frameSrc"
      class="flex-1 min-h-0 w-full border-0"
      title="Zashboard 控制面板"
    ></iframe>
  </main>
</template>

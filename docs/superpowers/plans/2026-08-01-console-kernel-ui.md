# 控制台与内核管理页 UI 优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按方案 B 美化并增强 `DashboardView` 与 `MihomoView`：共享日志预览与操作条、控制台快捷操作、内核页实时状态/日志、Dialog 确认危险操作，并修复 `any`/一致性问题。

**Architecture:** 抽出 `KernelLogPreview` 与 `KernelActionBar` 两个小组件；状态与操作仍走 `useMihomoStore`；确认 Dialog 只在 ActionBar UI 层；两页用 `useRealtime`（或薄封装）同步 `mihomo.status` / `log.message`。视觉用现有 Card/Button/Badge 与主题 token。

**Tech Stack:** Vue 3.5 + `<script setup lang="ts">`、Pinia、Vitest + `@vue/test-utils`、Tailwind 3.4、shadcn-vue Card/Button/Dialog（`ModalDialog` 封装）

**Spec:** `docs/superpowers/specs/2026-08-01-console-kernel-ui-design.md`

---

## File map

| 文件 | 职责 |
|---|---|
| Create `frontend/src/components/KernelLogPreview.vue` | 终端风格日志列表 + 空状态 |
| Create `frontend/src/components/KernelLogPreview.spec.ts` | 空/有数据渲染 |
| Create `frontend/src/components/KernelActionBar.vue` | 启停/重启/重载/(可选)更新 + 确认 Dialog |
| Create `frontend/src/components/KernelActionBar.spec.ts` | 确认前不调用 stop |
| Create `frontend/src/composables/useMihomoRealtime.ts` | 统一订阅 status/log 推送 |
| Modify `frontend/src/views/MihomoView.vue` | 页头、Card、ActionBar、LogPreview、realtime |
| Modify `frontend/src/views/DashboardView.vue` | 页头、Card、快捷操作、LogPreview、去 any |
| Optional type in `frontend/src/stores/mihomo.ts` | 导出日志行类型供组件复用（若需要） |

---

### Task 1: KernelLogPreview（TDD）

**Files:**
- Create: `frontend/src/components/KernelLogPreview.vue`
- Create: `frontend/src/components/KernelLogPreview.spec.ts`

- [ ] **Step 1: 写失败测试**

```ts
// frontend/src/components/KernelLogPreview.spec.ts
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import KernelLogPreview from './KernelLogPreview.vue'

describe('KernelLogPreview', () => {
  it('空列表显示空状态文案', () => {
    const w = mount(KernelLogPreview, { props: { lines: [] } })
    expect(w.text()).toMatch(/暂无|等待/)
  })

  it('渲染 time / stream / message', () => {
    const w = mount(KernelLogPreview, {
      props: {
        lines: [{ time: '12:00:01', stream: 'stdout', message: 'hello kernel' }],
      },
    })
    expect(w.text()).toContain('12:00:01')
    expect(w.text()).toContain('stdout')
    expect(w.text()).toContain('hello kernel')
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd frontend && npx vitest run src/components/KernelLogPreview.spec.ts
```

Expected: FAIL（组件不存在或未导出）

- [ ] **Step 3: 实现组件**

```vue
<!-- frontend/src/components/KernelLogPreview.vue -->
<script setup lang="ts">
export interface KernelLogLine {
  time: string
  stream: string
  message: string
}

withDefaults(
  defineProps<{
    lines: KernelLogLine[]
    /** 高度类，默认与原内核页接近 */
    heightClass?: string
    emptyText?: string
  }>(),
  {
    heightClass: 'h-64 sm:h-80',
    emptyText: '暂无日志',
  },
)
</script>

<template>
  <!-- 终端观感：等宽深色底，两主题都保留（见 Dashboard/Mihomo 原注释） -->
  <div
    class="bg-slate-900 text-slate-100 rounded-lg p-3 overflow-auto text-xs font-mono flex flex-col gap-1"
    :class="heightClass"
    role="log"
    aria-live="polite"
  >
    <div v-for="(l, i) in lines" :key="i" class="break-words">
      <span class="text-slate-500">{{ l.time }}</span>
      <span class="text-cyan-400"> [{{ l.stream }}]</span>
      {{ l.message }}
    </div>
    <div v-if="lines.length === 0" class="text-slate-500 italic">{{ emptyText }}</div>
  </div>
</template>
```

说明：日志区沿用项目既有「刻意终端 slate」写法（与现网 Dashboard/Logs 一致）；**不要**把 `text-slate-*` 扩散到状态卡等非终端区。若 `check-conventions` 对新增文件报 FE1，在注释中写明「终端区例外、与 LogsView 同源」并仅限本组件。

- [ ] **Step 4: 测试通过**

```bash
cd frontend && npx vitest run src/components/KernelLogPreview.spec.ts
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/KernelLogPreview.vue frontend/src/components/KernelLogPreview.spec.ts
git commit -m "feat(frontend): add KernelLogPreview shared terminal log list"
```

---

### Task 2: KernelActionBar（TDD + Dialog 确认）

**Files:**
- Create: `frontend/src/components/KernelActionBar.vue`
- Create: `frontend/src/components/KernelActionBar.spec.ts`
- Reference: `frontend/src/components/ModalDialog.vue`、`frontend/src/stores/mihomo.ts`

- [ ] **Step 1: 写失败测试（mock store）**

```ts
// frontend/src/components/KernelActionBar.spec.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, DOMWrapper, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import KernelActionBar from './KernelActionBar.vue'
import { useMihomoStore } from '../stores/mihomo'

vi.mock('../api', () => ({
  default: {
    post: vi.fn(),
    get: vi.fn(),
  },
}))

const body = () => new DOMWrapper(document.body)

afterEach(() => {
  document.body.removeAttribute('style')
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('KernelActionBar 危险操作确认', () => {
  it('点击停止先弹出确认，确认前不调用 stop', async () => {
    setActivePinia(createPinia())
    const store = useMihomoStore()
    const stopSpy = vi.spyOn(store, 'stop').mockResolvedValue()

    const w = mount(KernelActionBar, {
      attachTo: document.body,
      global: { plugins: [createPinia()] },
    })
    // 重新绑定同一 pinia：上面 setActive 后再 create 会错，统一如下：
    w.unmount()
    const pinia = createPinia()
    setActivePinia(pinia)
    const store2 = useMihomoStore()
    const stopSpy2 = vi.spyOn(store2, 'stop').mockResolvedValue()
    const wrapper = mount(KernelActionBar, {
      attachTo: document.body,
      global: { plugins: [pinia] },
    })

    await wrapper.get('[data-testid="kernel-stop"]').trigger('click')
    await wrapper.vm.$nextTick()
    expect(stopSpy2).not.toHaveBeenCalled()
    expect(body().find('[role="dialog"]').exists()).toBe(true)

    await body().get('[data-testid="kernel-confirm-ok"]').trigger('click')
    await flushPromises()
    expect(stopSpy2).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
```

**注意：** 实现测试时把 pinia 初始化整理成干净的一段（上面草稿有重复 mount，落地时只保留正确版）：

```ts
const pinia = createPinia()
setActivePinia(pinia)
const store = useMihomoStore()
const stopSpy = vi.spyOn(store, 'stop').mockResolvedValue(undefined as never)
const wrapper = mount(KernelActionBar, {
  attachTo: document.body,
  global: { plugins: [pinia] },
})
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd frontend && npx vitest run src/components/KernelActionBar.spec.ts
```

Expected: FAIL

- [ ] **Step 3: 实现 KernelActionBar**

要点：

- props: `showUpdate?: boolean`（默认 false）、`compact?: boolean`（控制台 true 时可用 `size="sm"`）
- 内部 `pendingAction: null | 'stop' | 'restart'`
- 使用 `ModalDialog`：`open`、title、说明文案、取消 / 确定按钮（`data-testid` 如上）
- 按钮：启动 default、停止 destructive、重启/重载 outline、更新 outline + `updating` 本地 state 调 `POST /update/mihomo`（与现 MihomoView 逻辑相同，可内联或 emit；**推荐内联**以免控制台也要复制）
- 直接 `useMihomoStore()` 调 `start/stop/restart/reload`

确认文案：

- stop: `停止内核将中断所有正在进行的代理连接，确定停止？`
- restart: `重启内核会短暂中断所有代理连接，确定重启？`

- [ ] **Step 4: 测试通过**

```bash
cd frontend && npx vitest run src/components/KernelActionBar.spec.ts
```

Expected: PASS（可再加：取消不调用 stop）

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/KernelActionBar.vue frontend/src/components/KernelActionBar.spec.ts
git commit -m "feat(frontend): add KernelActionBar with dialog confirm for stop/restart"
```

---

### Task 3: useMihomoRealtime composable

**Files:**
- Create: `frontend/src/composables/useMihomoRealtime.ts`
- Modify later: both views

- [ ] **Step 1: 实现 composable**（逻辑简单，可与 Task 4 同测；若单独测可用 mock useRealtime）

```ts
// frontend/src/composables/useMihomoRealtime.ts
import { useMihomoStore } from '../stores/mihomo'
import { useRealtime } from './useRealtime'

/**
 * 订阅内核状态与日志推送，写入 mihomo store。
 * 控制台与内核管理页共用，避免一边实时一边过期。
 */
export function useMihomoRealtime() {
  const store = useMihomoStore()
  return useRealtime((type, data) => {
    if (type === 'mihomo.status' && data && typeof data === 'object') {
      const d = data as { status?: string; version?: string; pid?: number; appVersion?: string }
      store.applyStatus({
        status: d.status,
        version: d.version,
        pid: d.pid,
        appVersion: d.appVersion,
      })
    }
    if (type === 'log.message' && data && typeof data === 'object') {
      const d = data as { time?: string; stream?: string; message?: string }
      store.pushLog(d)
    }
  })
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/composables/useMihomoRealtime.ts
git commit -m "feat(frontend): share mihomo status/log realtime subscription"
```

---

### Task 4: 重写 MihomoView

**Files:**
- Modify: `frontend/src/views/MihomoView.vue`

- [ ] **Step 1: 替换为 Card + 页头 + KernelActionBar + KernelLogPreview + useMihomoRealtime**

结构草稿：

```vue
<script setup lang="ts">
import { onMounted } from 'vue'
import { useMihomoStore } from '../stores/mihomo'
import { useMihomoRealtime } from '../composables/useMihomoRealtime'
import KernelActionBar from '../components/KernelActionBar.vue'
import KernelLogPreview from '../components/KernelLogPreview.vue'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import api from '../api'
import type { KernelLogLine } from '../components/KernelLogPreview.vue'

const store = useMihomoStore()
const { status: wsStatus } = useMihomoRealtime()

onMounted(async () => {
  await store.fetchStatus()
  try {
    const res = await api.get<KernelLogLine[]>('/mihomo/logs')
    ;(res.data || []).slice(-40).forEach((l) => store.pushLog(l))
  } catch {
    /* 内核未起时忽略 */
  }
})

// 状态字对比度：小字不用 text-success/destructive token（见原注释）
const statusClass =
  "font-bold " +
  (/* binding */ '') // 模板里 :class 运行中 emerald / 停止 rose
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8 flex flex-col gap-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 class="text-2xl sm:text-3xl font-bold text-fg">内核管理</h1>
        <p class="text-xs sm:text-sm text-fg-subtle mt-1">启动、停止、重载与更新 mihomo 内核</p>
      </div>
      <!-- 可选：展示 ws 状态，与控制台一致用 labels.wsStatusLabel -->
    </div>

    <Card>
      <CardHeader>
        <CardTitle>运行状态</CardTitle>
        <CardDescription>进程状态随实时通道更新</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <!-- 三格 status / version / pid，bg-elevated -->
        <KernelActionBar show-update />
      </CardContent>
    </Card>

    <Card>
      <CardHeader class="flex flex-row items-center justify-between gap-3 space-y-0">
        <CardTitle>运行日志</CardTitle>
        <RouterLink to="/logs" class="text-sm text-primary hover:underline">完整日志面板</RouterLink>
      </CardHeader>
      <CardContent>
        <KernelLogPreview :lines="store.recentLogs" empty-text="暂无日志" />
      </CardContent>
    </Card>
  </main>
</template>
```

删除本文件内的 `confirm` / 旧按钮组 / 内联日志 / `any`。

- [ ] **Step 2: 类型检查相关文件**

```bash
cd frontend && npx vue-tsc --noEmit -p tsconfig.app.json
```

Expected: 无 MihomoView / 新组件错误

- [ ] **Step 3: Commit**

```bash
git add frontend/src/views/MihomoView.vue
git commit -m "feat(frontend): polish MihomoView with shared actions, logs, realtime"
```

---

### Task 5: 重写 DashboardView

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`

- [ ] **Step 1: 接入共享能力并抛光**

- 页头：`控制台` + 说明「内核运行概况 · 订阅与任务 · 快捷操作」
- 实时通道 Badge 保留 `wsStatusLabel`
- 状态卡：可用 Card 或保持 grid + `bg-surface`；配置卡冲突提示保留
- **快捷操作**：`<KernelActionBar compact />` + `RouterLink` 到 `/mihomo`
- 任务区 / 日志区：Card；日志用 `<KernelLogPreview :lines="mihomoStore.recentLogs" height-class="h-56 sm:h-72" empty-text="等待内核日志输出…" />`
- realtime：改为 `useMihomoRealtime()`，**保留**原有对 `config.updated` 的处理——因此要么：

```ts
const { status: wsStatus } = useRealtime((type, data) => {
  // 先处理 mihomo（可内联调用与 composable 相同逻辑）
  // 再处理 config.updated
})
```

要么扩展 composable 接受 optional extra handler：

```ts
export function useMihomoRealtime(onExtra?: (type: string, data: unknown) => void) {
  ...
  onExtra?.(type, data)
}
```

**推荐扩展 composable**，Dashboard：

```ts
const { status: wsStatus } = useMihomoRealtime((type) => {
  if (type === 'config.updated') void refreshConfigState()
})
```

- 历史日志：`api.get<KernelLogLine[]>`，禁止 `any`
- 去掉本页重复的 start/stop 逻辑（全走 ActionBar）

- [ ] **Step 2: 跑相关测试 + tsc**

```bash
cd frontend && npx vitest run src/components/KernelLogPreview.spec.ts src/components/KernelActionBar.spec.ts
cd frontend && npx vue-tsc --noEmit -p tsconfig.app.json
cd frontend && npx eslint src/views/DashboardView.vue src/views/MihomoView.vue src/components/KernelLogPreview.vue src/components/KernelActionBar.vue src/composables/useMihomoRealtime.ts
```

Expected: 全绿

- [ ] **Step 3: Commit**

```bash
git add frontend/src/views/DashboardView.vue frontend/src/composables/useMihomoRealtime.ts
git commit -m "feat(frontend): polish DashboardView with quick kernel actions and shared UI"
```

---

### Task 6: 约定检查与收尾

- [ ] **Step 1: conventions + frontend check**

```bash
python scripts/check-conventions.py --baseline scripts/conventions-baseline.txt
cd frontend && npx eslint . --max-warnings 0 2>/dev/null || npx eslint src/views/DashboardView.vue src/views/MihomoView.vue src/components/Kernel*.vue src/composables/useMihomoRealtime.ts
```

若 FE1 仅因日志终端 `slate` 新增行：与 LogsView 对齐的既有模式可接受；**不得**在状态卡使用 slate。若 baseline 增加，需说明原因（本计划预期不增加 baseline）。

- [ ] **Step 2: 对照 spec 验收清单勾选**

- [ ] 控制台：说明、状态卡、快捷操作、任务、日志  
- [ ] 内核页：实时、Dialog 确认  
- [ ] 无新增 any  
- [ ] 测试通过  

- [ ] **Step 3: 最终 commit（若有约定/文案微调）** 或无则跳过

---

## Spec coverage

| Spec 项 | Task |
|---|---|
| KernelLogPreview | 1 |
| KernelActionBar + Dialog | 2 |
| 实时订阅共享 | 3, 4, 5 |
| MihomoView 抛光 | 4 |
| Dashboard 快捷操作 + 抛光 | 5 |
| 去 any / 测试 / lint | 1–2, 5–6 |
| 非目标（不做假指标等） | 未列入任务 |

## Placeholder scan

无 TBD；测试与实现代码已给出可粘贴骨架（ActionBar 测试 pinia 段以实现时整理版为准）。

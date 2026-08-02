# Zashboard 移动端 header 优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `< lg` 视口隐藏 Zashboard 内置页的页面级 header，把垂直空间让给 iframe；`≥ lg` 保持现有标题与两个按钮。

**Architecture:** 仅改 `ZashboardView.vue` 的 header 容器 class（`hidden lg:flex` + 原有 flex 布局），业务逻辑（`load` / `openExternally` / `syncZashboardBackend`）不动。用 vitest + `@vue/test-utils` 断言 header 的响应式 class 契约，以及 loading/错误态在小屏逻辑下仍会渲染。

**Tech Stack:** Vue 3 + Tailwind 3.4 + vitest + `@vue/test-utils` + happy-dom；API 用 `vi.mock` 桩掉。

**Spec:** `docs/superpowers/specs/2026-08-02-zashboard-mobile-header-design.md`

---

## File map

| 文件 | 职责 |
|---|---|
| `frontend/src/views/ZashboardView.vue` | 内嵌 Zashboard；本轮只改页面级 header 的显示 class 与注释 |
| `frontend/src/views/ZashboardView.spec.ts` | **新建**；锁定「header 小屏隐藏、桌面 flex」与错误态仍展示 |

不修改：`App.vue`、路由、后端、Zashboard 静态资源。

---

### Task 1: 失败测试 — header 响应式 class 契约

**Files:**
- Create: `frontend/src/views/ZashboardView.spec.ts`
- Modify: （无，本任务只写红测）

- [ ] **Step 1: 创建失败测试文件**

创建 `frontend/src/views/ZashboardView.spec.ts`，完整内容如下：

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

// api 必须在导入组件之前 mock：模块求值时就会持有 api 引用
vi.mock('../api', () => ({
  default: { get: vi.fn(), put: vi.fn(), post: vi.fn() },
}))

import api from '../api'
import ZashboardView from './ZashboardView.vue'

const mockedApi = vi.mocked(api, true)

/**
 * 页面级 header 在小屏与 App 顶栏重复占高，挤压 iframe。
 * 契约：header 容器必须带 hidden lg:flex，由 Tailwind 在 <lg 隐藏、≥lg 显示。
 * 只断言 class 字符串，不依赖真实视口宽度（happy-dom 不会跑媒体查询布局）。
 */
describe('ZashboardView 移动端 header', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('成功加载时页面级 header 带 hidden lg:flex，桌面仍保留操作入口文案', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        available: true,
        host: '127.0.0.1',
        port: '9090',
        url: 'http://127.0.0.1:9090/ui/?hostname=127.0.0.1&port=9090&secret=s',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    // 用标题文本定位 header 区域，避免依赖易变的 DOM 结构顺序
    const title = wrapper.get('h1')
    expect(title.text()).toBe('Zashboard')
    const header = title.element.closest('div')
    expect(header).toBeTruthy()
    const cls = header!.className
    expect(cls.split(/\s+/)).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))

    expect(wrapper.text()).toContain('重新加载')
    expect(wrapper.text()).toContain('在新标签页打开')
    expect(wrapper.find('iframe').exists()).toBe(true)

    wrapper.unmount()
  })

  it('入口不可用时错误说明仍完整展示（不因隐藏 header 而吞掉）', async () => {
    mockedApi.get.mockResolvedValue({
      data: {
        available: false,
        message: '内核未启用外部控制接口',
      },
    })

    const wrapper = mount(ZashboardView)
    await flushPromises()

    expect(wrapper.text()).toContain('面板暂时无法打开')
    expect(wrapper.text()).toContain('内核未启用外部控制接口')
    expect(wrapper.find('iframe').exists()).toBe(false)

    wrapper.unmount()
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

在仓库根（Git Bash）执行：

```bash
cd frontend && npm test -- src/views/ZashboardView.spec.ts
```

**Expected:** FAIL。当前 header 只有 `flex flex-wrap ...`，没有 `hidden` / `lg:flex`，类似：

```
AssertionError: expected [ ... ] to deeply equal ArrayContaining ["hidden", "lg:flex"]
```

若失败原因是 `h1` 的 `closest('div')` 指到了内层标题包装 div（只有无 class 或没有 flex 类），先不要改实现：把定位改成「含 `h1` 且 class 含 `border-b` 的祖先」——但**优先**在 Task 2 给外层 header 加 class 后，用更稳的选择器。本计划在 Task 2 给 header 加 `data-testid="zashboard-page-header"`，并在本任务若定位不稳时允许测试改用 testid（见 Task 2 Step 1 同步改测试）。

为避免红测定位脆弱，**推荐 Step 1 直接用 testid 契约**（实现尚未加 testid 时同样失败）：

把成功用例中定位 header 的段落换成：

```ts
    const header = wrapper.get('[data-testid="zashboard-page-header"]')
    const cls = header.classes()
    expect(cls).toEqual(expect.arrayContaining(['hidden', 'lg:flex']))
```

（`get` 在节点不存在时抛错 → 红测同样成立。）

- [ ] **Step 3: Commit 红测**

```bash
git add frontend/src/views/ZashboardView.spec.ts
git commit -m "test(frontend): Zashboard 小屏 header 隐藏契约（红）"
```

---

### Task 2: 实现 — header 小屏隐藏

**Files:**
- Modify: `frontend/src/views/ZashboardView.vue`（template 中 header 容器，约 102–116 行）
- Modify: `frontend/src/views/ZashboardView.spec.ts`（若 Task 1 已用 testid，仅实现侧加属性）

- [ ] **Step 1: 最小实现**

在 `ZashboardView.vue` 的 `<template>` 中，将页面级 header 从：

```vue
    <div class="flex flex-wrap items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-surface shrink-0">
```

改为（保留原有间距/边框/背景；**新增** `hidden lg:flex` 与 testid；注释说明为什么）：

```vue
    <!-- 小屏与 App 顶栏（菜单+标题）叠两层会挤掉 iframe 高度；
         ≥lg 再显示本页标题、对接信息与「重新加载 / 新标签页」。 -->
    <div
      data-testid="zashboard-page-header"
      class="hidden lg:flex flex-wrap items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-surface shrink-0"
    >
```

其余 template / script **不要改**（含 `load`、`openExternally`、loading、错误块、iframe）。

完整 header 块结果应类似：

```vue
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
```

注意：必须是 `hidden lg:flex`，**不要**写成 `hidden lg:block`（会丢掉横向 flex 布局）。`hidden` 是 `display:none`，`lg:flex` 在断点覆盖为 flex，与原先 `flex` 桌面行为一致。

- [ ] **Step 2: 跑测试确认通过**

```bash
cd frontend && npm test -- src/views/ZashboardView.spec.ts
```

**Expected:** PASS（2 tests）。

- [ ] **Step 3: 类型检查（快速）**

```bash
cd frontend && npm run type-check
```

**Expected:** 退出码 0。若全量 type-check 过慢且与本改无关失败，至少确认本文件无新增 TS 错误。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/ZashboardView.vue frontend/src/views/ZashboardView.spec.ts
git commit -m "fix(frontend): 小屏隐藏 Zashboard 页面级 header，让出高度给面板"
```

---

### Task 3: 手工验证清单 + 回归

**Files:** 无代码变更（除非手工发现 class 拼写问题）

- [ ] **Step 1: 跑相关前端测试**

```bash
cd frontend && npm test -- src/views/ZashboardView.spec.ts
```

**Expected:** PASS。

- [ ] **Step 2: 手工窄屏检查（开发者本机）**

1. `cd frontend && npm run dev`（或全栈 `make dev` + 前端 dev）。
2. 浏览器打开 `/zashboard`，DevTools 设备模式宽度 &lt; 1024px：
   - **不应**看到页面内大标题「Zashboard」条与「重新加载 / 在新标签页打开」；
   - **应**仍看到 App 全局顶栏标题；
   - iframe（或错误/loading）上方不再多出一截页面 header。
3. 宽度 ≥ 1024px（或桌面布局）：
   - 页面 header、对接副标题、两按钮可见可点；
   - 「重新加载」会再请求 `/dashboard/entry`。

- [ ] **Step 3: 若有偏差则修并补测后提交**

仅当手工与 spec 不符时改 `ZashboardView.vue`，保持 testid 与 `hidden lg:flex` 契约，再：

```bash
cd frontend && npm test -- src/views/ZashboardView.spec.ts
git add frontend/src/views/ZashboardView.vue frontend/src/views/ZashboardView.spec.ts
git commit -m "fix(frontend): 修正 Zashboard 移动端 header 显示"
```

若手工已符合，本步可跳过 commit。

---

## Spec coverage (self-review)

| Spec 要求 | 对应任务 |
|---|---|
| `< lg` 不展示页面级 header | Task 2：`hidden lg:flex` |
| `≥ lg` 标题+副标题+两按钮不变 | Task 2：保留原 DOM；Task 1 断言文案 |
| loading/错误态小屏仍展示 | Task 1 错误用例；loading 未单独测（与 header class 无关，YAGNI） |
| 不改 sync/load 业务 | Task 2 明确不改 script |
| 不改 App.vue / 方案 C | File map 排除 |
| 验证 type-check / 测试 | Task 2 Step 3、Task 3 |
| testid 非 spec 必需 | 为稳定单测引入，可接受 |

无 TBD / 无「similar to Task N」占位。类型与选择器：`data-testid="zashboard-page-header"`、`hidden`、`lg:flex` 在 Task 1–2 一致。

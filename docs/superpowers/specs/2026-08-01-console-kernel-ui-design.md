# 控制台与内核管理页 UI 优化设计

日期：2026-08-01  
范围：`frontend/src/views/DashboardView.vue`、`frontend/src/views/MihomoView.vue`，及二者共用的小组件  
方案：**B · 运维控制台**（视觉抛光 + 交互增强，不大改其它模块）

## 1. 背景与问题

### 现状

| 页面 | 路由 | 能力 |
|---|---|---|
| 控制台 | `/` `DashboardView` | 5 张状态卡、后台任务、实时日志；已接 `useRealtime` |
| 内核管理 | `/mihomo` `MihomoView` | 状态三卡、启停/重启/重载/更新、日志预览；**未**接实时推送 |

### 已识别问题

1. **内核页无实时**：控制台能收到 `mihomo.status` / `log.message`，内核页只在 `onMounted` 拉一次状态，打开后状态与日志会过期。
2. **控制台缺操作**：运维要启停内核必须再进「内核管理」，多一次跳转。
3. **危险确认简陋**：`window.confirm` 与站点 Dialog 风格不一致，移动端体验差。
4. **类型**：`DashboardView` 日志历史用 `any`；eslint 禁止新增 `any`。
5. **视觉/一致性**：页头无说明（其它页如设置已有）；状态色两页写法不同（注释已说明对比度权衡，但可统一到可复用写法）；原始 `div` 卡片与已接入的 Card/Button/Badge 混用；空状态文案弱。
6. **终端日志区**：两页复制粘贴深色 mono 列表，后续改一处易漏另一处。

### 非目标

- 不新增无后端支撑的指标（连接数、流量曲线等）。
- 不重做 `/logs` 完整日志页、侧栏与全局布局。
- 不引入新 UI 框架；不升 Tailwind 大版本。

## 2. 目标

- **好看**：页头层次、卡片、徽章、主次按钮、空状态与设置页等现有精致页对齐。
- **好用**：控制台可快捷操作内核；两页状态/日志实时一致。
- **干净**：修 `any`、确认 Dialog、共享预览组件，降低重复。

成功标准：

- 两页在浅色/深色下均可读（状态字对比度达到既有注释中的 WCAG 要求）。
- 内核页打开期间状态与日志随 WS 更新。
- 停止/重启必须经 Dialog 确认后才发请求。
- `vue-tsc`、相关 eslint、既有/新增 vitest 通过。

## 3. 信息架构

### 3.1 控制台

```
[页头：标题 + 说明]              [实时通道 Badge]
[状态卡 ×5：状态|版本|节点|订阅更新|配置(+冲突)]
[快捷操作条：启动|停止|重启|重载 | 链接→内核管理]
[后台任务 Card]     [实时日志预览 Card → /logs]
```

### 3.2 内核管理

```
[页头：标题 + 说明]
[运行状态 Card：状态|版本|PID + 操作按钮组]
[运行日志 Card → /logs]
```

### 3.3 按钮主次（两页一致）

| 操作 | 变体 | 确认 |
|---|---|---|
| 启动 | 默认（主） | 否 |
| 停止 | destructive | 是 |
| 重启 | outline | 是 |
| 重载配置 | outline | 否 |
| 更新内核版本 | outline（仅内核页） | 否（已有 toast「正在下载」） |

## 4. 组件拆分

仅服务这两页（或日志预览可被 Logs 外再复用），放在 `frontend/src/components/`：

| 组件 | 职责 |
|---|---|
| `KernelLogPreview.vue` | 终端风格日志列表 + 空状态；props：`lines`、可选 `maxHeight` class |
| `KernelActionBar.vue` | 启停/重启/重载按钮；内部处理确认 Dialog；emit 或直接调 store；props：`showUpdate?: boolean`、`compact?: boolean`（控制台用紧凑条） |

状态卡可先内联在各页用 Card 重写；若重复超过可接受阈值再抽 `KernelStatusSummary`，**不强制**一次抽完。

不修改 `stores/mihomo.ts` 的对外 API 语义；若确认逻辑放在 ActionBar 内，store 的 `stop`/`restart` 仍保持「直接执行」，由 UI 层先确认再调用（与现网一致：确认在 View，执行在 store）。

## 5. 数据流

```
onMounted:
  mihomoStore.fetchStatus()
  （控制台另：订阅/冲突/任务/配置/节点数；历史日志 slice）
  （内核页：可选拉 /mihomo/logs 最近 N 条填 recentLogs）

useRealtime:
  mihomo.status → applyStatus
  log.message   → pushLog
```

- 控制台保留现有 realtime 回调，可抽成与内核页共用的小函数或 composable `useMihomoRealtime()`（可选，避免两处复制 if 分支）。
- 节点数、配置状态逻辑保持现状，不借机大改（`collections/build` 仍仅控制台）。

## 6. 错误处理与反馈

- 所有内核操作结果继续走 `notify`（store `runAction` 已区分 `success: false`）。
- Dialog 确认取消不发请求、不 toast。
- 更新内核：保持「先 info toast → 请求 → success/error」；按钮 `updating` 禁用。
- 网络错误：沿用 axios 拦截 / catch 文案，不新增静默失败。

## 7. 视觉与 token 约定

- 页容器：与设置页类似的 `p-4 sm:p-6 lg:p-8`；内核页可保留 `max-w-5xl mx-auto` 或与控制台统一全宽——**统一为控制台同款全宽 + 内边距**，避免一宽一窄跳变；若内核操作区过散，用 Card 自身限宽即可。
- 表面：`Card` / `bg-surface` / `border`（DEFAULT 已接 token）。
- 文字：`text-fg` / `text-fg-muted` / `text-fg-subtle`。
- 状态色：抽共用 class 或 tiny helper（如运行中用已验证的 `text-emerald-600 dark:text-emerald-400`，停止用 rose 对应），**两页同一套**，注释保留「为何不用 text-success 当小字」的原因。
- 日志预览：保持深色终端观感（可读性优先）；中性色若触发 FE1，优先用已有 dark 表面 token 或项目已接受的终端写法，**不新增** `text-slate-*` 扩散到非日志区。
- 禁止：非终端区硬编码 slate/gray/zinc 等（FE1）。

## 8. 无障碍

- 页头 `h1` + 说明段落。
- Dialog 必须有 `DialogTitle`（可用 sr-only 若视觉上合并，但本设计标题可见）。
- 按钮在 loading/disabled 时保持可感知名称（如「更新中…」）。
- 状态不仅靠颜色：同时有「运行中 / 已停止」文字。

## 9. 测试计划

| 用例 | 期望 |
|---|---|
| KernelLogPreview 空列表 | 显示空状态文案 |
| KernelLogPreview 有数据 | 渲染 time/stream/message |
| KernelActionBar 点停止 | 先出 Dialog，确认前不调用 stop |
| KernelActionBar 确认停止 | 调用 store.stop（mock） |
| 回归 | 不破坏既有 LogsView 测试 |

手动：浅/深主题各扫两页；WS 连接时内核页日志滚动。

## 10. 实现顺序

1. 抽出 `KernelLogPreview`，两页改用。  
2. 抽出 `KernelActionBar`（含确认 Dialog），内核页接入；控制台加快捷条。  
3. 内核页补 realtime + 历史日志。  
4. 两页 Card/页头/Badge 抛光与状态色统一。  
5. 去 `any`、测通、eslint/vue-tsc。

## 11. 风险

- Dialog 替换 confirm 后，自动化若曾依赖 `window.confirm` 需改测（当前 Mihomo 无此测）。  
- 控制台加操作后误触停止影响面更大 → 必须确认框。  
- 共享组件放错目录导致循环依赖：只依赖 store/ui，不反向 import views。

## 12. 验收清单

- [ ] 控制台有说明、状态卡、快捷操作、任务、日志  
- [ ] 内核页有实时状态/日志与确认 Dialog  
- [ ] 两页视觉与设置页同级的 token 用法  
- [ ] 无新增 `any`；约定检查不因本改动新增 FE1 违规  
- [ ] 相关测试与类型检查通过  

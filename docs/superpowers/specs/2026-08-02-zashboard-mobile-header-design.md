# Zashboard 内置页移动端 header 优化

**日期：** 2026-08-02  
**状态：** 已定稿（方案 A）  
**范围：** 前端 `ZashboardView` 布局；不改 Zashboard 静态资源、不改 API、不改全局 `App.vue` 顶栏结构

## 问题

手机打开内置 Zashboard 页时，垂直方向同时存在：

1. `App.vue` 窄屏 sticky 顶栏（菜单 + 路由标题「Zashboard」+ 主题切换等）
2. `ZashboardView.vue` 页面级 header（再次展示「Zashboard」标题、内核对接副标题、「重新加载」「在新标签页打开」两个按钮）
3. 同源 iframe 内的 Zashboard 自身 UI

页面级 header 在小屏上占用过大，挤压 iframe 可用高度，影响面板可用性。

## 目标

- **优先**：移动端尽量把垂直空间让给 iframe 面板
- **桌面端**：保持现有页面级 header 与操作入口不变
- **改动面**：尽量小，避免为次要操作引入 `App.vue` 耦合

## 非目标

- 不改 Zashboard 上游面板内部布局
- 不在本轮把「重新加载 / 新标签页」迁入全局顶栏（方案 C）
- 不在本轮精修 `h-dvh` 与全局顶栏叠加后的滚动/精确可视高度（可后续单独立项）
- 不要求移动端保留与桌面同等密度的操作按钮

## 方案选择

曾评估三种做法：

| 方案 | 描述 | 结论 |
|---|---|---|
| **A. 小屏隐藏页面级 header** | `< lg` 不渲染标题区与两个按钮 | **采用** |
| B. 小屏极薄工具条 | 仅图标按钮，约 36–40px | 仍占高，次选 |
| C. 操作并入全局顶栏 | App 感知 Zashboard 动作 | 耦合高，本轮不做 |

用户明确偏好「尽量让出高度给面板」，故定稿 **A**。

## 行为规格

### 断点

与全站一致：`lg`（Tailwind 默认 1024px）。

| 视口 | 页面级 header（标题 + 副标题 + 两按钮） | iframe / 主内容 |
|---|---|---|
| `< lg` | **不展示**（`hidden lg:flex` 或等价条件） | 占满 `main` 内剩余 flex 高度 |
| `≥ lg` | 与当前实现一致 | 与当前实现一致 |

### 布局容器

保持现有结构意图：

- 根：`main` 使用 `h-dvh flex flex-col min-h-0`
- iframe：`flex-1 min-h-0 w-full border-0`
- loading / 错误态：小屏**仍完整展示**（不因隐藏 header 而吞掉错误说明）

### 功能取舍（移动端）

小屏隐藏后，下列操作不再出现在页面级 chrome：

- 「重新加载」
- 「在新标签页打开」
- 页面内重复的「Zashboard」标题与「已对接内核 host:port」副标题

用户仍可通过：

- 全局顶栏标题识别当前页
- iframe `title="Zashboard 控制面板"` 供辅助技术
- 需要独立窗口时自行打开 `/ui/` 入口或桌面端使用「在新标签页打开」（本轮不强制移动端替代路径）

自动同步 zashboard `localStorage` 后端信息的逻辑**不变**（与布局无关）。

### 无障碍

- 小屏不再渲染页面内 `h1` 时，依赖 App 顶栏可见标题 + iframe `title`，可接受
- 错误态文案与操作指引保持可读

## 实现要点

**文件：** `frontend/src/views/ZashboardView.vue`

1. 将页面级 header 容器改为默认在小屏隐藏、`lg` 及以上显示（例如根节点加 `hidden lg:flex`，保留现有 `flex flex-wrap items-center ...` 等桌面样式）。
2. 不改动 `load` / `openExternally` / `syncZashboardBackend` 业务逻辑；桌面按钮仍绑定原方法。
3. 注释补充**为什么**小屏隐藏：避免与 App 顶栏重复占高，优先 iframe。

可选（非必须）：

- 若有前端组件测试惯例，可为 class 条件加轻量断言；否则以 type-check / lint 与手工窄屏检查为准。

## 验证

1. 窄屏（或 devtools 设备模式 `< 1024px`）：Zashboard 路由下**无**页面级 header 条；iframe（或 loading/错误块）紧贴全局顶栏下方可用区域更大。
2. 宽屏（`≥ lg`）：标题、对接副标题、两按钮与现网一致且可点。
3. `make type-check`（或等价 `vue-tsc`）通过；相关前端 lint 不新增 suppressions。

## 风险与后续

- **风险**：移动端无法一键「重新加载」页面级入口；若现场反馈需要，可升级为方案 B 或 C。
- **后续**：若全局顶栏 + `h-dvh` 导致整页微滚或 iframe 底边被裁，再单独用 `calc(100dvh - 顶栏高度)` 或路由级全屏布局优化，不纳入本 spec。

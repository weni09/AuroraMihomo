# AuroraMihomo Frontend Design

## 1. Overview

Frontend is the Web control center of AuroraMihomo.

Technology:

-   Vue3
-   TypeScript
-   Vite
-   Pinia
-   Vue Router
-   TailwindCSS
-   shadcn-vue

Goals:

-   Simple operation
-   Configuration visualization
-   Real-time status
-   Conflict resolution
-   Mihomo management

------------------------------------------------------------------------

# 2. Frontend Architecture

    web/

    src/

    ├── api/
    │
    ├── stores/
    │
    ├── router/
    │
    ├── layouts/
    │
    ├── components/
    │
    ├── views/
    │
    └── schemas/

------------------------------------------------------------------------

# 3. Main Pages

## Dashboard

Display:

-   Mihomo status
-   Current version
-   Proxy count
-   Last subscription update
-   Configuration status

Components:

    StatusCard

    TaskCard

    RuntimeLog

------------------------------------------------------------------------

# 4. Mihomo Management

Page:

    /mihomo

Functions:

-   Start
-   Stop
-   Restart
-   Reload
-   View logs
-   Version update

------------------------------------------------------------------------

# 5. Subscription Management

Page:

    /subscriptions

Functions:

-   Add subscription
-   Edit URL
-   Enable/disable
-   Manual update
-   Update history

Display:

    Name

    Status

    Last Update

    Node Count

    Action

------------------------------------------------------------------------

# 6. Configuration Center

Page:

    /config

Modules:

    DNS

    TUN

    General

    Rules

    Proxy Groups

    Sniffer

    Ports

------------------------------------------------------------------------

# 7. Form Schema System

Configuration should not be hard coded.

Example:

``` json
{
"type":"switch",
"key":"dns.enable",
"title":"Enable DNS"
}
```

Future support:

-   Dynamic config plugins
-   Custom forms

------------------------------------------------------------------------

# 8. Conflict UI

Page:

    /conflicts

Example:

    Rule Conflict


    Local:

    DOMAIN-SUFFIX,google.com,DIRECT


    Remote:

    DOMAIN-SUFFIX,google.com,Proxy


    [Local]

    [Remote]

    [Merge]

------------------------------------------------------------------------

# 9. Diff Viewer

Show:

Added:

    + HK01

Removed:

    - OLD01

Changed:

    ~ Rule changed

------------------------------------------------------------------------

# 10. State Management

Pinia stores:

    stores/

    mihomo.ts

    subscription.ts

    config.ts

    conflict.ts

    task.ts

------------------------------------------------------------------------

# 11. API Client

Generated from go-zero API definition.

Recommended:

    openapi

    ↓

    typescript client

    ↓

    Vue api layer

------------------------------------------------------------------------

# 12. WebSocket

Realtime events:

    mihomo.status

    task.progress

    log.message

    config.updated

------------------------------------------------------------------------

# 13. UI Design Principle

Follow:

-   shadcn/vue
-   minimal dashboard style
-   dark mode support
-   responsive layout

------------------------------------------------------------------------

# 14. 移动端适配（强制约束）

本章是**强制要求**，不是可选项。面板的主要使用场景包含手机上临时查看
状态与切换节点，任何新增页面或组件在窄屏不可用即视为未完成。

## 14.1 底线要求

每一条都必须满足，提交前自查：

1.  **无横向滚动**。除刻意设计的横滑容器（见 14.3）外，任何视口宽度下
    页面主体都不得出现横向滚动条。测试宽度下限取 **320px**（iPhone SE
    等小屏机型）。
2.  **不裁切、不重叠**。窄屏下文字换行而非截断丢失信息；固定定位元素
    不得遮挡可操作内容。
3.  **触控热区不小于 44×44px**（见 14.4）。
4.  **贴边元素必须让开安全区**（见 14.5）。
5.  **不禁用缩放**。viewport 里不得出现 `maximum-scale` 或
    `user-scalable=no`，这会拦掉视障用户的放大操作。

## 14.2 断点分工

沿用 Tailwind 默认断点，不覆写 `screens`。项目只用两个主力断点，
职责不得混用：

| 断点         | 角色       | 用途                                                   |
| ------------ | ---------- | ------------------------------------------------------ |
| `lg` (1024)  | 结构断点   | 布局骨架切换：导航形态、表格/卡片形态、栏位方向、sticky |
| `sm` (640)   | 密度断点   | 只调内边距、间距、字号，**不改结构**                    |
| `md` (768)   | 表单栅格   | 表单字段的两栏/三栏 grid                                |

已固化的两条惯例，新页面直接沿用：

-   页面外壳内边距：`p-4 sm:p-6 lg:p-8`
-   页面主标题：`text-2xl sm:text-3xl font-bold`

## 14.3 布局形态切换

**移动优先**：先写窄屏样式，再用 `sm:`/`lg:` 向上覆盖。不要反过来
（写桌面样式再用 max-width 覆盖）。

已有三种既定形态，同类场景直接复用而不要另创：

-   **多列强表格 → 卡片翻转**。给 `<table>` 加 `.responsive-table`、
    每个 `<td>` 加 `data-label="列名"`，lg 以下每行自动折成卡片。
    规则在 `src/assets/main.css`。用 `components/ui/table` 下的
    `Table`/`TableCell` 组件时已自动带上，业务代码无需关心。
    **不要为窄屏复制一份卡片模板**——单元格内含下拉、进度条、多个按钮，
    两份模板此后每次改动都要同步，很快就会不一致。
-   **侧边栏 → 抽屉**。lg 以下脱离文档流、`translate` 滑入，配遮罩、
    Escape 关闭、背景滚动锁、焦点移入与归还（实现见 `App.vue`）。
-   **多标签/多分组导航 → 横向滑动**。容器 `overflow-x-auto no-scrollbar`，
    子项 `shrink-0 whitespace-nowrap`。不换行也不压缩字号。

信息量少的列表可以直接写卡片，无需表格翻转。

## 14.4 触控热区

指针设备的鼠标可以精确点击 20px 的目标，手指不行。

-   **可点击元素的实际可点区域不得小于 44×44px**。这是 WCAG 2.5.5 与
    iOS/Android 人机界面指南的共同下限。
-   文字按钮靠内边距达标；**图标按钮必须显式给最小尺寸**
    （`min-h-11 min-w-11` 或等效），不能只靠 `p-1` 撑。
-   视觉上需要小按钮时用 `.btn-sm`：它在 `@media (pointer: coarse)` 下
    把最小尺寸撑到 44px，桌面端仍保持紧凑（实现见 `main.css`）。
    不要为了紧凑手写更小的 padding。
-   图标按钮加 `.tap-target`（同样只在触屏生效）。
-   `title` 属性在触屏上无法触发，纯图标按钮必须另给 `aria-label`。

## 14.5 视口与安全区

-   `index.html` 的 viewport 固定为
    `width=device-width, initial-scale=1.0, viewport-fit=cover`。
-   `viewport-fit=cover` 让页面延伸到刘海与底部手势条之下，因此
    **所有贴边的 `fixed`/`sticky` 元素都必须用 `env(safe-area-inset-*)`
    补偿**，否则内容会被刘海遮住或压在手势条上。项目提供了
    `.safe-*` 辅助类（见 `main.css`），优先用它们。
-   **高度单位统一用 `dvh`，不用 `vh`**。移动浏览器地址栏会伸缩，
    `vh` 取的是最大视口高度，实际会超出屏幕导致底部内容被截。

## 14.6 其他必须处理的移动端差异

-   **背景滚动锁**：打开抽屉或弹窗时给 `body` 加 `overflow-hidden`。
    iOS 上不锁的话手指滑动会带动下层页面，浮层自身反而滚不动。
    所有退出路径（关闭、Escape、组件卸载）都要解锁。
-   **弹窗内部滚动**：弹窗自身限高（`max-h-[85dvh]`）并只让正文区滚动，
    标题栏与操作栏常驻，否则关闭按钮会被滚出视野。
-   **hover 不可依赖**：触屏没有 hover 态。悬停才出现的操作在手机上
    等于不存在，重要操作必须常驻可见。

## 14.7 可复用的辅助类

都定义在 `src/assets/main.css`，不要另写等效实现：

| 类名              | 用途                                             |
| ----------------- | ------------------------------------------------ |
| `.tap-target`     | 图标按钮补足 44px 热区（仅 `pointer: coarse` 生效） |
| `.safe-pt` / `.safe-pb` | 顶部 / 底部让开安全区                       |
| `.safe-py`        | 顶天立地的元素（抽屉）上下同时让位                 |
| `.safe-pl`        | 左侧贴边元素在横屏刘海侧内缩                       |
| `.safe-inset-tr`  | 右上角浮层（toast）避开顶部与右侧                  |
| `.safe-overlay`   | 全屏遮罩内的居中浮层（弹窗）四向让位                |
| `.no-scrollbar`   | 横滑容器隐藏滚动条，省下垂直空间                    |
| `.responsive-table` | 宽表格窄屏折成卡片                               |

## 14.8 自动化约束

第 14.1、14.4、14.5 条中可静态检查的部分已由
`tests/mobile-constraints.spec.ts` 固化，`npm run test` 会执行：

-   全项目不得出现裸 `vh`（必须用 `dvh`）
-   纯图标按钮必须带 `.tap-target`
-   `pointer: coarse` 下 `.btn` / `.btn-sm` / `.tap-target` 有 44px 下限
-   viewport 保留 `viewport-fit=cover` 且未禁用缩放
-   `.safe-*` 辅助类齐全且带 `0px` 回退
-   抽屉、移动顶栏、toast、弹窗遮罩已挂对应的 `.safe-*`

这些规则靠人工 review 很难守住——改动看起来完全正常，只有真机窄屏才暴露。
新增贴边浮层时，请同时在该测试的用例表里补一行。

## 14.9 自查清单

无法静态检查的部分，改动涉及界面时人工确认：

-   [ ] 320px 宽度下无横向滚动、无内容裁切
-   [ ] 实际用手指点一遍密集操作区，没有误触
-   [ ] 浮层已锁背景滚动，且所有退出路径（关闭 / Escape / 卸载）都解锁
-   [ ] 纯图标按钮有 `aria-label`（`title` 在触屏无效）
-   [ ] 不依赖 hover 才能发现的操作
-   [ ] 深色模式下同样检查一遍

------------------------------------------------------------------------

# 15. Future Features

-   Multi-node management
-   User permission
-   Plugin marketplace

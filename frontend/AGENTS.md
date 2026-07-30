## Vue 3 前端开发规范（frontend）

> ## ⚠️ 先读这一节：现状与本文其余部分的差异
>
> 经核对代码，本文档下方多数条款描述的是**尚未落地的目标态**，不是当前实现。动手前请以本节为准；下方条款中与本节冲突的部分，在对应依赖/目录真正引入之前一律不适用。
>
> **实际技术栈**（`package.json` 核实）
>
> | 项 | 实际 |
> |---|---|
> | 框架 | Vue 3.5 + `<script setup lang="ts">` ✅ 与下文一致 |
> | 构建 | Vite 8 + TypeScript ✅ |
> | 状态 | Pinia 4，Setup Store ✅ |
> | 路由 | Vue Router 5 ✅ |
> | 图标 | `lucide-vue-next` ✅ |
> | 样式 | **Tailwind CSS 3.4**（非 4） |
> | 组件库 | shadcn-vue **已接入**：有 `components.json`，装了 `reka-ui` / `clsx` / `tailwind-merge` / `class-variance-authority`；已落地 Card / Table / Badge / Separator |
> | 表单校验 | **无 vee-validate、无 zod** |
> | 国际化 | **无 i18n 依赖**，UI 文案目前硬编码 |
> | 编辑器 | CodeMirror 6（下文未提及，实际重度使用） |
> | YAML | `js-yaml` |
>
> **实际目录结构**（`src/` 下）
>
> ```
> api.ts          Axios 实例与拦截器（不是 lib/request.ts）
> App.vue  main.ts
> assets/         base.css、main.css（主题 token 定义处）
> components/     业务组件平铺：AppLogo CodeEditor ModalDialog PipelineEditor
>                 PreviewPanel ShareDialog ThemeToggle ToastHost
>                 ui/         shadcn-vue 组件（card table badge separator）
>                 —— 无 app/ layout/ 子目录
> lib/utils.ts    shadcn 的 cn()（clsx + tailwind-merge）
> composables/    useRealtime.ts useTheme.ts
> layouts/        （空）
> router/index.ts
> schemas/baseConfig.ts   配置字段元数据（FieldType 等）
> stores/         config configlab conflict files mihomo notify
>                 preview settings shares subscription task
> utils/          labels.ts operators.ts yaml.ts
> views/          页面组件（**不是** pages/）
> ```
>
> 下文提到的 `src/pages`、`src/lib/request.ts`、`src/lib/status-tokens.ts`、`src/styles/globals.css`、`src/components/app`、`src/components/layout`、`PageContainer`、`QueryPanel`、`DataTableShell`、`app-shell.vue`、`frontend-new`、"旧前端" —— **均不存在**。
> （`src/lib/utils.ts` 的 `cn()` 与 `src/components/ui` 随 shadcn-vue 接入后**已存在**。）
>
> **主题 token 的真实体系**
>
> 项目自建了一套语义色，定义在 `src/assets/main.css`（CSS 变量存裸 RGB 分量），经 `tailwind.config.js` 以 `rgb(var(--c-x) / <alpha-value>)` 接入：
>
> - 背景层次：`canvas`（页底）/ `surface`（卡片）/ `elevated`（浮层）
> - 文字：`fg` / `fg-muted` / `fg-subtle`
> - 边框：`line` / `line-strong`，且 `borderColor.DEFAULT` 已接 token（裸 `border` 会跟随主题）
>
> shadcn 语义 token（`primary` / `muted` / `accent` / `ring` / `destructive` / `card` / `popover` / `background` / `foreground` / `input`）随 shadcn-vue 接入后**已定义并生效**：`main.css` 把它们接到上面这套 `--c-*` 上（不给新值，配色仍只有一个来源），`tailwind.config.js` 做了映射。
>
> 状态 token（`success` / `warning` / `info` / `running` / `pending`）**仍未定义**，写了不生效。
>
> 需要新语义时在 `main.css` 补变量并在 config 里接出去，浅色/深色两套值都要给。`check-conventions.py` 的 FE2 规则会读 `tailwind.config.js`，用了没注册的 token 即报错。
>
> 深色模式：`darkMode: 'class'`，由 `composables/useTheme.ts` 切根节点 class，支持浅色/深色/跟随系统三态。
>
> **仍然完全有效、必须遵守的条款**：中文注释规范（见「编码准则 · 注释规范」，现有代码已是这个风格，注释解释权衡与踩过的坑）、双主题同步验证、Lucide 图标优先、Pinia Setup Store + `storeToRefs`、禁用 `any`、独立滚动区域、通用问题改通用组件、错误处理与防抖节流、以及「基于真实回归反馈不要提前宣布完成」。
>
> **若要继续向下文描述的目标态迁移**（升 Tailwind 4、建 app/layout 分层、接 i18n、views→pages），那是独立的重构，需先与用户确认范围。shadcn-vue 已完成 `init`，新增组件用 MCP 服务器 `shadcn-vue` 或 `npx shadcn-vue add <name>`。
>
> `src/components/ui/**` 是从上游拷入、不由本项目维护的代码：其中的单词组件名（Card / Table）在 eslint 配置里已关掉 `vue/multi-word-component-names`，**不要为了过 lint 去改名** —— 那会在下次 `add`/升级时与上游冲突。
>
> ### 提交前必须通过的检查
>
> ```bash
> cd frontend && npx vue-tsc --noEmit -p tsconfig.app.json   # 类型
> cd frontend && npx eslint . --no-fix                        # 规范（含禁止 any）
> python scripts/check-conventions.py --baseline scripts/conventions-baseline.txt
> ```
>
> 或在仓库根一次跑完：`make check`（Windows 若只有 mingw32-make 则用 `mingw32-make check`）。
>
> 两条与前端相关的机检规则：
> - **禁止 `any`** —— 由 eslint 强制。此前 CI 未跑 lint，已积累 87 处，冻结在 `frontend/eslint-suppressions.json`；**新增的 `any` 会导致 CI 失败**。修好存量后用 `npx eslint . --prune-suppressions` 收紧。
> - **禁止硬编码中性色**（slate/gray/zinc/neutral/stone）与**禁止未在 `tailwind.config.js` 注册的语义 token** —— 由 `scripts/check-conventions.py` 强制。19 处存量冻结在 `scripts/conventions-baseline.txt`（主要是 `.tint-neutral` 徽标类与遮罩层）。
>
> 基线只减不增，不要把新代码加进去。
>
> ---

### 🎯 目标技术栈（部分尚未落地，见上节）
- 框架：Vue 3（Composition API、<script setup>）
- 
- 构建：Vite + TypeScript
- 
- 样式：Tailwind CSS 4
- 
- 组件：shadcn-vue
-
- 状态：Pinia（Setup Store）
- 
- 路由：Vue Router 4（动态路由 + 菜单驱动）
- 
- 请求：Axios（统一封装于 `src/lib/request.ts`）


> UI：必须以 shadcn-vue 及项目内 `src/components/ui/*` 的 shadcn 风格封装为唯一基础组件来源；`src/components/app/*` 只能在其上组合业务壳层，不允许另起一套视觉体系。

#### 图标：
- 整个前端静态页面优先使用Lucide图标

> 表单：页面表单控件必须优先使用 shadcn-vue / `src/components/ui` 中的 `Input`、`Textarea`、`Select`、`Checkbox`、`Switch`、`RadioGroup`、`Form` 等组件；禁止新增裸 `input` / `textarea` / `select` / `checkbox` / `button` 作为业务 UI。`vee-validate + zod` 仅在明确需要强校验表单时引入，不强制所有页面一刀切。

### 🏗 目录结构规范（以 frontend-new 为准）
- `src/components/app`：应用级通用业务壳组件，如弹窗、列表壳、分页壳、查询区、详情区、树面板等。
- 
- `src/components/layout`：后台布局相关组件，包含壳层、tags、breadcrumb、侧栏等布局部件。
- 
- `src/components/ui`：项目内基础原子组件。
- 
- `src/pages`：页面级组件。当前项目不使用 `src/views` 作为主目录规范。
- 
- `src/stores`：Pinia 模块化存储。
- 
- `src/api`：按业务模块拆分 API 请求。
- 
- `src/types`：统一管理接口类型、表单类型、列表项类型。
- 
- `src/lib/utils.ts`：存放 `cn()` 与其他通用辅助函数。

### 🌗 多主题开发规则
1. 主题体系
- 默认至少支持浅色主题 / 深色主题；如后续扩展品牌主题，必须建立可枚举的主题键，不允许散落布尔值硬编码
- 主题切换状态应统一收敛到 store 或持久化存储，不要在页面局部各自维护
- 主题初始化必须早于主要布局渲染，避免首屏闪烁或主题回跳

2. 主题实现
- 优先通过根节点 class、`data-theme`、CSS 变量或 Tailwind 主题变量实现，不要在每个页面手写一套颜色分支
- 当一个通用组件已承担主题责任时，优先修改通用组件，而不是在页面里逐个补 class
- 新增颜色、边框、背景、阴影时，必须同时检查浅色 / 深色主题视觉效果
- 禁止只修某一个主题；任何布局、菜单、tags、弹窗、表格、表单改动都必须同步验证双主题

3. 主题审查
- 需要确认 hover、active、disabled、focus、selected、danger 等状态在多主题下都可识别
- 需要确认图标、分隔线、阴影、浮层、Tooltip、右键菜单在多主题下均有足够对比度
- 需要确认主题切换后，路由切换、刷新页面、重新登录后状态可保持一致

### 🌍 国际化开发规则
1. 文案原则
- 面向最终用户的稳定 UI 文案应优先走国际化资源，不要长期硬编码在模板中
- 系统内部调试文案、临时占位文案、一次性迁移过渡文案可短期硬编码，但在交付前应清理
- 后端直接返回的动态标题（如菜单 `meta.title`、字典值、内容标题）允许直接显示，不强制二次翻译

2. 国际化边界
- 布局级固定文案、按钮文案、弹窗标题、确认文案、空态文案、筛选区标题，应优先纳入 i18n
- 页面业务数据字段值（如用户名、文章标题、分类名）不做前端翻译映射
- 若旧前端已经存在成熟 i18n key，优先复用旧 key 语义，而不是新造大量重复 key

3. 开发要求
- 新增国际化文案时，至少同步补齐 `zh-cn`，若项目已有 `en` / `zh-tw` 语言包，也应同步占位或补全
- 不要在组件里拼接难以翻译的整句，优先拆分为可维护的 key
- 错误提示、表单校验提示、删除确认文案必须具备可国际化能力
- 涉及菜单、tags、breadcrumb 时，要区分“后端返回标题”与“前端固定文案”两类来源，不要混淆

### 📝 编码准则
1. Vue + TypeScript
- 始终使用 `<script setup lang="ts">`
- `defineProps` / `defineEmits` 必须显式声明类型
- 禁止使用 `any`；若模板类型推导受限，优先补类型，不要直接逃逸为 `any`
- 基本类型与列表优先 `ref()`；表单对象、查询对象、弹窗编辑对象可用 `reactive()`
- 双向绑定优先使用 `v-model` 对应的显式 `ref` / `reactive` 字段，除非确有必要再用 `defineModel`
- 页面列表、详情、表单请优先补齐 `ListItem`、`FormData`、`Query`、`ParamsResponse` 等类型

2. 注释规范（必须使用中文）
- **业务代码注释必须使用中文**（说明职责、边界、为何如此、失败/空态怎么办）；标识符（变量/函数/类型/文件名）仍用英文
- **用户可见文案**走 i18n（见「国际化开发规则」），不要把中文 UI 文案长期硬编码进组件；**代码注释**与 **i18n 文案**是两回事
- 适用范围：
  - 组件/组合式函数/工具函数的职责说明（文件头或导出符号上方）
  - 复杂逻辑、竞态、权限、主题/双主题特例、与后端约定不一致处
  - store 关键 action、关键 watch/副作用、路由/菜单/tags 特殊行为
  - 非显而易见的类型设计、兼容旧前端/后端字段的映射
- 写法要求：
  - 优先写「意图与约束」，禁止复述代码（如 `// 设置 loading`）
  - 说明失败路径、空态、关闭/刷新/持久化等边界
  - 与周围文件注释密度对齐；简单一行赋值可不注释
- 禁止：新增业务逻辑只有英文注释、或关键流程零注释
- 示例：
  - 正确：`// 手动执行返回 logId 后立即轮询；worker 尚未落库时接口可能暂时无数据，需继续等`
  - 正确：`// 主题初始化须早于布局渲染，避免首屏闪烁`
  - 错误：`// set loading true`（业务注释使用英文）
  - 错误：复杂权限/tags 逻辑无任何说明

3. UI 与样式
- 强制优先使用 shadcn-vue 组件（https://shadcn-vue.com/docs/components）及项目内 `src/components/ui` 的 shadcn 风格封装；新增页面、弹窗、表单、筛选、列表操作、菜单、Tooltip、Popover、Dropdown、Tabs、Dialog、Sheet、Drawer、Toast 等交互 UI，必须先从 shadcn-vue 组件体系选型。
- 禁止在业务页面新增裸原生控件作为最终 UI，包括 `button`、`input`、`textarea`、`select`、`checkbox`、`radio`、`switch` 等；确需底层原生元素时，只允许封装在 `src/components/ui` 或 `src/components/app` 内，并必须保留可审查的语义 hook（如 `data-admin-component-control`）。
- shadcn-vue 组件满足不了功能需求时，先通过组合、slot、variant、class、二次封装等方式适配；仍无法满足时，才允许新增自定义组件，且自定义组件必须遵循 shadcn 的结构、交互状态、圆角、边框、阴影、焦点环和可访问性习惯。
- 强制适配 shadcn 组件风格与主题 token：颜色、背景、边框、文字、阴影、hover、active、selected、disabled、focus、danger 等状态必须优先使用 `background`、`foreground`、`card`、`popover`、`muted`、`accent`、`primary`、`secondary`、`destructive`、`border`、`input`、`ring` 等语义 token；禁止新增硬编码 `slate` / `gray` / `zinc` / `neutral` 等中性色作为长期 UI 主色。
- 强制适配项目主题色：主操作、选中态、焦点态、强调态必须跟随 `primary` / `accent` / `ring` 等主题变量，不允许使用固定蓝色、绿色、紫色等绕过主题系统；危险操作必须使用 `destructive` token。
- 业务状态展示（成功/警告/信息/进行中/等待/失败）优先使用状态语义 token：`success`、`warning`、`info`、`running`、`pending`、`destructive`（定义于 `src/styles/globals.css`，工具见 `src/lib/status-tokens.ts`）；禁止用 `emerald` / `amber` / `sky` 等固定色相绕过主题，也避免把成功/进行中都挂到 `primary` 导致不可区分。
- 强制同时适配浅色 / 深色模式：任何新增或修改的组件、页面、弹窗、表格、表单、浮层、右键菜单、空态、加载态，都必须在 light / dark 下具备足够对比度和一致层级；禁止只针对单一主题补样式。
- 优先复用 `src/components/app` 与 `src/components/ui`，避免页面里重复写壳层结构；业务壳组件也必须建立在 shadcn-vue / shadcn 风格基础组件之上。
- 页面头部、筛选区、列表区、弹窗区应遵循统一视觉层级。
- `PageContainer` 负责页面头部壳层；`QueryPanel` 负责筛选区；`DataTableShell` 负责列表壳层。
- 当需要修改全局布局时，优先改通用组件，不要在每个页面单独打补丁。
- 动态类名合并使用 `cn()`；不要手工拼接一长串互斥 class。
- 暗黑模式与浅色模式必须同时考虑，不允许只修一个主题。
- 滚动模型必须明确：侧栏、列表、弹窗、树面板优先各自独立滚动，避免整页兜底滚动。

4. 图标规范
- 菜单图标、tags 图标、breadcrumb 图标优先读取后端返回的 `meta.icon`
- 如果是 class 类图标（如 `fa fa-home` / `iconfont icon-xxx`），应直接按 class 渲染，不要截取字符串占位
- 需要图标选择器时，应优先做成独立组件，支持搜索、预览、清空，并使用本地静态图标资源，不依赖运行时外链 CDN

5. 列表与树表格
- 树结构列表必须是真正的树形展开/折叠模型，不能只靠缩进伪装
- tags 行为必须采用 visitedTags 持久列表思路，避免只显示 dashboard + 当前页
- 右键菜单、关闭其他、关闭全部、刷新当前等行为要按旧版交互逐项验证

6. 状态管理（Pinia）
- Store 必须使用 Setup Store 语法
- 组件内解构 store 状态时使用 `storeToRefs`
- 与布局、认证、菜单、权限有关的状态优先放入 store 或持久化存储，不要分散在多个页面临时变量中

7. 多主题与国际化联动
- 主题切换组件本身的提示文案、Tooltip、菜单项文案应支持国际化
- 国际化切换后，不应导致主题状态丢失；主题切换后，也不应导致文案回退
- 若新增主题配置面板、语言切换面板，优先组件化，不要继续把逻辑堆进 `app-shell.vue`

### 🤖 AI 交互指令（Roo Code Specific）
- 修改前先确认当前项目“真实实现”而不是套用通用模板
- 若用户明确要求“参考旧前端”，必须先读旧前端对应组件，再落到 frontend-new
- 若涉及布局 / tags / 侧栏 / breadcrumb / 右键菜单，请优先组件化拆分，不要继续把复杂逻辑堆在 `app-shell.vue`
- 所有 API 调用或副作用必须包含错误处理；失败时至少保证界面可回退、不崩溃
- 频繁触发操作（搜索输入、滚动监听、窗口事件）需考虑节流/防抖和清理监听
- 发现“通用问题在多个页面重复出现”时，应优先改通用组件而不是逐页修补
- 当用户基于真实回归指出“无效”“位置不对”“行为不对”时，不要宣布完成，应先回到真实现象继续修正
- **新增/修改业务代码时，关键逻辑的注释使用中文**（见「编码准则 · 注释规范」）；不要只写英文注释或不写注释

### 🔍 审查清单（Review Checklist）
- [ ] 是否使用了 `frontend-new/src/pages`、`frontend-new/src/components/app`、`frontend-new/src/components/layout` 的真实目录规范？
- [ ] 是否避免了 `any`，并补齐了页面所需类型？
- [ ] 图标是否按真实 `meta.icon` / class 正确渲染，而不是字符串占位？
- [ ] 新增或修改的业务 UI 是否强制使用 shadcn-vue / `src/components/ui` 组件，而不是裸原生控件？
- [ ] 自定义组件是否保持 shadcn 组件风格、交互状态、焦点环、圆角、边框、阴影和可访问性习惯？
- [ ] 是否使用 `primary` / `accent` / `destructive` / `border` / `muted` / `ring` 等语义 token 适配项目主题色，而不是硬编码固定色？
- [ ] 是否同时检查了浅色 / 深色主题？
- [ ] 主题状态是否具备统一来源，并避免路由切换 / 刷新后回跳？
- [ ] 面向用户的固定文案是否考虑国际化，而不是长期硬编码？
- [ ] 后端返回标题与前端固定文案是否已正确区分？
- [ ] 滚动区域是否独立，而不是依赖整页滚动？
- [ ] 通用问题是否优先在壳组件中修复？
- [ ] 若参考旧前端，是否已经读取并对齐对应旧组件的实现方式？
- [ ] 新增交互是否经过“位置、状态、关闭、刷新、持久化”完整链路验证？
- [ ] **业务代码注释是否使用中文**，关键逻辑/边界/失败路径是否说明清楚？
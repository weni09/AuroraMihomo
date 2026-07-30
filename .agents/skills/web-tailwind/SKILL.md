---
name: web-tailwind
cluster: web-dev
description: "Tailwind CSS 3.4 (本仓库实际版本): utility classes, tailwind.config.js, arbitrary values, darkMode, plugins"
tags: ["tailwind","css","utility","web"]
dependencies: []
composes: []
similar_to: []
called_by: []
authorization_required: false
scope: general
model_hint: claude-sonnet
embedding_hint: "tailwind css utility classes responsive dark mode shadcn jit"
---

# web-tailwind

> **本仓库使用 Tailwind CSS 3.4**（`frontend/package.json`: `tailwindcss ^3.4.19`），配置走 `frontend/tailwind.config.js`（JS 配置文件 + `@tailwind base/components/utilities` 指令）。
>
> 不要套用 Tailwind 4 的写法：本项目**不适用** `@import "tailwindcss"`、`@theme`、CSS-first 配置、`@plugin` 指令、v4 的 oxide 引擎选项。JIT 在 3.x 中已是默认行为，无需 `mode: 'jit'`。
>
> 项目已有一套自建语义色 token（`canvas` / `surface` / `elevated` / `fg` / `line`），定义在 `frontend/src/assets/main.css` 的 CSS 变量里（裸 RGB 分量），经 `tailwind.config.js` 以 `rgb(var(--c-x) / <alpha-value>)` 接入。新增样式优先复用这些 token，不要另起色板。shadcn/ui 相关内容目前不适用于本仓库（未安装）。

## Purpose
This skill enables the AI to assist with Tailwind CSS for building responsive, utility-first web interfaces, focusing on features like utility classes, configuration, arbitrary values, dark mode, and plugins.

## When to Use
Use this skill when developing web projects that require rapid styling with reusable classes, such as creating responsive layouts, theming with dark mode, or integrating pre-built components from shadcn/ui. Apply it in scenarios like frontend development for React/Vue apps or static sites where CSS bloat needs minimization.

## Key Capabilities
- Utility classes for direct styling (e.g., 'flex', 'p-4', 'hover:bg-blue-700').
- JIT mode for on-demand CSS generation to reduce bundle size.
- Arbitrary values for custom styling (e.g., 'w-[14rem]' for width).
- Dark mode support via 'dark:' prefix or media queries.
- Plugins for extending functionality, like adding custom utilities.
- shadcn/ui integration for component-based UI kits.
- Responsive design with breakpoints (e.g., 'md:flex' for medium screens and up).

## Usage Patterns
To apply Tailwind classes, add them directly to HTML elements in your markup. For configuration, edit the tailwind.config.js file to customize themes, extend utilities, or enable JIT. Always import Tailwind's base, components, and utilities in your CSS entry point. For shadcn/ui, clone the repository and integrate components via Tailwind classes. To enable dark mode, set it in config and use the 'dark:' variant.

Example 1: Create a responsive button.
```html
<button class="bg-blue-500 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded md:px-6">
  Click me
</button>
```

Example 2: Configure dark mode in tailwind.config.js.
```js
module.exports = {
  darkMode: 'class',  // Use 'media' for OS-level or 'class' for manual toggle
  theme: { extend: {} },
};
```

## Common Commands/API
Run `npx tailwindcss init` to generate tailwind.config.js. For purging unused styles, add content paths in config: `content: ['./src/**/*.{html,js}']`. Access config via JavaScript modules. To add plugins, require them in config: `plugins: [require('tailwindcss/plugin')(function({ addUtilities }) { ... })]`.

本仓库不需要手动跑 Tailwind CLI：样式由 Vite + PostCSS 在 `npm run dev` / `npm run build` 时处理（见 `frontend/postcss.config.js`）。类型与构建校验走 `make type-check` 与 `make build-frontend`。

## Integration Notes
Integrate Tailwind by installing via npm: `npm install -D tailwindcss`. With Vite or Webpack, configure PostCSS to process Tailwind.

本仓库已完成集成：`frontend/src/assets/main.css` 引入三条 `@tailwind` 指令并定义主题变量，`main.ts` 导入该 CSS。深色模式采用 `darkMode: 'class'`，由 `src/composables/useTheme.ts` 切换根节点 class（支持浅色/深色/跟随系统三态）。

## Error Handling
For purging errors, verify content paths include all relevant files. Missing utilities might indicate uninstalled plugins—install via npm and restart build. If dark mode fails, ensure the 'dark' class is added to the HTML element and check for CSS conflicts. Handle arbitrary values errors by validating CSS units (e.g., use 'px' or 'rem').

注意：本项目给 `borderColor.DEFAULT` 接了主题 token，所以不带颜色的 `border` 会跟随主题；若发现边框在深色下过亮，先确认没有被硬编码色覆盖。

## Graph Relationships
- Related to cluster: web-dev (shares tools for web development).
- Connected to tags: tailwind (core framework), css (styling language), utility (class-based approach), web (domain focus).
- Links to other skills: web-react (for integrating with React components), web-vue (for Vue.js projects).

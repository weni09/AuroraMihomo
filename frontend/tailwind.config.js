import animate from 'tailwindcss-animate'

/** @type {import('tailwindcss').Config} */
export default {
  // class 策略而非 media：面板需要提供「浅色 / 深色 / 跟随系统」三态，
  // 纯 media 无法让用户覆盖系统偏好。
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      // 语义色统一走 CSS 变量，深浅两套值在 assets/base.css 里定义。
      // 变量存的是裸 RGB 分量（"255 255 255"）而非 rgb(...)，
      // 这样 <alpha-value> 占位符才能生效，bg-surface/50 这类透明度语法照常可用。
      colors: {
        canvas: 'rgb(var(--c-canvas) / <alpha-value>)',
        surface: 'rgb(var(--c-surface) / <alpha-value>)',
        elevated: 'rgb(var(--c-elevated) / <alpha-value>)',
        fg: {
          DEFAULT: 'rgb(var(--c-fg) / <alpha-value>)',
          muted: 'rgb(var(--c-fg-muted) / <alpha-value>)',
          subtle: 'rgb(var(--c-fg-subtle) / <alpha-value>)',
        },
        line: {
          DEFAULT: 'rgb(var(--c-line) / <alpha-value>)',
          strong: 'rgb(var(--c-line-strong) / <alpha-value>)',
        },
        // 日志终端：固定深底，不随主题切换（见 main.css 的说明）
        term: {
          DEFAULT: 'rgb(var(--c-term-bg) / <alpha-value>)',
          fg: 'rgb(var(--c-term-fg) / <alpha-value>)',
          meta: 'rgb(var(--c-term-meta) / <alpha-value>)',
          dim: 'rgb(var(--c-term-dim) / <alpha-value>)',
        },

        // shadcn-vue 组件源码写死了 bg-background / text-muted-foreground
        // 这类类名，需要注册对应的语义色名。变量本身在 assets/main.css 里
        // 桥接到上面那套 --c-* token，两边共享同一配色来源。
        background: 'rgb(var(--background) / <alpha-value>)',
        foreground: 'rgb(var(--foreground) / <alpha-value>)',
        border: 'rgb(var(--border) / <alpha-value>)',
        input: 'rgb(var(--input) / <alpha-value>)',
        ring: 'rgb(var(--ring) / <alpha-value>)',
        primary: {
          DEFAULT: 'rgb(var(--primary) / <alpha-value>)',
          foreground: 'rgb(var(--primary-foreground) / <alpha-value>)',
        },
        secondary: {
          DEFAULT: 'rgb(var(--secondary) / <alpha-value>)',
          foreground: 'rgb(var(--secondary-foreground) / <alpha-value>)',
        },
        destructive: {
          DEFAULT: 'rgb(var(--destructive) / <alpha-value>)',
          foreground: 'rgb(var(--destructive-foreground) / <alpha-value>)',
        },
        success: {
          DEFAULT: 'rgb(var(--success) / <alpha-value>)',
          foreground: 'rgb(var(--success-foreground) / <alpha-value>)',
        },
        warning: {
          DEFAULT: 'rgb(var(--warning) / <alpha-value>)',
          foreground: 'rgb(var(--warning-foreground) / <alpha-value>)',
        },
        // 与 shadcn 内置的 accent（用作 hover/选中底色，见 --accent）区分：
        // accent-solid 是可直接当按钮/徽标实色背景的强调色
        'accent-solid': {
          DEFAULT: 'rgb(var(--accent-solid) / <alpha-value>)',
          foreground: 'rgb(var(--accent-solid-foreground) / <alpha-value>)',
        },
        muted: {
          DEFAULT: 'rgb(var(--muted) / <alpha-value>)',
          foreground: 'rgb(var(--muted-foreground) / <alpha-value>)',
        },
        accent: {
          DEFAULT: 'rgb(var(--accent) / <alpha-value>)',
          foreground: 'rgb(var(--accent-foreground) / <alpha-value>)',
        },
        card: {
          DEFAULT: 'rgb(var(--card) / <alpha-value>)',
          foreground: 'rgb(var(--card-foreground) / <alpha-value>)',
        },
        popover: {
          DEFAULT: 'rgb(var(--popover) / <alpha-value>)',
          foreground: 'rgb(var(--popover-foreground) / <alpha-value>)',
        },
      },
      // 项目里大量地方只写了不带颜色的 `border`，它落到 Tailwind preflight 的
      // 默认色（gray-200）上，深色模式下会亮得刺眼。把默认边框色也接到 token 上，
      // 这些既有写法无需逐个改动就能跟随主题。
      borderColor: {
        DEFAULT: 'rgb(var(--c-line) / <alpha-value>)',
      },
      // shadcn 组件用 rounded-lg/md/sm 表达统一圆角，值由 --radius 派生
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
    },
  },
  // shadcn 的浮层组件（下拉、提示框）依赖 animate-in / fade-in 等类
  plugins: [animate],
}

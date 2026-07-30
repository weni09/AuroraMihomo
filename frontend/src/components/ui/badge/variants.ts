import { cva, type VariantProps } from 'class-variance-authority'

/**
 * 状态徽标的样式变体。
 *
 * 项目原有 tint-ok / tint-err / tint-warn 等类（assets/main.css）表达同一套
 * 语义，这里的变体与它们配色保持一致，以免同一个「生效中」在不同页面
 * 呈现两种绿。深色侧统一用 色/15 半透明底叠在卡片面上，理由见 tint-* 的注释。
 */
export const badgeVariants = cva(
  'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-colors',
  {
    variants: {
      variant: {
        // 浅底配色统一走 success/warning/destructive/accent-solid 四个
        // token（见 assets/main.css），与 Button 的同名变体共享同一份定义，
        // 调色只需改一处 CSS 变量。深色侧仍用 /15 半透明底叠在卡片面上——
        // 直接把实色 token 当浅色底会失去「浅底配色」应有的柔和感，
        // 这点与 Button（本身就要用实色）不同，保留 tint-* 系的做法。
        ok: 'border-success/20 bg-success/10 text-success dark:border-success/30 dark:bg-success/15',
        err: 'border-destructive/20 bg-destructive/10 text-destructive dark:border-destructive/30 dark:bg-destructive/15',
        warn: 'border-warning/30 bg-warning/15 text-amber-700 dark:border-warning/30 dark:bg-warning/15 dark:text-amber-300',
        info: 'border-primary/20 bg-primary/10 text-primary dark:border-primary/30 dark:bg-primary/15',
        accent:
          'border-accent-solid/20 bg-accent-solid/10 text-accent-solid dark:border-accent-solid/30 dark:bg-accent-solid/15',
        neutral: 'border-line bg-elevated text-fg-muted',
        outline: 'border-line-strong bg-transparent text-fg-muted',
      },
    },
    defaultVariants: {
      variant: 'neutral',
    },
  },
)

export type BadgeVariants = VariantProps<typeof badgeVariants>

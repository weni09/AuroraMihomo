import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * 合并 class 名，后写的同类工具类覆盖先写的。
 *
 * shadcn 组件都靠它把外部传入的 class 与组件自带样式合并：单纯拼接会让
 * 二者同时存在（如 px-4 与 px-2），最终取哪个取决于 CSS 里的声明顺序，
 * 而非调用方的意图。twMerge 会按 Tailwind 的类别识别冲突并只保留后者。
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
